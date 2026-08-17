package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestApplyCanonicalIssue_RevisionMatchWritesCanonical: the guard's
// happy path. If the local sync_revision equals synced_revision (no
// unpushed edits), the canonical response overwrites text and bumps
// synced_revision.
func TestApplyCanonicalIssue_RevisionMatchWritesCanonical(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "canonical-match")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, true)

	// Seed matches: sync_revision = synced_revision = 1.
	tracker, err := testHandler.Queries.GetGitlabTrackerConnection(context.Background(), parseUUID(trackerID))
	if err != nil {
		t.Fatalf("load tracker: %v", err)
	}
	remote := gitlabtracker.Issue{
		ID: 999, IID: 42, State: "closed",
		Title: "canonical", Description: "from remote",
		WebURL: "https://gitlab.example.com/x/42", UpdatedAt: "2026-08-16T00:05:00Z",
		StartDate: "2026-08-17", DueDate: "2026-08-20",
		Labels: []string{"workflow::in-progress", "priority::high"},
	}
	if err := gitlabtracker.ApplyCanonicalIssueAtRevision(context.Background(), tracker, parseUUID(issueID), remote, testPool, 1); err != nil {
		t.Fatalf("ApplyCanonicalIssue: %v", err)
	}

	var title, syncState, status, priority string
	var startDate, dueDate pgtype.Date
	var rev, synced int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT title, sync_state, sync_revision, synced_revision, status, priority, start_date, due_date FROM issue WHERE id=$1`,
		parseUUID(issueID)).Scan(&title, &syncState, &rev, &synced, &status, &priority, &startDate, &dueDate); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if title != "canonical" || syncState != "synced" || rev != 1 || synced != 1 {
		t.Fatalf("title=%q sync=%q rev=%d synced=%d, want canonical/synced/1/1",
			title, syncState, rev, synced)
	}
	if status != "done" || priority != "high" || !startDate.Valid || !dueDate.Valid ||
		startDate.Time.Format("2006-01-02") != "2026-08-17" || dueDate.Time.Format("2006-01-02") != "2026-08-20" {
		t.Fatalf("mapped fields status=%q priority=%q start=%v due=%v", status, priority, startDate, dueDate)
	}
}

// TestApplyCanonicalIssue_RevisionMismatchPreservesLocal: the guard's
// protective branch. If the user snuck an edit in (sync_revision >
// synced_revision), the canonical write MUST NOT clobber local title;
// the link row still updates so metadata stays fresh.
func TestApplyCanonicalIssue_RevisionMismatchPreservesLocal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "canonical-mismatch")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, true)

	// Simulate a queued local edit: title changed and sync_revision bumped
	// past synced_revision.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET title='user-edit', sync_revision=2 WHERE id=$1`,
		parseUUID(issueID)); err != nil {
		t.Fatalf("simulate local edit: %v", err)
	}

	tracker, err := testHandler.Queries.GetGitlabTrackerConnection(context.Background(), parseUUID(trackerID))
	if err != nil {
		t.Fatalf("load tracker: %v", err)
	}
	remote := gitlabtracker.Issue{
		ID: 999, IID: 42, State: "opened",
		Title: "server-edit", Description: "server desc",
		WebURL: "https://gitlab.example.com/x/42", UpdatedAt: "2026-08-16T00:10:00Z",
	}
	if err := gitlabtracker.ApplyCanonicalIssueAtRevision(context.Background(), tracker, parseUUID(issueID), remote, testPool, 1); err != nil {
		t.Fatalf("ApplyCanonicalIssue: %v", err)
	}

	var title string
	var rev, synced int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT title, sync_revision, synced_revision FROM issue WHERE id=$1`,
		parseUUID(issueID)).Scan(&title, &rev, &synced); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if title != "user-edit" {
		t.Fatalf("title = %q, want user-edit (guard should have preserved local)", title)
	}
	if rev != 2 || synced != 1 {
		t.Fatalf("rev=%d synced=%d, want 2/1 (guard branch)", rev, synced)
	}

	// Link row should still reflect the canonical pull metadata (opened→opened).
	var remoteState string
	if err := testPool.QueryRow(context.Background(),
		`SELECT remote_state FROM gitlab_issue_link WHERE issue_id=$1`,
		parseUUID(issueID)).Scan(&remoteState); err != nil {
		t.Fatalf("reload link: %v", err)
	}
	if remoteState != "opened" {
		t.Fatalf("link remote_state = %q, want opened", remoteState)
	}

	var localLabelID string
	if err := testPool.QueryRow(context.Background(), `
