package gitlabtracker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TxStarter is the small transaction surface required by ImportSnapshot.
type TxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

// ImportedNote describes a note row newly created while synchronizing GitLab.
// Live is false during the first historical baseline and true for later notes.
type ImportedNote struct {
	CommentID   pgtype.UUID
	IssueID     pgtype.UUID
	WorkspaceID pgtype.UUID
	Content     string
	Action      string // created, updated, deleted
	Live        bool
}

// ImportIssueNotes mirrors the non-system GitLab notes for one linked issue.
// Remote note identity is the idempotency key; the first call establishes a
// historical baseline and subsequent inserts are returned with Live=true.
func ImportIssueNotes(ctx context.Context, conn db.GitlabTrackerConnection, link db.GitlabIssueLink, notes []Note, txStarter TxStarter) ([]ImportedNote, error) {
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin note import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	live := link.NotesInitializedAt.Valid
	seen := make(map[int64]struct{}, len(notes))
	created := make([]ImportedNote, 0)
	for _, note := range notes {
		if note.System {
			continue
		}
		seen[note.ID] = struct{}{}
		createdAt := parseRemoteTime(note.CreatedAt)
		updatedAt := parseRemoteTime(note.UpdatedAt)
		if note.UpdatedAt == "" {
			updatedAt = createdAt
		}

		var commentID pgtype.UUID
		err := tx.QueryRow(ctx, `
SELECT comment_id FROM gitlab_note_link
WHERE tracker_connection_id=$1 AND remote_note_id=$2`, conn.ID, note.ID).Scan(&commentID)
		if err == pgx.ErrNoRows {
			err = tx.QueryRow(ctx, `
INSERT INTO comment (
  issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at
) VALUES ($1,$2,'system',$3,$4,'comment',$5,$6)
RETURNING id`, link.IssueID, conn.WorkspaceID, conn.CreatedBy, note.Body, createdAt, updatedAt).Scan(&commentID)
			if err != nil {
				return nil, fmt.Errorf("insert note %d comment: %w", note.ID, err)
			}
			_, err = tx.Exec(ctx, `
INSERT INTO gitlab_note_link (
  comment_id, issue_id, tracker_connection_id, remote_issue_iid, remote_note_id,
  remote_author_id, remote_author_name, remote_author_url,
  remote_created_at, remote_updated_at, last_remote_body, remote_owned
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,true)`,
				commentID, link.IssueID, conn.ID, link.RemoteIid, note.ID,
				pgInt8(note.Author.ID), pgText(note.Author.Name), pgText(note.Author.URL),
				createdAt, updatedAt, note.Body)
			if err != nil {
				return nil, fmt.Errorf("insert note %d link: %w", note.ID, err)
			}
			created = append(created, ImportedNote{
				CommentID: commentID, IssueID: link.IssueID, WorkspaceID: conn.WorkspaceID,
				Content: note.Body, Action: "created", Live: live,
			})
		} else if err != nil {
			return nil, fmt.Errorf("lookup note %d: %w", note.ID, err)
		} else {
			var previousBody string
			if err := tx.QueryRow(ctx, `SELECT last_remote_body FROM gitlab_note_link WHERE comment_id=$1`, commentID).Scan(&previousBody); err != nil {
				return nil, fmt.Errorf("load note %d snapshot: %w", note.ID, err)
			}
			if _, err := tx.Exec(ctx, `
UPDATE comment SET content=$2, updated_at=$3 WHERE id=$1`, commentID, note.Body, updatedAt); err != nil {
				return nil, fmt.Errorf("update note %d comment: %w", note.ID, err)
			}
			if _, err := tx.Exec(ctx, `
UPDATE gitlab_note_link SET remote_author_id=$2, remote_author_name=$3,
 remote_author_url=$4, remote_updated_at=$5, last_remote_body=$6, last_pulled_at=now()
WHERE comment_id=$1`, commentID, pgInt8(note.Author.ID), pgText(note.Author.Name),
				pgText(note.Author.URL), updatedAt, note.Body); err != nil {
				return nil, fmt.Errorf("update note %d link: %w", note.ID, err)
			}
			if live && previousBody != note.Body {
				created = append(created, ImportedNote{CommentID: commentID, IssueID: link.IssueID, WorkspaceID: conn.WorkspaceID, Content: note.Body, Action: "updated", Live: true})
			}
		}
	}

	rows, err := tx.Query(ctx, `
SELECT comment_id, remote_note_id FROM gitlab_note_link
WHERE issue_id=$1 AND remote_owned=true`, link.IssueID)
	if err != nil {
		return nil, fmt.Errorf("list mirrored notes: %w", err)
	}
	var stale []pgtype.UUID
	for rows.Next() {
		var commentID pgtype.UUID
		var remoteID int64
		if err := rows.Scan(&commentID, &remoteID); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := seen[remoteID]; !ok {
			stale = append(stale, commentID)
		}
	}
	rows.Close()
	for _, commentID := range stale {
		if _, err := tx.Exec(ctx, `DELETE FROM comment WHERE id=$1`, commentID); err != nil {
			return nil, fmt.Errorf("delete stale mirrored note: %w", err)
		}
		if live {
			created = append(created, ImportedNote{CommentID: commentID, IssueID: link.IssueID, WorkspaceID: conn.WorkspaceID, Action: "deleted", Live: true})
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE gitlab_issue_link SET notes_initialized_at=COALESCE(notes_initialized_at,now())
WHERE issue_id=$1`, link.IssueID); err != nil {
		return nil, fmt.Errorf("mark notes initialized: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit note import: %w", err)
	}
	return created, nil
}

func pgInt8(value int64) pgtype.Int8 {
	return pgtype.Int8{Int64: value, Valid: value != 0}
}

// ImportSnapshot fetches the complete label and issue snapshot and writes it
// in page-sized transactions. Re-running it is idempotent: GitLab label ids
// and remote issue IIDs are the stable identities for their tracker.
func ImportSnapshot(ctx context.Context, conn db.GitlabTrackerConnection, client *RestClient, txStarter TxStarter, workspaceID pgtype.UUID) error {
	if client == nil {
		return fmt.Errorf("gitlabtracker: nil client")
	}
	labels, err := client.ListProjectLabels(ctx, conn.RemoteProjectID)
	if err != nil {
		return fmt.Errorf("list labels: %w", err)
	}
	if err := importLabels(ctx, conn, labels, txStarter, workspaceID); err != nil {
		return err
	}
	issues, err := client.ListProjectIssues(ctx, conn.RemoteProjectID, ListIssuesOptions{State: "all"})
	if err != nil {
		return fmt.Errorf("list issues: %w", err)
	}
	if err := importIssues(ctx, conn, issues, txStarter, workspaceID); err != nil {
		return err
	}
	return nil
}

func importLabels(ctx context.Context, conn db.GitlabTrackerConnection, labels []Label, txStarter TxStarter, workspaceID pgtype.UUID) error {
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin label import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, label := range labels {
		mappingKind := ClassifyLabel(label.Name)
		if _, err := tx.Exec(ctx, `
INSERT INTO issue_label (
  workspace_id, name, color, source_type, gitlab_tracker_connection_id,
  gitlab_label_id, is_project_label, is_archived, mapping_kind
) VALUES ($1,$2,$3,'gitlab',$4,$5,$6,false,$7)
ON CONFLICT (gitlab_tracker_connection_id, gitlab_label_id)
WHERE source_type = 'gitlab'
DO UPDATE SET name = EXCLUDED.name, color = EXCLUDED.color,
              is_project_label = EXCLUDED.is_project_label,
              is_archived = false, mapping_kind = EXCLUDED.mapping_kind,
              updated_at = now()
`, workspaceID, label.Name, label.Color, conn.ID, label.ID, label.IsProjectLabel, string(mappingKind)); err != nil {
			return fmt.Errorf("upsert label %d: %w", label.ID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit label import: %w", err)
	}
	return nil
}

// ImportLabels persists a GitLab label snapshot idempotently.
func ImportLabels(ctx context.Context, conn db.GitlabTrackerConnection, labels []Label, txStarter TxStarter, workspaceID pgtype.UUID) error {
	return importLabels(ctx, conn, labels, txStarter, workspaceID)
}

// ImportIssues persists a GitLab issue snapshot idempotently.
func ImportIssues(ctx context.Context, conn db.GitlabTrackerConnection, issues []Issue, txStarter TxStarter, workspaceID pgtype.UUID) error {
	return importIssues(ctx, conn, issues, txStarter, workspaceID)
}

func importIssues(ctx context.Context, conn db.GitlabTrackerConnection, issues []Issue, txStarter TxStarter, workspaceID pgtype.UUID) error {
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin issue import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, remote := range issues {
		status, priority := ProjectIssueFields(remote.State, remote.Labels)
		startDate, err := parseRemoteDate(remote.StartDate)
		if err != nil {
			return fmt.Errorf("issue %d start_date: %w", remote.IID, err)
		}
		dueDate, err := parseRemoteDate(remote.DueDate)
		if err != nil {
			return fmt.Errorf("issue %d due_date: %w", remote.IID, err)
		}
		updatedAt := parseRemoteTime(remote.UpdatedAt)
		applyLabels := false
		snapshot, _ := json.Marshal(remote)
		var issueID pgtype.UUID
		err = tx.QueryRow(ctx, `
SELECT issue_id FROM gitlab_issue_link
WHERE tracker_connection_id = $1 AND remote_iid = $2
`, conn.ID, remote.IID).Scan(&issueID)
		if err == pgx.ErrNoRows {
			var issueNumber int32
			if err := tx.QueryRow(ctx, `
UPDATE workspace SET issue_counter = issue_counter + 1
WHERE id=$1 RETURNING issue_counter`, workspaceID).Scan(&issueNumber); err != nil {
				return fmt.Errorf("allocate issue number: %w", err)
			}
			err = tx.QueryRow(ctx, `
INSERT INTO issue (
 workspace_id, title, description, status, priority, creator_type, creator_id,
 position, number, project_id, source_type, tracker_connection_id, sync_state,
 sync_revision, synced_revision, start_date, due_date
) VALUES (
 $1,$2,$3,$4,$5,'member',$6,
 $7,$8,$9,'gitlab',$10,'synced',1,1,$11,$12
) RETURNING id
`, workspaceID, remote.Title, remote.Description, status, priority, conn.CreatedBy,
				float64(remote.IID), issueNumber, conn.ProjectID, conn.ID, startDate, dueDate).Scan(&issueID)
			if err != nil {
				return fmt.Errorf("insert issue %d: %w", remote.IID, err)
			}
			_, err = tx.Exec(ctx, `
INSERT INTO gitlab_issue_link (
 issue_id, tracker_connection_id, remote_issue_id, remote_iid, remote_web_url,
 remote_state, remote_updated_at, remote_author_name, remote_author_url,
 last_remote_snapshot, last_pulled_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now())
ON CONFLICT (tracker_connection_id, remote_iid) DO NOTHING
`, issueID, conn.ID, remote.ID, remote.IID, remote.WebURL, normalizeState(remote.State), updatedAt,
				pgText(remote.Author.Name), pgText(remote.Author.URL), snapshot)
			applyLabels = err == nil
		} else if err == nil {
			var result pgconn.CommandTag
			result, err = tx.Exec(ctx, `
UPDATE issue SET title=$2, description=$3, status=$4, priority=$5,
 start_date=$6, due_date=$7, sync_state='synced',
 synced_revision=sync_revision, updated_at=now()
WHERE id=$1 AND source_type='gitlab' AND sync_revision=synced_revision
`, issueID, remote.Title, remote.Description, status, priority, startDate, dueDate)
			applyLabels = err == nil && result.RowsAffected() == 1
			if err == nil {
				_, err = tx.Exec(ctx, `
UPDATE gitlab_issue_link SET remote_issue_id=$3, remote_web_url=$4,
 remote_state=$5, remote_updated_at=$6, remote_author_name=$7,
 remote_author_url=$8, last_remote_snapshot=$9, last_pulled_at=now()
WHERE issue_id=$1 AND tracker_connection_id=$2
`, issueID, conn.ID, remote.ID, remote.WebURL, normalizeState(remote.State), updatedAt,
					pgText(remote.Author.Name), pgText(remote.Author.URL), snapshot)
			}
		}
		if err != nil {
			return fmt.Errorf("upsert issue %d: %w", remote.IID, err)
		}
		if applyLabels {
			if err := syncIssueLabels(ctx, tx, issueID, conn, remote.Labels); err != nil {
				return fmt.Errorf("sync labels for issue %d: %w", remote.IID, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issue import: %w", err)
	}
	return nil
}

func syncIssueLabels(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, conn db.GitlabTrackerConnection, names []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM issue_to_label WHERE issue_id = $1 AND label_id IN (
SELECT id FROM issue_label WHERE gitlab_tracker_connection_id = $2 AND source_type='gitlab')`, issueID, conn.ID); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := tx.Exec(ctx, `
INSERT INTO issue_to_label(issue_id, label_id)
SELECT $1, id FROM issue_label
WHERE gitlab_tracker_connection_id=$2 AND source_type='gitlab' AND LOWER(name)=LOWER($3)
ON CONFLICT DO NOTHING`, issueID, conn.ID, name); err != nil {
			return err
		}
	}
	return nil
}

