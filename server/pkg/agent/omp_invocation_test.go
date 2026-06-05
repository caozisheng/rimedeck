package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

func TestChooseOmpInvocation_PassthroughForNonLauncher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	execName := "omp"
	lookedUp := filepath.Join(t.TempDir(), "omp")
	args := []string{
		"-p",
		"--mode", "json",
		"--session", "/tmp/omp-session.jsonl",
		"hello world",
	}

	gotExec, gotArgs := chooseOmpInvocation(execName, lookedUp, args, logger)

	if gotExec != execName {
		t.Errorf("argv0 changed unexpectedly: got %q want %q", gotExec, execName)
	}
	if !reflect.DeepEqual(gotArgs, args) {
		t.Errorf("argv changed unexpectedly:\n got  %#v\n want %#v", gotArgs, args)
	}
}
