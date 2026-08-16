package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// seedGitlabIssue creates a bare-minimum gitlab-sourced issue row + optional
// link, returning the local UUID string. Caller cleans up via t.Cleanup.
func seedGitlabIssue(t *testing.T, projectID, trackerID string, linked bool) string {
	t.Helper()
	var issueID pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO issue(workspace_id, title, description, status, priority, creator_type, creator_id, project_id, number, source_type, tracker_connection_id, sync_state, sync_revision, synced_revision)
		 VALUES ($1,'seed','desc','todo','none','member',$2,$3,
		         (SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id=$1),
		         'gitlab',$4,'synced',1,1)
		 RETURNING id`,
		parseUUID(testWorkspaceID), parseUUID(testUserID), parseUUID(projectID), parseUUID(trackerID)).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	if linked {
		if _, err := testPool.Exec(context.Background(),
			`INSERT INTO gitlab_issue_link(issue_id, tracker_connection_id, remote_issue_id, remote_iid, remote_web_url, remote_state, remote_updated_at)
			 VALUES ($1,$2,999,42,'https://gitlab.example.com/x/42','opened', now())`,
			issueID, parseUUID(trackerID)); err != nil {
			t.Fatalf("seed link: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	return uuidToString(issueID)
}

func loadIssueSync(t *testing.T, issueID string) (string, string, int64, int64) {
	t.Helper()
	var sourceType, syncState string
	var rev, synced int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT source_type, sync_state, sync_revision, synced_revision FROM issue WHERE id=$1`,
		parseUUID(issueID)).Scan(&sourceType, &syncState, &rev, &synced); err != nil {
		t.Fatalf("load issue: %v", err)
	}
	return sourceType, syncState, rev, synced
}

type outboxSummary struct {
	operation       string
	status          string
	desiredRevision int64
}

