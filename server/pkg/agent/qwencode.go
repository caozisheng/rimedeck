package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// qwencodeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var qwencodeBlockedArgs = map[string]blockedArgMode{
	"--format":                       blockedWithValue,
	"--dangerously-skip-permissions": blockedStandalone,
}

// qwencodeBackend implements Backend by spawning `qwen-code run --format json`
// and reading streaming JSON events from stdout.
//
// Qwen Code (https://github.com/QwenLM/qwen-code) is Alibaba's coding agent
// CLI powered by the Qwen3-Coder model family. It uses a stream-json output
// format similar to OpenCode's `run --format json`.
type qwencodeBackend struct {
	cfg Config
}

func (b *qwencodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "qwen-code"
	}
	if b.cfg.IsWSL {
		if err := wslLookPath(execPath); err != nil {
			return nil, fmt.Errorf("qwen-code executable not found in WSL at %q: %w", execPath, err)
		}
	} else if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("qwen-code executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := []string{"run", "--format", "json", "--dangerously-skip-permissions"}
	if opts.Cwd != "" {
		args = append(args, "--dir", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, qwencodeBlockedArgs, b.cfg.Logger)...)
	args = append(args, prompt)

	var cmd *exec.Cmd
	if b.cfg.IsWSL {
		cmd = wslCommand(runCtx, execPath, args, opts.Cwd, b.cfg.Env)
	} else {
		cmd = exec.CommandContext(runCtx, execPath, args...)
		if opts.Cwd != "" {
			cmd.Dir = opts.Cwd
		}
		cmd.Env = buildEnv(b.cfg.Env)
	}
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args, "wsl", b.cfg.IsWSL)
	cmd.WaitDelay = 10 * time.Second
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[qwen-code:stderr] ")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("qwen-code stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start qwen-code: %w", err)
	}

	b.cfg.Logger.Info("qwen-code started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processEvents(stdout, msgCh)

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("qwen-code timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("qwen-code exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("qwen-code finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// qwencodeEventResult holds accumulated state from processing the event stream.
type qwencodeEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage
}

// qwencodeEvent mirrors the JSON-line event structure emitted by qwen-code.
// The layout follows the OpenCode convention (type + part + error).
type qwencodeEvent struct {
	Type      string               `json:"type"`
	Timestamp int64                `json:"timestamp,omitempty"`
	SessionID string               `json:"sessionID,omitempty"`
	Part      qwencodeEventPart    `json:"part"`
	Error     *qwencodeEventError  `json:"error,omitempty"`
}

type qwencodeEventPart struct {
	Text   string                `json:"text,omitempty"`
	Tool   string                `json:"tool,omitempty"`
	CallID string                `json:"callID,omitempty"`
	State  *qwencodeToolState    `json:"state,omitempty"`
	Tokens *qwencodeTokens       `json:"tokens,omitempty"`
}

type qwencodeToolState struct {
	Status string `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output any             `json:"output,omitempty"`
}

type qwencodeTokens struct {
	Input  int64           `json:"input"`
	Output int64           `json:"output"`
	Cache  *qwencodeCache  `json:"cache,omitempty"`
}

type qwencodeCache struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

type qwencodeEventError struct {
	Name string              `json:"name,omitempty"`
	Data *qwencodeErrData    `json:"data,omitempty"`
}

func (e *qwencodeEventError) message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type qwencodeErrData struct {
	Message string `json:"message,omitempty"`
}

func (b *qwencodeBackend) processEvents(r io.Reader, ch chan<- Message) qwencodeEventResult {
	var output strings.Builder
	var sessionID string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event qwencodeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.SessionID != "" {
			sessionID = event.SessionID
		}

		switch event.Type {
		case "text":
			text := event.Part.Text
			if text != "" {
				output.WriteString(text)
				trySend(ch, Message{Type: MessageText, Content: text})
			}
		case "tool_use":
			var input map[string]any
			if event.Part.State != nil && event.Part.State.Input != nil {
				_ = json.Unmarshal(event.Part.State.Input, &input)
			}
			trySend(ch, Message{
				Type:   MessageToolUse,
				Tool:   event.Part.Tool,
				CallID: event.Part.CallID,
				Input:  input,
			})
			if event.Part.State != nil && event.Part.State.Status == "completed" {
				outputStr := qwencodeExtractToolOutput(event.Part.State.Output)
				trySend(ch, Message{
					Type:   MessageToolResult,
					Tool:   event.Part.Tool,
					CallID: event.Part.CallID,
					Output: outputStr,
				})
			}
		case "error":
			errMsg := ""
			if event.Error != nil {
				errMsg = event.Error.message()
			}
			if errMsg == "" {
				errMsg = "unknown qwen-code error"
			}
			b.cfg.Logger.Warn("qwen-code error event", "error", errMsg)
			trySend(ch, Message{Type: MessageError, Content: errMsg})
			finalStatus = "failed"
			finalError = errMsg
		case "step_start":
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		case "step_finish":
			if t := event.Part.Tokens; t != nil {
				usage.InputTokens += t.Input
				usage.OutputTokens += t.Output
				if t.Cache != nil {
					usage.CacheReadTokens += t.Cache.Read
					usage.CacheWriteTokens += t.Cache.Write
				}
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("qwen-code stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	return qwencodeEventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
	}
}

func qwencodeExtractToolOutput(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	data, _ := json.Marshal(output)
	return string(data)
}
