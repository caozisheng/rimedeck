package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/gitlabtracker"
)

// installGitlabValidateStub swaps the package-level factory so
// ValidateGitlabTracker points at an httptest GitLab. Returned cleanup
// restores the previous factory and closes the stub.
func installGitlabValidateStub(t *testing.T, handler http.Handler) (baseURL string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	origFactory := gitlabTrackerClientFactory
	gitlabTrackerClientFactory = func(_, token string) (*gitlabtracker.RestClient, error) {
		transport, err := gitlabtracker.NewClient(gitlabtracker.Config{})
		if err != nil {
			return nil, err
		}
		return gitlabtracker.NewRestClient(transport, srv.URL, token), nil
	}
	t.Cleanup(func() {
		gitlabTrackerClientFactory = origFactory
		srv.Close()
	})
	return srv.URL
}

// TestValidateGitlabTracker_HappyPath returns a safe summary that
// includes the permission bits derived from the mocked access level.
func TestValidateGitlabTracker_HappyPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler fixture not initialized")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  99,
			"path_with_namespace": "group/project",
			"web_url":             "https://gitlab.example.com/group/project",
			"default_branch":      "main",
			"permissions":         map[string]any{"project_access": map[string]any{"access_level": 40}},
		})
	})
	installGitlabValidateStub(t, handler)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/gitlab-trackers/validate", map[string]any{
		"repository_url": "https://gitlab.example.com/group/project",
		"access_token":   "glpat-secret",
	})
	testHandler.ValidateGitlabTracker(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	var got ValidateGitlabTrackerResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RemoteProjectID != 99 || got.PathWithNamespace != "group/project" {
		t.Fatalf("unexpected summary: %+v", got)
	}
	if got.Host != "gitlab.example.com" {
		t.Errorf("Host = %q, want gitlab.example.com", got.Host)
	}
	if got.Permissions.CanWriteIssues != true || got.Permissions.CanConfigureWebhook != true {
		t.Errorf("permissions = %+v, want writes=true webhook=true (access_level 40)", got.Permissions)
	}
	if jsonContains(body, "glpat-secret") {
		t.Errorf("response body leaked token: %s", body)
	}
}

// TestValidateGitlabTracker_ErrorMapping walks the error branches so the
// frontend can rely on the `code` field.
func TestValidateGitlabTracker_ErrorMapping(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler fixture not initialized")
	}
	cases := []struct {
		name     string
		status   int
		wantCode string
		wantHTTP int
	}{
		{"unauthorized", http.StatusUnauthorized, "invalid_token", http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden, "permission_denied", http.StatusForbidden},
		{"not_found", http.StatusNotFound, "not_found", http.StatusNotFound},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})
			installGitlabValidateStub(t, handler)

			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/gitlab-trackers/validate", map[string]any{
				"repository_url": "https://gitlab.example.com/g/p",
				"access_token":   "glpat",
			})
			testHandler.ValidateGitlabTracker(w, req)
			if w.Code != tc.wantHTTP {
				t.Fatalf("status = %d, want %d, body = %s", w.Code, tc.wantHTTP, w.Body.String())
			}
			var body map[string]string
			json.NewDecoder(w.Body).Decode(&body)
			if body["code"] != tc.wantCode {
				t.Errorf("code = %q, want %q", body["code"], tc.wantCode)
			}
		})
	}
}

// TestValidateGitlabTracker_InvalidURL surfaces the parser's error code
// verbatim so the UI can render an actionable message without pattern-
// matching English strings.
func TestValidateGitlabTracker_InvalidURL(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler fixture not initialized")
	}
	// No stub — the request never reaches GitLab.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/gitlab-trackers/validate", map[string]any{
		"repository_url": "https://user:pass@gitlab.com/g/p",
		"access_token":   "glpat",
	})
	testHandler.ValidateGitlabTracker(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["code"] != "userinfo_forbidden" {
		t.Errorf("code = %q, want userinfo_forbidden", body["code"])
	}
}

// TestValidateGitlabTracker_MissingToken rejects before reaching the
// parser so the transport is never invoked with an empty PAT.
func TestValidateGitlabTracker_MissingToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler fixture not initialized")
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/gitlab-trackers/validate", map[string]any{
		"repository_url": "https://gitlab.com/g/p",
		"access_token":   "",
	})
	testHandler.ValidateGitlabTracker(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["code"] != "access_token_required" {
		t.Errorf("code = %q, want access_token_required", body["code"])
	}
}

// jsonContains is a tiny helper for the "token not echoed" assertion.
// Avoids importing strings just for the test file.
func jsonContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

