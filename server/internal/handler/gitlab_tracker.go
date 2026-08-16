package handler

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cryptoRandReader is the concrete OS-RNG read wrapper the crypto path
// uses. Kept as its own named function so cryptoRandRead can point at it
// without an explicit indirection level in the assignment.
func cryptoRandReader(p []byte) (int, error) { return cryptorand.Read(p) }

// defaultRandomUUID mints a v4 UUID for outbox idempotency keys. Split
// out so tests can pin the value without touching every callsite.
func defaultRandomUUID() pgtype.UUID {
	id := uuid.New()
	var out pgtype.UUID
	copy(out.Bytes[:], id[:])
	out.Valid = true
	return out
}

// GitlabTrackerResponse is the public tracker summary. Credential material is
// intentionally absent; callers only learn whether a token is configured.
type GitlabTrackerResponse struct {
	ID                 string  `json:"id"`
	InstanceURL        string  `json:"instance_url"`
	PathWithNamespace  string  `json:"path_with_namespace"`
	WebURL             string  `json:"web_url"`
	State              string  `json:"state"`
	WebhookState       string  `json:"webhook_state"`
	LastPullAt         *string `json:"last_pull_at"`
	PendingOutboxCount int64   `json:"pending_outbox_count"`
	FailedOutboxCount  int64   `json:"failed_outbox_count"`
	TokenConfigured    bool    `json:"token_configured"`
	CanManage          bool    `json:"can_manage"`
}

func gitlabTrackerToResponse(row db.GitlabTrackerConnection, pending, failed int64, canManage bool) GitlabTrackerResponse {
	return GitlabTrackerResponse{
		ID:                 uuidToString(row.ID),
		InstanceURL:        row.InstanceUrl,
		PathWithNamespace:  row.PathWithNamespace,
		WebURL:             row.WebUrl,
		State:              row.State,
		WebhookState:       row.WebhookState,
		LastPullAt:         timestampToPtr(row.LastPullAt),
		PendingOutboxCount: pending,
		FailedOutboxCount:  failed,
		TokenConfigured:    len(row.TokenCiphertext) > 0,
		CanManage:          canManage,
	}
}

// ListProjectGitlabTrackers returns non-disabled tracker summaries for a
// project. It is member-readable; management capability is a response hint.
func (h *Handler) ListProjectGitlabTrackers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	member, _ := middleware.MemberFromContext(ctx)
	canManage := roleAllowed(member.Role, "owner", "admin")
	rows, err := h.Queries.ListGitlabTrackerConnectionsByProject(ctx, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list GitLab trackers")
		return
	}

	trackers := make([]GitlabTrackerResponse, 0, len(rows))
	for _, row := range rows {
		counts, err := h.Queries.CountTrackerOutboxByStatus(ctx, row.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to count tracker outbox")
			return
		}
		var pending, failed int64
		for _, count := range counts {
			switch count.Status {
			case "pending", "running", "retrying":
				pending += count.Cnt
			case "failed":
				failed = count.Cnt
			}
		}
		trackers = append(trackers, gitlabTrackerToResponse(row, pending, failed, canManage))
	}
	writeJSON(w, http.StatusOK, map[string]any{"trackers": trackers})
}

// ---------------------------------------------------------------------------
// Validate endpoint (Phase 2 Task 7)
// ---------------------------------------------------------------------------

// GitlabTrackerAllowedHosts is the operator-configured list of self-hosted
// GitLab hosts (comma-separated, GITLAB_ALLOWED_HOSTS). gitlab.com is
// always accepted and does not need to appear here. Exposed as a variable
// so tests can override without setenv gymnastics.
var GitlabTrackerAllowedHosts = allowedGitlabHostsFromEnv

// gitlabTrackerClientFactory builds a *gitlabtracker.RestClient for a
// given base URL + PAT. Tests replace it with a factory that points at an
// httptest server; production uses the SSRF-safe transport.
var gitlabTrackerClientFactory = defaultGitlabTrackerClientFactory

func allowedGitlabHostsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("GITLAB_ALLOWED_HOSTS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func defaultGitlabTrackerClientFactory(baseURL, token string) (*gitlabtracker.RestClient, error) {
	transport, err := gitlabtracker.NewClient(gitlabtracker.Config{
		AllowedHosts:   GitlabTrackerAllowedHosts(),
		RequestTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return gitlabtracker.NewRestClient(transport, baseURL, token), nil
}

// ValidateGitlabTrackerRequest carries the two pieces of input a caller
// needs to check before we ever persist anything. The token is held in
// memory for the duration of the request only.
type ValidateGitlabTrackerRequest struct {
	RepositoryURL string `json:"repository_url"`
	AccessToken   string `json:"access_token"`
}

// ValidateGitlabTrackerResponse is the safe summary returned on success.
// It intentionally omits the token echo and the raw project ref.
type ValidateGitlabTrackerResponse struct {
	Host              string                       `json:"host"`
	InstanceURL       string                       `json:"instance_url"`
	PathWithNamespace string                       `json:"path_with_namespace"`
	RemoteProjectID   int64                        `json:"remote_project_id"`
	WebURL            string                       `json:"web_url"`
	DefaultBranch     string                       `json:"default_branch"`
	Permissions       ValidateGitlabTrackerPermits `json:"permissions"`
}

type ValidateGitlabTrackerPermits struct {
	CanWriteIssues      bool `json:"can_write_issues"`
	CanConfigureWebhook bool `json:"can_configure_webhook"`
}

// ValidateGitlabTracker checks whether the URL/token pair maps to a
// reachable GitLab project. The endpoint is workspace-member-gated but
// stops short of a role check: validate is a read-only preflight that
// the Create Project modal calls before the operator commits to storing
// the token. Never persists anything.
func (h *Handler) ValidateGitlabTracker(w http.ResponseWriter, r *http.Request) {
	var req ValidateGitlabTrackerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.RepositoryURL = strings.TrimSpace(req.RepositoryURL)
	req.AccessToken = strings.TrimSpace(req.AccessToken)
	if req.AccessToken == "" {
		writeStructuredGitlabError(w, http.StatusBadRequest, "access_token_required", "access_token is required")
		return
	}

	parsed, err := gitlabtracker.ParseProjectURL(req.RepositoryURL, GitlabTrackerAllowedHosts())
	if err != nil {
		var ue *gitlabtracker.URLError
		if errors.As(err, &ue) {
			writeStructuredGitlabError(w, http.StatusBadRequest, ue.Code, ue.Message)
			return
		}
		writeStructuredGitlabError(w, http.StatusBadRequest, "invalid_url", err.Error())
		return
	}

	client, err := gitlabTrackerClientFactory(parsed.InstanceURL, req.AccessToken)
	if err != nil {
		writeStructuredGitlabError(w, http.StatusInternalServerError, "internal", "failed to build GitLab client")
		return
	}
	project, err := client.GetProject(r.Context(), parsed.PathWithNamespace)
	if err != nil {
		status, code, msg := mapGitlabValidationError(err)
		writeStructuredGitlabError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusOK, ValidateGitlabTrackerResponse{
		Host:              parsed.Host,
		InstanceURL:       parsed.InstanceURL,
		PathWithNamespace: project.PathWithNamespace,
		RemoteProjectID:   project.ID,
		WebURL:            project.WebURL,
		DefaultBranch:     project.DefaultBranch,
		Permissions: ValidateGitlabTrackerPermits{
			CanWriteIssues:      project.CanWriteIssues,
			CanConfigureWebhook: project.CanConfigureWebhook,
		},
	})
}

// mapGitlabValidationError translates a REST error into the (status,
// code, message) triple the frontend maps to a user-visible reason. The
// message is a safe summary — never the URL or token.
func mapGitlabValidationError(err error) (int, string, string) {
	switch {
	case errors.Is(err, gitlabtracker.ErrInvalidToken):
		return http.StatusUnauthorized, "invalid_token", "the access token was rejected by GitLab"
	case errors.Is(err, gitlabtracker.ErrPermissionDenied):
		return http.StatusForbidden, "permission_denied", "the token lacks permission to read this project"
	case errors.Is(err, gitlabtracker.ErrNotFound):
		return http.StatusNotFound, "not_found", "the project was not found; check the URL and token scope"
	}
	var rate *gitlabtracker.RateLimitedError
	if errors.As(err, &rate) {
		return http.StatusTooManyRequests, "rate_limited", "GitLab rate limited the validation request"
	}
	if errors.Is(err, gitlabtracker.ErrRemote) {
		return http.StatusBadGateway, "network", "unexpected response from GitLab"
	}
	// Every other error (dial guard rejection, TLS failure) surfaces as a
	// generic network error rather than leaking the transport detail.
	return http.StatusBadGateway, "network", "could not reach GitLab"
}

func writeStructuredGitlabError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}

// Ensure imports used only by ctx-shaped helpers stay referenced.

// ---------------------------------------------------------------------------
// Create tracker connection (Phase 2 Task 8)
// ---------------------------------------------------------------------------

// GitlabTrackerCipherProvider returns the cipher used to encrypt tokens
// and webhook secrets. Tests replace it with an in-memory cipher; prod
// loads keys from GITLAB_TRACKER_KEYS at startup.
var GitlabTrackerCipherProvider = defaultGitlabCipherProvider

// gitlabCipherOnce memoizes the env-derived cipher so we parse
// GITLAB_TRACKER_KEYS at most once per process even if many endpoints
// call for it. Zero value is safe.
var gitlabCipherOnce struct {
	c   *gitlabtracker.Cipher
	err error
	set bool
}

// defaultGitlabCipherProvider parses GITLAB_TRACKER_KEYS on first call.
// Format: `v1=<base64>[,v2=<base64>...]`. A missing env fails-closed:
// endpoints that require the cipher (create/rotate) refuse to accept
// credentials, matching design §11.1 ("无 key 拒绝保存凭据").
func defaultGitlabCipherProvider() (*gitlabtracker.Cipher, error) {
	if gitlabCipherOnce.set {
		return gitlabCipherOnce.c, gitlabCipherOnce.err
	}
	gitlabCipherOnce.set = true
	raw := strings.TrimSpace(os.Getenv("GITLAB_TRACKER_KEYS"))
	if raw == "" {
		gitlabCipherOnce.err = errors.New("GITLAB_TRACKER_KEYS is not configured; refusing to accept GitLab credentials")
		return nil, gitlabCipherOnce.err
	}
	entries := strings.Split(raw, ",")
	keys := make(map[int16]string, len(entries))
	for _, e := range entries {
		parts := strings.SplitN(strings.TrimSpace(e), "=", 2)
		if len(parts) != 2 {
			gitlabCipherOnce.err = errors.New("GITLAB_TRACKER_KEYS: expected `vN=<base64>` entries")
			return nil, gitlabCipherOnce.err
		}
		verStr := strings.TrimPrefix(strings.TrimSpace(parts[0]), "v")
		var ver int16
		if _, err := fmt.Sscanf(verStr, "%d", &ver); err != nil {
			gitlabCipherOnce.err = fmt.Errorf("GITLAB_TRACKER_KEYS: invalid version %q", parts[0])
			return nil, gitlabCipherOnce.err
		}
		keys[ver] = strings.TrimSpace(parts[1])
	}
	c, err := gitlabtracker.NewCipher(keys)
	if err != nil {
		gitlabCipherOnce.err = err
		return nil, err
	}
	gitlabCipherOnce.c = c
	return c, nil
}

// CreateProjectGitlabTrackerRequest is the body for `POST
// /api/projects/{id}/gitlab-trackers`. Same shape as validate but with a
// commit intent: after the same read-only preflight, the token and a
// freshly minted webhook secret get encrypted and written to the row.
type CreateProjectGitlabTrackerRequest struct {
	RepositoryURL string `json:"repository_url"`
	AccessToken   string `json:"access_token"`
}

// createGitlabTracker validates, encrypts, persists and enqueues a tracker's
// first import. Both the standalone endpoint and project-create bundle use
// this path so credentials and initial sync cannot diverge.
func (h *Handler) createGitlabTracker(ctx context.Context, project db.Project, workspaceID, userID pgtype.UUID, req CreateProjectGitlabTrackerRequest) (db.GitlabTrackerConnection, error) {
	req.RepositoryURL = strings.TrimSpace(req.RepositoryURL)
	req.AccessToken = strings.TrimSpace(req.AccessToken)
	if req.AccessToken == "" {
		return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusBadRequest, code: "access_token_required", message: "access_token is required"}
	}
	cipher, err := GitlabTrackerCipherProvider()
	if err != nil {
		return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusServiceUnavailable, code: "encryption_unavailable", message: err.Error()}
	}
	parsed, err := gitlabtracker.ParseProjectURL(req.RepositoryURL, GitlabTrackerAllowedHosts())
	if err != nil {
		var ue *gitlabtracker.URLError
		if errors.As(err, &ue) {
			return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusBadRequest, code: ue.Code, message: ue.Message}
		}
		return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusBadRequest, code: "invalid_url", message: err.Error()}
	}
	client, err := gitlabTrackerClientFactory(parsed.InstanceURL, req.AccessToken)
	if err != nil {
		return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusInternalServerError, code: "internal", message: "failed to build GitLab client"}
	}
	remote, err := client.GetProject(ctx, parsed.PathWithNamespace)
	if err != nil {
		status, code, msg := mapGitlabValidationError(err)
		return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: status, code: code, message: msg}
	}

	tokenCT, err := cipher.Encrypt([]byte(req.AccessToken))
	if err != nil {
		return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusInternalServerError, code: "internal", message: "failed to encrypt token"}
	}
	webhookSecret := make([]byte, 32)
	if _, err := cryptoRandRead(webhookSecret); err != nil {
		return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusInternalServerError, code: "internal", message: "failed to generate webhook secret"}
	}
	secretCT, err := cipher.Encrypt(webhookSecret)
	if err != nil {
		return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusInternalServerError, code: "internal", message: "failed to encrypt webhook secret"}
	}
	created, err := h.Queries.CreateGitlabTrackerConnection(ctx, db.CreateGitlabTrackerConnectionParams{
		ProjectID: project.ID, WorkspaceID: workspaceID, InstanceUrl: parsed.InstanceURL,
		RemoteProjectID: remote.ID, PathWithNamespace: remote.PathWithNamespace,
		WebUrl: remote.WebURL, CloneUrl: parsed.CloneURL,
		DefaultBranch:   pgtype.Text{String: remote.DefaultBranch, Valid: remote.DefaultBranch != ""},
		TokenCiphertext: tokenCT, TokenKeyVersion: cipher.LatestVersion(),
		WebhookSecretCiphertext: secretCT, WebhookState: "unavailable", CreatedBy: userID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return db.GitlabTrackerConnection{}, &gitlabTrackerCreateError{status: http.StatusConflict, code: "tracker_already_exists", message: "a tracker for this GitLab project already exists on this RimeDeck project"}
		}
		return db.GitlabTrackerConnection{}, fmt.Errorf("create tracker connection: %w", err)
	}
	for _, op := range []string{"pull_labels", "reconcile"} {
		if _, err := h.Queries.CreateTrackerOutbox(ctx, db.CreateTrackerOutboxParams{WorkspaceID: workspaceID, TrackerConnectionID: created.ID, Operation: op, Payload: []byte("{}"), IdempotencyKey: newRandomUUID()}); err != nil {
			return db.GitlabTrackerConnection{}, fmt.Errorf("enqueue first import: %w", err)
		}
	}
	return created, nil
}

