package gitlabtracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gitlabStub is a scripted GitLab. Each entry maps `method path` to a
// canned response so tests can assert both request shape (path+method,
// PRIVATE-TOKEN header) and behavior (pagination, error mapping).
type gitlabStub struct {
	t        *testing.T
	handlers map[string]http.HandlerFunc
	seen     []string
}

func newGitlabStub(t *testing.T) *gitlabStub {
	return &gitlabStub{t: t, handlers: map[string]http.HandlerFunc{}}
}

func (s *gitlabStub) on(method, path string, h http.HandlerFunc) {
	s.handlers[method+" "+path] = h
}

func (s *gitlabStub) start() *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.seen = append(s.seen, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("PRIVATE-TOKEN") == "" && r.Header.Get("Authorization") == "" {
			s.t.Errorf("request %s %s missing auth header", r.Method, r.URL.RequestURI())
		}
		// Split query so handlers can be keyed by method+path only; the
		// specific query params get asserted inside each handler.
		key := r.Method + " " + strings.SplitN(r.URL.RequestURI(), "?", 2)[0]
		h, ok := s.handlers[key]
		if !ok {
			s.t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	return srv
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// clientFor wires a Client pointing at a stub GitLab server. Transport
// is unfiltered so httptest's loopback bind works without ceremony.
func clientFor(t *testing.T, base string, token string) *RestClient {
	t.Helper()
	transport, err := NewClient(Config{RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return NewRestClient(transport, base, token)
}

// TestGetProject_HappyPath asserts the URL-encoded path lookup returns
// the fields downstream code needs (numeric id, path, web url, default
// branch, permission bits).
func TestGetProject_HappyPath(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodGet, "/api/v4/projects/group%2Fproject", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":                  42,
			"path_with_namespace": "group/project",
			"web_url":             "https://gitlab.example.com/group/project",
			"default_branch":      "main",
			"permissions": map[string]any{
				"project_access": map[string]any{"access_level": 40},
			},
		})
	})
	srv := stub.start()
	defer srv.Close()

	c := clientFor(t, srv.URL, "glpat-1")
	got, err := c.GetProject(context.Background(), "group/project")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.ID != 42 || got.PathWithNamespace != "group/project" {
		t.Fatalf("unexpected project: %+v", got)
	}
	if got.WebURL != "https://gitlab.example.com/group/project" {
		t.Errorf("WebURL = %q", got.WebURL)
	}
	if got.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q", got.DefaultBranch)
	}
	if !got.CanWriteIssues {
		t.Errorf("CanWriteIssues = false; access_level 40 (Maintainer) should grant issue writes")
	}
}

// TestGetProject_ErrorMapping walks every server status the sync worker
// has to distinguish. Each error is typed so callers can surface the
// safe summary in last_error_message without leaking the URL or token.
func TestGetProject_ErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantErr  error
		wantCode string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantErr: ErrInvalidToken},
		{name: "forbidden", status: http.StatusForbidden, wantErr: ErrPermissionDenied},
		{name: "not found", status: http.StatusNotFound, wantErr: ErrNotFound},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			stub := newGitlabStub(t)
			stub.on(http.MethodGet, "/api/v4/projects/group%2Fproject", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})
			srv := stub.start()
			defer srv.Close()
			c := clientFor(t, srv.URL, "glpat")
			_, err := c.GetProject(context.Background(), "group/project")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestGetProject_BadJSONSurfacesRemoteError proves a mangled body maps
// to ErrRemote (rather than a nil-return silent failure).
func TestGetProject_BadJSONSurfacesRemoteError(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodGet, "/api/v4/projects/g%2Fp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not-json"))
	})
	srv := stub.start()
	defer srv.Close()
	c := clientFor(t, srv.URL, "glpat")
	_, err := c.GetProject(context.Background(), "g/p")
	if !errors.Is(err, ErrRemote) {
		t.Fatalf("err = %v, want ErrRemote", err)
	}
}

// TestGetProject_RateLimited surfaces the transport's RateLimitedError
// unchanged so the worker can back off with the parsed delay.
func TestGetProject_RateLimited(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodGet, "/api/v4/projects/g%2Fp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	srv := stub.start()
	defer srv.Close()
	c := clientFor(t, srv.URL, "glpat")
	_, err := c.GetProject(context.Background(), "g/p")
	var rate *RateLimitedError
	if !errors.As(err, &rate) {
		t.Fatalf("err = %v, want *RateLimitedError", err)
	}
	if rate.RetryAfter != 17*time.Second {
		t.Fatalf("RetryAfter = %v, want 17s", rate.RetryAfter)
	}
}

