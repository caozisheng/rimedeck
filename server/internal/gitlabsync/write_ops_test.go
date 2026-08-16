package gitlabsync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// writeOpsWorker wires a worker with in-memory importer hooks that
// record what the canonical apply / create-link path was called with.
// Real SQL isn't exercised here — Task 5's importer helper tests cover
// the ApplyCanonicalIssue revision-guard invariants directly.
func writeOpsWorker(t *testing.T, fq *fakeQueries, upstreamURL string, seenCreate *db.Issue, seenApply *gitlabtracker.Issue) *Worker {
	t.Helper()
	cipher := newCipher(t)
	tracker := newTracker(t, cipher, upstreamURL)
	fq.tracker = tracker
	w := testWorker(fq, cipher)
	w.CreateIssueImporter = func(_ context.Context, _ db.GitlabTrackerConnection, issue db.Issue, remote gitlabtracker.Issue) error {
		*seenCreate = issue
		*seenApply = remote
		return nil
	}
	w.CanonicalApplier = func(_ context.Context, _ db.GitlabTrackerConnection, _ pgtype.UUID, remote gitlabtracker.Issue) error {
		*seenApply = remote
		return nil
	}
	return w
}

// TestTick_CreateIssueOp: the worker POSTs the local title+description
// to GitLab and hands the returned issue to the create importer hook.
func TestTick_CreateIssueOp(t *testing.T) {
	var seenBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":901,"iid":7,"state":"opened","title":"local","description":"body","web_url":"https://x/7","updated_at":"2026-08-16T00:00:00Z"}`))
	}))
	defer upstream.Close()

	issueID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	issue := db.Issue{ID: issueID, Title: "local", Description: pgtype.Text{String: "body", Valid: true}}
	row := newOutboxRow("create_issue")
	row.IssueID = issueID

	fq := &fakeQueries{
		claim:  []db.TrackerSyncOutbox{row},
		issues: map[string]db.Issue{string(issueID.Bytes[:]): issue},
	}
	var gotCreate db.Issue
	var gotRemote gitlabtracker.Issue
	res, err := writeOpsWorker(t, fq, upstream.URL, &gotCreate, &gotRemote).Tick(context.Background())
	if err != nil || res.Success != 1 || len(fq.success) != 1 {
		t.Fatalf("res=%+v err=%v success=%d", res, err, len(fq.success))
	}
	if seenBody["title"] != "local" || seenBody["description"] != "body" {
		t.Fatalf("POST body = %+v, want title/description", seenBody)
	}
	if gotCreate.ID != issueID || gotRemote.IID != 7 {
		t.Fatalf("importer got issue=%+v remote=%+v", gotCreate, gotRemote)
	}
}

// TestTick_UpdateIssueOp: worker PUTs the payload fields and forwards
// the canonical response to the applier hook.
func TestTick_UpdateIssueOp(t *testing.T) {
	var seenBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v4/projects/42/issues/7" {
			t.Errorf("expected PUT /api/v4/projects/42/issues/7, got %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":901,"iid":7,"state":"opened","title":"renamed","description":"","web_url":"https://x/7","updated_at":"2026-08-16T00:01:00Z"}`))
	}))
	defer upstream.Close()

	issueID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	payload := map[string]any{"title": "renamed"}
	payloadBytes, _ := json.Marshal(payload)
	row := newOutboxRow("update_issue")
	row.IssueID = issueID
	row.Payload = payloadBytes

	fq := &fakeQueries{
		claim: []db.TrackerSyncOutbox{row},
		links: map[string]db.GitlabIssueLink{string(issueID.Bytes[:]): {IssueID: issueID, RemoteIid: 7}},
	}
	var gotCreate db.Issue
	var gotRemote gitlabtracker.Issue
	res, err := writeOpsWorker(t, fq, upstream.URL, &gotCreate, &gotRemote).Tick(context.Background())
	if err != nil || res.Success != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if seenBody["title"] != "renamed" {
		t.Fatalf("PUT body = %+v, want title=renamed", seenBody)
	}
	if gotRemote.Title != "renamed" {
		t.Fatalf("applier remote = %+v, want title=renamed", gotRemote)
	}
}

// TestTick_SetLabelsOp: worker PUTs the desired labels list and the
// canonical response feeds the applier hook.
func TestTick_SetLabelsOp(t *testing.T) {
	var seenLabels []any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		_ = json.Unmarshal(body, &raw)
		if l, ok := raw["labels"].([]any); ok {
			seenLabels = l
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":901,"iid":7,"state":"opened","title":"x","labels":["bug"],"updated_at":"2026-08-16T00:00:00Z"}`))
	}))
	defer upstream.Close()

	issueID := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	payloadBytes, _ := json.Marshal(map[string]any{"labels": []string{"bug"}})
	row := newOutboxRow("set_labels")
	row.IssueID = issueID
	row.Payload = payloadBytes

	fq := &fakeQueries{
		claim: []db.TrackerSyncOutbox{row},
		links: map[string]db.GitlabIssueLink{string(issueID.Bytes[:]): {IssueID: issueID, RemoteIid: 7}},
	}
	var gotCreate db.Issue
	var gotRemote gitlabtracker.Issue
	res, err := writeOpsWorker(t, fq, upstream.URL, &gotCreate, &gotRemote).Tick(context.Background())
	if err != nil || res.Success != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(seenLabels) != 1 || seenLabels[0] != "bug" {
		t.Fatalf("labels sent = %v, want [bug]", seenLabels)
	}
	if len(gotRemote.Labels) != 1 || gotRemote.Labels[0] != "bug" {
		t.Fatalf("applier remote labels = %v", gotRemote.Labels)
	}
}

