package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGitlabRepoResourceLifecycle mirrors the github_repo happy path so
// gitlab_repo behaves like a first-class citizen at the resource layer:
// validator accepts it, response echoes the normalized ref, list surfaces
// it, and delete removes it. Phase 2 promise (§13.1): "新 resource 类型
// `gitlab_repo`：`validateAndNormalizeResourceRef` 加一个 case".
func TestGitlabRepoResourceLifecycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	_ = ctx

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "gitlab-repo-lifecycle",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	t.Cleanup(func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})

	// Attach a gitlab_repo — mirrors the github_repo ref shape.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "gitlab_repo",
		"resource_ref":  map[string]any{"url": "https://gitlab.com/group/project", "default_branch_hint": "main"},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource(gitlab_repo): %d %s", w.Code, w.Body.String())
	}
	var created ProjectResourceResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.ResourceType != "gitlab_repo" {
		t.Errorf("ResourceType = %q, want gitlab_repo", created.ResourceType)
	}
	var ref struct {
		URL               string `json:"url"`
		DefaultBranchHint string `json:"default_branch_hint,omitempty"`
	}
	json.Unmarshal(created.ResourceRef, &ref)
	if ref.URL != "https://gitlab.com/group/project" {
		t.Errorf("ref.URL = %q", ref.URL)
	}
	if ref.DefaultBranchHint != "main" {
		t.Errorf("ref.DefaultBranchHint = %q, want main", ref.DefaultBranchHint)
	}

	// Reject a gitlab_repo with an empty URL — same message shape as
	// github_repo so error handling stays uniform.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "gitlab_repo",
		"resource_ref":  map[string]any{"url": ""},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty url: got %d, want 400", w.Code)
	}

	// Reject a garbage URL — the isValidGitRepoURL check should refuse it.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "gitlab_repo",
		"resource_ref":  map[string]any{"url": "not-a-url"},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad url: got %d, want 400", w.Code)
	}
}
