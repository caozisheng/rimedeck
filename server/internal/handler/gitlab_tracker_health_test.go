package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestGetGitlabTrackerHealth_ReturnsSafeCounters exercises the happy
// path: member context, real tracker with a mix of outbox row statuses,
// response shape matches the design's numeric-only contract.
func TestGetGitlabTrackerHealth_ReturnsSafeCounters(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "health-happy")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	// Seed three outbox rows in the three statuses the panel surfaces.
	// The create-tracker path already left two pending rows (pull_labels
	// + reconcile from Task 8 of Phase 2), so we flip statuses inline.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE tracker_sync_outbox SET status='failed' WHERE tracker_connection_id=$1 AND operation='reconcile'`,
		parseUUID(trackerID)); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO tracker_sync_outbox(workspace_id, tracker_connection_id, operation, payload, idempotency_key, status)
		 VALUES ($1,$2,'reconcile','{}',gen_random_uuid(),'retrying')`,
		parseUUID(testWorkspaceID), parseUUID(trackerID)); err != nil {
		t.Fatalf("seed retrying: %v", err)
	}

	req := newRequest("GET", "/api/projects/"+project.ID+"/gitlab-trackers/"+trackerID+"/health", nil)
	req = withURLParams(req, "id", project.ID, "trackerId", trackerID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "member"}))
	w := httptest.NewRecorder()
	testHandler.GetGitlabTrackerHealth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp GitlabTrackerHealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PendingOutboxCount != 1 {
		t.Fatalf("pending=%d, want 1", resp.PendingOutboxCount)
	}
	if resp.RetryingOutboxCount != 1 {
		t.Fatalf("retrying=%d, want 1", resp.RetryingOutboxCount)
	}
	if resp.FailedOutboxCount != 1 {
		t.Fatalf("failed=%d, want 1", resp.FailedOutboxCount)
	}
	if resp.State == "" {
		t.Fatalf("state empty, want a non-empty enum")
	}
}

// TestGetGitlabTrackerHealth_TrackerFromAnotherProjectRejected: the
// health endpoint MUST scope by (project_id, tracker_id) so a member
// of workspace A can't peek at a tracker attached to project B.
func TestGetGitlabTrackerHealth_TrackerFromAnotherProjectRejected(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	projectA := projectForCreateTracker(t, "health-scope-a")
	projectB := projectForCreateTracker(t, "health-scope-b")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerA := createTrackerHelper(t, projectA.ID)

	// Ask projectB for projectA's tracker.
	req := newRequest("GET", "/api/projects/"+projectB.ID+"/gitlab-trackers/"+trackerA+"/health", nil)
	req = withURLParams(req, "id", projectB.ID, "trackerId", trackerA)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "member"}))
	w := httptest.NewRecorder()
	testHandler.GetGitlabTrackerHealth(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-project access should 404, got %d %s", w.Code, w.Body.String())
	}
}