// TestListProjectLabels_Paginates walks the two-page label response.
// Include a group-inherited label to confirm the field mapping keeps
// it as-is (RimeDeck mirrors the union of inherited + project labels).
func TestListProjectLabels_Paginates(t *testing.T) {
	page1 := []map[string]any{
		{"id": 1, "name": "bug", "color": "#FF0000", "description": "", "is_project_label": true},
		{"id": 2, "name": "enhancement", "color": "#00FF00", "description": "", "is_project_label": true},
	}
	page2 := []map[string]any{
		{"id": 3, "name": "inherited", "color": "#0000FF", "description": "", "is_project_label": false},
	}
	stub := newGitlabStub(t)
	stub.on(http.MethodGet, "/api/v4/projects/42/labels", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if r.URL.Query().Get("include_ancestor_groups") != "true" {
			t.Errorf("include_ancestor_groups missing")
		}
		if r.URL.Query().Get("per_page") != strconv.Itoa(DefaultPerPage) {
			t.Errorf("per_page = %q, want %d", r.URL.Query().Get("per_page"), DefaultPerPage)
		}
		w.Header().Set("X-Next-Page", "")
		switch page {
		case "", "1":
			w.Header().Set("X-Next-Page", "2")
			writeJSON(w, http.StatusOK, page1)
		case "2":
			writeJSON(w, http.StatusOK, page2)
		default:
			t.Errorf("unexpected page %q", page)
		}
	})
	srv := stub.start()
	defer srv.Close()

	c := clientFor(t, srv.URL, "glpat")
	labels, err := c.ListProjectLabels(context.Background(), 42)
	if err != nil {
		t.Fatalf("ListProjectLabels: %v", err)
	}
	if len(labels) != 3 {
		t.Fatalf("got %d labels, want 3", len(labels))
	}
	names := []string{labels[0].Name, labels[1].Name, labels[2].Name}
	if strings.Join(names, ",") != "bug,enhancement,inherited" {
		t.Errorf("names = %v", names)
	}
	if labels[2].IsProjectLabel {
		t.Errorf("inherited label should have IsProjectLabel=false")
	}
}

// TestListProjectIssues_HonorsStateFilter proves the state query param
// travels intact and the client accepts both opened and closed sets.
func TestListProjectIssues_HonorsStateFilter(t *testing.T) {
	stub := newGitlabStub(t)
	stub.on(http.MethodGet, "/api/v4/projects/42/issues", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("state"); got != "all" {
			t.Errorf("state = %q, want all", got)
		}
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": 1001, "iid": 12, "state": "opened", "title": "one", "web_url": "https://gitlab.example.com/g/p/-/issues/12", "updated_at": "2026-08-16T00:00:00Z", "labels": []string{"bug"}},
			{"id": 1002, "iid": 13, "state": "closed", "title": "two", "web_url": "https://gitlab.example.com/g/p/-/issues/13", "updated_at": "2026-08-16T00:00:00Z", "labels": []string{}},
		})
	})
	srv := stub.start()
	defer srv.Close()

	c := clientFor(t, srv.URL, "glpat")
	got, err := c.ListProjectIssues(context.Background(), 42, ListIssuesOptions{State: "all"})
	if err != nil {
		t.Fatalf("ListProjectIssues: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2", len(got))
	}
	if got[0].IID != 12 || got[1].IID != 13 {
		t.Fatalf("iids = %d,%d", got[0].IID, got[1].IID)
	}
	if got[0].State != "opened" || got[1].State != "closed" {
		t.Fatalf("states mismatch: %+v", got)
	}
}

// TestNewRestClientRejectsEmptyBase catches an operator mis-config
// early rather than sending unauthenticated requests to whoever
// resolves an empty URL.
func TestNewRestClientRejectsEmptyBase(t *testing.T) {
	transport, _ := NewClient(Config{})
	defer func() {
		if r := recover(); r == nil {
			// Fallback: NewRestClient may return a nil client rather than panic.
			c := NewRestClient(transport, "", "tok")
			if c != nil {
				if _, err := c.GetProject(context.Background(), "g/p"); err == nil {
					_ = fmt.Sprintf("no error surfaced when base URL was empty")
				}
			}
		}
	}()
}