INSERT INTO issue_label(workspace_id,name,color,source_type,gitlab_tracker_connection_id,gitlab_label_id,mapping_kind)
VALUES ($1,'local-pending-label','#123456','gitlab',$2,5999,'none') RETURNING id`, parseUUID(testWorkspaceID), parseUUID(trackerID)).Scan(&localLabelID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `INSERT INTO issue_to_label(issue_id,label_id) VALUES ($1,$2)`, issueID, localLabelID); err != nil {
		t.Fatal(err)
	}
	remote.Labels = []string{"server-label"}
	if err := gitlabtracker.ApplyCanonicalIssueAtRevision(context.Background(), tracker, parseUUID(issueID), remote, testPool, 1); err != nil {
		t.Fatal(err)
	}
	var relationExists bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM issue_to_label WHERE issue_id=$1 AND label_id=$2)`, issueID, localLabelID).Scan(&relationExists); err != nil {
		t.Fatal(err)
	}
	if !relationExists {
		t.Fatal("canonical response overwrote labels while a local revision was pending")
	}
}

// Suppress unused var warning if the file is imported without db.
var _ = db.Issue{}
var _ pgtype.UUID

func TestImportIssues_MapsFieldsAndKeepsCompleteLabels(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "import-mapped-fields")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)
	tracker, err := testHandler.Queries.GetGitlabTrackerConnection(context.Background(), parseUUID(trackerID))
	if err != nil {
		t.Fatal(err)
	}
	labels := []gitlabtracker.Label{
		{ID: 5101, Name: "workflow::in-progress", Color: "#111111", IsProjectLabel: true},
		{ID: 5102, Name: "priority::high", Color: "#222222", IsProjectLabel: true},
		{ID: 5103, Name: "bug", Color: "#333333", IsProjectLabel: true},
	}
	if err := gitlabtracker.ImportLabels(context.Background(), tracker, labels, testPool, parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	remote := gitlabtracker.Issue{
		ID: 5100, IID: 5100, State: "opened", Title: "mapped import", UpdatedAt: "2026-08-17T00:00:00Z",
		StartDate: "2026-08-17", DueDate: "2026-08-20",
		Labels: []string{"workflow::in-progress", "priority::high", "bug"},
	}
	if err := gitlabtracker.ImportIssues(context.Background(), tracker, []gitlabtracker.Issue{remote}, testPool, parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}

	var issueID, status, priority string
	var startDate, dueDate pgtype.Date
	if err := testPool.QueryRow(context.Background(), `
SELECT id,status,priority,start_date,due_date FROM issue
WHERE tracker_connection_id=$1 AND title='mapped import'`, parseUUID(trackerID)).Scan(&issueID, &status, &priority, &startDate, &dueDate); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID) })
	if status != "in_progress" || priority != "high" || startDate.Time.Format("2006-01-02") != "2026-08-17" || dueDate.Time.Format("2006-01-02") != "2026-08-20" {
		t.Fatalf("fields = %s/%s/%v/%v", status, priority, startDate, dueDate)
	}
	var relationCount, visibleCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue_to_label WHERE issue_id=$1`, issueID).Scan(&relationCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `
SELECT count(*) FROM issue_to_label itl JOIN issue_label l ON l.id=itl.label_id
WHERE itl.issue_id=$1 AND l.mapping_kind='none'`, issueID).Scan(&visibleCount); err != nil {
		t.Fatal(err)
	}
	if relationCount != 3 || visibleCount != 1 {
		t.Fatalf("label relations complete=%d visible=%d", relationCount, visibleCount)
	}
}
