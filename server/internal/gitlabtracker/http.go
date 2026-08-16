package gitlabtracker

import (
	"context"
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

// AllowLoopbackFlag opts the dial guard into localhost. Only tests should
// set it; production always leaves loopback rejected.
const AllowLoopbackFlag = "AllowLoopback"

// allowPrivatePrefix marks an allowlist entry as "this host may resolve
// into an RFC1918 range". Format: "AllowPrivate:<host>". This is the
// self-hosted GitLab escape hatch operators need without turning the
// private-address block off globally.
const allowPrivatePrefix = "AllowPrivate:"

// DialError is the pre-connect verdict raised by the dial guard. Code
// values are stable so callers can map to a safe user-facing message.
type DialError struct {
	Code    string
	Message string
}

func (e *DialError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

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

// Config wires the transport to an allowlist. AllowedHosts is the same
// list ParseProjectURL consumes; RequestTimeout applies to the whole
// round-trip. Callers that need finer control (dial timeout, TLS
// override) should extend this struct rather than reaching into the
// exported http.Client.
type Config struct {
	AllowedHosts   []string
	RequestTimeout time.Duration
}

// Client is a small facade around an http.Client wired with the dial
// guard, cross-host redirect rejection, and typed 429 handling. Callers
// receive it from NewClient — never construct Client directly.
type Client struct {
	http    *http.Client
	allow   []string
	bodyMax int64
}

// NewClient builds a Client from a Config. The AllowedHosts contract is
// the same one ParseProjectURL enforces (gitlab.com implicit, others
// explicit) with two extra flags recognised here:
//
//   - "AllowLoopback" opts localhost into the dial guard (tests only).
//   - "AllowPrivate:<host>" lets a specific host resolve into RFC1918.
//
// A missing config is not an error — an empty AllowedHosts list still
// gets gitlab.com by default.
func NewClient(cfg Config) (*Client, error) {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 30 * time.Second
	}
	allow := append([]string(nil), cfg.AllowedHosts...)

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		DialContext:           newGuardedDialContext(dialer, allow),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.RequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 {
				return nil
			}
			original := via[0].URL.Hostname()
			next := req.URL.Hostname()
			if !strings.EqualFold(original, next) {
				return fmt.Errorf("gitlabtracker: cross-host redirect blocked (%s -> %s)", original, next)
			}
			if len(via) > 5 {
				return errors.New("gitlabtracker: too many redirects")
			}
			return nil
		},
	}
	return &Client{http: httpClient, allow: allow, bodyMax: DefaultBodyLimit}, nil
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

// newGuardedDialContext builds the DialContext callback: it resolves the
// host, applies checkDialAddress to the resulting IP, and only forwards
// accepted addresses to the underlying dialer. Rejects surface as errors
// wrapping *DialError.
func newGuardedDialContext(dialer *net.Dialer, allow []string) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("gitlabtracker: no IPs for %q", host)
		}
		// We only dial the FIRST accepted IP so a partial-happy-eyeballs
		// scenario cannot leak requests to a rejected address. If every
		// candidate is rejected we surface the first rejection so the
		// operator sees the strictest reason.
		var firstReject error
		for _, ip := range ips {
			if verdict := checkDialAddress(host, ip, allow); verdict != nil {
				if firstReject == nil {
					firstReject = verdict
				}
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		if firstReject != nil {
			return nil, firstReject
		}
		return nil, fmt.Errorf("gitlabtracker: no candidate IPs for %q", host)
	}
}

// checkDialAddress inspects a resolved IP and returns a *DialError when
// the host+IP pair violates the SSRF guard. Public IPs are always
// accepted; loopback and multicast are always rejected; link-local is
// always rejected; RFC1918 requires an explicit AllowPrivate:<host>
// entry on the allowlist. IPv6 mirrors the IPv4 rules.
func checkDialAddress(host string, ip net.IP, allow []string) error {
	if ip == nil {
		return &DialError{Code: "invalid_ip", Message: "could not parse resolved IP"}
	}
	if ip.IsUnspecified() {
		return &DialError{Code: "unspecified_forbidden", Message: "unspecified address is not routable"}
	}
	if ip.IsMulticast() {
		return &DialError{Code: "multicast_forbidden", Message: "multicast addresses are not routable"}
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return &DialError{Code: "link_local_forbidden", Message: "link-local addresses are not routable"}
	}
	if ip.IsLoopback() {
		if allowFlag(allow, AllowLoopbackFlag) {
			return nil
		}
		return &DialError{Code: "loopback_forbidden", Message: "loopback addresses are not permitted"}
	}
	if ip.IsPrivate() {
		if allowFlag(allow, allowPrivatePrefix+strings.ToLower(host)) {
			return nil
		}
		return &DialError{Code: "private_forbidden", Message: "private-range addresses require an explicit AllowPrivate:<host> allowlist entry"}
	}
	return nil
}

// allowFlag is a case-insensitive membership check that treats the
// allowlist as a bag of opaque strings. Kept small so the tests can pass
// synthetic flags without introducing a separate flag surface.
func allowFlag(allow []string, needle string) bool {
	for _, item := range allow {
		if strings.EqualFold(strings.TrimSpace(item), needle) {
			return true
		}
	}
	return false
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
