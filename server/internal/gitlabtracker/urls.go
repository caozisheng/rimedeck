package gitlabtracker

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// DefaultAllowedHost is the well-known SaaS host. Operators do not have to
// list it; anything else must appear in GITLAB_ALLOWED_HOSTS.
const DefaultAllowedHost = "gitlab.com"

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

// ProjectURL is the normalized triple: instance root (protocol+host, no
// path), the parsed host on its own, and the group/subgroup/project path
// with the trailing `.git` stripped. WebURL and CloneURL are canonicalized
// so downstream code never has to re-normalize.
type ProjectURL struct {
	InstanceURL       string
	Host              string
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

// ParseProjectURL takes a user-typed URL (either HTTPS or scp-like SSH)
// plus the operator's self-hosted allowlist and returns the normalized
// ProjectURL, or a *URLError. gitlab.com is always allowed. Loopback /
// link-local / RFC1918 / multicast literal hosts are rejected regardless
// of the allowlist — the allowlist is a name allowlist, not an IP
// allowlist; direct-IP GitLab access would require operator opt-in via a
// future explicit setting.
func ParseProjectURL(raw string, allowedHosts []string) (ProjectURL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || blankRE.MatchString(raw) {
		return ProjectURL{}, &URLError{Code: "empty_url", Message: "repository URL is required"}
	}

	// scp-like SSH shortcut: convert to https and fall through to the same
	// validator so the allowlist / path checks stay in one place.
	if m := sshShortRE.FindStringSubmatch(raw); m != nil {
		raw = "https://" + m[1] + "/" + m[2]
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ProjectURL{}, &URLError{Code: "invalid_url", Message: err.Error()}
	}
	if u.Scheme != "https" {
		return ProjectURL{}, &URLError{Code: "https_required", Message: "only https URLs are accepted"}
	}
	if u.User != nil {
		return ProjectURL{}, &URLError{Code: "userinfo_forbidden", Message: "URL must not include a userinfo component"}
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return ProjectURL{}, &URLError{Code: "fragment_forbidden", Message: "URL must not include a fragment"}
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ProjectURL{}, &URLError{Code: "invalid_url", Message: "URL has no host"}
	}
	if !hostAllowed(host, allowedHosts) {
		return ProjectURL{}, &URLError{Code: "host_not_allowed", Message: fmt.Sprintf("host %q is not on the GitLab allowlist", host)}
	}

	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	if path == "" || !strings.Contains(path, "/") {
		return ProjectURL{}, &URLError{Code: "path_needs_namespace", Message: "URL must include at least namespace/project"}
	}

	// InstanceURL keeps only scheme+host so the REST client can concat
	// `/api/v4/...` without re-parsing.
	instance := "https://" + host

	return ProjectURL{
		InstanceURL:       instance,
		Host:              host,
		PathWithNamespace: path,
		WebURL:            instance + "/" + path,
		CloneURL:          instance + "/" + path + ".git",
	}, nil
}

// hostAllowed centralizes the two-branch decision so ParseProjectURL stays
// linear. IP literals are always rejected — the allowlist is a name
// allowlist; if an operator ever needs to accept a bare IP, they can add
// an explicit dedicated setting rather than blurring this predicate.
func hostAllowed(host string, allowed []string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return false
	}
	if host == DefaultAllowedHost {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate), host) {
			return true
		}
	}
	return false
}
