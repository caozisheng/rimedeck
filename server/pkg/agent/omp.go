package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ompBackend implements Backend by spawning the OMP CLI (oh-my-pi, a fork
// of Pi) in non-interactive JSON mode. OMP shares the same event stream
// protocol and CLI flags as Pi, so we reuse all the pi event parsing
// helpers (piStreamEvent, drainPiTextBuffer, etc.).
type ompBackend struct {
	cfg Config
}

func (b *ompBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "omp"
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("omp executable not found at %q: %w", execName, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}

	sessionPath := opts.ResumeSessionID
	if sessionPath == "" {
		p, err := newOmpSessionPath()
		if err != nil {
			return nil, fmt.Errorf("omp session path: %w", err)
		}
		sessionPath = p
	}
	if err := ensurePiSessionFile(sessionPath); err != nil {
		return nil, fmt.Errorf("omp session file: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildOmpArgs(prompt, sessionPath, opts, b.cfg.Logger)
	argv0, cmdArgs := chooseOmpInvocation(execName, lookedUp, args, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, argv0, cmdArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", argv0, "args", cmdArgs)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omp stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omp stdin pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[omp:stderr] ")

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return nil, fmt.Errorf("start omp: %w", err)
	}
	_ = stdin.Close()

	b.cfg.Logger.Info("omp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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
		var output strings.Builder
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)
		var textBuffer strings.Builder

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var evt piStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "agent_start":
				trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

			case "message_update":
				if evt.AssistantMessageEvent == nil {
					continue
				}
				switch evt.AssistantMessageEvent.Type {
				case "text_delta":
					if d := drainPiTextBuffer(&textBuffer, evt.AssistantMessageEvent.Delta); d != "" {
						output.WriteString(d)
						trySend(msgCh, Message{Type: MessageText, Content: d})
					}
				case "thinking_delta":
					if d := evt.AssistantMessageEvent.Delta; d != "" {
						trySend(msgCh, Message{Type: MessageThinking, Content: d})
					}
				}

			case "tool_execution_start":
				var params map[string]any
				if len(evt.Args) > 0 {
					_ = json.Unmarshal(evt.Args, &params)
				}
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.ToolName,
					CallID: evt.ToolCallID,
					Input:  params,
				})

			case "tool_execution_end":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: evt.ToolCallID,
					Output: decodePiResult(evt.Result),
				})

			case "turn_end":
				if msg := decodePiMessage(evt.Message); msg != nil && msg.Usage != nil {
					model := msg.Model
					if model == "" {
						model = opts.Model
					}
					if model == "" {
						model = "unknown"
					}
					u := usage[model]
					u.InputTokens += msg.Usage.Input
					u.OutputTokens += msg.Usage.Output
					u.CacheReadTokens += msg.Usage.CacheRead
					u.CacheWriteTokens += msg.Usage.CacheWrite
					usage[model] = u
				}

			case "error":
				errText := decodePiString(evt.Message)
				trySend(msgCh, Message{Type: MessageError, Content: errText})
				if finalStatus == "completed" {
					finalStatus = "failed"
					finalError = errText
				}

			case "auto_retry_end":
				if !evt.Success && finalStatus == "completed" {
					finalStatus = "failed"
					if evt.FinalError != "" {
						finalError = evt.FinalError
					} else {
						finalError = "omp exhausted automatic retries"
					}
				}
			}
		}
		if d := flushPiTextBuffer(&textBuffer); d != "" {
			output.WriteString(d)
			trySend(msgCh, Message{Type: MessageText, Content: d})
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("omp timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("omp exited with error: %v", waitErr)
		}

		b.cfg.Logger.Info("omp finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionPath,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// ── Arg builder ──

var ompBlockedArgs = map[string]blockedArgMode{
	"-p":        blockedStandalone,
	"--print":   blockedStandalone,
	"--mode":    blockedWithValue,
	"--session": blockedWithValue,
}

func buildOmpArgs(prompt, sessionPath string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--mode", "json",
	}
	if sessionPath != "" {
		args = append(args, "--session", sessionPath)
	}
	if opts.Model != "" {
		provider, model := splitPiModel(opts.Model)
		if provider != "" {
			args = append(args, "--provider", provider)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, ompBlockedArgs, logger)...)
	args = append(args, prompt)
	return args
}

// ── Session path ──

func ompSessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".rimedeck", "omp-sessions"), nil
}

func newOmpSessionPath() (string, error) {
	dir, err := ompSessionDir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.jsonl", time.Now().UTC().Format("20060102T150405.000000000"))
	return filepath.Join(dir, name), nil
}

// OmpSessionDir exposes ompSessionDir to other packages in this module.
func OmpSessionDir() (string, error) {
	return ompSessionDir()
}
