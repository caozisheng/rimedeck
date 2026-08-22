package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// staticGitlabProjectHandler mirrors the happy-path GetProject response
// used by TestCreateProjectGitlabTracker_HappyPath, so lifecycle tests
// can seed a tracker without re-writing the stub.
func staticGitlabProjectHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  201,
			"path_with_namespace": "group/proj",
			"web_url":             "https://gitlab.example.com/group/proj",
			"default_branch":      "main",
			"permissions":         map[string]any{"project_access": map[string]any{"access_level": 40}},
		})
	})
}

// createTrackerHelper walks through Task 8's create endpoint against the
// installed stub and returns the row id, so lifecycle tests don't have
// to duplicate its plumbing.
func createTrackerHelper(t *testing.T, projectID string) string {
	t.Helper()
	req := newRequest("POST", "/api/projects/"+projectID+"/gitlab-trackers", map[string]any{
		"repository_url": "https://gitlab.example.com/group/proj",
		"access_token":   "glpat-lifecycle",
	})
	req = withURLParam(req, "id", projectID)
	req = req.WithContext(memberOwnerContext(req.Context()))
	w := httptest.NewRecorder()
	testHandler.CreateProjectGitlabTracker(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create tracker for lifecycle test: %d %s", w.Code, w.Body.String())
	}
	var created GitlabTrackerResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return created.ID
}

// lifecycleRequest prepares a request with both URL params + owner
// context in one call so every test body reads the same.
func lifecycleRequest(method, url, projectID, trackerID string, body any) *http.Request {
	req := newRequest(method, url, body)
	req = withURLParams(req, "id", projectID, "trackerId", trackerID)
	return req.WithContext(memberOwnerContext(req.Context()))
}

func TestRotateGitlabTrackerToken_HappyPath(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "lifecycle-rotate")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	before := loadTrackerCiphertext(t, trackerID)
	req := lifecycleRequest("PUT", "/api/projects/"+project.ID+"/gitlab-trackers/"+trackerID+"/token", project.ID, trackerID,
		map[string]any{"access_token": "glpat-rotated"})
	w := httptest.NewRecorder()
	testHandler.RotateGitlabTrackerToken(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("rotate: %d %s", w.Code, w.Body.String())
	}
	after := loadTrackerCiphertext(t, trackerID)
	if string(before) == string(after) {
		t.Fatalf("ciphertext unchanged after rotation")
	}
}

func TestSyncGitlabTracker_EnqueuesLabelsAndReconcile(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "lifecycle-sync")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	req := lifecycleRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers/"+trackerID+"/sync", project.ID, trackerID, nil)
	w := httptest.NewRecorder()
	testHandler.SyncGitlabTracker(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("sync: %d %s", w.Code, w.Body.String())
	}

	ops := loadOutboxOperations(t, trackerID)
	if len(ops) != 2 || !containsAll(ops, []string{"pull_labels", "reconcile"}) {
		t.Fatalf("ops = %v, want one row for each scheduled operation", ops)
	}
}

func TestRetryGitlabTrackerFailedOutbox_ResetsFailedRows(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "lifecycle-retry")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	if _, err := testPool.Exec(context.Background(),
		`UPDATE tracker_sync_outbox SET status = 'failed' WHERE tracker_connection_id = $1`,
		parseUUID(trackerID)); err != nil {
		t.Fatalf("force fail: %v", err)
	}

	req := lifecycleRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers/"+trackerID+"/retry", project.ID, trackerID, nil)
	w := httptest.NewRecorder()
	testHandler.RetryGitlabTrackerFailedOutbox(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		ResetCount int64 `json:"reset_count"`
	}
	json.NewDecoder(w.Body).Decode(&body)
	if body.ResetCount != 2 {
		t.Fatalf("reset_count = %d, want 2 (create enqueues pull_labels + reconcile)", body.ResetCount)
	}
}

