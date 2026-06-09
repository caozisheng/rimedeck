//go:build windows

package agent

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// wslLookPath validates that execPath (a Linux absolute path) is an executable
// file inside the default WSL distro.
func wslLookPath(execPath string) error {
	wslExe, err := exec.LookPath("wsl.exe")
	if err != nil {
		return fmt.Errorf("wsl.exe not found: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, wslExe, "--", "test", "-x", execPath).Run(); err != nil {
		return fmt.Errorf("WSL executable not found at %q: %w", execPath, err)
	}
	return nil
}

// wslCommand builds an exec.Cmd that runs execPath inside the default WSL
// distro via wsl.exe. If cwd is non-empty it uses --cd so wsl.exe translates
// the Windows path to /mnt/... internally. Environment variables from envMap
// are injected via an `env` prefix inside WSL.
func wslCommand(ctx context.Context, execPath string, args []string, cwd string, envMap map[string]string) *exec.Cmd {
	wslArgs := make([]string, 0, 4+len(args))
	if cwd != "" {
		wslArgs = append(wslArgs, "--cd", cwd)
	}
	wslArgs = append(wslArgs, "--")

	envArgs := buildWSLEnvArgs(envMap)
	if len(envArgs) > 0 {
		wslArgs = append(wslArgs, "env")
		wslArgs = append(wslArgs, envArgs...)
	}

	wslArgs = append(wslArgs, execPath)
	wslArgs = append(wslArgs, args...)

	return exec.CommandContext(ctx, "wsl.exe", wslArgs...)
}

// wslDetectVersion runs `wsl.exe -- <execPath> --version` and returns the raw
// output. Used for agent version detection when the CLI lives inside WSL.
func wslDetectVersion(ctx context.Context, execPath string) (string, error) {
	wslExe, err := exec.LookPath("wsl.exe")
	if err != nil {
		return "", fmt.Errorf("wsl.exe not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, wslExe, "--", execPath, "--version")
	hideAgentWindow(cmd)
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("detect version via WSL for %s: %w", execPath, err)
	}
	return string(data), nil
}
