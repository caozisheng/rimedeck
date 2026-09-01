package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateIssueGitLabSource verifies that creating an issue with
// source_type=gitlab writes the local row + an outbox entry.
func TestCreateIssueGitLabSource(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Seed project + tracker.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "gitlab-create-test",
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

	var trackerID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO gitlab_tracker_connection (
			project_id, workspace_id, instance_url, remote_project_id,
			path_with_namespace, web_url, clone_url,
			token_ciphertext, token_key_version,
			webhook_secret_ciphertext, webhook_state, state, created_by
		) VALUES ($1, $2, 'https://gitlab.example.com', 99,
			'ns/proj', 'https://gitlab.example.com/ns/proj',
			'https://gitlab.example.com/ns/proj.git',
			'\x00', 0, '\x00', 'unavailable', 'active', $3)
		RETURNING id::text`,
		project.ID, testWorkspaceID, testUserID,
	).Scan(&trackerID)
	if err != nil {
		t.Fatalf("seed tracker: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, "DELETE FROM tracker_sync_outbox WHERE tracker_connection_id = $1::uuid", trackerID)
		testPool.Exec(ctx, "DELETE FROM gitlab_tracker_connection WHERE id = $1::uuid", trackerID)
	})

	t.Run("gitlab source with valid tracker creates issue + outbox", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":                 "gitlab-issue-from-api",
			"project_id":            project.ID,
			"source_type":           "gitlab",
			"tracker_connection_id": trackerID,
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create gitlab issue: %d %s", w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)

		if issue.SourceType != "gitlab" {
			t.Errorf("source_type = %q, want gitlab", issue.SourceType)
		}
		if issue.SyncState != "pending" {
			t.Errorf("sync_state = %q, want pending", issue.SyncState)
		}

		// Verify outbox row exists.
		var outboxCount int
		testPool.QueryRow(ctx,
			"SELECT count(*) FROM tracker_sync_outbox WHERE issue_id = $1::uuid AND operation = 'create_issue'",
			issue.ID).Scan(&outboxCount)
		if outboxCount != 1 {
			t.Errorf("outbox create_issue count = %d, want 1", outboxCount)
		}
	})

	t.Run("stale workspace counter is repaired before GitLab create", func(t *testing.T) {
		if _, err := testPool.Exec(ctx, `
UPDATE workspace SET issue_counter = 0 WHERE id=$1`, testWorkspaceID); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":                 "gitlab-stale-counter",
			"project_id":            project.ID,
			"source_type":           "gitlab",
			"tracker_connection_id": trackerID,
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create with stale counter: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("gitlab source without tracker_connection_id returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":       "should-fail",
			"project_id":  project.ID,
			"source_type": "gitlab",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("gitlab source with tracker from another project returns 400", func(t *testing.T) {
		// Create a second project and try to use the first project's tracker.
		w2 := httptest.NewRecorder()
		req2 := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
			"title": "other-project",
		})
		testHandler.CreateProject(w2, req2)
		if w2.Code != http.StatusCreated {
			t.Fatalf("create other project: %d", w2.Code)
		}
		var otherProject ProjectResponse
		json.NewDecoder(w2.Body).Decode(&otherProject)
		t.Cleanup(func() {
			r := newRequest("DELETE", "/api/projects/"+otherProject.ID, nil)
			r = withURLParam(r, "id", otherProject.ID)
			testHandler.DeleteProject(httptest.NewRecorder(), r)
		})

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":                 "cross-project",
			"project_id":            otherProject.ID,
			"source_type":           "gitlab",
			"tracker_connection_id": trackerID, // belongs to project, not otherProject
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for cross-project tracker, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// TestDeleteProjectWithGitlabIssue detaches mirrored issues before the
// project's tracker connection is cascade-deleted, preserving the issue
// source/connection constraint.
func TestDeleteProjectWithGitlabIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	project := projectForCreateTracker(t, "delete-project-with-gitlab-issue")
	var trackerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO gitlab_tracker_connection (
			project_id, workspace_id, instance_url, remote_project_id,
			path_with_namespace, web_url, clone_url,
			token_ciphertext, token_key_version,
			webhook_secret_ciphertext, webhook_state, state, created_by
		) VALUES ($1, $2, 'https://gitlab.example.com', 1001,
			'ns/delete-project', 'https://gitlab.example.com/ns/delete-project',
			'https://gitlab.example.com/ns/delete-project.git',
			'\\x00', 0, '\\x00', 'unavailable', 'active', $3)
		RETURNING id::text`,
		project.ID, testWorkspaceID, testUserID,
	).Scan(&trackerID); err != nil {
		t.Fatalf("seed tracker: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":                 "mirrored issue for project deletion",
		"project_id":            project.ID,
		"source_type":           "gitlab",
		"tracker_connection_id": trackerID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create mirrored issue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode mirrored issue: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete project with mirrored issue: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var sourceType, syncState string
	if err := testPool.QueryRow(ctx,
		`SELECT source_type, sync_state FROM issue WHERE id = $1::uuid`, issue.ID,
	).Scan(&sourceType, &syncState); err != nil {
		t.Fatalf("read detached issue: %v", err)
	}
	if sourceType != "detached" || syncState != "detached" {
		t.Fatalf("deleted project's issue = (%q, %q), want (detached, detached)", sourceType, syncState)
	}
}

// TestCreateIssueLocalGolden verifies that creating a local issue (no
// source_type or source_type=local) behaves identically to before: no
// outbox, source_type=local in response.
func TestCreateIssueLocalGolden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "golden-create-test",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	t.Cleanup(func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})

	t.Run("omitted source_type defaults to local", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":      "plain-local",
			"project_id": project.ID,
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)
		if issue.SourceType != "local" {
			t.Errorf("source_type = %q, want local", issue.SourceType)
		}
		if issue.SyncState != "local" {
			t.Errorf("sync_state = %q, want local", issue.SyncState)
		}

		var outboxCount int
		testPool.QueryRow(context.Background(),
			"SELECT count(*) FROM tracker_sync_outbox WHERE issue_id = $1::uuid", issue.ID).Scan(&outboxCount)
		if outboxCount != 0 {
			t.Errorf("local issue has %d outbox rows, want 0", outboxCount)
		}
	})

	t.Run("explicit source_type=local works", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":       "explicit-local",
			"project_id":  project.ID,
			"source_type": "local",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)
		if issue.SourceType != "local" {
			t.Errorf("source_type = %q, want local", issue.SourceType)
		}
	})
}
