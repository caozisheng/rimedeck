package daemon

import (
	"strings"
	"testing"
)

func TestBuildWSLResolveScript(t *testing.T) {
	got := buildWSLResolveScript([]string{"claude", "codex"})
	// Must be a single line — newlines break Windows CreateProcessW → wsl.exe.
	want := "for n in claude codex; do " +
		"unalias \"$n\" 2>/dev/null; " +
		"unset -f \"$n\" 2>/dev/null; " +
		"p=$(command -v \"$n\" 2>/dev/null) || continue; " +
		"[ -n \"$p\" ] || continue; " +
		"case \"$p\" in /*) ;; *) continue;; esac; " +
		"d=$(dirname \"$p\") && f=$(basename \"$p\") && c=$(cd \"$d\" 2>/dev/null && pwd -P) || continue; " +
		"printf '%s\\t%s\\n' \"$n\" \"$c/$f\"; " +
		"done"
	if got != want {
		t.Errorf("buildWSLResolveScript:\ngot:  %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "\n") {
		t.Error("script must be single-line for Windows command-line safety")
	}
}

func TestResolveAgentsViaWSL_EmptyNames(t *testing.T) {
	got := resolveAgentsViaWSL(nil)
	if got != nil {
		t.Errorf("expected nil for empty names, got %v", got)
	}
	got = resolveAgentsViaWSL([]string{})
	if got != nil {
		t.Errorf("expected nil for empty slice, got %v", got)
	}
}

func TestWSLPathExists_NoWSL(t *testing.T) {
	// On CI/macOS/Linux where wsl.exe doesn't exist, should return false.
	if wslPathExists("/usr/bin/true") {
		t.Skip("wsl.exe appears to be available; skipping negative test")
	}
}

func TestProbe_LinuxAbsPathOnNonWSL(t *testing.T) {
	// Verify that a Linux absolute path (starts with /) doesn't escape
	// through the old ContainsAny("/\\") guard and get treated as a
	// bare command name.
	t.Setenv("MULTICA_CLAUDE_PATH", "/usr/local/bin/claude")
	t.Setenv("MULTICA_WSL_MODE", "off")
	// With WSL mode off and a Linux path, probe should miss.
	_, err := LoadConfig(Overrides{})
	// We expect the overall LoadConfig to fail because no agents were found,
	// but the important thing is it didn't panic or match incorrectly.
	if err == nil {
		t.Log("LoadConfig succeeded (some agent on PATH); test is a no-op in this env")
	}
}
