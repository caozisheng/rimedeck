// Package gitlabsync owns the outbox drain loop that turns pending
// tracker_sync_outbox rows into real GitLab REST calls and local mirror
// writes. Phase 2 only implements the read-side operations
// (`pull_labels`, `reconcile`); the write-side (`create_issue`,
// `update_issue`, `delete_issue`, `set_labels`) is deferred to Phase 3
// per the design doc §12.
//
// The worker is deliberately transport-free: it exposes a single
// `Tick(ctx)` method that pulls up to N ready rows, processes each in
// turn, and writes success or backoff to the row. Callers own the
// scheduling loop, so tests can drive one deterministic tick without a
// goroutine.
package gitlabsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MaxAttempts caps the retry ladder before a row terminates as `failed`.
// 6 attempts × the backoff schedule below covers ~= 63 s of retries,
// which is enough to ride out a transient upstream blip without pinning
// the row in-flight forever.
const MaxAttempts = 6

// BatchSize is the default per-tick claim ceiling. Small enough to keep
// a single misbehaving connection from starving the queue.
const BatchSize = 25

// ClientFactory builds a REST client for a given tracker + decrypted
// token. Made a struct field so tests can inject an httptest-backed
// client without env or network.
type ClientFactory func(instanceURL, token string) (*gitlabtracker.RestClient, error)

// Queries is the sqlc surface the worker uses. Kept as an interface so
// tests can plug a fake without a live PostgreSQL.
type Queries interface {
	ClaimReadyTrackerOutbox(ctx context.Context, limit int32) ([]db.TrackerSyncOutbox, error)
	GetGitlabTrackerConnection(ctx context.Context, id pgtype.UUID) (db.GitlabTrackerConnection, error)
	MarkTrackerOutboxSucceeded(ctx context.Context, id pgtype.UUID) error
	MarkTrackerOutboxRetry(ctx context.Context, arg db.MarkTrackerOutboxRetryParams) error
	MarkTrackerOutboxFailed(ctx context.Context, arg db.MarkTrackerOutboxFailedParams) error
	TouchTrackerLastPull(ctx context.Context, id pgtype.UUID) error
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	GetGitlabIssueLinkByIssueID(ctx context.Context, issueID pgtype.UUID) (db.GitlabIssueLink, error)
	CancelTrackerOutboxByIssue(ctx context.Context, issueID pgtype.UUID) error
	DeleteIssue(ctx context.Context, arg db.DeleteIssueParams) error
	ListGitlabIssueLinkIIDs(ctx context.Context, trackerConnectionID pgtype.UUID) ([]db.ListGitlabIssueLinkIIDsRow, error)
	TouchLastFullReconcile(ctx context.Context, id pgtype.UUID) error
}

// TxStarter is the transaction surface the sync worker forwards to
// gitlabtracker.ApplyCanonicalIssue and friends. Kept small so tests
// can plug a fake without spinning a real pgx pool.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Worker binds every dependency once; call Tick per interval.
type Worker struct {
	Queries             Queries
	TxStarter           TxStarter
	Cipher              *gitlabtracker.Cipher
	ClientFactory       ClientFactory
	BatchSize           int32
	LabelImporter       func(context.Context, db.GitlabTrackerConnection, []gitlabtracker.Label) error
	IssueImporter       func(context.Context, db.GitlabTrackerConnection, []gitlabtracker.Issue) error
	CreateIssueImporter func(context.Context, db.GitlabTrackerConnection, db.Issue, gitlabtracker.Issue) error
	CanonicalApplier    func(context.Context, db.GitlabTrackerConnection, pgtype.UUID, gitlabtracker.Issue) error
}

// TickResult summarises one drain pass. Zero counts mean the queue was
// empty (or fully processed) — useful for adaptive backoff at the
// caller layer.
type TickResult struct {
	Claimed int
	Success int
	Retried int
	Failed  int
	Skipped int // decrypt/cfg errors that block progress but don't touch the row
}

