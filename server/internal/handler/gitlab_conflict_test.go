package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestDetachIssueTracker_FlipsSourceAndCancelsOutbox: the "Convert to
// local" action turns a mirrored issue into a plain local record and
// drops any queued push so the worker cannot resurrect it.
func TestDetachIssueTracker_FlipsSourceAndCancelsOutbox(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "conflict-detach")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t), []string{"gitlab.example.com"})
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, true)

	// Simulate a queued update_issue outbox row.
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO tracker_sync_outbox(workspace_id, tracker_connection_id, issue_id, operation, payload, idempotency_key, desired_revision, status)
		 VALUES ($1,$2,$3,'update_issue','{}',gen_random_uuid(),2,'pending')`,
		parseUUID(testWorkspaceID), parseUUID(trackerID), parseUUID(issueID)); err != nil {
		t.Fatalf("seed outbox: %v", err)
	}

	req := newRequest("POST", "/api/issues/"+issueID+"/detach-tracker", nil)
	req = withURLParam(req, "id", issueID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "owner"}))
	w := httptest.NewRecorder()
	testHandler.DetachIssueTracker(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("detach: %d %s", w.Code, w.Body.String())
	}

	var sourceType, syncState string
	var trackerConn pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`SELECT source_type, sync_state, tracker_connection_id FROM issue WHERE id=$1`,
		parseUUID(issueID)).Scan(&sourceType, &syncState, &trackerConn); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if sourceType != "detached" || syncState != "detached" || trackerConn.Valid {
		t.Fatalf("post-detach: source=%q sync=%q tracker=%v, want detached/detached/null",
			sourceType, syncState, trackerConn.Valid)
	}

	var cancelled int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM tracker_sync_outbox WHERE issue_id=$1 AND status='cancelled'`,
		parseUUID(issueID)).Scan(&cancelled); err != nil {
		t.Fatalf("count cancelled: %v", err)
	}
	if cancelled != 1 {
		t.Fatalf("cancelled outbox rows = %d, want 1", cancelled)
	}
}

// TestDiscardIssuePending_RollsRevisionAndCancelsOutbox: "Discard local
// changes" resets sync_revision to synced_revision and cancels the
// queued outbox row.
func TestDiscardIssuePending_RollsRevisionAndCancelsOutbox(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "conflict-discard")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t), []string{"gitlab.example.com"})
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, true)

	// Simulate a local edit that pushed sync_revision past synced_revision.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET sync_revision=3, sync_state='pending' WHERE id=$1`,
		parseUUID(issueID)); err != nil {
		t.Fatalf("simulate local edit: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO tracker_sync_outbox(workspace_id, tracker_connection_id, issue_id, operation, payload, idempotency_key, desired_revision, status)
		 VALUES ($1,$2,$3,'update_issue','{}',gen_random_uuid(),3,'failed')`,
		parseUUID(testWorkspaceID), parseUUID(trackerID), parseUUID(issueID)); err != nil {
		t.Fatalf("seed outbox: %v", err)
	}

	req := newRequest("POST", "/api/issues/"+issueID+"/discard-pending", nil)
	req = withURLParam(req, "id", issueID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "owner"}))
	w := httptest.NewRecorder()
	testHandler.DiscardIssuePendingRevision(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("discard: %d %s", w.Code, w.Body.String())
	}

	var rev, synced int64
	var syncState string
	if err := testPool.QueryRow(context.Background(),
		`SELECT sync_revision, synced_revision, sync_state FROM issue WHERE id=$1`,
		parseUUID(issueID)).Scan(&rev, &synced, &syncState); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if rev != synced || syncState != "synced" {
		t.Fatalf("post-discard: rev=%d synced=%d sync=%q, want equal/synced", rev, synced, syncState)
	}
	// The failed outbox row is not in {pending,running,retrying}, so
	// CancelTrackerOutboxByIssue leaves it as-is per query definition.
	// Assert no non-terminal rows remain.
	var pending int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM tracker_sync_outbox WHERE issue_id=$1 AND status IN ('pending','running','retrying')`,
		parseUUID(issueID)).Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 0 {
		t.Fatalf("pending rows after discard = %d, want 0", pending)
	}
}

// TestDetachIssueTracker_NonOwnerForbidden: role gate matches the
// tracker-lifecycle endpoints so a plain member cannot mutate the
// mirror state.
func TestDetachIssueTracker_NonOwnerForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "conflict-role")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t), []string{"gitlab.example.com"})
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, true)

	req := newRequest("POST", "/api/issues/"+issueID+"/detach-tracker", nil)
	req = withURLParam(req, "id", issueID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "member"}))
	w := httptest.NewRecorder()
	testHandler.DetachIssueTracker(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("member should be forbidden: got %d %s", w.Code, w.Body.String())
	}
}
