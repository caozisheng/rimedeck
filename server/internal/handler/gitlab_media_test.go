package handler

import (
	"net/http"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestResolveGitlabMediaURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"relative upload", "/uploads/abc/image.png", "https://gitlab.example.com/uploads/abc/image.png", true},
		{"same origin absolute", "https://gitlab.example.com/uploads/a.png?width=10", "https://gitlab.example.com/uploads/a.png?width=10", true},
		{"foreign host", "https://evil.example/uploads/a.png", "", false},
		{"non upload path", "/api/v4/projects", "", false},
		{"userinfo", "https://user@gitlab.example.com/uploads/a.png", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveGitlabMediaURL("https://gitlab.example.com", tc.raw)
			if tc.ok && err != nil {
				t.Fatal(err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected rejection, got %s", got)
			}
			if tc.ok && got.String() != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestGitlabMediaCandidatesPreferOfficialUploadAPI(t *testing.T) {
	tracker := db.GitlabTrackerConnection{InstanceUrl: "https://jihulab.com", RemoteProjectID: 123, PathWithNamespace: "group/project"}
	got, err := gitlabMediaCandidates(tracker, "/uploads/secret/image.png")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].String() != "https://jihulab.com/api/v4/projects/123/uploads/secret/image.png" {
		t.Fatalf("first candidate = %q", got[0])
	}
}

func TestDetectGitlabImageContentTypeUsesBytesWhenHeaderIsOctetStream(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nimage")
	if got := detectGitlabImageContentType("application/octet-stream", png); got != "image/png" {
		t.Fatalf("content type = %q, want image/png", got)
	}
	if got := detectGitlabImageContentType("text/html", []byte("<html>")); got != "" {
		t.Fatalf("content type = %q, want rejection", got)
	}
	_ = http.StatusOK
}
