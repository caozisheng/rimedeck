package gitlabsync

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeQueries struct {
	claim                []db.TrackerSyncOutbox
	claimLimit           int32
	tracker              db.GitlabTrackerConnection
	success              []pgtype.UUID
	retries              []db.MarkTrackerOutboxRetryParams
	failures             []db.MarkTrackerOutboxFailedParams
	touched              []pgtype.UUID
	getErr               error
	issues               map[string]db.Issue
	labels               map[string][]db.IssueLabel
	links                map[string]db.GitlabIssueLink
	remoteLinks          map[int32]db.GitlabIssueLink
	comments             map[string]db.Comment
	noteLinks            map[string]db.GitlabNoteLink
	deleted              []pgtype.UUID
	canceled             []pgtype.UUID
	linkIIDs             []db.ListGitlabIssueLinkIIDsRow
	fullReconcileTouched []pgtype.UUID
	degraded             []db.MarkTrackerDegradedParams
	active               []pgtype.UUID
}

func (f *fakeQueries) ClaimReadyTrackerOutbox(_ context.Context, limit int32) ([]db.TrackerSyncOutbox, error) {
	f.claimLimit = limit
	rows := f.claim
	f.claim = nil
	return rows, nil
}

func TestTickClaimsOneRowAtATime(t *testing.T) {
	fq := &fakeQueries{}
	worker := &Worker{Queries: fq}
	if _, err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fq.claimLimit != 1 {
		t.Fatalf("claim limit=%d, want 1", fq.claimLimit)
	}
}
func (f *fakeQueries) GetGitlabTrackerConnection(context.Context, pgtype.UUID) (db.GitlabTrackerConnection, error) {
	return f.tracker, f.getErr
}
func (f *fakeQueries) MarkTrackerOutboxSucceeded(_ context.Context, id pgtype.UUID) error {
	f.success = append(f.success, id)
	return nil
}
func (f *fakeQueries) MarkTrackerOutboxRetry(_ context.Context, arg db.MarkTrackerOutboxRetryParams) error {
	f.retries = append(f.retries, arg)
	return nil
}
func (f *fakeQueries) MarkTrackerOutboxFailed(_ context.Context, arg db.MarkTrackerOutboxFailedParams) error {
	f.failures = append(f.failures, arg)
	return nil
}
func (f *fakeQueries) GetIssue(_ context.Context, id pgtype.UUID) (db.Issue, error) {
	if issue, ok := f.issues[string(id.Bytes[:])]; ok {
		return issue, nil
	}
	return db.Issue{}, errors.New("issue not found")
}
func (f *fakeQueries) GetComment(_ context.Context, id pgtype.UUID) (db.Comment, error) {
	if comment, ok := f.comments[string(id.Bytes[:])]; ok {
		return comment, nil
	}
	return db.Comment{}, errors.New("comment not found")
}
func (f *fakeQueries) ListAllLabelsByIssue(_ context.Context, arg db.ListAllLabelsByIssueParams) ([]db.IssueLabel, error) {
	return f.labels[string(arg.IssueID.Bytes[:])], nil
}
func (f *fakeQueries) GetGitlabIssueLinkByIssueID(_ context.Context, id pgtype.UUID) (db.GitlabIssueLink, error) {
	if link, ok := f.links[string(id.Bytes[:])]; ok {
		return link, nil
	}
	return db.GitlabIssueLink{}, errors.New("link not found")
}
func (f *fakeQueries) GetGitlabIssueLinkByRemoteIID(_ context.Context, arg db.GetGitlabIssueLinkByRemoteIIDParams) (db.GitlabIssueLink, error) {
	if link, ok := f.remoteLinks[arg.RemoteIid]; ok {
		return link, nil
	}
	return db.GitlabIssueLink{}, errors.New("link not found")
}
func (f *fakeQueries) GetGitlabNoteLinkByCommentID(_ context.Context, id pgtype.UUID) (db.GitlabNoteLink, error) {
	if link, ok := f.noteLinks[string(id.Bytes[:])]; ok {
		return link, nil
	}
	return db.GitlabNoteLink{}, errors.New("note link not found")
}
func (f *fakeQueries) UpsertGitlabNoteLink(_ context.Context, arg db.UpsertGitlabNoteLinkParams) (db.GitlabNoteLink, error) {
	link := db.GitlabNoteLink{CommentID: arg.CommentID, IssueID: arg.IssueID, TrackerConnectionID: arg.TrackerConnectionID, RemoteIssueIid: arg.RemoteIssueIid, RemoteNoteID: arg.RemoteNoteID, RemoteOwned: arg.RemoteOwned}
	if f.noteLinks == nil {
		f.noteLinks = map[string]db.GitlabNoteLink{}
	}
	f.noteLinks[string(arg.CommentID.Bytes[:])] = link
	return link, nil
}
func (f *fakeQueries) DeleteGitlabNoteLinkByCommentID(_ context.Context, id pgtype.UUID) error {
	delete(f.noteLinks, string(id.Bytes[:]))
	return nil
}
func (f *fakeQueries) CancelTrackerOutboxByIssue(_ context.Context, id pgtype.UUID) error {
	f.canceled = append(f.canceled, id)
	return nil
}
func (f *fakeQueries) DeleteIssue(_ context.Context, arg db.DeleteIssueParams) error {
	f.deleted = append(f.deleted, arg.ID)
	return nil
}
func (f *fakeQueries) ListGitlabIssueLinkIIDs(_ context.Context, _ pgtype.UUID) ([]db.ListGitlabIssueLinkIIDsRow, error) {
	return f.linkIIDs, nil
}
func (f *fakeQueries) MarkTrackerDegraded(_ context.Context, arg db.MarkTrackerDegradedParams) error {
	f.degraded = append(f.degraded, arg)
	return nil
}
func (f *fakeQueries) MarkTrackerActive(_ context.Context, id pgtype.UUID) error {
	f.active = append(f.active, id)
	return nil
}
func (f *fakeQueries) TouchLastFullReconcile(_ context.Context, id pgtype.UUID) error {
	f.fullReconcileTouched = append(f.fullReconcileTouched, id)
	return nil
}
func (f *fakeQueries) TouchTrackerLastPull(_ context.Context, id pgtype.UUID) error {
	f.touched = append(f.touched, id)
	return nil
}