// Tick drains up to BatchSize ready rows once. Never panics on a single
// row's failure — the row is marked and the loop continues.
func (w *Worker) Tick(ctx context.Context) (TickResult, error) {
	limit := w.BatchSize
	if limit == 0 {
		limit = BatchSize
	}
	rows, err := w.Queries.ClaimReadyTrackerOutbox(ctx, limit)
	if err != nil {
		return TickResult{}, fmt.Errorf("claim outbox: %w", err)
	}
	res := TickResult{Claimed: len(rows)}
	for _, row := range rows {
		outcome := w.processRow(ctx, row)
		switch outcome {
		case outcomeSuccess:
			res.Success++
		case outcomeRetry:
			res.Retried++
		case outcomeFailed:
			res.Failed++
		case outcomeSkipped:
			res.Skipped++
		}
	}
	return res, nil
}

type outcome int

const (
	outcomeSuccess outcome = iota
	outcomeRetry
	outcomeFailed
	outcomeSkipped
)

// processRow dispatches on operation and encodes the terminal-vs-retry
// decision. Returns the outcome so Tick can tally without a second DB
// round-trip.
func (w *Worker) processRow(ctx context.Context, row db.TrackerSyncOutbox) outcome {
	tracker, err := w.Queries.GetGitlabTrackerConnection(ctx, row.TrackerConnectionID)
	if err != nil {
		// The tracker vanished (delete-mirrors race) — the FK cascade
		// should have taken this row too, but if we see it here it's
		// safe to terminate.
		_ = w.Queries.MarkTrackerOutboxFailed(ctx, db.MarkTrackerOutboxFailedParams{
			ID:               row.ID,
			LastErrorCode:    pgText("tracker_missing"),
			LastErrorMessage: pgText(err.Error()),
		})
		return outcomeFailed
	}
	if tracker.State == "disabled" {
		// Disabled connection: drop the row terminally rather than
		// spinning. UI already told the user the tracker is off.
		_ = w.Queries.MarkTrackerOutboxFailed(ctx, db.MarkTrackerOutboxFailedParams{
			ID:               row.ID,
			LastErrorCode:    pgText("tracker_disabled"),
			LastErrorMessage: pgText("tracker is disabled"),
		})
		return outcomeFailed
	}

	if w.Cipher == nil {
		return w.backoff(ctx, row, "encryption_unavailable", "cipher not configured")
	}
	tokenBytes, err := w.Cipher.Decrypt(tracker.TokenCiphertext)
	if err != nil {
		return w.backoff(ctx, row, "decrypt_failed", err.Error())
	}
	client, err := w.ClientFactory(tracker.InstanceUrl, string(tokenBytes))
	if err != nil {
		return w.backoff(ctx, row, "client_build_failed", err.Error())
	}

	switch row.Operation {
	case "pull_labels":
		if err := w.handlePullLabels(ctx, client, tracker); err != nil {
			return w.classifyError(ctx, row, err)
		}
	case "reconcile", "pull_issue":
		if err := w.handleReconcile(ctx, client, tracker); err != nil {
			return w.classifyError(ctx, row, err)
		}
	case "full_reconcile":
		if err := w.handleFullReconcile(ctx, client, tracker); err != nil {
			return w.classifyError(ctx, row, err)
		}
	case "create_issue":
		if err := w.handleCreateIssue(ctx, client, tracker, row); err != nil {
			return w.classifyError(ctx, row, err)
		}
	case "update_issue":
		if err := w.handleUpdateIssue(ctx, client, tracker, row); err != nil {
			return w.classifyError(ctx, row, err)
		}
	case "set_labels":
		if err := w.handleSetLabels(ctx, client, tracker, row); err != nil {
			return w.classifyError(ctx, row, err)
		}
	case "delete_issue":
		if err := w.handleDeleteIssue(ctx, client, tracker, row); err != nil {
			return w.classifyError(ctx, row, err)
		}
	default:
		// Unknown operation — mark succeeded so an accidentally-enqueued
		// row cannot pin the queue indefinitely. Real ops all live in a
		// case above, so this is only a defensive terminal branch.
		_ = w.Queries.MarkTrackerOutboxSucceeded(ctx, row.ID)
		return outcomeSuccess
	}

	if err := w.Queries.MarkTrackerOutboxSucceeded(ctx, row.ID); err != nil {
		return w.backoff(ctx, row, "mark_success_failed", err.Error())
	}
	// Best-effort last_pull_at bump; a stale timestamp is harmless.
	_ = w.Queries.TouchTrackerLastPull(ctx, tracker.ID)
	return outcomeSuccess
}