type gitlabTrackerCreateError struct {
	status        int
	code, message string
}

func (e *gitlabTrackerCreateError) Error() string { return e.message }

func writeGitlabTrackerCreateError(w http.ResponseWriter, err error) {
	var structured *gitlabTrackerCreateError
	if errors.As(err, &structured) {
		writeStructuredGitlabError(w, structured.status, structured.code, structured.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "failed to create GitLab tracker")
}

// CreateProjectGitlabTracker persists a validated GitLab tracker under a
// project. Owner/admin only. The request is idempotent by (project_id,
// instance_url, remote_project_id): the second call surfaces 409 rather
// than silently creating a duplicate row.
func (h *Handler) CreateProjectGitlabTracker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project_id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	member, _ := middleware.MemberFromContext(ctx)
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}
	var req CreateProjectGitlabTrackerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	created, err := h.createGitlabTracker(ctx, project, wsUUID, userUUID, req)
	if err != nil {
		writeGitlabTrackerCreateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, gitlabTrackerToResponse(created, 2, 0, true))
}

// cryptoRandRead wraps crypto/rand.Read so tests can stub it if needed.
// Kept private; production callers use it as a plain read of the OS RNG.
var cryptoRandRead = cryptoRandReader

// newRandomUUID returns a fresh idempotency key. Uses gen_random_uuid on
// the DB side would be preferable, but the outbox insert wants the value
// in the params so we mint it here.
var newRandomUUID = defaultRandomUUID

