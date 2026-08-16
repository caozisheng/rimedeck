package gitlabtracker

import (
	"net/url"
	"regexp"
	"strings"
)

// URLError carries a stable machine-readable code alongside the human
// message. Consumers pattern-match on Code so the safe error mapping in
// last_error_message stays UI-actionable without leaking the URL's tail.
type URLError struct {
	Code    string
	Message string
}

func (e *URLError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// ProjectURL is the normalized set of URL fields the rest of the tracker
// stack consumes. Host preserves the port when the user supplied one so
// LAN GitLab installs like `https://192.168.1.10:8443/g/p` reach the
// right listener. InstanceURL is scheme + Host so the REST client can
// concatenate `/api/v4/…` without re-parsing.
type ProjectURL struct {
	InstanceURL       string
	Host              string // hostname or IP, plus port if the URL had one
	PathWithNamespace string
	WebURL            string
	CloneURL          string
}

var (
	// git@host:namespace/project(.git)? — the scp-like short form GitLab
	// hands out under "Clone with SSH". We don't clone over SSH from the
	// server side, so this parses to the equivalent https CloneURL.
	sshShortRE = regexp.MustCompile(`^git@([^:]+):(.+?)(?:\.git)?$`)
	// Reject the four ASCII whitespace runs before parsing; url.Parse
	// happily accepts spaces in some paths.
	blankRE = regexp.MustCompile(`^\s*$`)
)

// ParseProjectURL takes a user-typed URL (either http/https or scp-like
// SSH) and returns the normalized ProjectURL, or a *URLError. Any
// resolvable host is accepted — hostname, bare IP literal, custom port,
// http or https. Self-hosted / regional GitLab installs (jihulab.com,
// gitlab.internal:9080, 192.168.1.10:8443) work with zero operator
// config; the REST validation call is what proves it's actually GitLab.
// Structural checks stay: userinfo and fragments are still rejected
// because GitLab never emits them and they only appear when a URL was
// pasted from a phish/token-in-URL context.
func ParseProjectURL(raw string) (ProjectURL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || blankRE.MatchString(raw) {
		return ProjectURL{}, &URLError{Code: "empty_url", Message: "repository URL is required"}
	}

	// scp-like SSH shortcut: convert to https and fall through so the
	// path/userinfo/fragment checks stay in one place.
	if m := sshShortRE.FindStringSubmatch(raw); m != nil {
		raw = "https://" + m[1] + "/" + m[2]
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ProjectURL{}, &URLError{Code: "invalid_url", Message: err.Error()}
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ProjectURL{}, &URLError{Code: "scheme_required", Message: "only http and https URLs are accepted"}
	}
	if u.User != nil {
		return ProjectURL{}, &URLError{Code: "userinfo_forbidden", Message: "URL must not include a userinfo component"}
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return ProjectURL{}, &URLError{Code: "fragment_forbidden", Message: "URL must not include a fragment"}
	}
	// u.Host keeps the port (u.Hostname() would drop it), which we need
	// so LAN installs on non-default ports round-trip correctly. Lower-
	// case the hostname portion for stable equality but preserve the
	// caller's port digits verbatim.
	host := strings.ToLower(u.Host)
	if host == "" {
		return ProjectURL{}, &URLError{Code: "invalid_url", Message: "URL has no host"}
	}

	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	if path == "" || !strings.Contains(path, "/") {
		return ProjectURL{}, &URLError{Code: "path_needs_namespace", Message: "URL must include at least namespace/project"}
	}

	instance := u.Scheme + "://" + host

	return ProjectURL{
		InstanceURL:       instance,
		Host:              host,
		PathWithNamespace: path,
		WebURL:            instance + "/" + path,
		CloneURL:          instance + "/" + path + ".git",
	}, nil
}
