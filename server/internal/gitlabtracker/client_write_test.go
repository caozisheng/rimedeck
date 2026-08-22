package gitlabtracker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

// TestCreateIssue_HappyPath asserts a POST body carries the fields we
// push and the decoded response echoes what GitLab returns — the worker
// pulls-through from this value, so the mapping must be exact.
func TestCreateIssue_HappyPath(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodPost, "/api/v4/projects/42/issues", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if raw["title"] != "hello" || raw["description"] != "world" || raw["labels"] != "bug" {
			t.Fatalf("body = %+v", raw)
		}
		if raw["start_date"] != "2026-08-17" || raw["due_date"] != "2026-08-20" {
			t.Fatalf("request dates = %+v", raw)
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": 900, "iid": 7, "state": "opened", "title": "hello",
			"description": "world", "web_url": "https://gitlab/x/7", "updated_at": "2026-08-16T00:00:00Z",
			"start_date": "2026-08-17", "due_date": "2026-08-20", "labels": []string{"bug"},
			"author": map[string]any{"name": "alice", "web_url": "https://gitlab/alice"},
		})
	})
	srv := stub.start()
	defer srv.Close()
	client := clientFor(t, srv.URL, "glpat-x")

	issue, err := client.CreateIssue(context.Background(), 42, CreateIssueRequest{Title: "hello", Description: "world", Labels: []string{"bug"}, StartDate: "2026-08-17", DueDate: "2026-08-20"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if issue.ID != 900 || issue.IID != 7 || issue.State != "opened" || issue.Title != "hello" {
		t.Fatalf("issue = %+v", issue)
	}
	if issue.Author.Name != "alice" {
		t.Fatalf("author = %+v", issue.Author)
	}
	if issue.StartDate != "2026-08-17" || issue.DueDate != "2026-08-20" {
		t.Fatalf("response dates = %+v", issue)
	}
}

// TestUpdateIssue_SendsOnlySetFields proves nil fields stay out of the
// wire body — GitLab's PUT is destructive-replace on any provided field,
// so we cannot leak a zero-value description clobber.
func TestUpdateIssue_SendsOnlySetFields(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodPut, "/api/v4/projects/42/issues/7", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, present := raw["description"]; present {
			t.Fatalf("description leaked into PUT body: %s", body)
		}
		if raw["title"] != "renamed" {
			t.Fatalf("title = %v", raw["title"])
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": 1, "iid": 7, "state": "opened", "title": "renamed"})
	})
	srv := stub.start()
	defer srv.Close()
	client := clientFor(t, srv.URL, "glpat-x")

	title := "renamed"
	if _, err := client.UpdateIssue(context.Background(), 42, 7, UpdateIssueRequest{Title: &title}); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
}

func TestUpdateIssue_SendsDateClears(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodPut, "/api/v4/projects/42/issues/7", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatal(err)
		}
		if raw["start_date"] != "" || raw["due_date"] != "" {
			t.Fatalf("date clears = %s", body)
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": 1, "iid": 7, "state": "opened"})
	})
	srv := stub.start()
	defer srv.Close()
	client := clientFor(t, srv.URL, "glpat-x")
	empty := ""
	if _, err := client.UpdateIssue(context.Background(), 42, 7, UpdateIssueRequest{StartDate: &empty, DueDate: &empty}); err != nil {
		t.Fatal(err)
	}
}

// TestCloseIssue_SendsStateEvent locks the close/reopen wire contract
// (`state_event`, per GitLab docs). Same body shape for both directions.
func TestCloseIssue_SendsStateEvent(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodPut, "/api/v4/projects/42/issues/7", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		_ = json.Unmarshal(body, &raw)
		if raw["state_event"] != "close" {
			t.Fatalf("state_event = %v", raw["state_event"])
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": 1, "iid": 7, "state": "closed"})
	})
	srv := stub.start()
	defer srv.Close()
	client := clientFor(t, srv.URL, "glpat-x")

	if _, err := client.CloseIssue(context.Background(), 42, 7); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
}

// TestSetLabels_ReplacesFullSet asserts the request sends GitLab's
// comma-separated label parameter; an empty string clears all labels.
func TestSetLabels_ReplacesFullSet(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodPut, "/api/v4/projects/42/issues/7", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		_ = json.Unmarshal(body, &raw)
		if labels, ok := raw["labels"].(string); !ok || labels != "" {
			t.Fatalf("nil labels should send empty string, got %v", raw["labels"])
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": 1, "iid": 7, "state": "opened", "labels": []string{}})
	})
	srv := stub.start()
	defer srv.Close()
	client := clientFor(t, srv.URL, "glpat-x")

	if _, err := client.SetLabels(context.Background(), 42, 7, nil); err != nil {
		t.Fatalf("SetLabels: %v", err)
	}
}

// TestDeleteIssue_TreatsMissingAsSuccess: 404 is not an error because
// worker logic is "converge remote to gone"; a manual GitLab delete
// racing our worker should still let the outbox row terminate cleanly.
func TestDeleteIssue_TreatsMissingAsSuccess(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodDelete, "/api/v4/projects/42/issues/7", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	})
	srv := stub.start()
	defer srv.Close()
	client := clientFor(t, srv.URL, "glpat-x")

	if err := client.DeleteIssue(context.Background(), 42, 7); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
}

// TestWriteMethods_ErrorMappingMatchesReads covers each REST write path
// against the same sentinels the worker's classifier expects. Kept as a
// table so adding a new op only extends the fixture.
func TestWriteMethods_ErrorMappingMatchesReads(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
		call   func(*RestClient) error
	}{
		{"create_401", http.StatusUnauthorized, ErrInvalidToken, func(c *RestClient) error {
			_, err := c.CreateIssue(context.Background(), 1, CreateIssueRequest{Title: "x"})
			return err
		}},
		{"update_403", http.StatusForbidden, ErrPermissionDenied, func(c *RestClient) error {
			_, err := c.UpdateIssue(context.Background(), 1, 1, UpdateIssueRequest{})
			return err
		}},
		{"delete_500", http.StatusInternalServerError, ErrRemote, func(c *RestClient) error {
			return c.DeleteIssue(context.Background(), 1, 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newGitlabStub(t)
			stub.on(http.MethodPost, "/api/v4/projects/1/issues", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
			stub.on(http.MethodPut, "/api/v4/projects/1/issues/1", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
			stub.on(http.MethodDelete, "/api/v4/projects/1/issues/1", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
			srv := stub.start()
			defer srv.Close()
			client := clientFor(t, srv.URL, "glpat-x")

			err := tc.call(client)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