func parseRemoteDate(value string) (pgtype.Date, error) {
	if value == "" {
		return pgtype.Date{}, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return pgtype.Date{}, fmt.Errorf("invalid GitLab date %q: %w", value, err)
	}
	return pgtype.Date{Time: parsed, Valid: true}, nil
}
func normalizeState(state string) string {
	if strings.EqualFold(state, "closed") {
		return "closed"
	}
	return "opened"
}
func parseRemoteTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05 MST"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Now().UTC()
}
func pgText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

// ApplyCanonicalIssue writes a fresh remote snapshot to the local mirror,
// honoring the revision guard from design §8.4: if the user's local
// sync_revision has moved past synced_revision (they made an edit that
// hasn't been pushed yet), the canonical response only updates
// last_remote_snapshot and remote metadata; local text stays.
//
// Otherwise (revision equal), the canonical response overwrites title /
// description / status and bumps synced_revision to sync_revision so
// both sides converge. Label sync is included so post-write label
// changes on GitLab race back into the mirror correctly.
//
// Runs in its own transaction — callers hand it a TxStarter, not a
// pgx.Tx, so it composes with the outbox worker's per-op unit of work.
// ParseRemoteTimestamp parses GitLab timestamps for sync consumers.
func ParseRemoteTimestamp(value string) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: parseRemoteTime(value), Valid: strings.TrimSpace(value) != ""}
}

