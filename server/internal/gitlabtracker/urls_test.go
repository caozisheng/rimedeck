package gitlabtracker

import (
	"strings"
	"testing"
)

// TestParseProjectURL pins the parser's contract in one table. Every
// case declares either the exact success shape or the exact error code.
// New URL flavors (custom port, IP host, http scheme) each get a row.
func TestParseProjectURL(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		wantHost     string // host (including port when supplied)
		wantPath     string
		wantWebURL   string
		wantCloneURL string
		wantScheme   string // "" defaults to https
		wantErrCode  string
	}{
		{
			name:         "gitlab.com https",
			input:        "https://gitlab.com/group/project",
			wantHost:     "gitlab.com",
			wantPath:     "group/project",
			wantWebURL:   "https://gitlab.com/group/project",
			wantCloneURL: "https://gitlab.com/group/project.git",
		},
		{
			name:         "jihulab.com works without any operator config",
			input:        "https://jihulab.com/group/project",
			wantHost:     "jihulab.com",
			wantPath:     "group/project",
			wantWebURL:   "https://jihulab.com/group/project",
			wantCloneURL: "https://jihulab.com/group/project.git",
		},
		{
			name:         ".git suffix stripped",
			input:        "https://gitlab.com/group/project.git",
			wantHost:     "gitlab.com",
			wantPath:     "group/project",
			wantCloneURL: "https://gitlab.com/group/project.git",
		},
		{
			name:         "nested subgroup path",
			input:        "https://gitlab.com/group/sub/project",
			wantHost:     "gitlab.com",
			wantPath:     "group/sub/project",
			wantCloneURL: "https://gitlab.com/group/sub/project.git",
		},
		{
			name:         "ssh short form converts to https",
			input:        "git@gitlab.com:group/project.git",
			wantHost:     "gitlab.com",
			wantPath:     "group/project",
			wantCloneURL: "https://gitlab.com/group/project.git",
		},
		{
			name:         "self-hosted with custom port preserves port everywhere",
			input:        "https://gitlab.internal:9080/g/p",
			wantHost:     "gitlab.internal:9080",
			wantPath:     "g/p",
			wantWebURL:   "https://gitlab.internal:9080/g/p",
			wantCloneURL: "https://gitlab.internal:9080/g/p.git",
		},
		{
			name:         "bare IPv4 with port (LAN GitLab)",
			input:        "https://192.168.1.10:8443/team/app",
			wantHost:     "192.168.1.10:8443",
			wantPath:     "team/app",
			wantCloneURL: "https://192.168.1.10:8443/team/app.git",
		},
		{
			name:         "bare IPv4 default port",
			input:        "https://10.0.0.5/team/app",
			wantHost:     "10.0.0.5",
			wantPath:     "team/app",
			wantCloneURL: "https://10.0.0.5/team/app.git",
		},
		{
			name:         "http scheme accepted for internal deployments",
			input:        "http://gitlab.internal/g/p",
			wantHost:     "gitlab.internal",
			wantPath:     "g/p",
			wantWebURL:   "http://gitlab.internal/g/p",
			wantCloneURL: "http://gitlab.internal/g/p.git",
			wantScheme:   "http://",
		},
		{
			name:        "userinfo forbidden",
			input:       "https://user:pass@gitlab.com/g/p",
			wantErrCode: "userinfo_forbidden",
		},
		{
			name:        "fragment forbidden",
			input:       "https://gitlab.com/g/p#frag",
			wantErrCode: "fragment_forbidden",
		},
		{
			name:        "scheme other than http/https rejected",
			input:       "ftp://gitlab.com/g/p",
			wantErrCode: "scheme_required",
		},
		{
			name:        "empty group segment",
			input:       "https://gitlab.com/onlygroup",
			wantErrCode: "path_needs_namespace",
		},
		{
			name:        "blank input",
			input:       "   ",
			wantErrCode: "empty_url",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseProjectURL(tc.input)
			if tc.wantErrCode != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil (parsed=%+v)", tc.wantErrCode, got)
				}
				var perr *URLError
				if !errorsAs(err, &perr) {
					t.Fatalf("error type = %T, want *URLError (err=%v)", err, err)
				}
				if perr.Code != tc.wantErrCode {
					t.Fatalf("error code = %q, want %q (err=%v)", perr.Code, tc.wantErrCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Host != tc.wantHost {
				t.Errorf("Host = %q, want %q", got.Host, tc.wantHost)
			}
			if got.PathWithNamespace != tc.wantPath {
				t.Errorf("Path = %q, want %q", got.PathWithNamespace, tc.wantPath)
			}
			if tc.wantWebURL != "" && got.WebURL != tc.wantWebURL {
				t.Errorf("WebURL = %q, want %q", got.WebURL, tc.wantWebURL)
			}
			if got.CloneURL != tc.wantCloneURL {
				t.Errorf("CloneURL = %q, want %q", got.CloneURL, tc.wantCloneURL)
			}
			wantScheme := tc.wantScheme
			if wantScheme == "" {
				wantScheme = "https://"
			}
			if !strings.HasPrefix(got.InstanceURL, wantScheme) {
				t.Errorf("InstanceURL = %q, want %s prefix", got.InstanceURL, wantScheme)
			}
		})
	}
}

// Tiny local errors.As so the test does not have to depend on the errors
// package import surface. Kept next to the test to avoid coupling the
// public API's dependencies.
func errorsAs(err error, target **URLError) bool {
	for err != nil {
		if u, ok := err.(*URLError); ok {
			*target = u
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		return false
	}
	return false
}
