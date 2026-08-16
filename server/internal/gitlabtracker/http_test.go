package gitlabtracker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// dialCase exercises the pre-connect IP guard by driving a synthetic
// resolver instead of a real DNS lookup. Every case is either an accept
// or an explicit reject with a stable code so the caller can map to a
// safe error message without inspecting error strings.
type dialCase struct {
	name       string
	host       string
	resolvedIP string
	allow      []string // self-hosted host allowlist
	wantErr    string   // "" = accept
}

func TestDialGuardVerdict(t *testing.T) {
	cases := []dialCase{
		{
			name:       "public IPv4 accepted",
			host:       "gitlab.com",
			resolvedIP: "172.65.251.78",
		},
		{
			name:       "public IPv6 accepted",
			host:       "gitlab.com",
			resolvedIP: "2606:4700:90:0:f22e:fbec:5bed:a9b9",
		},
		{
			name:       "loopback IPv4 rejected even for allowed host",
			host:       "gitlab.example.com",
			resolvedIP: "127.0.0.1",
			allow:      []string{"gitlab.example.com"},
			wantErr:    "loopback_forbidden",
		},
		{
			name:       "loopback IPv6 rejected",
			host:       "gitlab.example.com",
			resolvedIP: "::1",
			allow:      []string{"gitlab.example.com"},
			wantErr:    "loopback_forbidden",
		},
		{
			name:       "link-local IPv4 rejected",
			host:       "gitlab.example.com",
			resolvedIP: "169.254.169.254",
			allow:      []string{"gitlab.example.com"},
			wantErr:    "link_local_forbidden",
		},
		{
			name:       "link-local IPv6 rejected",
			host:       "gitlab.example.com",
			resolvedIP: "fe80::1",
			allow:      []string{"gitlab.example.com"},
			wantErr:    "link_local_forbidden",
		},
		{
			name:       "RFC1918 rejected without allowlist even when host is allowed",
			host:       "gitlab.example.com",
			resolvedIP: "10.0.0.1",
			allow:      []string{"gitlab.example.com"},
			wantErr:    "private_forbidden",
		},
		{
			name:       "RFC1918 accepted for allowlisted self-hosted install",
			host:       "gitlab.internal",
			resolvedIP: "10.0.0.5",
			allow:      []string{"gitlab.internal", "AllowPrivate:gitlab.internal"},
		},
		{
			name:       "multicast rejected",
			host:       "gitlab.com",
			resolvedIP: "224.0.0.1",
			wantErr:    "multicast_forbidden",
		},
		{
			name:       "unspecified 0.0.0.0 rejected",
			host:       "gitlab.com",
			resolvedIP: "0.0.0.0",
			wantErr:    "unspecified_forbidden",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verdict := checkDialAddress(tc.host, net.ParseIP(tc.resolvedIP), tc.allow)
			if tc.wantErr == "" {
				if verdict != nil {
					t.Fatalf("expected accept, got %q", verdict.Error())
				}
				return
			}
			if verdict == nil {
				t.Fatalf("expected reject %q, got accept", tc.wantErr)
			}
			var perr *DialError
			if !errors.As(verdict, &perr) {
				t.Fatalf("verdict type = %T, want *DialError", verdict)
			}
			if perr.Code != tc.wantErr {
				t.Fatalf("verdict code = %q, want %q", perr.Code, tc.wantErr)
			}
		})
	}
}

// TestRedirectRejectsCrossHost proves the transport refuses to follow a
// 302 that would send the request to a host outside the allowlist —
// including a redirect from an allowed host to a public one.
func TestRedirectRejectsCrossHost(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://gitlab.example.com/redirected", http.StatusFound)
	}))
	defer upstream.Close()

	client := newTestClient(t, nil)
	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/x", nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected cross-host redirect to be rejected")
	}
	if !strings.Contains(err.Error(), "cross-host redirect") {
		t.Fatalf("error %q should mention cross-host redirect", err.Error())
	}
}

// TestBodyCapEnforced proves the body reader stops early when an upstream
// tries to hand us more than DefaultBodyLimit bytes. The tail must be
// truncated, not buffered, so a malicious GitLab can't exhaust memory.
func TestBodyCapEnforced(t *testing.T) {
	oversized := strings.Repeat("A", int(DefaultBodyLimit)+2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(oversized)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(oversized))
	}))
	defer srv.Close()

	client := newTestClient(t, nil)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("expected read error when body exceeds cap")
	}
	if int64(len(body)) != DefaultBodyLimit {
		t.Fatalf("read %d bytes, want exactly %d", len(body), DefaultBodyLimit)
	}
}

// TestRetryAfterReturnsTypedError pins the 429 contract: the Do wrapper
// hands callers a typed error carrying the parsed Retry-After delay so
// the sync worker can back off without inspecting response text.
func TestRetryAfterReturnsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := newTestClient(t, nil)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected 429 to surface as an error")
	}
	var rate *RateLimitedError
	if !errors.As(err, &rate) {
		t.Fatalf("error type = %T, want *RateLimitedError", err)
	}
	if rate.RetryAfter != 42*time.Second {
		t.Fatalf("RetryAfter = %v, want 42s", rate.RetryAfter)
	}
}

// TestRetryAfterHTTPDate parses the RFC-1123 date form GitLab sends when
// it prefers absolute times over relative seconds.
func TestRetryAfterHTTPDate(t *testing.T) {
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", future)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := newTestClient(t, nil)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected 429 to error")
	}
	var rate *RateLimitedError
	if !errors.As(err, &rate) {
		t.Fatalf("error type = %T, want *RateLimitedError", err)
	}
	if rate.RetryAfter <= 0 || rate.RetryAfter > 45*time.Second {
		t.Fatalf("RetryAfter = %v, want positive and near 30s", rate.RetryAfter)
	}
}

// TestDoContextCancels verifies the transport honors an already-cancelled
// context by aborting fast rather than hanging on the read deadline.
func TestDoContextCancels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := newTestClient(t, nil)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected cancelled context to produce an error")
	}
}

// newTestClient wires a Client with a minimal AllowedHosts list that
// accepts localhost so httptest servers work. Real callers never allow
// loopback — that's covered by TestDialGuardVerdict.
func newTestClient(t *testing.T, extra []string) *Client {
	t.Helper()
	// httptest binds to 127.0.0.1; the dial guard rejects loopback by
	// default. Tests opt in via the "AllowLoopback" flag so the guard's
	// production defaults stay strict.
	allow := append([]string{"AllowLoopback"}, extra...)
	c, err := NewClient(Config{AllowedHosts: allow, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}
