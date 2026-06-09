//go:build windows

package daemon

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const wslResolveTimeout = 5 * time.Second

// resolveAgentsViaWSL asks the default WSL distro to resolve each command name
// in names to its canonical absolute path. It returns a map of name → linux-path
// for whatever WSL could find, and an empty/nil map when WSL is unavailable,
// unconfigured, or produces no usable output.
//
// This is the Windows-only counterpart of resolveAgentsViaLoginShell: the login-
// shell resolver relies on $SHELL which is empty on Windows, while this function
// reaches into WSL where npm-installed CLIs (claude, codex, …) commonly live.
func resolveAgentsViaWSL(names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}

	wslExe, err := exec.LookPath("wsl.exe")
	if err != nil {
		return nil
	}

	// Verify a usable default distro exists. `wsl.exe -e true` exits 0 when
	// a default distro is registered and boots successfully.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), wslResolveTimeout)
	defer probeCancel()
	if err := exec.CommandContext(probeCtx, wslExe, "-e", "true").Run(); err != nil {
		return nil
	}

	safe := make([]string, 0, len(names))
	for _, n := range names {
		if isSafeAgentName(n) {
			safe = append(safe, n)
		}
	}
	if len(safe) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), wslResolveTimeout)
	defer cancel()

	script := buildWSLResolveScript(safe)
	cmd := exec.CommandContext(ctx, wslExe, "--", "sh", "-c", script)
	raw, err := cmd.Output()
	if err != nil {
		return nil
	}

	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name, path := parts[0], strings.TrimSpace(parts[1])
		if path == "" || !strings.HasPrefix(path, "/") {
			continue
		}
		out[name] = path
	}
	return out
}

// buildWSLResolveScript returns a POSIX shell script that resolves each command
// to its absolute path inside WSL, printing name<TAB>path per line. Mirrors the
// login-shell script but omits unalias/unset (WSL shells start non-interactive).
func buildWSLResolveScript(names []string) string {
	var b strings.Builder
	b.WriteString("for n in")
	for _, n := range names {
		b.WriteByte(' ')
		b.WriteString(n)
	}
	b.WriteString("; do\n")
	b.WriteString("  p=$(command -v \"$n\" 2>/dev/null) || continue\n")
	b.WriteString("  [ -n \"$p\" ] || continue\n")
	b.WriteString("  case \"$p\" in /*) ;; *) continue ;; esac\n")
	b.WriteString("  printf '%s\\t%s\\n' \"$n\" \"$p\"\n")
	b.WriteString("done\n")
	return b.String()
}

// wslPathExists checks whether path (a Linux absolute path) is an executable
// file inside the default WSL distro.
func wslPathExists(path string) bool {
	wslExe, err := exec.LookPath("wsl.exe")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, wslExe, "--", "test", "-x", path).Run() == nil
}
