package gitlabtracker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
)

func TestListIssueNotesPaginates(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodGet, "/api/v4/projects/42/issues/7/notes", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != strconv.Itoa(DefaultPerPage) {
			t.Fatalf("per_page=%q", r.URL.Query().Get("per_page"))
		}
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("X-Next-Page", "2")
			writeJSON(w, http.StatusOK, []map[string]any{{
				"id": 101, "body": "first", "system": false,
				"created_at": "2026-08-21T01:00:00Z", "updated_at": "2026-08-21T01:00:00Z",
				"author": map[string]any{"id": 9, "name": "Alice", "web_url": "https://gitlab.example/alice"},
			}})
			return
		}
		writeJSON(w, http.StatusOK, []map[string]any{{
			"id": 102, "body": "second", "system": false,
			"created_at": "2026-08-21T02:00:00Z", "updated_at": "2026-08-21T02:00:00Z",
		}})
	})
	srv := stub.start()
	defer srv.Close()

	notes, err := clientFor(t, srv.URL, "token").ListIssueNotes(context.Background(), 42, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].ID != 101 || notes[0].Author.Name != "Alice" || notes[1].Body != "second" {
		t.Fatalf("notes=%+v", notes)
	}
}

func TestIssueNoteCRUD(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodPost, "/api/v4/projects/42/issues/7/notes", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["body"] != "created" {
			t.Fatalf("body=%v", body)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": 11, "body": "created", "created_at": "2026-08-21T01:00:00Z", "updated_at": "2026-08-21T01:00:00Z"})
	})
	stub.on(http.MethodPut, "/api/v4/projects/42/issues/7/notes/11", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]string
		_ = json.Unmarshal(raw, &body)
		if body["body"] != "updated" {
			t.Fatalf("body=%s", raw)
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": 11, "body": "updated", "created_at": "2026-08-21T01:00:00Z", "updated_at": "2026-08-21T02:00:00Z"})
	})
	stub.on(http.MethodDelete, "/api/v4/projects/42/issues/7/notes/11", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	srv := stub.start()
	defer srv.Close()
	client := clientFor(t, srv.URL, "token")

	created, err := client.CreateIssueNote(context.Background(), 42, 7, "created")
	if err != nil || created.ID != 11 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	updated, err := client.UpdateIssueNote(context.Background(), 42, 7, 11, "updated")
	if err != nil || updated.Body != "updated" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if err := client.DeleteIssueNote(context.Background(), 42, 7, 11); err != nil {
		t.Fatal(err)
	}
}
