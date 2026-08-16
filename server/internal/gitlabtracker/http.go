package gitlabtracker

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBodyLimit caps every response body the transport surfaces. GitLab
// REST responses for the surfaces we consume (project, labels, issues)
// stay well under 4 MiB even at per_page=100; capping here prevents a
// malicious or misconfigured upstream from exhausting server memory.
const DefaultBodyLimit int64 = 4 << 20 // 4 MiB

// RateLimitedError is the typed 429 the transport surfaces so the sync
// worker can back off without inspecting response text. RetryAfter is
// zero when GitLab omitted the header.
type RateLimitedError struct {
	RetryAfter time.Duration
	StatusCode int
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("gitlab rate limited (retry after %s)", e.RetryAfter)
}

// Config sets the request timeout. Access control lives at the API
// boundary (the operator's PAT is the source of truth for what the
// tracker can reach); the transport itself is deliberately unfiltered so
// LAN GitLab installs on private IPs, custom ports, and internal DNS
// zones work without operator config.
type Config struct {
	RequestTimeout time.Duration
}

// Client is a small facade around an http.Client with a body cap and
// typed 429 handling. Callers receive it from NewClient — never
// construct Client directly.
type Client struct {
	http    *http.Client
	bodyMax int64
}

// NewClient builds a Client from a Config. A missing timeout defaults
// to 30 seconds.
func NewClient(cfg Config) (*Client, error) {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 30 * time.Second
	}
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.RequestTimeout,
		// A capped redirect chain prevents an accidental loop from
		// hanging the request; the specific target is not policed
		// because the operator trusts every host their PAT can reach.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return errors.New("gitlabtracker: too many redirects")
			}
			return nil
		},
	}
	return &Client{http: httpClient, bodyMax: DefaultBodyLimit}, nil
}

// Do sends the request, translates a 429 into a RateLimitedError, and
// wraps the response body in a capped reader so callers cannot buffer
// more than DefaultBodyLimit bytes. Every other status code is returned
// unchanged for the caller to decode.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		defer resp.Body.Close()
		delay := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return nil, &RateLimitedError{RetryAfter: delay, StatusCode: resp.StatusCode}
	}
	resp.Body = &cappedBody{ReadCloser: resp.Body, remaining: c.bodyMax}
	return resp, nil
}

// cappedBody wraps the response body so a runaway upstream cannot exhaust
// memory. When the cap is reached the reader returns a sentinel error
// instead of silently truncating — the caller then treats it the same
// way it handles any other transport failure.
type cappedBody struct {
	io.ReadCloser
	remaining int64
}

// errBodyLimitExceeded is the sentinel returned once the cap is exhausted.
var errBodyLimitExceeded = errors.New("gitlabtracker: response body exceeds cap")

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errBodyLimitExceeded
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining <= 0 && err == nil {
		err = errBodyLimitExceeded
	}
	return n, err
}

// parseRetryAfter follows RFC 7231 §7.1.3: a bare integer is seconds; a
// full HTTP-date is an absolute instant. Returns zero when the header
// is missing or unparsable so callers can apply a default backoff.
func parseRetryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		if t.Before(now) {
			return 0
		}
		return t.Sub(now)
	}
	return 0
}
