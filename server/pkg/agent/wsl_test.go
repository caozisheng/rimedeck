package agent

import (
	"testing"
)

func TestWindowsToWSLPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"C:\\Users\\foo\\project", "/mnt/c/Users/foo/project"},
		{"D:\\work", "/mnt/d/work"},
		{"c:\\lower", "/mnt/c/lower"},
		// Forward slashes should also work (filepath.ToSlash is idempotent)
		{"C:/Users/bar", "/mnt/c/Users/bar"},
		// Non-drive path (unusual, but should not panic)
		{"/already/linux", "/already/linux"},
	}
	for _, tt := range tests {
		got := windowsToWSLPath(tt.input)
		if got != tt.want {
			t.Errorf("windowsToWSLPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildWSLEnvArgs(t *testing.T) {
	env := map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-123",
		"MULTICA_TOKEN":     "tok-456",
		"PATH":              "C:\\Windows;C:\\Go\\bin", // should be skipped
		"CODEX_HOME":        "C:\\Users\\.codex",      // should be skipped
	}
	got := buildWSLEnvArgs(env)
	// Expect only the two non-skipped keys, sorted.
	if len(got) != 2 {
		t.Fatalf("expected 2 env args, got %d: %v", len(got), got)
	}
	if got[0] != "ANTHROPIC_API_KEY=sk-ant-123" {
		t.Errorf("got[0] = %q, want ANTHROPIC_API_KEY=sk-ant-123", got[0])
	}
	if got[1] != "MULTICA_TOKEN=tok-456" {
		t.Errorf("got[1] = %q, want MULTICA_TOKEN=tok-456", got[1])
	}
}

func TestBuildWSLEnvArgs_Empty(t *testing.T) {
	got := buildWSLEnvArgs(nil)
	if got != nil {
		t.Errorf("expected nil for nil envMap, got %v", got)
	}
	got = buildWSLEnvArgs(map[string]string{})
	if got != nil {
		t.Errorf("expected nil for empty envMap, got %v", got)
	}
}

func TestIsWSLSkippedEnvKey(t *testing.T) {
	skipped := []string{
		"CODEX_HOME", "APPDATA", "LOCALAPPDATA", "PATH", "PATHEXT",
		"COMSPEC", "SYSTEMDRIVE", "HOMEDRIVE", "HOMEPATH",
	}
	for _, k := range skipped {
		if !isWSLSkippedEnvKey(k) {
			t.Errorf("expected %q to be skipped", k)
		}
	}

	passed := []string{
		"ANTHROPIC_API_KEY", "MULTICA_TOKEN", "OPENAI_API_KEY",
		"HOME", "SHELL", "USER",
	}
	for _, k := range passed {
		if isWSLSkippedEnvKey(k) {
			t.Errorf("expected %q to NOT be skipped", k)
		}
	}
}
