package gitlabtracker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// TestBodyCapEnforced proves the body reader stops early when an upstream
// tries to hand us more than DefaultBodyLimit bytes. The tail must be
// truncated, not buffered, so a malicious GitLab can't exhaust memory.
func TestBodyCapEnforced(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write cap+1024 bytes; the reader must truncate at cap.
		chunk := make([]byte, 4096)
		for written := int64(0); written < DefaultBodyLimit+1024; written += int64(len(chunk)) {
			_, _ = w.Write(chunk)
		}
	}))
	defer upstream.Close()

	client := newTestClient(t)
	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/x", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	_, readErr := io.ReadAll(resp.Body)
	if !errors.Is(readErr, errBodyLimitExceeded) {
		t.Fatalf("read err = %v, want errBodyLimitExceeded", readErr)
	}
}

// TestRetryAfterReturnsTypedError pins the 429 contract: the Do wrapper
// hands callers a typed error carrying the parsed Retry-After delay so
// the sync worker can back off without inspecting response text.
func TestRetryAfterReturnsTypedError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", strconv.Itoa(42))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	client := newTestClient(t)
	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/x", nil)
	_, err := client.Do(req)
	var rle *RateLimitedError
	if !errors.As(err, &rle) {
		t.Fatalf("expected *RateLimitedError, got %T (%v)", err, err)
	}
	if rle.RetryAfter != 42*time.Second {
		t.Fatalf("RetryAfter = %v, want 42s", rle.RetryAfter)
	}
	if rle.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, want 429", rle.StatusCode)
	}
}

// TestRetryAfterHTTPDate parses the RFC-1123 date form GitLab sends when
// it prefers absolute times over relative seconds.
func TestRetryAfterHTTPDate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
		w.Header().Set("Retry-After", future)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	client := newTestClient(t)
	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/x", nil)
	_, err := client.Do(req)
	var rle *RateLimitedError
	if !errors.As(err, &rle) {
		t.Fatalf("expected *RateLimitedError, got %T (%v)", err, err)
	}
	// Allow a few seconds of drift; only care that we didn't zero it out.
	if rle.RetryAfter < 20*time.Second || rle.RetryAfter > 40*time.Second {
		t.Fatalf("RetryAfter = %v, want ~30s", rle.RetryAfter)
	}
}

// TestDoContextCancels verifies the transport honors an already-cancelled
// context by aborting fast rather than hanging on the read deadline.
func TestDoContextCancels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	client := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL+"/x", nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected cancelled context to surface as error")
	}
}

// newTestClient wires a Client with a short timeout so tests fail fast
// when the stub is unreachable. The transport is deliberately unfiltered
// — the production Client behaves the same way (access control lives at
// the API boundary, not in the transport).
func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(Config{RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}