func newCipher(t *testing.T) *gitlabtracker.Cipher {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	c, err := gitlabtracker.NewCipher(map[int16]string{1: base64.StdEncoding.EncodeToString(raw)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func newTracker(t *testing.T, cipher *gitlabtracker.Cipher, baseURL string) db.GitlabTrackerConnection {
	t.Helper()
	ct, err := cipher.Encrypt([]byte("glpat-worker"))
	if err != nil {
		t.Fatal(err)
	}
	return db.GitlabTrackerConnection{
		ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, InstanceUrl: baseURL,
		RemoteProjectID: 42, TokenCiphertext: ct, TokenKeyVersion: cipher.LatestVersion(), State: "active",
	}
}
func newOutboxRow(op string) db.TrackerSyncOutbox {
	return db.TrackerSyncOutbox{
		ID: pgtype.UUID{Bytes: [16]byte{9}, Valid: true}, TrackerConnectionID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Operation: op, Payload: []byte("{}"), DesiredRevision: pgtype.Int8{Int64: 1, Valid: true},
	}
}
func testWorker(fq *fakeQueries, cipher *gitlabtracker.Cipher) *Worker {
	return &Worker{
		Queries: fq, Cipher: cipher,
		LabelImporter: func(context.Context, db.GitlabTrackerConnection, []gitlabtracker.Label) error { return nil },
		IssueImporter: func(context.Context, db.GitlabTrackerConnection, []gitlabtracker.Issue) error { return nil },
		ClientFactory: func(instanceURL, token string) (*gitlabtracker.RestClient, error) {
			transport, err := gitlabtracker.NewClient(gitlabtracker.Config{})
			if err != nil {
				return nil, err
			}
			return gitlabtracker.NewRestClient(transport, instanceURL, token), nil
		},
	}
}

func TestTick_PullLabelsHappyPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":1,"name":"bug","color":"#ff0000"}]`))
	}))
	defer upstream.Close()
	cipher := newCipher(t)
	fq := &fakeQueries{claim: []db.TrackerSyncOutbox{newOutboxRow("pull_labels")}, tracker: newTracker(t, cipher, upstream.URL)}
	res, err := testWorker(fq, cipher).Tick(context.Background())
	if err != nil || res.Success != 1 || len(fq.success) != 1 || len(fq.touched) != 1 {
		t.Fatalf("res=%+v err=%v success=%d touched=%d", res, err, len(fq.success), len(fq.touched))
	}
}

func TestTick_AuthErrorTerminates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer upstream.Close()
	cipher := newCipher(t)
	fq := &fakeQueries{claim: []db.TrackerSyncOutbox{newOutboxRow("pull_labels")}, tracker: newTracker(t, cipher, upstream.URL)}
	res, err := testWorker(fq, cipher).Tick(context.Background())
	if err != nil || res.Failed != 1 || len(fq.failures) != 1 || fq.failures[0].LastErrorCode.String != "auth_revoked" {
		t.Fatalf("res=%+v err=%v failures=%+v", res, err, fq.failures)
	}
}

func TestTick_RateLimitBacksOff(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, `{"message":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer upstream.Close()
	cipher := newCipher(t)
	fq := &fakeQueries{claim: []db.TrackerSyncOutbox{newOutboxRow("pull_labels")}, tracker: newTracker(t, cipher, upstream.URL)}
	res, err := testWorker(fq, cipher).Tick(context.Background())
	if err != nil || res.Retried != 1 || len(fq.retries) != 1 || fq.retries[0].LastErrorCode.String != "transient" {
		t.Fatalf("res=%+v err=%v retries=%+v", res, err, fq.retries)
	}
}

func TestTick_DisabledTrackerTerminates(t *testing.T) {
	cipher := newCipher(t)
	tracker := newTracker(t, cipher, "http://ignored")
	tracker.State = "disabled"
	fq := &fakeQueries{claim: []db.TrackerSyncOutbox{newOutboxRow("pull_labels")}, tracker: tracker}
	worker := testWorker(fq, cipher)
	worker.ClientFactory = func(string, string) (*gitlabtracker.RestClient, error) { return nil, errors.New("must not call") }
	res, err := worker.Tick(context.Background())
	if err != nil || res.Failed != 1 || len(fq.failures) != 1 || fq.failures[0].LastErrorCode.String != "tracker_disabled" {
		t.Fatalf("res=%+v err=%v failures=%+v", res, err, fq.failures)
	}
}

func TestComputeBackoff(t *testing.T) {
	cases := []struct {
		attempts int
		want     time.Duration
	}{{0, time.Second}, {1, 2 * time.Second}, {4, 16 * time.Second}, {9, 300 * time.Second}}
	for _, tc := range cases {
		if got := computeBackoff(tc.attempts); got != tc.want {
			t.Errorf("computeBackoff(%d)=%s want %s", tc.attempts, got, tc.want)
		}
	}
}
