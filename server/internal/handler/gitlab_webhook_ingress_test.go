package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/gitlabtracker"
)

// webhookFixture wires a tracker with a known-plaintext webhook secret
// so tests can hand-craft valid + invalid X-Gitlab-Token values.
type webhookFixture struct {
	trackerID string
	secret    string
}

// installWebhookTracker seeds a project + tracker whose webhook secret
// ciphertext decrypts to the returned plaintext. Reuses the create-stub
// so the tracker rows land through the normal path (and thus honor
// every FK/constraint the real flow exercises).
func installWebhookTracker(t *testing.T) webhookFixture {
	t.Helper()
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	project := projectForCreateTracker(t, "webhook-"+t.Name())
	trackerID := createTrackerHelper(t, project.ID)

	// Overwrite the auto-generated random secret with a deterministic
	// plaintext so the test can produce a matching X-Gitlab-Token.
	cipher, err := GitlabTrackerCipherProvider()
	if err != nil {
		t.Fatalf("cipher provider: %v", err)
	}
	plaintext := "webhook-secret-fixture"
	ct, err := cipher.Encrypt([]byte(plaintext))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE gitlab_tracker_connection SET webhook_secret_ciphertext=$1 WHERE id=$2`,
		ct, parseUUID(trackerID)); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	return webhookFixture{trackerID: trackerID, secret: plaintext}
}

// makeWebhookRequest builds a POST for the ingress handler. Callers can
// override the token to exercise the auth failure branch and the event
// UUID to exercise dedupe.
func makeWebhookRequest(trackerID, token, eventUUID string, payload any) *http.Request {
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab/"+trackerID, bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", token)
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	req.Header.Set("X-Gitlab-Event-UUID", eventUUID)
	req.Header.Set("Content-Type", "application/json")
	return withURLParam(req, "trackerId", trackerID)
}

// gitlabWebhookPayloadFor returns a minimal Issue Hook payload matching
// the tracker's remote project id (staticGitlabProjectHandler seeds 201
// during Phase 2 tests).
func gitlabWebhookPayloadFor(iid int32) map[string]any {
	return map[string]any{
		"project":           map[string]any{"id": 201},
		"object_attributes": map[string]any{"iid": iid},
	}
}

// TestHandleGitlabWebhook_HappyPathEnqueuesPullIssue proves a valid
// delivery lands one pull_issue outbox row keyed by remote iid.
func TestHandleGitlabWebhook_HappyPathEnqueuesPullIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := installWebhookTracker(t)

	req := makeWebhookRequest(fx.trackerID, fx.secret, "evt-happy-1", gitlabWebhookPayloadFor(42))
	w := httptest.NewRecorder()
	testHandler.HandleGitlabWebhook(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d, want 202. body=%s", w.Code, w.Body.String())
	}

	var op string
	var payload []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT operation, payload FROM tracker_sync_outbox WHERE tracker_connection_id=$1 AND operation='pull_issue' ORDER BY created_at DESC LIMIT 1`,
		parseUUID(fx.trackerID)).Scan(&op, &payload); err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	if op != "pull_issue" {
		t.Fatalf("operation=%q, want pull_issue", op)
	}
	var decoded struct {
		IID int32 `json:"iid"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.IID != 42 {
		t.Fatalf("iid=%d, want 42", decoded.IID)
	}
}

// TestHandleGitlabWebhook_DuplicateEventIsNoop replays the same UUID and
// asserts the second call returns 200 with no new outbox row.
func TestHandleGitlabWebhook_DuplicateEventIsNoop(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := installWebhookTracker(t)
	eventID := "evt-dup-1"

	first := httptest.NewRecorder()
	testHandler.HandleGitlabWebhook(first, makeWebhookRequest(fx.trackerID, fx.secret, eventID, gitlabWebhookPayloadFor(7)))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first delivery: %d %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	testHandler.HandleGitlabWebhook(second, makeWebhookRequest(fx.trackerID, fx.secret, eventID, gitlabWebhookPayloadFor(7)))
	if second.Code != http.StatusOK {
		t.Fatalf("replay: %d %s, want 200 no-op", second.Code, second.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM tracker_sync_outbox WHERE tracker_connection_id=$1 AND operation='pull_issue'`,
		parseUUID(fx.trackerID)).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("pull_issue rows = %d, want 1 (dedupe should have suppressed the replay)", count)
	}
}

// TestHandleGitlabWebhook_BadTokenReturns401 walks the auth failure
// branch. The wrong token must never leak whether the tracker exists.
func TestHandleGitlabWebhook_BadTokenReturns401(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := installWebhookTracker(t)

	req := makeWebhookRequest(fx.trackerID, "wrong-secret", "evt-bad-token", gitlabWebhookPayloadFor(1))
	w := httptest.NewRecorder()
	testHandler.HandleGitlabWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// TestHandleGitlabWebhook_ProjectMismatchReturns400 protects against a
// misdirected webhook: same tracker URL, wrong project payload.
func TestHandleGitlabWebhook_ProjectMismatchReturns400(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := installWebhookTracker(t)

	req := makeWebhookRequest(fx.trackerID, fx.secret, "evt-mismatch",
		map[string]any{"project": map[string]any{"id": 999}})
	w := httptest.NewRecorder()
	testHandler.HandleGitlabWebhook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400. body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGitlabWebhook_MissingEventUUIDReturns400 forces the header
// contract: without X-Gitlab-Event-UUID the ingress cannot dedupe, so
// it refuses the delivery rather than silently double-processing.
func TestHandleGitlabWebhook_MissingEventUUIDReturns400(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := installWebhookTracker(t)

	body, _ := json.Marshal(gitlabWebhookPayloadFor(1))
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab/"+fx.trackerID, bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", fx.secret)
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	req.Header.Set("Content-Type", "application/json")
	req = withURLParam(req, "trackerId", fx.trackerID)
	w := httptest.NewRecorder()
	testHandler.HandleGitlabWebhook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// suppress unused vars if the package skips.
var (
	_ = gitlabtracker.Config{}
	_ = pgtype.UUID{}
)