// TestTick_DeleteIssueOp: worker DELETEs the remote issue, cancels
// follow-up outbox rows on that issue, and hard-deletes the local mirror.
func TestTick_DeleteIssueOp(t *testing.T) {
	var deleteHit bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteHit = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected method %s", r.Method)
	}))
	defer upstream.Close()

	issueID := pgtype.UUID{Bytes: [16]byte{5}, Valid: true}
	wsID := pgtype.UUID{Bytes: [16]byte{6}, Valid: true}
	row := newOutboxRow("delete_issue")
	row.IssueID = issueID

	fq := &fakeQueries{
		claim:  []db.TrackerSyncOutbox{row},
		issues: map[string]db.Issue{string(issueID.Bytes[:]): {ID: issueID, WorkspaceID: wsID}},
		links:  map[string]db.GitlabIssueLink{string(issueID.Bytes[:]): {IssueID: issueID, RemoteIid: 7}},
	}
	var gotCreate db.Issue
	var gotRemote gitlabtracker.Issue
	res, err := writeOpsWorker(t, fq, upstream.URL, &gotCreate, &gotRemote).Tick(context.Background())
	if err != nil || res.Success != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !deleteHit {
		t.Fatalf("expected DELETE to remote issue")
	}
	if len(fq.canceled) != 1 || string(fq.canceled[0].Bytes[:]) != string(issueID.Bytes[:]) {
		t.Fatalf("canceled outbox = %+v, want [issue]", fq.canceled)
	}
	if len(fq.deleted) != 1 || string(fq.deleted[0].Bytes[:]) != string(issueID.Bytes[:]) {
		t.Fatalf("deleted issue = %+v, want [issue]", fq.deleted)
	}
}

// TestTick_UpdateAuthErrorTerminates: 401 on a write op terminates the
// row as auth_revoked, same policy as read ops (design §11.6).
func TestTick_UpdateAuthErrorTerminates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	issueID := pgtype.UUID{Bytes: [16]byte{7}, Valid: true}
	row := newOutboxRow("update_issue")
	row.IssueID = issueID
	row.Payload, _ = json.Marshal(map[string]any{"title": "x"})

	fq := &fakeQueries{
		claim: []db.TrackerSyncOutbox{row},
		links: map[string]db.GitlabIssueLink{string(issueID.Bytes[:]): {IssueID: issueID, RemoteIid: 7}},
	}
	var seenCreate db.Issue
	var seenRemote gitlabtracker.Issue
	res, err := writeOpsWorker(t, fq, upstream.URL, &seenCreate, &seenRemote).Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if res.Failed != 1 {
		t.Fatalf("res=%+v want 1 failed", res)
	}
	if len(fq.failures) != 1 || fq.failures[0].LastErrorCode.String != "auth_revoked" {
		t.Fatalf("failures = %+v, want auth_revoked", fq.failures)
	}
}

var _ = errors.New // pin errors import
