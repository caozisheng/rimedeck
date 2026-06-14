//go:build windows

package daemon

import (
	"os"
	"path/filepath"
)

// npmGlobalBinPaths returns well-known Windows directories where npm (and
// pnpm/yarn) install global CLI shims. Electron-launched daemon processes
// often miss these because the user's PATH wasn't fully inherited.
func npmGlobalBinPaths() []string {
	var dirs []string
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		dirs = append(dirs, filepath.Join(appdata, "npm"))
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		dirs = append(dirs, filepath.Join(localAppData, "pnpm"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, "scoop", "shims"),
		)
	}
	return dirs
}

// resolveAgentViaNpmGlobal searches well-known Windows npm global bin
// directories for a .cmd shim matching the given command name. Returns
// the absolute path to the .cmd file, or "" if not found.
func resolveAgentViaNpmGlobal(cmd string) string {
	for _, dir := range npmGlobalBinPaths() {
		// npm creates <name>.cmd on Windows; also check extensionless
		// (npm creates a bash shim alongside the .cmd).
		for _, name := range []string{cmd + ".cmd", cmd} {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}
