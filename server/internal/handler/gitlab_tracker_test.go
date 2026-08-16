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

func TestListProjectGitlabTrackersSummary(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	project := func() ProjectResponse {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{"title": "gitlab-tracker-list"})
		testHandler.CreateProject(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create project: %d %s", w.Code, w.Body.String())
		}
		var p ProjectResponse
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatalf("decode project: %v", err)
		}
		t.Cleanup(func() {
			r := newRequest("DELETE", "/api/projects/"+p.ID, nil)
			r = withURLParam(r, "id", p.ID)
			testHandler.DeleteProject(httptest.NewRecorder(), r)
		})
		return p
	}()
	_, err := testPool.Exec(ctx, `
		INSERT INTO gitlab_tracker_connection (
			project_id, workspace_id, instance_url, remote_project_id,
			path_with_namespace, web_url, clone_url, token_ciphertext,
			token_key_version, webhook_secret_ciphertext, webhook_state, state, created_by
		) VALUES ($1, $2, 'https://gitlab.example.com', 101,
			'group/project', 'https://gitlab.example.com/group/project',
			'https://gitlab.example.com/group/project.git', '\\x01', 1, '\\x02',
			'unavailable', 'active', $3)`,
		project.ID, testWorkspaceID, testUserID)
	if err != nil {
		t.Fatalf("seed tracker: %v", err)
	}
	var tracker string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM gitlab_tracker_connection WHERE project_id = $1`, project.ID).Scan(&tracker); err != nil {
		t.Fatalf("read tracker id: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM gitlab_tracker_connection WHERE id = $1::uuid`, tracker)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO tracker_sync_outbox (
			workspace_id, tracker_connection_id, operation, payload, idempotency_key, status
		) VALUES ($1, $2::uuid, 'reconcile', '{}'::jsonb, gen_random_uuid(), 'pending')`,
		testWorkspaceID, tracker); err != nil {
		t.Fatalf("seed pending outbox: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO tracker_sync_outbox (
			workspace_id, tracker_connection_id, operation, payload, idempotency_key, status
		) VALUES ($1, $2::uuid, 'reconcile', '{}'::jsonb, gen_random_uuid(), 'failed')`,
		testWorkspaceID, tracker); err != nil {
		t.Fatalf("seed failed outbox: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/gitlab-trackers", nil)
	req = withURLParam(req, "id", project.ID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "member"}))
	w := httptest.NewRecorder()
	testHandler.ListProjectGitlabTrackers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var body struct {
		Trackers []map[string]any `json:"trackers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Trackers) != 1 {
		t.Fatalf("trackers = %d, want 1", len(body.Trackers))
	}
	row := body.Trackers[0]
	if row["path_with_namespace"] != "group/project" || row["state"] != "active" {
		t.Fatalf("unexpected tracker summary: %#v", row)
	}
	if row["token_ciphertext"] != nil || row["webhook_secret_ciphertext"] != nil {
		t.Fatal("tracker response leaked credential ciphertext")
	}
	if row["token_configured"] != true || row["can_manage"] != false {
		t.Fatalf("security/capability fields = %#v", row)
	}
	if row["pending_outbox_count"] != float64(1) || row["failed_outbox_count"] != float64(1) {
		t.Fatalf("outbox counts = %#v", row)
	}
}

func TestListProjectGitlabTrackersRejectsUnknownProject(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/projects/00000000-0000-0000-0000-000000000000/gitlab-trackers", nil)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "member"}))
	w := httptest.NewRecorder()
	testHandler.ListProjectGitlabTrackers(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}
func createHandlerTestProject(t *testing.T, title string) ProjectResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{"title": title})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.Unmarshal(w.Body.Bytes(), &project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	t.Cleanup(func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})
	return project
}