// handlePullLabels fetches and persists the complete remote label snapshot.
// handlePullLabels fetches and persists the complete remote label snapshot.
func (w *Worker) handlePullLabels(ctx context.Context, client *gitlabtracker.RestClient, tracker db.GitlabTrackerConnection) error {
	labels, err := client.ListProjectLabels(ctx, tracker.RemoteProjectID)
	if err != nil {
		return err
	}
	if w.LabelImporter != nil {
		return w.LabelImporter(ctx, tracker, labels)
	}
	if w.TxStarter == nil {
		return errors.New("gitlabtracker: importer transaction starter is not configured")
	}
	return gitlabtracker.ImportLabels(ctx, tracker, labels, w.TxStarter, tracker.WorkspaceID)
}

// handleReconcile fetches and persists the complete remote issue snapshot.
func (w *Worker) handleReconcile(ctx context.Context, client *gitlabtracker.RestClient, tracker db.GitlabTrackerConnection) error {
	issues, err := client.ListProjectIssues(ctx, tracker.RemoteProjectID, gitlabtracker.ListIssuesOptions{State: "all"})
	if err != nil {
		return err
	}
	if w.IssueImporter != nil {
		return w.IssueImporter(ctx, tracker, issues)
	}
	if w.TxStarter == nil {
		return errors.New("gitlabtracker: importer transaction starter is not configured")
	}
	return gitlabtracker.ImportIssues(ctx, tracker, issues, w.TxStarter, tracker.WorkspaceID)
}

// handleFullReconcile is the 6-hour safety net. Pages state=all to find
// (a) remote issue rows we don't have (imported like a normal reconcile)
// and (b) local link rows for which the remote issue has vanished
// (a manual GitLab delete or a missed webhook). Orphans get local
// cleanup: cancel outbox, delete mirror, broadcast happens via the
// existing publish path in DeleteIssue's cascade.
func (w *Worker) handleFullReconcile(ctx context.Context, client *gitlabtracker.RestClient, tracker db.GitlabTrackerConnection) error {
	issues, err := client.ListProjectIssues(ctx, tracker.RemoteProjectID, gitlabtracker.ListIssuesOptions{State: "all"})
	if err != nil {
		return err
	}
	// Import current set — same path reconcile uses so idempotency is
	// preserved. Skips ImportIssues if a test injects IssueImporter.
	if w.IssueImporter != nil {
		if err := w.IssueImporter(ctx, tracker, issues); err != nil {
			return err
		}
	} else if w.TxStarter != nil {
		if err := gitlabtracker.ImportIssues(ctx, tracker, issues, w.TxStarter, tracker.WorkspaceID); err != nil {
			return err
		}
	}

	remoteIIDs := make(map[int32]struct{}, len(issues))
	for _, issue := range issues {
		remoteIIDs[issue.IID] = struct{}{}
	}
	links, err := w.Queries.ListGitlabIssueLinkIIDs(ctx, tracker.ID)
	if err != nil {
		return fmt.Errorf("list local links: %w", err)
	}
	for _, link := range links {
		if _, present := remoteIIDs[link.RemoteIid]; present {
			continue
		}
		// Orphan: remote issue was deleted (or was never visible via
		// the token's scope). Cancel any queued follow-ups first so
		// the FK cascade doesn't fire mid-delete, then remove the
		// local mirror. DeleteIssue cascades gitlab_issue_link.
		_ = w.Queries.CancelTrackerOutboxByIssue(ctx, link.IssueID)
		if err := w.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: link.IssueID, WorkspaceID: tracker.WorkspaceID}); err != nil {
			return fmt.Errorf("delete orphan issue: %w", err)
		}
	}
	_ = w.Queries.TouchLastFullReconcile(ctx, tracker.ID)
	return nil
}

