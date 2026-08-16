package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// memberOwnerContext / memberMemberContext are thin wrappers around
// middleware.SetMemberContext used to inject a role into the request the
// test hands directly to a handler (bypassing the chi middleware chain).
func memberOwnerContext(ctx context.Context) context.Context {
	return middleware.SetMemberContext(ctx, testWorkspaceID, db.Member{Role: "owner"})
}
func memberMemberContext(ctx context.Context) context.Context {
	return middleware.SetMemberContext(ctx, testWorkspaceID, db.Member{Role: "member"})
}

// installGitlabCreateStub sets up both the REST client factory (pointing
// at the given httptest handler) and the cipher provider (an in-memory
// cipher) so CreateProjectGitlabTracker can run without env config.
// Returns the cipher so tests can decrypt token_ciphertext for the
// "round-trip" assertion.
func installGitlabCreateStub(t *testing.T, handler http.Handler, allowedHosts []string) *gitlabtracker.Cipher {
	t.Helper()
	srv := httptest.NewServer(handler)
	origFactory := gitlabTrackerClientFactory
	origHosts := GitlabTrackerAllowedHosts
	origProvider := GitlabTrackerCipherProvider
	GitlabTrackerAllowedHosts = func() []string { return allowedHosts }
	gitlabTrackerClientFactory = func(_, token string) (*gitlabtracker.RestClient, error) {
		transport, err := gitlabtracker.NewClient(gitlabtracker.Config{
			AllowedHosts: []string{gitlabtracker.AllowLoopbackFlag},
		})
		if err != nil {
			return nil, err
		}
		return gitlabtracker.NewRestClient(transport, srv.URL, token), nil
	}
	rawKey := make([]byte, 32)
	for i := range rawKey {
		rawKey[i] = byte(i + 3)
	}
	cipher, err := gitlabtracker.NewCipher(map[int16]string{1: base64.StdEncoding.EncodeToString(rawKey)})
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	GitlabTrackerCipherProvider = func() (*gitlabtracker.Cipher, error) { return cipher, nil }
	t.Cleanup(func() {
		gitlabTrackerClientFactory = origFactory
		GitlabTrackerAllowedHosts = origHosts
		GitlabTrackerCipherProvider = origProvider
		srv.Close()
	})
	return cipher
}

// projectForCreateTracker seeds a project and returns its response. The
// cleanup delete removes the tracker + its outbox rows via the cascade
// on gitlab_tracker_connection.
func projectForCreateTracker(t *testing.T, title string) ProjectResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	t.Cleanup(func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})
	return project
}

