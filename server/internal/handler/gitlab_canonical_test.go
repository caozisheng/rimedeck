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
	installGitlabCreateStub(t, staticGitlabProjectHandler(t), []string{"gitlab.example.com"})
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
	}
	if err := gitlabtracker.ApplyCanonicalIssue(context.Background(), tracker, parseUUID(issueID), remote, testPool); err != nil {
		t.Fatalf("ApplyCanonicalIssue: %v", err)
	}

	var title, syncState string
	var rev, synced int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT title, sync_state, sync_revision, synced_revision FROM issue WHERE id=$1`,
		parseUUID(issueID)).Scan(&title, &syncState, &rev, &synced); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if title != "canonical" || syncState != "synced" || rev != 1 || synced != 1 {
		t.Fatalf("title=%q sync=%q rev=%d synced=%d, want canonical/synced/1/1",
			title, syncState, rev, synced)
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
	installGitlabCreateStub(t, staticGitlabProjectHandler(t), []string{"gitlab.example.com"})
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
	if err := gitlabtracker.ApplyCanonicalIssue(context.Background(), tracker, parseUUID(issueID), remote, testPool); err != nil {
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
}

// Suppress unused var warning if the file is imported without db.
var _ = db.Issue{}
var _ pgtype.UUID