func TestDisableGitlabTracker_MarksDisabledAndDetaches(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "lifecycle-disable")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	req := lifecycleRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers/"+trackerID+"/disable", project.ID, trackerID, nil)
	w := httptest.NewRecorder()
	testHandler.DisableGitlabTracker(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	if got := loadTrackerState(t, trackerID); got != "disabled" {
		t.Fatalf("tracker state = %q, want disabled", got)
	}
}

func TestDeleteGitlabTrackerMirrors_RequiresConfirmationHeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "lifecycle-delete-noheader")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	req := lifecycleRequest("DELETE", "/api/projects/"+project.ID+"/gitlab-trackers/"+trackerID+"/mirrors", project.ID, trackerID, nil)
	w := httptest.NewRecorder()
	testHandler.DeleteGitlabTrackerMirrors(w, req)
	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("delete without header: %d %s", w.Code, w.Body.String())
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["code"] != "confirmation_required" {
		t.Errorf("code = %q, want confirmation_required", body["code"])
	}
}

func TestDeleteGitlabTrackerMirrors_RefusesWhenOutboxNonTerminal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "lifecycle-delete-nondrain")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	req := lifecycleRequest("DELETE", "/api/projects/"+project.ID+"/gitlab-trackers/"+trackerID+"/mirrors", project.ID, trackerID, nil)
	req.Header.Set("X-Confirm-Delete-Mirrors", "true")
	w := httptest.NewRecorder()
	testHandler.DeleteGitlabTrackerMirrors(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("delete with pending outbox: %d %s", w.Code, w.Body.String())
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["code"] != "outbox_not_drained" {
		t.Errorf("code = %q, want outbox_not_drained", body["code"])
	}
}

func TestDeleteGitlabTrackerMirrors_HappyPath(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "lifecycle-delete-happy")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)

	// 'succeeded' is the terminal-success state per migration 128 constraint.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE tracker_sync_outbox SET status = 'succeeded' WHERE tracker_connection_id = $1`,
		parseUUID(trackerID)); err != nil {
		t.Fatalf("drain outbox: %v", err)
	}

	req := lifecycleRequest("DELETE", "/api/projects/"+project.ID+"/gitlab-trackers/"+trackerID+"/mirrors", project.ID, trackerID, nil)
	req.Header.Set("X-Confirm-Delete-Mirrors", "true")
	w := httptest.NewRecorder()
	testHandler.DeleteGitlabTrackerMirrors(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete happy: %d %s", w.Code, w.Body.String())
	}
	var exists bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM gitlab_tracker_connection WHERE id = $1)`,
		parseUUID(trackerID)).Scan(&exists); err != nil {
		t.Fatalf("existence check: %v", err)
	}
	if exists {
		t.Fatalf("tracker row still present after delete")
	}
}

// --- small DB helpers -------------------------------------------------------

func loadTrackerCiphertext(t *testing.T, trackerID string) []byte {
	t.Helper()
	var ct []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT token_ciphertext FROM gitlab_tracker_connection WHERE id = $1`,
		parseUUID(trackerID)).Scan(&ct); err != nil {
		t.Fatalf("load ciphertext: %v", err)
	}
	return ct
}

func loadTrackerState(t *testing.T, trackerID string) string {
	t.Helper()
	var s string
	if err := testPool.QueryRow(context.Background(),
		`SELECT state FROM gitlab_tracker_connection WHERE id = $1`,
		parseUUID(trackerID)).Scan(&s); err != nil {
		t.Fatalf("load state: %v", err)
	}
	return s
}

func loadOutboxOperations(t *testing.T, trackerID string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT operation FROM tracker_sync_outbox WHERE tracker_connection_id = $1 ORDER BY created_at, id`,
		parseUUID(trackerID))
	if err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var op string
		if err := rows.Scan(&op); err != nil {
			t.Fatalf("scan op: %v", err)
		}
		out = append(out, op)
	}
	return out
}

func containsAll(haystack, needles []string) bool {
	set := map[string]struct{}{}
	for _, h := range haystack {
		set[h] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}
