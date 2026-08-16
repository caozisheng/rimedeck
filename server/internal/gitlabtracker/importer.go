package gitlabtracker

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TxStarter is the small transaction surface required by ImportSnapshot.
type TxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
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
		if _, err := tx.Exec(ctx, `
INSERT INTO issue_label (
  workspace_id, name, color, source_type, gitlab_tracker_connection_id,
  gitlab_label_id, is_project_label, is_archived
) VALUES ($1,$2,$3,'gitlab',$4,$5,$6,false)
ON CONFLICT (gitlab_tracker_connection_id, gitlab_label_id)
WHERE source_type = 'gitlab'
DO UPDATE SET name = EXCLUDED.name, color = EXCLUDED.color,
              is_project_label = EXCLUDED.is_project_label,
              is_archived = false, updated_at = now()
`, workspaceID, label.Name, label.Color, conn.ID, label.ID, label.IsProjectLabel); err != nil {
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
		updatedAt := parseRemoteTime(remote.UpdatedAt)
		snapshot, _ := json.Marshal(remote)
		var issueID pgtype.UUID
		err := tx.QueryRow(ctx, `
SELECT issue_id FROM gitlab_issue_link
WHERE tracker_connection_id = $1 AND remote_iid = $2
`, conn.ID, remote.IID).Scan(&issueID)
		if err == pgx.ErrNoRows {
			err = tx.QueryRow(ctx, `
INSERT INTO issue (
 workspace_id, title, description, status, priority, creator_type, creator_id,
 position, number, project_id, source_type, tracker_connection_id, sync_state,
 sync_revision, synced_revision
) VALUES (
 $1,$2,$3,$4,'none','member',$5,
 $6,(SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id = $1),$7,
 'gitlab',$8,'synced',1,1
) RETURNING id
`, workspaceID, remote.Title, remote.Description, issueStatus(remote.State), conn.CreatedBy,
				float64(remote.IID), conn.ProjectID, conn.ID).Scan(&issueID)
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
		} else if err == nil {
			_, err = tx.Exec(ctx, `
UPDATE issue SET title=$2, description=$3, status=$4, sync_state='synced',
 synced_revision=sync_revision, updated_at=now()
WHERE id=$1 AND source_type='gitlab' AND sync_revision=synced_revision
`, issueID, remote.Title, remote.Description, issueStatus(remote.State))
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
		if err := syncIssueLabels(ctx, tx, issueID, conn, remote.Labels); err != nil {
			return fmt.Errorf("sync labels for issue %d: %w", remote.IID, err)
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

func issueStatus(state string) string {
	if strings.EqualFold(state, "closed") {
		return "done"
	}
	return "todo"
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

// Keep strconv linked for old GitLab installations returning numeric dates
// in JSON snapshots; the importer intentionally stores the raw snapshot.
var _ = strconv.Itoa