// classifyError distinguishes retryable transports (network / 5xx / 429)
// from terminal ones (auth revoked, permission removed). Only rate-limit
// and network faults get a retry; the rest terminate immediately so the
// UI can prompt the operator to rotate the token or re-invite the bot.
func (w *Worker) classifyError(ctx context.Context, row db.TrackerSyncOutbox, err error) outcome {
	switch {
	case isRateLimited(err), isNetworkErr(err):
		return w.backoff(ctx, row, "transient", err.Error())
	case errors.Is(err, gitlabtracker.ErrInvalidToken),
		errors.Is(err, gitlabtracker.ErrPermissionDenied):
		_ = w.Queries.MarkTrackerOutboxFailed(ctx, db.MarkTrackerOutboxFailedParams{
			ID:               row.ID,
			LastErrorCode:    pgText("auth_revoked"),
			LastErrorMessage: pgText(err.Error()),
		})
		return outcomeFailed
	case errors.Is(err, gitlabtracker.ErrNotFound):
		_ = w.Queries.MarkTrackerOutboxFailed(ctx, db.MarkTrackerOutboxFailedParams{
			ID:               row.ID,
			LastErrorCode:    pgText("not_found"),
			LastErrorMessage: pgText(err.Error()),
		})
		return outcomeFailed
	default:
		return w.backoff(ctx, row, "remote_error", err.Error())
	}
}

// handleCreateIssue posts the local pending issue to GitLab and links
// the returned iid. Payload is the JSON body the handler enqueued; we
// re-read the local row so title/description/labels reflect any edits
// the user applied while the row waited in the queue.
func (w *Worker) handleCreateIssue(ctx context.Context, client *gitlabtracker.RestClient, tracker db.GitlabTrackerConnection, row db.TrackerSyncOutbox) error {
	issue, err := w.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		return fmt.Errorf("load issue for create: %w", err)
	}
	req := gitlabtracker.CreateIssueRequest{
		Title:       issue.Title,
		Description: issue.Description.String,
	}
	remote, err := client.CreateIssue(ctx, tracker.RemoteProjectID, req)
	if err != nil {
		return err
	}
	if w.CreateIssueImporter != nil {
		return w.CreateIssueImporter(ctx, tracker, issue, remote)
	}
	if w.TxStarter == nil {
		return errors.New("gitlabtracker: importer transaction starter is not configured")
	}
	return gitlabtracker.CreateGitlabIssueLinkTx(ctx, tracker, issue.ID, remote, w.TxStarter)
}

// handleUpdateIssue applies the payload fields against the remote issue
// and canonical-pulls the response. The payload was minted at enqueue
// time from the fields the user actually touched.
func (w *Worker) handleUpdateIssue(ctx context.Context, client *gitlabtracker.RestClient, tracker db.GitlabTrackerConnection, row db.TrackerSyncOutbox) error {
	link, err := w.Queries.GetGitlabIssueLinkByIssueID(ctx, row.IssueID)
	if err != nil {
		return fmt.Errorf("load link for update: %w", err)
	}
	var payload struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		DueDate     *string `json:"due_date"`
		StateEvent  *string `json:"state_event"`
	}
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return fmt.Errorf("decode update payload: %w", err)
	}
	req := gitlabtracker.UpdateIssueRequest{
		Title:       payload.Title,
		Description: payload.Description,
		DueDate:     payload.DueDate,
		StateEvent:  payload.StateEvent,
	}
	remote, err := client.UpdateIssue(ctx, tracker.RemoteProjectID, link.RemoteIid, req)
	if err != nil {
		return err
	}
	if w.CanonicalApplier != nil {
		return w.CanonicalApplier(ctx, tracker, row.IssueID, remote)
	}
	if w.TxStarter == nil {
		return errors.New("gitlabtracker: importer transaction starter is not configured")
	}
	return gitlabtracker.ApplyCanonicalIssue(ctx, tracker, row.IssueID, remote, w.TxStarter)
}

