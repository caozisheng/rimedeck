//go:build windows

package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlatformOmpInvocation_RewritesCmdLauncherToPowerShellFile(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "omp.cmd")
	ps1Path := filepath.Join(dir, "omp.ps1")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0omp.ps1\" %*\r\n")
	writeFile(t, ps1Path, "# fake omp.ps1\r\n")

	fakePS := filepath.Join(dir, "powershell.exe")
	writeFile(t, fakePS, "")
	stubPowerShell(t, fakePS, true)

	multiLinePrompt := "You are running as a chat assistant.\n\nUser message:\nhello\n"
	args := []string{
		"-p",
		"--mode", "json",
		"--session", `C:\Users\X\.rimedeck\omp-sessions\20260528T040000.jsonl`,
		multiLinePrompt,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gotExec, gotArgs, ok := platformOmpInvocation(cmdPath, args, logger)
	if !ok {
		t.Fatalf("expected platform rewrite to be applied, got ok=false")
	}
	if gotExec != fakePS {
		t.Errorf("argv0: got %q want %q", gotExec, fakePS)
	}

	wantArgs := append([]string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", ps1Path,
	}, args...)
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("argv mismatch:\n got  %#v\n want %#v", gotArgs, wantArgs)
	}

	if gotArgs[len(gotArgs)-1] != multiLinePrompt {
		t.Errorf("multi-line prompt was mangled:\n got  %q\n want %q", gotArgs[len(gotArgs)-1], multiLinePrompt)
	}
}

func TestPlatformOmpInvocation_SkipsWhenNotCmdOrBat(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "omp.exe")
	writeFile(t, exePath, "")
	writeFile(t, filepath.Join(dir, "omp.ps1"), "")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformOmpInvocation(exePath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false for non-.cmd/.bat launcher")
	}
}

func TestPlatformOmpInvocation_SkipsWhenPS1Missing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "omp.cmd")
	writeFile(t, cmdPath, "@echo off\r\n")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformOmpInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when omp.ps1 is missing")
	}
}

func TestPlatformOmpInvocation_SkipsWhenPowerShellMissing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "omp.cmd")
	ps1Path := filepath.Join(dir, "omp.ps1")
	writeFile(t, cmdPath, "@echo off\r\n")
	writeFile(t, ps1Path, "# fake\r\n")

	stubPowerShell(t, "", false)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformOmpInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when no powershell host is available")
	}
}
