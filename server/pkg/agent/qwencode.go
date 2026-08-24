package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// qwencodeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var qwencodeBlockedArgs = map[string]blockedArgMode{
	"--output-format": blockedWithValue,
	"-o":              blockedWithValue,
	"--yolo":          blockedStandalone,
	"-y":              blockedStandalone,
}

// qwencodeBackend implements Backend by spawning `qwen --output-format stream-json`
// and reading Qwen Code's JSONL event stream from stdout.
//
// Qwen Code (https://github.com/QwenLM/qwen-code) is Alibaba's coding agent
// CLI powered by the Qwen model family.

func buildQwenArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"--output-format", "stream-json", "--yolo"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	return append(args, filterCustomArgs(opts.CustomArgs, qwencodeBlockedArgs, logger)...)
}

func buildQwenInput(prompt, systemPrompt string) string {
	if systemPrompt == "" {
		return prompt
	}
	return systemPrompt + "\n\n---\n\n" + prompt
}

type qwencodeBackend struct {
	cfg Config
}

func (b *qwencodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "qwen"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("qwen executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildQwenArgs(opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[qwen:stderr] ")
	cmd.Stdin = strings.NewReader(buildQwenInput(prompt, opts.SystemPrompt))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("qwen stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start qwen: %w", err)
	}

	b.cfg.Logger.Info("qwen started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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
			scanResult.errMsg = fmt.Sprintf("qwen timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("qwen exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("qwen finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

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

// qwencodeEvent mirrors Qwen Code's --output-format stream-json JSONL frames.
type qwencodeEvent struct {
	Type      string                 `json:"type"`
	Subtype   string                 `json:"subtype,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
	Result    string                 `json:"result,omitempty"`
	Message   *qwencodeAssistant     `json:"message,omitempty"`
	Usage     *qwencodeOfficialUsage `json:"usage,omitempty"`
	Error     *qwencodeOfficialError `json:"error,omitempty"`
}

type qwencodeAssistant struct {
	Content []qwencodeContentBlock `json:"content,omitempty"`
	Usage   *qwencodeOfficialUsage `json:"usage,omitempty"`
}

type qwencodeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   any             `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type qwencodeOfficialUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
}

func (u qwencodeOfficialUsage) tokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

type qwencodeOfficialError struct {
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
		case "system":
			if event.Subtype == "init" {
				trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			}
		case "assistant":
			if event.Message == nil {
				continue
			}
			for _, block := range event.Message.Content {
				switch block.Type {
				case "text":
					if block.Text != "" {
						output.WriteString(block.Text)
						trySend(ch, Message{Type: MessageText, Content: block.Text})
					}
				case "thinking":
					if block.Thinking != "" {
						trySend(ch, Message{Type: MessageThinking, Content: block.Thinking})
					}
				case "tool_use":
					var input map[string]any
					if len(block.Input) > 0 {
						_ = json.Unmarshal(block.Input, &input)
					}
					trySend(ch, Message{Type: MessageToolUse, Tool: block.Name, CallID: block.ID, Input: input})
				case "tool_result":
					trySend(ch, Message{Type: MessageToolResult, CallID: block.ToolUseID, Output: qwencodeExtractToolOutput(block.Content)})
				}
			}
		case "result":
			if event.Usage != nil {
				usage = event.Usage.tokenUsage()
			}
			if event.IsError {
				finalStatus = "failed"
				if event.Error != nil {
					finalError = event.Error.Message
				}
				if finalError == "" {
					finalError = event.Result
				}
				if finalError == "" {
					finalError = "unknown qwen error"
				}
				trySend(ch, Message{Type: MessageError, Content: finalError})
			} else if output.Len() == 0 && event.Result != "" {
				output.WriteString(event.Result)
				trySend(ch, Message{Type: MessageText, Content: event.Result})
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("qwen stdout scanner error", "error", scanErr)
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
