//go:build windows

package daemon

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const wslResolveTimeout = 8 * time.Second
const wslResolveWaitDelay = 3 * time.Second

// resolveAgentsViaWSL asks the default WSL distro's login shell to resolve
// each command name in names to its canonical absolute path. It returns a map
// of name → path for whatever WSL could find, and an empty/nil map when WSL
// is unavailable, unconfigured, or produces no usable output.
//
// This is the Windows-only counterpart of resolveAgentsViaLoginShell: the
// login-shell resolver relies on $SHELL which is empty on Windows, while this
// function reaches into WSL where npm-installed CLIs (claude, codex, …)
// commonly live.
//
// Key design: we invoke `bash -ilc <script>` inside WSL so .bashrc / .profile
// are sourced, which is where nvm/fnm/volta add their bin dirs to PATH. A
// plain `sh -c` would miss these entirely — the root cause of the original
// "not found" reports.
func resolveAgentsViaWSL(names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}

	wslExe, err := exec.LookPath("wsl.exe")
	if err != nil {
		return nil
	}

	// Verify a usable default distro exists.
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

	// IMPORTANT: use `-e` (--exec), NOT `--`.
	// `wsl.exe --` passes the command through the default Linux shell, which
	// adds an extra parsing layer that mangles quotes and $ in the script.
	// `wsl.exe -e` calls execve directly — no intermediate shell.
	//
	// We use `bash -ilc` so .bashrc / .profile are sourced (nvm/fnm/volta
	// add their bin dirs there). Fall back to `sh -lc` for minimal distros.
	var raw []byte
	for _, argv := range [][]string{
		{wslExe, "-e", "bash", "-ilc", script},
		{wslExe, "-e", "sh", "-lc", script},
	} {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.WaitDelay = wslResolveWaitDelay
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			raw = out
			break
		}
	}
	if len(raw) == 0 {
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

// buildWSLResolveScript returns a single-line POSIX shell script that
// resolves each command to its canonical absolute path inside WSL. It
// mirrors the native resolveAgentsViaLoginShell script: unalias + unset -f
// to see past aliases/functions, command -v for the real binary, pwd -P to
// chase symlinks (nvm/fnm multishell dirs vanish on shell exit).
//
// The script MUST be a single line: it's passed as an argument through
// Windows CreateProcessW → wsl.exe, and embedded newlines in the command-
// line string are not reliably preserved through that chain.
func buildWSLResolveScript(names []string) string {
	var b strings.Builder
	b.WriteString("for n in")
	for _, n := range names {
		b.WriteByte(' ')
		b.WriteString(n)
	}
	b.WriteString("; do ")
	b.WriteString("unalias \"$n\" 2>/dev/null; ")
	b.WriteString("unset -f \"$n\" 2>/dev/null; ")
	b.WriteString("p=$(command -v \"$n\" 2>/dev/null) || continue; ")
	b.WriteString("[ -n \"$p\" ] || continue; ")
	b.WriteString("case \"$p\" in /*) ;; *) continue;; esac; ")
	b.WriteString("d=$(dirname \"$p\") && f=$(basename \"$p\") && c=$(cd \"$d\" 2>/dev/null && pwd -P) || continue; ")
	b.WriteString("printf '%s\\t%s\\n' \"$n\" \"$c/$f\"; ")
	b.WriteString("done")
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
	return exec.CommandContext(ctx, wslExe, "-e", "test", "-x", path).Run() == nil
}