func ApplyCanonicalIssue(ctx context.Context, conn db.GitlabTrackerConnection, issueID pgtype.UUID, remote Issue, txStarter TxStarter) error {
	return applyCanonicalIssue(ctx, conn, issueID, remote, txStarter, pgtype.Int8{})
}

// ApplyCanonicalIssueAtRevision applies a write response only if it still
// corresponds to the current local desired revision.
func ApplyCanonicalIssueAtRevision(ctx context.Context, conn db.GitlabTrackerConnection, issueID pgtype.UUID, remote Issue, txStarter TxStarter, desiredRevision int64) error {
	return applyCanonicalIssue(ctx, conn, issueID, remote, txStarter, pgtype.Int8{Int64: desiredRevision, Valid: true})
}

func applyCanonicalIssue(ctx context.Context, conn db.GitlabTrackerConnection, issueID pgtype.UUID, remote Issue, txStarter TxStarter, desiredRevision pgtype.Int8) error {
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin canonical apply: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, _ := json.Marshal(remote)
	updatedAt := parseRemoteTime(remote.UpdatedAt)
	status, priority := ProjectIssueFields(remote.State, remote.Labels)
	startDate, err := parseRemoteDate(remote.StartDate)
	if err != nil {
		return fmt.Errorf("canonical start_date: %w", err)
	}
	dueDate, err := parseRemoteDate(remote.DueDate)
	if err != nil {
		return fmt.Errorf("canonical due_date: %w", err)
	}
	// The UPDATE is guarded by sync_revision=synced_revision. If a user
	// snuck an edit in between the worker's REST call and this commit,
	// zero rows update — that's the design's "leave local alone" branch.
	var result pgconn.CommandTag
	if desiredRevision.Valid {
		result, err = tx.Exec(ctx, `
UPDATE issue SET title=$2, description=$3, status=$4, priority=$5,
 start_date=$6, due_date=$7, sync_state='synced', synced_revision=$8, updated_at=now()
WHERE id=$1 AND source_type='gitlab' AND sync_revision=$8`,
			issueID, remote.Title, remote.Description, status, priority, startDate, dueDate, desiredRevision.Int64)
	} else {
		result, err = tx.Exec(ctx, `
UPDATE issue SET title=$2, description=$3, status=$4, priority=$5,
 start_date=$6, due_date=$7, sync_state='synced', synced_revision=sync_revision, updated_at=now()
WHERE id=$1 AND source_type='gitlab' AND sync_revision=synced_revision`,
			issueID, remote.Title, remote.Description, status, priority, startDate, dueDate)
	}
	if err != nil {
		return fmt.Errorf("update issue canonical: %w", err)
	}
	applyLabels := result.RowsAffected() == 1
	// Link row is updated regardless — remote metadata is authoritative
	// even when local text stays put.
	if _, err := tx.Exec(ctx, `
UPDATE gitlab_issue_link SET remote_issue_id=$3, remote_web_url=$4,
 remote_state=$5, remote_updated_at=$6, remote_author_name=$7,
 remote_author_url=$8, last_remote_snapshot=$9, last_pulled_at=now()
WHERE issue_id=$1 AND tracker_connection_id=$2`,
		issueID, conn.ID, remote.ID, remote.WebURL, normalizeState(remote.State), updatedAt,
		pgText(remote.Author.Name), pgText(remote.Author.URL), snapshot); err != nil {
		return fmt.Errorf("update link canonical: %w", err)
	}
	if applyLabels {
		if err := syncIssueLabels(ctx, tx, issueID, conn, remote.Labels); err != nil {
			return fmt.Errorf("sync canonical labels: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// CreateGitlabIssueLinkTx inserts the (issue, remote_iid) mapping in one
// tx and applies the canonical snapshot right after. Used by the worker's
// create_issue handler: the local row already exists in `pending` state
// with sync_revision=1; after GitLab returns, we bind the iid and let
// ApplyCanonicalIssue converge title/description/labels.
func CreateGitlabIssueLinkTx(ctx context.Context, conn db.GitlabTrackerConnection, issueID pgtype.UUID, remote Issue, txStarter TxStarter, desiredRevision int64) error {
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin link create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, _ := json.Marshal(remote)
	updatedAt := parseRemoteTime(remote.UpdatedAt)
	if _, err := tx.Exec(ctx, `
INSERT INTO gitlab_issue_link (
 issue_id, tracker_connection_id, remote_issue_id, remote_iid, remote_web_url,
 remote_state, remote_updated_at, remote_author_name, remote_author_url,
 last_remote_snapshot, last_pulled_at, last_pushed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),now())
ON CONFLICT (tracker_connection_id, remote_iid) DO NOTHING`,
		issueID, conn.ID, remote.ID, remote.IID, remote.WebURL, normalizeState(remote.State), updatedAt,
		pgText(remote.Author.Name), pgText(remote.Author.URL), snapshot); err != nil {
		return fmt.Errorf("insert link: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit link create: %w", err)
	}
	// Second tx: apply canonical (labels + text). Split from the link
	// insert so ApplyCanonicalIssue's guard runs against the just-
	// committed sync_revision value.
	return ApplyCanonicalIssueAtRevision(ctx, conn, issueID, remote, txStarter, desiredRevision)
}