// TestCreateProjectGitlabTracker_HappyPath persists a validated tracker,
// enqueues the two first-import outbox rows, and never leaks the token.
func TestCreateProjectGitlabTracker_HappyPath(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	cipher := installGitlabCreateStub(t, handler, []string{"gitlab.example.com"})

	project := projectForCreateTracker(t, "tracker-happy")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers?workspace_id="+testWorkspaceID, map[string]any{
		"repository_url": "https://gitlab.example.com/group/proj",
		"access_token":   "glpat-live",
	})
	req = withURLParam(req, "id", project.ID)
	req = req.WithContext(memberOwnerContext(req.Context()))
	testHandler.CreateProjectGitlabTracker(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "glpat-live") {
		t.Fatalf("response leaked token: %s", body)
	}
	var got GitlabTrackerResponse
	json.Unmarshal([]byte(body), &got)
	if got.PathWithNamespace != "group/proj" || got.TokenConfigured != true {
		t.Fatalf("summary = %+v", got)
	}
	if got.PendingOutboxCount != 2 {
		t.Errorf("PendingOutboxCount = %d, want 2 (pull_labels + reconcile)", got.PendingOutboxCount)
	}

	// Row + ciphertext + outbox invariants — the JSON summary alone
	// cannot prove them since it hides secrets by design.
	ctx := context.Background()
	var (
		encTok  []byte
		encSec  []byte
		verHave int16
	)
	if err := testPool.QueryRow(ctx,
		`SELECT token_ciphertext, webhook_secret_ciphertext, token_key_version FROM gitlab_tracker_connection WHERE id = $1::uuid`,
		got.ID,
	).Scan(&encTok, &encSec, &verHave); err != nil {
		t.Fatalf("select tracker: %v", err)
	}
	if verHave != cipher.LatestVersion() {
		t.Errorf("token_key_version = %d, want %d", verHave, cipher.LatestVersion())
	}
	decrypted, err := cipher.Decrypt(encTok)
	if err != nil {
		t.Fatalf("decrypt token: %v", err)
	}
	if string(decrypted) != "glpat-live" {
		t.Errorf("decrypted token = %q, want glpat-live", decrypted)
	}
	if bytes := len(encSec); bytes <= 32 {
		t.Errorf("webhook_secret_ciphertext len = %d, expected wrapped >= 32 bytes", bytes)
	}
	// Outbox rows must reference the new tracker with the two seed ops.
	var opsCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM tracker_sync_outbox WHERE tracker_connection_id = $1::uuid AND operation IN ('pull_labels','reconcile')`,
		got.ID,
	).Scan(&opsCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if opsCount != 2 {
		t.Errorf("outbox seed ops = %d, want 2", opsCount)
	}
}

// TestCreateProjectGitlabTracker_DuplicateReturns409 exercises the
// (project, instance, remote_project_id) unique constraint.
func TestCreateProjectGitlabTracker_DuplicateReturns409(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  202,
			"path_with_namespace": "dup/proj",
			"web_url":             "https://gitlab.example.com/dup/proj",
			"default_branch":      "main",
			"permissions":         map[string]any{"project_access": map[string]any{"access_level": 40}},
		})
	})
	installGitlabCreateStub(t, handler, []string{"gitlab.example.com"})
	project := projectForCreateTracker(t, "tracker-dup")
	makeReq := func() *http.Request {
		req := newRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers?workspace_id="+testWorkspaceID, map[string]any{
			"repository_url": "https://gitlab.example.com/dup/proj",
			"access_token":   "glpat",
		})
		req = withURLParam(req, "id", project.ID)
		return req.WithContext(memberOwnerContext(req.Context()))
	}

	w := httptest.NewRecorder()
	testHandler.CreateProjectGitlabTracker(w, makeReq())
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: %d %s", w.Code, w.Body.String())
	}

	// Second identical call must 409, not 500 or a silent duplicate.
	w = httptest.NewRecorder()
	testHandler.CreateProjectGitlabTracker(w, makeReq())
	if w.Code != http.StatusConflict {
		t.Fatalf("second create: %d %s (want 409)", w.Code, w.Body.String())
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["code"] != "tracker_already_exists" {
		t.Errorf("code = %q, want tracker_already_exists", body["code"])
	}
}

// TestCreateProjectGitlabTracker_MemberRoleRejected keeps the owner/admin
// gate honest: a regular member's request stops at 403.
func TestCreateProjectGitlabTracker_MemberRoleRejected(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	installGitlabCreateStub(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("member request must not reach GitLab")
	}), []string{"gitlab.example.com"})
	project := projectForCreateTracker(t, "tracker-role-gate")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers?workspace_id="+testWorkspaceID, map[string]any{
		"repository_url": "https://gitlab.example.com/x/y",
		"access_token":   "glpat",
	})
	req = withURLParam(req, "id", project.ID)
	req = req.WithContext(memberMemberContext(req.Context()))
	testHandler.CreateProjectGitlabTracker(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", w.Code, w.Body.String())
	}
}

// TestCreateProjectGitlabTracker_CipherMissingIs503 proves the fail-closed
// contract: without GITLAB_TRACKER_KEYS the endpoint refuses to accept
// credentials even from an owner.
func TestCreateProjectGitlabTracker_CipherMissingIs503(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler fixture not initialized")
	}
	orig := GitlabTrackerCipherProvider
	GitlabTrackerCipherProvider = func() (*gitlabtracker.Cipher, error) {
		return nil, gitlabtracker.ErrRemote // any non-nil error; message is opaque
	}
	t.Cleanup(func() { GitlabTrackerCipherProvider = orig })

	project := projectForCreateTracker(t, "tracker-no-key")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers?workspace_id="+testWorkspaceID, map[string]any{
		"repository_url": "https://gitlab.com/g/p",
		"access_token":   "glpat",
	})
	req = withURLParam(req, "id", project.ID)
	req = req.WithContext(memberOwnerContext(req.Context()))
	testHandler.CreateProjectGitlabTracker(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", w.Code, w.Body.String())
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["code"] != "encryption_unavailable" {
		t.Errorf("code = %q, want encryption_unavailable", body["code"])
	}
}