func loadOutboxForIssue(t *testing.T, issueID string) []outboxSummary {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT operation, status, COALESCE(desired_revision,0) FROM tracker_sync_outbox WHERE issue_id=$1 ORDER BY created_at`,
		parseUUID(issueID))
	if err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	defer rows.Close()
	var out []outboxSummary
	for rows.Next() {
		var summary outboxSummary
		if err := rows.Scan(&summary.operation, &summary.status, &summary.desiredRevision); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		out = append(out, summary)
	}
	return out
}

// TestUpdateIssueGitlab_EnqueuesOutbox: editing a gitlab issue's title
// bumps sync_revision, flips sync_state to 'pending', and lands one
// update_issue outbox row carrying only the changed field.
func TestUpdateIssueGitlab_EnqueuesOutbox(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "gitlab-update-op")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, true)

	title := "renamed"
	req := newRequest("PUT", "/api/issues/"+issueID, UpdateIssueRequest{Title: &title})
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}

	_, syncState, rev, synced := loadIssueSync(t, issueID)
	if syncState != "pending" || rev != 2 || synced != 1 {
		t.Fatalf("sync_state=%q rev=%d synced=%d, want pending/2/1", syncState, rev, synced)
	}
	outbox := loadOutboxForIssue(t, issueID)
	if len(outbox) != 1 || outbox[0].operation != "update_issue" || outbox[0].status != "pending" || outbox[0].desiredRevision != 2 {
		t.Fatalf("outbox = %+v, want 1 pending update_issue @ rev 2", outbox)
	}
}

// TestUpdateIssueLocal_SkipsOutbox: golden check — the local fast path
// stays untouched. Editing a local issue must not create any outbox row.
func TestUpdateIssueLocal_SkipsOutbox(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	var localID pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO issue(workspace_id, title, description, status, priority, creator_type, creator_id, number)
		 VALUES ($1,'local-plain','','todo','none','member',$2,
		         (SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id=$1))
		 RETURNING id`,
		parseUUID(testWorkspaceID), parseUUID(testUserID)).Scan(&localID); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, localID)
	})

	title := "renamed"
	req := newRequest("PUT", "/api/issues/"+uuidToString(localID), UpdateIssueRequest{Title: &title})
	req = withURLParam(req, "id", uuidToString(localID))
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update local: %d %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM tracker_sync_outbox WHERE issue_id=$1`, localID).Scan(&count); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if count != 0 {
		t.Fatalf("local update created %d outbox rows, want 0", count)
	}
}

// TestDeleteIssueGitlab_LinkedGoesPendingDelete: linked mirror → local
// row stays around in pending_delete and a delete_issue outbox row is
// queued. HTTP status is 202, not 204 (deletion is async).
func TestDeleteIssueGitlab_LinkedGoesPendingDelete(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "gitlab-delete-linked")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, true)

	req := newRequest("DELETE", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.DeleteIssue(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("delete linked: %d %s", w.Code, w.Body.String())
	}
	_, syncState, rev, _ := loadIssueSync(t, issueID)
	if syncState != "pending_delete" {
		t.Fatalf("sync_state=%q, want pending_delete", syncState)
	}
	outbox := loadOutboxForIssue(t, issueID)
	if len(outbox) != 1 || outbox[0].operation != "delete_issue" || outbox[0].desiredRevision != rev {
		t.Fatalf("outbox = %+v, want delete_issue @ rev %d", outbox, rev)
	}
}

// TestDeleteIssueGitlab_UnlinkedDeletesLocally: no link row means the
// remote create never succeeded — local delete + outbox cancel is
// enough, no delete_issue outbox row.
func TestDeleteIssueGitlab_UnlinkedDeletesLocally(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "gitlab-delete-unlinked")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, false)
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO tracker_sync_outbox(workspace_id, tracker_connection_id, issue_id, operation, payload, idempotency_key, desired_revision, status)
		 VALUES ($1,$2,$3,'create_issue','{}',gen_random_uuid(),1,'pending')`,
		parseUUID(testWorkspaceID), parseUUID(trackerID), parseUUID(issueID)); err != nil {
		t.Fatalf("seed create outbox: %v", err)
	}

	req := newRequest("DELETE", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.DeleteIssue(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete unlinked: %d %s", w.Code, w.Body.String())
	}
	var exists bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM issue WHERE id=$1)`, parseUUID(issueID)).Scan(&exists); err != nil {
		t.Fatalf("existence: %v", err)
	}
	if exists {
		t.Fatalf("local row still present after unlinked delete")
	}
}

// TestAttachDetachLabel_EnqueuesSetLabels: attaching then detaching a
// gitlab label on a gitlab issue yields one compressed set_labels row
// with the final desired set.
func TestAttachDetachLabel_EnqueuesSetLabels(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	project := projectForCreateTracker(t, "gitlab-labels")
	installGitlabCreateStub(t, staticGitlabProjectHandler(t))
	trackerID := createTrackerHelper(t, project.ID)
	issueID := seedGitlabIssue(t, project.ID, trackerID, true)

	labelIDs := make([]pgtype.UUID, 2)
	names := []string{"bug", "feature"}
	for i, name := range names {
		if err := testPool.QueryRow(context.Background(),
			`INSERT INTO issue_label(workspace_id, name, color, source_type, gitlab_tracker_connection_id, gitlab_label_id)
			 VALUES ($1,$2,'#000','gitlab',$3,$4) RETURNING id`,
			parseUUID(testWorkspaceID), name, parseUUID(trackerID), 1000+i).Scan(&labelIDs[i]); err != nil {
			t.Fatalf("seed label %s: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for _, id := range labelIDs {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_label WHERE id=$1`, id)
		}
	})

	attach := func(labelID pgtype.UUID) {
		req := newRequest("POST", "/api/issues/"+issueID+"/labels", AttachLabelRequest{LabelID: uuidToString(labelID)})
		req = withURLParam(req, "id", issueID)
		w := httptest.NewRecorder()
		testHandler.AttachLabel(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("attach %s: %d %s", uuidToString(labelID), w.Code, w.Body.String())
		}
	}
	attach(labelIDs[0])
	attach(labelIDs[1])

	req := newRequest("DELETE", fmt.Sprintf("/api/issues/%s/labels/%s", issueID, uuidToString(labelIDs[0])), nil)
	req = withURLParams(req, "id", issueID, "labelId", uuidToString(labelIDs[0]))
	w := httptest.NewRecorder()
	testHandler.DetachLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("detach: %d %s", w.Code, w.Body.String())
	}

	outbox := loadOutboxForIssue(t, issueID)
	var pending []outboxSummary
	for _, row := range outbox {
		if row.status == "pending" && row.operation == "set_labels" {
			pending = append(pending, row)
		}
	}
	if len(pending) != 1 {
		t.Fatalf("pending set_labels = %d rows, want 1: %+v", len(pending), outbox)
	}
	var payload struct {
		Labels []string `json:"labels"`
	}
	var raw []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT payload FROM tracker_sync_outbox WHERE issue_id=$1 AND status='pending' AND operation='set_labels' ORDER BY desired_revision DESC LIMIT 1`,
		parseUUID(issueID)).Scan(&raw); err != nil {
		t.Fatalf("load payload: %v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Labels) != 1 || payload.Labels[0] != "feature" {
		t.Fatalf("payload labels = %v, want [feature]", payload.Labels)
	}
}
