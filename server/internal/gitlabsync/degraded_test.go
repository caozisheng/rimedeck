package gitlabsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestTick_AuthFailureMarksDegraded asserts a persistent 401 flips the
// tracker connection to degraded so the UI can surface a banner.
func TestTick_AuthFailureMarksDegraded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	cipher := newCipher(t)
	fq := &fakeQueries{
		claim:   []db.TrackerSyncOutbox{newOutboxRow("pull_labels")},
		tracker: newTracker(t, cipher, upstream.URL),
	}
	worker := testWorker(fq, cipher)
	res, err := worker.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if res.Failed != 1 {
		t.Fatalf("res=%+v want 1 failed", res)
	}
	if len(fq.degraded) != 1 || fq.degraded[0].LastErrorCode.String != "auth_revoked" {
		t.Fatalf("degraded=%+v, want one auth_revoked entry", fq.degraded)
	}
}

// TestTick_SuccessMarksActive asserts a happy tick clears any prior
// degradation. Uses the pull_labels path so the applier is a no-op.
func TestTick_SuccessMarksActive(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"name":"bug","color":"#ff0000"}]`))
	}))
	defer upstream.Close()

	cipher := newCipher(t)
	fq := &fakeQueries{
		claim:   []db.TrackerSyncOutbox{newOutboxRow("pull_labels")},
		tracker: newTracker(t, cipher, upstream.URL),
	}
	worker := testWorker(fq, cipher)
	res, err := worker.Tick(context.Background())
	if err != nil || res.Success != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(fq.active) != 1 {
		t.Fatalf("active calls = %d, want 1", len(fq.active))
	}
}

// Suppress an accidental import prune.
var _ = gitlabtracker.Config{}
