package gitlabtracker

import (
	"strings"
	"testing"
)

// TestParseProjectURL is the parser's contract in one table. Every case
// pins either the exact success shape or the exact error code — no
// wildcards. Add cases when GitLab surfaces a new URL flavor; never delete
// one without moving its behavior into another case.
func TestParseProjectURL(t *testing.T) {
	// Two AllowedHosts fixtures cover the two operator postures we
	// document (§11.3): (a) "cloud only" — no allowlist, gitlab.com is
	// implicit; (b) "self-hosted" — an explicit host is on the allowlist.
	// Loopback / RFC1918 stay rejected in both.
	allowSelfHosted := []string{"gitlab.example.com"}

	cases := []struct {
		name         string
		input        string
		allow        []string
		wantHost     string
		wantPath     string
		wantWebURL   string
		wantCloneURL string
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
			name:         "gitlab.com https .git suffix stripped",
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
			name:         "self-hosted host on allowlist",
			input:        "https://gitlab.example.com/g/p",
			allow:        allowSelfHosted,
			wantHost:     "gitlab.example.com",
			wantPath:     "g/p",
			wantCloneURL: "https://gitlab.example.com/g/p.git",
		},
		{
			name:        "self-hosted host missing from allowlist",
			input:       "https://gitlab.example.com/g/p",
			wantErrCode: "host_not_allowed",
		},
		{
			name:        "userinfo forbidden even for gitlab.com",
			input:       "https://user:pass@gitlab.com/g/p",
			wantErrCode: "userinfo_forbidden",
		},
		{
			name:        "fragment forbidden",
			input:       "https://gitlab.com/g/p#frag",
			wantErrCode: "fragment_forbidden",
		},
		{
			name:        "http (non-tls) rejected",
			input:       "http://gitlab.com/g/p",
			wantErrCode: "https_required",
		},
		{
			name:        "loopback rejected regardless of allowlist",
			input:       "https://127.0.0.1/g/p",
			wantErrCode: "host_not_allowed",
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
			got, err := ParseProjectURL(tc.input, tc.allow)
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
			if !strings.HasPrefix(got.InstanceURL, "https://") {
				t.Errorf("InstanceURL = %q, want https:// prefix", got.InstanceURL)
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
