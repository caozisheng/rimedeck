package agent

import (
	"path/filepath"
	"sort"
	"strings"
)

// windowsToWSLPath converts a Windows absolute path to its /mnt/ equivalent.
// Example: C:\Users\foo\bar → /mnt/c/Users/foo/bar
func windowsToWSLPath(winPath string) string {
	if winPath == "" {
		return ""
	}
	p := filepath.ToSlash(winPath)
	if len(p) >= 2 && p[1] == ':' {
		drive := strings.ToLower(string(p[0]))
		return "/mnt/" + drive + p[2:]
	}
	return p
}

// buildWSLEnvArgs builds KEY=VALUE strings for the env command prefix inside
// WSL. It forwards agent-relevant env vars (API keys, daemon tokens) and
// skips Windows-specific path vars that would be meaningless inside WSL.
func buildWSLEnvArgs(envMap map[string]string) []string {
	if len(envMap) == 0 {
		return nil
	}

	var args []string
	for k, v := range envMap {
		if isWSLSkippedEnvKey(k) {
			continue
		}
		args = append(args, k+"="+v)
	}
	if len(args) == 0 {
		return nil
	}
	sort.Strings(args)
	return args
}

// isWSLSkippedEnvKey returns true for env keys that should NOT be forwarded
// into WSL because they contain Windows-specific paths or internal state that
// is meaningless inside the Linux environment.
func isWSLSkippedEnvKey(key string) bool {
	switch key {
	case "CODEX_HOME", "APPDATA", "LOCALAPPDATA", "USERPROFILE",
		"PROGRAMDATA", "PROGRAMFILES", "SYSTEMROOT", "TEMP", "TMP",
		"PATH", "PATHEXT", "COMSPEC", "SYSTEMDRIVE", "HOMEDRIVE", "HOMEPATH":
		return true
	}
	return false
}
