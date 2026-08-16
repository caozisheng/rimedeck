package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestEnqueueTrackerOutbox_CompressesEarlierRevisions locks the core
// Task 2 invariant: three rapid updates to the same issue yield exactly
// one pending row carrying the newest desired_revision. The older two
// flip to 'cancelled' so the worker never pushes stale intent.
func TestEnqueueTrackerOutbox_CompressesEarlierRevisions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "outbox-compress")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	// Seed a fake issue row scoped to the tracker so the outbox rows
	// have a real (tracker, issue, operation) group to compress against.
	var issueID pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO issue(workspace_id, title, status, priority, creator_type, creator_id, project_id, number, source_type, tracker_connection_id, sync_state, sync_revision, synced_revision)
		 VALUES ($1,'compress','todo','none','member',$2,$3,
		         (SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id=$1),
		         'gitlab',$4,'pending',0,0)
		 RETURNING id`,
		parseUUID(testWorkspaceID), parseUUID(testUserID), parseUUID(project.ID), parseUUID(trackerID)).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})

	base := db.CreateTrackerOutboxParams{
		WorkspaceID:         parseUUID(testWorkspaceID),
		TrackerConnectionID: parseUUID(trackerID),
		IssueID:             issueID,
		Operation:           "update_issue",
		Payload:             []byte(`{}`),
	}
	// Enqueue three revisions in ascending order. Each call should
	// leave exactly one pending row: the one just inserted.
	for _, rev := range []int64{2, 5, 9} {
		params := base
		params.IdempotencyKey = newRandomUUID()
		params.DesiredRevision = pgtype.Int8{Int64: rev, Valid: true}
		if _, err := enqueueTrackerOutbox(context.Background(), testHandler.Queries, params); err != nil {
			t.Fatalf("enqueue rev %d: %v", rev, err)
		}
	}

	rows, err := testPool.Query(context.Background(),
		`SELECT status, COALESCE(desired_revision,0) FROM tracker_sync_outbox
		 WHERE tracker_connection_id=$1 AND issue_id=$2 AND operation='update_issue'
		 ORDER BY desired_revision`,
		parseUUID(trackerID), issueID)
	if err != nil {
		t.Fatalf("load rows: %v", err)
	}
	defer rows.Close()
	var pendingRevs []int64
	var cancelled int
	for rows.Next() {
		var status string
		var rev int64
		if err := rows.Scan(&status, &rev); err != nil {
			t.Fatalf("scan: %v", err)
		}
		switch status {
		case "pending":
			pendingRevs = append(pendingRevs, rev)
		case "cancelled":
			cancelled++
		default:
			t.Fatalf("unexpected status %q rev=%d", status, rev)
		}
	}
	if len(pendingRevs) != 1 || pendingRevs[0] != 9 {
		t.Fatalf("pending revisions = %v, want [9]", pendingRevs)
	}
	if cancelled != 2 {
		t.Fatalf("cancelled = %d, want 2", cancelled)
	}
}

// TestEnqueueTrackerOutbox_LeavesRunningRows verifies the invariant
// that we never touch a row currently on the wire — if the worker is
// mid-flight, compression only trims the queue behind it, not itself.
func TestEnqueueTrackerOutbox_LeavesRunningRows(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "outbox-compress-inflight")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	var issueID pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO issue(workspace_id, title, status, priority, creator_type, creator_id, project_id, number, source_type, tracker_connection_id, sync_state, sync_revision, synced_revision)
		 VALUES ($1,'inflight','todo','none','member',$2,$3,
		         (SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id=$1),
		         'gitlab',$4,'pending',0,0)
		 RETURNING id`,
		parseUUID(testWorkspaceID), parseUUID(testUserID), parseUUID(project.ID), parseUUID(trackerID)).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})

	// First enqueue then flip to 'running' — simulates the worker just
	// having claimed it. A later enqueue must not cancel it.
	first := db.CreateTrackerOutboxParams{
		WorkspaceID: parseUUID(testWorkspaceID), TrackerConnectionID: parseUUID(trackerID),
		IssueID: issueID, Operation: "update_issue", Payload: []byte(`{}`),
		IdempotencyKey: newRandomUUID(), DesiredRevision: pgtype.Int8{Int64: 1, Valid: true},
	}
	firstRow, err := enqueueTrackerOutbox(context.Background(), testHandler.Queries, first)
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE tracker_sync_outbox SET status='running' WHERE id=$1`, firstRow.ID); err != nil {
		t.Fatalf("simulate claim: %v", err)
	}

	second := first
	second.IdempotencyKey = newRandomUUID()
	second.DesiredRevision = pgtype.Int8{Int64: 2, Valid: true}
	if _, err := enqueueTrackerOutbox(context.Background(), testHandler.Queries, second); err != nil {
		t.Fatalf("enqueue second: %v", err)
	}

	var runningCount, pendingCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT
		   count(*) FILTER (WHERE status='running'),
		   count(*) FILTER (WHERE status='pending')
		 FROM tracker_sync_outbox WHERE tracker_connection_id=$1 AND issue_id=$2`,
		parseUUID(trackerID), issueID).Scan(&runningCount, &pendingCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if runningCount != 1 || pendingCount != 1 {
		t.Fatalf("running=%d pending=%d, want 1/1", runningCount, pendingCount)
	}
}

// TestClaimReadyTrackerOutbox_SerializesPerConnection asserts the
// per-connection DISTINCT ON layer: two ready rows on the same
// connection produce only one claim; two rows on distinct connections
// both claim in a single tick.
func TestClaimReadyTrackerOutbox_SerializesPerConnection(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	projectA := projectForCreateTracker(t, "serial-A")
	projectB := projectForCreateTracker(t, "serial-B")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerA := createTrackerHelper(t, projectA.ID)
	trackerB := createTrackerHelper(t, projectB.ID)
	// The Task 8 create path already enqueued 2 pull rows per tracker;
	// clear them so this test observes only its own inserts.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE tracker_sync_outbox SET status='succeeded' WHERE tracker_connection_id = ANY($1::uuid[])`,
		[]pgtype.UUID{parseUUID(trackerA), parseUUID(trackerB)}); err != nil {
		t.Fatalf("drain seed: %v", err)
	}

	insertReady := func(trackerID string) {
		if _, err := enqueueTrackerOutbox(context.Background(), testHandler.Queries, db.CreateTrackerOutboxParams{
			WorkspaceID: parseUUID(testWorkspaceID), TrackerConnectionID: parseUUID(trackerID),
			Operation: "pull_labels", Payload: []byte(`{}`), IdempotencyKey: newRandomUUID(),
		}); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
	// Two rows on A, one on B. Claim tick must yield: 1 for A, 1 for B.
	insertReady(trackerA)
	insertReady(trackerA)
	insertReady(trackerB)

	claimed, err := testHandler.Queries.ClaimReadyTrackerOutbox(context.Background(), 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	perConnection := map[string]int{}
	for _, row := range claimed {
		perConnection[uuidToString(row.TrackerConnectionID)]++
	}
	if perConnection[trackerA] != 1 || perConnection[trackerB] != 1 {
		t.Fatalf("claimed distribution = %+v, want 1 per tracker", perConnection)
	}
}

// Suppress the unused-import lint when the tests all skip: encoding/json
// and http/httptest are pulled in for future write-op tests that live
// next to this file, so keep the imports live now.
var _ = json.Marshal
var _ = httptest.NewServer
var _ = http.MethodPost
