package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// projectResponseFor lets each provisioning test choose the
// access_level GitLab reports.
func projectResponseFor(accessLevel int) map[string]any {
	return map[string]any{
		"id":                  201,
		"path_with_namespace": "group/proj",
		"web_url":             "https://gitlab.example.com/group/proj",
		"default_branch":      "main",
		"permissions":         map[string]any{"project_access": map[string]any{"access_level": accessLevel}},
	}
}

// upstreamWithProvision returns an httptest handler that:
//   - answers GetProject with the given permission level
//   - counts POSTs to /projects/{id}/hooks in hookCreates, returning
//     whatever hookStatus asks for (201 with body {"id":hookID} on 2xx)
//   - rejects everything else so mis-routed requests fail loudly.
func upstreamWithProvision(t *testing.T, accessLevel int, hookStatus int, hookID int64, hookCreates *int) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// GetProject: /api/v4/projects/<encoded-path> and NOT the hooks endpoint.
		if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v4/projects/") && !strings.Contains(path, "/hooks") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(projectResponseFor(accessLevel))
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(path, "/hooks") {
			*hookCreates++
			body, _ := io.ReadAll(r.Body)
			var raw map[string]any
			_ = json.Unmarshal(body, &raw)
			if raw["url"] == "" || raw["token"] == "" {
				t.Errorf("hook body missing url/token: %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(hookStatus)
			if hookStatus >= 200 && hookStatus < 300 {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": hookID})
			}
			return
		}
		t.Errorf("unexpected upstream request %s %s", r.Method, path)
		http.NotFound(w, r)
	})
}

// TestCreateProjectGitlabTracker_ProvisionsWebhookWhenAllowed: token
// has Maintainer access, so we POST a hook and flip webhook_state
// to 'active'.
func TestCreateProjectGitlabTracker_ProvisionsWebhookWhenAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	hookCreates := 0
	installGitlabCreateStub(t, upstreamWithProvision(t, 40, http.StatusCreated, 4242, &hookCreates), []string{"gitlab.example.com"})

	prev := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = "https://rimedeck.example"
	t.Cleanup(func() { testHandler.cfg.PublicURL = prev })

	project := projectForCreateTracker(t, "provision-happy")
	req := newRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers?workspace_id="+testWorkspaceID, map[string]any{
		"repository_url": "https://gitlab.example.com/group/proj",
		"access_token":   "glpat-happy",
	})
	req = withURLParam(req, "id", project.ID)
	req = req.WithContext(memberOwnerContext(req.Context()))
	w := httptest.NewRecorder()
	testHandler.CreateProjectGitlabTracker(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if hookCreates != 1 {
		t.Fatalf("hookCreates=%d, want 1", hookCreates)
	}

	var body GitlabTrackerResponse
	_ = json.NewDecoder(w.Body).Decode(&body)
	var state string
	var hookID int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT webhook_state, COALESCE(webhook_id,0) FROM gitlab_tracker_connection WHERE id=$1`,
		parseUUID(body.ID)).Scan(&state, &hookID); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state != "active" || hookID != 4242 {
		t.Fatalf("webhook_state=%q webhook_id=%d, want active/4242", state, hookID)
	}
}

// TestCreateProjectGitlabTracker_SkipsWebhookWhenReadOnly: Developer
// access (30) means we don't even attempt a hook POST — reconcile
// picks up the slack.
func TestCreateProjectGitlabTracker_SkipsWebhookWhenReadOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	hookCreates := 0
	installGitlabCreateStub(t, upstreamWithProvision(t, 30, http.StatusForbidden, 0, &hookCreates), []string{"gitlab.example.com"})

	prev := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = "https://rimedeck.example"
	t.Cleanup(func() { testHandler.cfg.PublicURL = prev })

	project := projectForCreateTracker(t, "provision-skip")
	req := newRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers?workspace_id="+testWorkspaceID, map[string]any{
		"repository_url": "https://gitlab.example.com/group/proj",
		"access_token":   "glpat-readonly",
	})
	req = withURLParam(req, "id", project.ID)
	req = req.WithContext(memberOwnerContext(req.Context()))
	w := httptest.NewRecorder()
	testHandler.CreateProjectGitlabTracker(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if hookCreates != 0 {
		t.Fatalf("hookCreates=%d, want 0 (Developer access should skip provisioning)", hookCreates)
	}

	var body GitlabTrackerResponse
	_ = json.NewDecoder(w.Body).Decode(&body)
	var state string
	if err := testPool.QueryRow(context.Background(),
		`SELECT webhook_state FROM gitlab_tracker_connection WHERE id=$1`,
		parseUUID(body.ID)).Scan(&state); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state != "unavailable" {
		t.Fatalf("webhook_state=%q, want unavailable", state)
	}
}

// TestCreateProjectGitlabTracker_SkipsWebhookWhenPublicURLEmpty:
// deployments without a public URL don't try to provision — GitLab
// can't reach us anyway.
func TestCreateProjectGitlabTracker_SkipsWebhookWhenPublicURLEmpty(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	hookCreates := 0
	installGitlabCreateStub(t, upstreamWithProvision(t, 40, http.StatusCreated, 999, &hookCreates), []string{"gitlab.example.com"})

	prev := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = ""
	t.Cleanup(func() { testHandler.cfg.PublicURL = prev })

	project := projectForCreateTracker(t, "provision-nopublicurl")
	req := newRequest("POST", "/api/projects/"+project.ID+"/gitlab-trackers?workspace_id="+testWorkspaceID, map[string]any{
		"repository_url": "https://gitlab.example.com/group/proj",
		"access_token":   "glpat-x",
	})
	req = withURLParam(req, "id", project.ID)
	req = req.WithContext(memberOwnerContext(req.Context()))
	w := httptest.NewRecorder()
	testHandler.CreateProjectGitlabTracker(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if hookCreates != 0 {
		t.Fatalf("hookCreates=%d, want 0 (no PublicURL should skip provisioning)", hookCreates)
	}
}
