package agent

import (
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func TestBuildQwenArgsUsesOfficialCLI(t *testing.T) {
	opts := ExecOptions{
		Cwd:             `C:\work`,
		Model:           "qwen3-coder-plus",
		SystemPrompt:    "Follow the issue brief.\nDo not lose this line.",
		ResumeSessionID: "session-123",
	}

	got := buildQwenArgs(opts, nil)
	want := []string{
		"--output-format", "stream-json",
		"--yolo",
		"--model", "qwen3-coder-plus",
		"--resume", "session-123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildQwenArgs() = %#v, want %#v", got, want)
	}
	for _, arg := range got {
		if arg == "run" || arg == "--dir" || arg == "--format" || arg == "--session" || strings.Contains(arg, "Follow the issue brief") {
			t.Fatalf("buildQwenArgs() contains unsupported or multiline argument %q", arg)
		}
	}
}

func TestBuildQwenInputPreservesSystemPrompt(t *testing.T) {
	got := buildQwenInput("Fix the bug", "Follow the issue brief.\nDo not lose this line.")
	want := "Follow the issue brief.\nDo not lose this line.\n\n---\n\nFix the bug"
	if got != want {
		t.Fatalf("buildQwenInput() = %q, want %q", got, want)
	}
}

func TestQwenProcessEventsParsesOfficialStreamJSON(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"session-123"}`,
		`{"type":"assistant","uuid":"msg-1","session_id":"session-123","message":{"id":"msg-1","type":"message","role":"assistant","model":"qwen3-coder-plus","content":[{"type":"text","text":"Fixed."}],"usage":{"input_tokens":11,"output_tokens":3,"cache_read_input_tokens":2}}}`,
		`{"type":"result","subtype":"success","uuid":"result-1","session_id":"session-123","is_error":false,"duration_ms":15,"result":"Fixed.","usage":{"input_tokens":11,"output_tokens":3,"cache_read_input_tokens":2}}`,
	}, "\n")

	backend := &qwencodeBackend{cfg: Config{Logger: discardLogger()}}
	messages := make(chan Message, 8)
	got := backend.processEvents(strings.NewReader(stream), messages)
	close(messages)

	if got.status != "completed" || got.errMsg != "" {
		t.Fatalf("status=%q error=%q", got.status, got.errMsg)
	}
	if got.sessionID != "session-123" {
		t.Fatalf("sessionID=%q, want session-123", got.sessionID)
	}
	if got.output != "Fixed." {
		t.Fatalf("output=%q, want Fixed.", got.output)
	}
	if got.usage.InputTokens != 11 || got.usage.OutputTokens != 3 || got.usage.CacheReadTokens != 2 {
		t.Fatalf("usage=%+v", got.usage)
	}

	var text string
	for message := range messages {
		if message.Type == MessageText {
			text += message.Content
		}
	}
	if text != "Fixed." {
		t.Fatalf("streamed text=%q, want Fixed.", text)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