// handleSetLabels replaces the full remote label set with the payload's
// list, then canonical-pulls (labels + metadata).
func (w *Worker) handleSetLabels(ctx context.Context, client *gitlabtracker.RestClient, tracker db.GitlabTrackerConnection, row db.TrackerSyncOutbox) error {
	link, err := w.Queries.GetGitlabIssueLinkByIssueID(ctx, row.IssueID)
	if err != nil {
		return fmt.Errorf("load link for set_labels: %w", err)
	}
	var payload struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return fmt.Errorf("decode set_labels payload: %w", err)
	}
	remote, err := client.SetLabels(ctx, tracker.RemoteProjectID, link.RemoteIid, payload.Labels)
	if err != nil {
		return err
	}
	if w.CanonicalApplier != nil {
		return w.CanonicalApplier(ctx, tracker, row.IssueID, remote)
	}
	if w.TxStarter == nil {
		return errors.New("gitlabtracker: importer transaction starter is not configured")
	}
	return gitlabtracker.ApplyCanonicalIssue(ctx, tracker, row.IssueID, remote, w.TxStarter)
}

// handleDeleteIssue removes the remote issue then the local mirror. 404
// is treated as success by the REST client so a manual GitLab delete
// racing our worker still lets local cleanup run once. Local delete
// cascades gitlab_issue_link and remaining outbox rows via FK.
func (w *Worker) handleDeleteIssue(ctx context.Context, client *gitlabtracker.RestClient, tracker db.GitlabTrackerConnection, row db.TrackerSyncOutbox) error {
	link, err := w.Queries.GetGitlabIssueLinkByIssueID(ctx, row.IssueID)
	if err != nil {
		return fmt.Errorf("load link for delete: %w", err)
	}
	if err := client.DeleteIssue(ctx, tracker.RemoteProjectID, link.RemoteIid); err != nil {
		return err
	}
	// Cancel any queued follow-up ops on this issue before removing the
	// local row so their FK cascade doesn't fire mid-delete.
	_ = w.Queries.CancelTrackerOutboxByIssue(ctx, row.IssueID)
	issue, err := w.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		return fmt.Errorf("load issue for delete: %w", err)
	}
	return w.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
}

// backoff writes the row back as `retrying` with an exponential
// available_at. Once attempts exceeds MaxAttempts the row terminates.
func (w *Worker) backoff(ctx context.Context, row db.TrackerSyncOutbox, code, msg string) outcome {
	if row.Attempts >= MaxAttempts {
		_ = w.Queries.MarkTrackerOutboxFailed(ctx, db.MarkTrackerOutboxFailedParams{
			ID:               row.ID,
			LastErrorCode:    pgText(code),
			LastErrorMessage: pgText(msg),
		})
		return outcomeFailed
	}
	next := time.Now().Add(computeBackoff(int(row.Attempts)))
	_ = w.Queries.MarkTrackerOutboxRetry(ctx, db.MarkTrackerOutboxRetryParams{
		ID:               row.ID,
		AvailableAt:      pgTS(next),
		LastErrorCode:    pgText(code),
		LastErrorMessage: pgText(msg),
	})
	return outcomeRetry
}

// computeBackoff is `2^attempts` seconds capped at 5 minutes. Keeps
// hot-looping under control without waiting hours between retries.
func computeBackoff(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	secs := math.Pow(2, float64(attempts))
	if secs > 300 {
		secs = 300
	}
	return time.Duration(secs) * time.Second
}

// isRateLimited detects the transport's typed 429.
func isRateLimited(err error) bool {
	var re *gitlabtracker.RateLimitedError
	return errors.As(err, &re)
}

// isNetworkErr treats anything without a mapped sentinel as transient.
// Deliberately conservative: a DNS blip is not a reason to terminate.
func isNetworkErr(err error) bool {
	if isRateLimited(err) {
		return false
	}
	return !errors.Is(err, gitlabtracker.ErrInvalidToken) &&
		!errors.Is(err, gitlabtracker.ErrPermissionDenied) &&
		!errors.Is(err, gitlabtracker.ErrNotFound) &&
		!errors.Is(err, gitlabtracker.ErrRemote)
}

// --- tiny pgtype helpers ---------------------------------------------------

func pgText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
func pgTS(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// Ensure json import is retained for future payload use without a lint
// flap; the write-side ops in Phase 3 will unmarshal payload here.
var _ = json.Marshal
