//go:build !windows

package agent

import (
	"context"
	"fmt"
	"os/exec"
)

// wslLookPath validates a WSL-resident binary. On non-Windows platforms
// it falls back to exec.LookPath.
func wslLookPath(execPath string) error {
	_, err := exec.LookPath(execPath)
	return err
}

// wslCommand is unused on non-Windows but kept for compile-time parity.
func wslCommand(ctx context.Context, execPath string, args []string, cwd string, envMap map[string]string) *exec.Cmd {
	return exec.CommandContext(ctx, execPath, args...)
}

// wslDetectVersion is a no-op stub on non-Windows.
func wslDetectVersion(ctx context.Context, execPath string) (string, error) {
	return "", fmt.Errorf("WSL is not available on this platform")
}

// windowsToWSLPath is a passthrough on non-Windows.
func windowsToWSLPath(winPath string) string { return winPath }