// enqueueTrackerOutbox inserts an outbox row and immediately compresses
// older pending/retrying rows in the same
// (tracker_connection_id, issue_id, operation) group with strictly lower
// desired_revision. Callers pass their transactional Queries handle
// (`h.Queries.WithTx(tx)`) so the insert + compression happens atomically
// with the caller's own local mutation.
func enqueueTrackerOutbox(ctx context.Context, q *db.Queries, params db.CreateTrackerOutboxParams) (db.TrackerSyncOutbox, error) {
	row, err := q.CreateTrackerOutbox(ctx, params)
	if err != nil {
		return db.TrackerSyncOutbox{}, err
	}
	// Compression only runs when the caller declared a desired revision;
	// pull_labels/reconcile/pull_issue rows leave it null and are never
	// compressed — they represent distinct pull attempts, not overlapping
	// desired states.
	if params.DesiredRevision.Valid {
		if err := q.CompressPendingTrackerOutbox(ctx, db.CompressPendingTrackerOutboxParams{
			TrackerConnectionID: params.TrackerConnectionID,
			IssueID:             params.IssueID,
			Operation:           params.Operation,
			DesiredRevision:     params.DesiredRevision,
		}); err != nil {
			return db.TrackerSyncOutbox{}, fmt.Errorf("compress outbox: %w", err)
		}
	}
	return row, nil
}

