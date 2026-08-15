package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListIssuesSourceFilter verifies that the source/tracker_id/sync_state
// query params correctly filter the mixed issue list. Seeded via raw SQL
// since Phase 1 has no tracker-management API yet.
func TestListIssuesSourceFilter(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Seed a project.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "gitlab-filter-test",
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

	// Seed a tracker connection via raw SQL (no API in Phase 1).
	var trackerID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO gitlab_tracker_connection (
			project_id, workspace_id, instance_url, remote_project_id,
			path_with_namespace, web_url, clone_url,
			token_ciphertext, token_key_version,
			webhook_secret_ciphertext, webhook_state, state, created_by
		) VALUES ($1, $2, 'https://gitlab.example.com', 42,
			'group/project', 'https://gitlab.example.com/group/project',
			'https://gitlab.example.com/group/project.git',
			'\x00', 0, '\x00', 'unavailable', 'active', $3)
		RETURNING id::text`,
		project.ID, testWorkspaceID, testUserID,
	).Scan(&trackerID)
	if err != nil {
		t.Fatalf("seed tracker: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, "DELETE FROM gitlab_tracker_connection WHERE id = $1::uuid", trackerID)
	})

	// Create two local issues.
	for _, title := range []string{"local-1", "local-2"} {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":      title,
			"project_id": project.ID,
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create local issue %q: %d %s", title, w.Code, w.Body.String())
		}
	}

	// Seed a gitlab-source issue via raw SQL (Phase 1: no outbox write path yet).
	_, err = testPool.Exec(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			project_id, source_type, tracker_connection_id, sync_state, number
		) VALUES ($1, 'gitlab-issue-1', 'todo', 'none', 'member', $2::uuid,
			$3::uuid, 'gitlab', $4::uuid, 'synced',
			(SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id = $1))`,
		testWorkspaceID, testUserID, project.ID, trackerID,
	)
	if err != nil {
		t.Fatalf("seed gitlab issue: %v", err)
	}

	// Helper to list issues with params.
	listIssues := func(params string) []map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&project_id="+project.ID+params, nil)
		testHandler.ListIssues(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("list issues %q: %d %s", params, w.Code, w.Body.String())
		}
		var resp struct {
			Issues []map[string]any `json:"issues"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Issues
	}

	// 1. No filter → all 3 issues.
	all := listIssues("")
	if len(all) != 3 {
		t.Errorf("no filter: got %d issues, want 3", len(all))
	}

	// 2. source=local → only local issues.
	locals := listIssues("&source=local")
	if len(locals) != 2 {
		t.Errorf("source=local: got %d issues, want 2", len(locals))
	}
	for _, iss := range locals {
		if iss["source_type"] != "local" {
			t.Errorf("source=local returned issue with source_type=%v", iss["source_type"])
		}
	}

	// 3. source=gitlab → only gitlab issue.
	gitlabs := listIssues("&source=gitlab")
	if len(gitlabs) != 1 {
		t.Errorf("source=gitlab: got %d issues, want 1", len(gitlabs))
	}
	if gitlabs[0]["title"] != "gitlab-issue-1" {
		t.Errorf("source=gitlab: got title %v, want gitlab-issue-1", gitlabs[0]["title"])
	}

	// 4. tracker_id filter.
	byTracker := listIssues("&tracker_id=" + trackerID)
	if len(byTracker) != 1 {
		t.Errorf("tracker_id: got %d issues, want 1", len(byTracker))
	}

	// 5. sync_state=synced → only the gitlab issue.
	synced := listIssues("&sync_state=synced")
	if len(synced) != 1 {
		t.Errorf("sync_state=synced: got %d, want 1", len(synced))
	}

	// 6. sync_state=local → only local issues.
	localSync := listIssues("&sync_state=local")
	if len(localSync) != 2 {
		t.Errorf("sync_state=local: got %d, want 2", len(localSync))
	}
}

// TestListIssuesSourceFilterGoldenLocal verifies that projects without any
// tracker behave identically to before: no filter params → all issues returned,
// source_type defaults to "local" in the response, no outbox rows exist.
func TestListIssuesSourceFilterGoldenLocal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// Create a plain local project + issue (no tracker).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "golden-local-test",
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

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "golden-issue",
		"project_id": project.ID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)

	// Verify source_type is in the response.
	if issue.SourceType != "local" {
		t.Errorf("new issue source_type = %q, want local", issue.SourceType)
	}

	// Verify no outbox rows for this issue.
	var outboxCount int
	testPool.QueryRow(context.Background(),
		"SELECT count(*) FROM tracker_sync_outbox WHERE issue_id = $1::uuid",
		issue.ID).Scan(&outboxCount)
	if outboxCount != 0 {
		t.Errorf("local issue has %d outbox rows, want 0", outboxCount)
	}
}
