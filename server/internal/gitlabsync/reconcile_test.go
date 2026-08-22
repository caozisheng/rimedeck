package gitlabsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestTick_FullReconcileDetectsOrphan: local link references remote iid
// 99, but the remote /issues endpoint only returns iid 7 → the orphan
// gets its outbox cancelled and local row deleted; last_full_reconcile
// timestamp is bumped.
func TestTick_FullReconcileDetectsOrphan(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issues") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 700, "iid": 7, "state": "opened", "title": "kept", "updated_at": "2026-08-16T00:00:00Z"},
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/issues/7/notes") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		t.Errorf("unexpected upstream request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	orphanIssueID := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	row := newOutboxRow("full_reconcile")
	fq := &fakeQueries{
		claim: []db.TrackerSyncOutbox{row},
		linkIIDs: []db.ListGitlabIssueLinkIIDsRow{
			{IssueID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, RemoteIid: 7},
			{IssueID: orphanIssueID, RemoteIid: 99},
		},
	}
	var seenCreate db.Issue
	var seenRemote gitlabtracker.Issue
	worker := writeOpsWorker(t, fq, upstream.URL, &seenCreate, &seenRemote)
	// Force the issue-import branch to no-op so the test focuses on
	// the orphan-detection half of full_reconcile.
	worker.IssueImporter = func(_ context.Context, _ db.GitlabTrackerConnection, _ []gitlabtracker.Issue) error { return nil }
	worker.NoteImporter = func(_ context.Context, _ db.GitlabTrackerConnection, _ db.GitlabIssueLink, _ []gitlabtracker.Note) ([]gitlabtracker.ImportedNote, error) {
		return nil, nil
	}
	fq.remoteLinks = map[int32]db.GitlabIssueLink{7: {IssueID: fq.linkIIDs[0].IssueID, RemoteIid: 7}}

	res, err := worker.Tick(context.Background())
	if err != nil || res.Success != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(fq.canceled) != 1 || string(fq.canceled[0].Bytes[:]) != string(orphanIssueID.Bytes[:]) {
		t.Fatalf("canceled orphan outbox = %+v", fq.canceled)
	}
	if len(fq.deleted) != 1 || string(fq.deleted[0].Bytes[:]) != string(orphanIssueID.Bytes[:]) {
		t.Fatalf("deleted orphan issue = %+v", fq.deleted)
	}
	if len(fq.fullReconcileTouched) != 1 {
		t.Fatalf("expected last_full_reconcile_at touch, got %d", len(fq.fullReconcileTouched))
	}
}