// enqueueGitlabWriteOp is the post-write hook Issue CRUD handlers call
// once their local mutation has committed. It bumps sync_revision (which
// also flips sync_state to 'pending' or 'pending_delete') and enqueues
// the matching outbox operation with compression applied. The bump +
// insert + compress happen in one transaction so a concurrent update
// cannot slip a revision in between.
//
// `payload` is JSON with the fields the worker will push (labels for
// set_labels, {title,description,...} for update_issue, empty for
// delete_issue). The worker re-reads the issue row at claim time so any
// desired-state field it needs beyond payload can be looked up fresh.
func (h *Handler) enqueueGitlabWriteOp(ctx context.Context, issueID, workspaceID, trackerID pgtype.UUID, operation string, payload []byte, deleting bool) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin write outbox tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	nextState := "pending"
	if deleting {
		nextState = "pending_delete"
	}
	rev, err := qtx.BumpIssueSyncRevision(ctx, db.BumpIssueSyncRevisionParams{ID: issueID, Column2: nextState})
	if err != nil {
		return fmt.Errorf("bump sync_revision: %w", err)
	}
	if _, err := enqueueTrackerOutbox(ctx, qtx, db.CreateTrackerOutboxParams{
		WorkspaceID:         workspaceID,
		TrackerConnectionID: trackerID,
		IssueID:             issueID,
		Operation:           operation,
		Payload:             payload,
		IdempotencyKey:      newRandomUUID(),
		DesiredRevision:     pgtype.Int8{Int64: rev, Valid: true},
	}); err != nil {
		return fmt.Errorf("enqueue write op: %w", err)
	}
	return tx.Commit(ctx)
}

// cancelUnlinkedGitlabIssue is the delete counterpart when the local
// mirror was never successfully created remotely (no gitlab_issue_link
// row). Cancels any lingering create_issue outbox rows for that issue
// so the worker doesn't push a phantom create after the local delete.
func (h *Handler) cancelUnlinkedGitlabIssue(ctx context.Context, issueID pgtype.UUID) error {
	return h.Queries.CancelTrackerOutboxByIssue(ctx, issueID)
}

// ---------------------------------------------------------------------------
// Lifecycle endpoints (Phase 2 Task 9)
// ---------------------------------------------------------------------------

// resolveTrackerForOwner is the common preamble: parses UUIDs, checks
// role, loads the tracker row scoped to the URL project and workspace.
// Returns zero values + false when the response has already been written.
func (h *Handler) resolveTrackerForOwner(w http.ResponseWriter, r *http.Request) (db.GitlabTrackerConnection, pgtype.UUID, bool) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.GitlabTrackerConnection{}, pgtype.UUID{}, false
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project_id")
	if !ok {
		return db.GitlabTrackerConnection{}, pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.GitlabTrackerConnection{}, pgtype.UUID{}, false
	}
	member, _ := middleware.MemberFromContext(ctx)
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.GitlabTrackerConnection{}, pgtype.UUID{}, false
	}
	trackerID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "trackerId"), "tracker_id")
	if !ok {
		return db.GitlabTrackerConnection{}, pgtype.UUID{}, false
	}
	tracker, err := h.Queries.GetGitlabTrackerConnectionInWorkspace(ctx, db.GetGitlabTrackerConnectionInWorkspaceParams{ID: trackerID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "tracker not found")
		return db.GitlabTrackerConnection{}, pgtype.UUID{}, false
	}
	if uuidToString(tracker.ProjectID) != uuidToString(projectID) {
		writeError(w, http.StatusNotFound, "tracker not found")
		return db.GitlabTrackerConnection{}, pgtype.UUID{}, false
	}
	return tracker, wsUUID, true
}

// RotateGitlabTrackerToken re-validates a new PAT against the tracker's
// GitLab project and, on success, replaces the stored ciphertext. State
// stays 'active' (or the flow refuses to rotate a disabled connection).
// Body: { access_token }.
func (h *Handler) RotateGitlabTrackerToken(w http.ResponseWriter, r *http.Request) {
	tracker, _, ok := h.resolveTrackerForOwner(w, r)
	if !ok {
		return
	}
	if tracker.State == "disabled" {
		writeStructuredGitlabError(w, http.StatusConflict, "tracker_disabled", "re-enable the tracker before rotating the token")
		return
	}

	cipher, err := GitlabTrackerCipherProvider()
	if err != nil {
		writeStructuredGitlabError(w, http.StatusServiceUnavailable, "encryption_unavailable", err.Error())
		return
	}
	var req struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.AccessToken = strings.TrimSpace(req.AccessToken)
	if req.AccessToken == "" {
		writeStructuredGitlabError(w, http.StatusBadRequest, "access_token_required", "access_token is required")
		return
	}

	client, err := gitlabTrackerClientFactory(tracker.InstanceUrl, req.AccessToken)
	if err != nil {
		writeStructuredGitlabError(w, http.StatusInternalServerError, "internal", "failed to build GitLab client")
		return
	}
	if _, err := client.GetProject(r.Context(), tracker.PathWithNamespace); err != nil {
		status, code, msg := mapGitlabValidationError(err)
		writeStructuredGitlabError(w, status, code, msg)
		return
	}

	newCT, err := cipher.Encrypt([]byte(req.AccessToken))
	if err != nil {
		writeStructuredGitlabError(w, http.StatusInternalServerError, "internal", "failed to encrypt token")
		return
	}
	if _, err := h.Queries.UpdateGitlabTrackerToken(r.Context(), db.UpdateGitlabTrackerTokenParams{
		ID:              tracker.ID,
		TokenCiphertext: newCT,
		TokenKeyVersion: cipher.LatestVersion(),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SyncGitlabTracker enqueues a full re-import of labels + issues. The
// worker (Phase 2 Task 10) picks up the `pull_labels` + `reconcile`
// rows on its next tick.
func (h *Handler) SyncGitlabTracker(w http.ResponseWriter, r *http.Request) {
	tracker, wsUUID, ok := h.resolveTrackerForOwner(w, r)
	if !ok {
		return
	}
	if tracker.State == "disabled" {
		writeStructuredGitlabError(w, http.StatusConflict, "tracker_disabled", "re-enable the tracker before syncing")
		return
	}
	for _, op := range []string{"pull_labels", "reconcile"} {
		if _, err := h.Queries.CreateTrackerOutbox(r.Context(), db.CreateTrackerOutboxParams{
			WorkspaceID:         wsUUID,
			TrackerConnectionID: tracker.ID,
			IssueID:             pgtype.UUID{},
			Operation:           op,
			Payload:             []byte("{}"),
			IdempotencyKey:      newRandomUUID(),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to enqueue sync")
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// RetryGitlabTrackerFailedOutbox flips this connection's `failed` outbox
// rows back to `pending`. Response counts the rows reset so the UI can
// surface "Retried N tasks".
func (h *Handler) RetryGitlabTrackerFailedOutbox(w http.ResponseWriter, r *http.Request) {
	tracker, _, ok := h.resolveTrackerForOwner(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ResetFailedTrackerOutbox(r.Context(), tracker.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry outbox")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset_count": rows})
}

// DisableGitlabTracker soft-disables the connection: state → 'disabled',
// mirrored issues are detached, mirror data stays put. The tracker row
// survives until DeleteGitlabTrackerMirrors is invoked with the
// double-confirmation header.
func (h *Handler) DisableGitlabTracker(w http.ResponseWriter, r *http.Request) {
	tracker, _, ok := h.resolveTrackerForOwner(w, r)
	if !ok {
		return
	}
	if _, err := h.Queries.DisableGitlabTrackerConnection(r.Context(), tracker.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable tracker")
		return
	}
	if err := h.Queries.DetachIssuesFromTracker(r.Context(), tracker.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to detach mirrored issues")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteGitlabTrackerMirrors physically deletes mirrored issues and the
// connection row itself. Requires the header
// `X-Confirm-Delete-Mirrors: true` and refuses when any outbox row is
// still non-terminal (pending/running/retrying) so the worker cannot
// race the deletion.
func (h *Handler) DeleteGitlabTrackerMirrors(w http.ResponseWriter, r *http.Request) {
	tracker, _, ok := h.resolveTrackerForOwner(w, r)
	if !ok {
		return
	}
	if !strings.EqualFold(r.Header.Get("X-Confirm-Delete-Mirrors"), "true") {
		writeStructuredGitlabError(w, http.StatusPreconditionRequired, "confirmation_required", "set X-Confirm-Delete-Mirrors: true to confirm destructive delete")
		return
	}
	nonTerminal, err := h.Queries.CountNonTerminalTrackerOutbox(r.Context(), tracker.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect outbox")
		return
	}
	if nonTerminal > 0 {
		writeStructuredGitlabError(w, http.StatusConflict, "outbox_not_drained", "outbox has non-terminal rows; disable the tracker and let the worker drain first")
		return
	}
	if err := h.Queries.DeleteMirroredIssuesForTracker(r.Context(), tracker.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete mirrored issues")
		return
	}
	// Outbox rows (all in terminal state at this point) cascade-delete
	// with the tracker row via the FK.
	if err := h.Queries.DeleteGitlabTrackerConnection(r.Context(), tracker.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete tracker row")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
