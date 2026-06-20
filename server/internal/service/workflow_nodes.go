package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/utils/maps"

	"github.com/multica-ai/multica/server/internal/llm"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentLLMNode delegates LLM calls to a Rimedeck Agent. Primary path:
// creates a chat session + task so the agent's runtime handles the call.
// Fallback: direct LLM API call if no runtime available.
type AgentLLMNode struct {
	Config AgentLLMNodeConfig
}

type AgentLLMNodeConfig struct {
	PromptTemplate string  `json:"promptTemplate"`
	MaxTokens      int     `json:"maxTokens"`
	Temperature    float64 `json:"temperature"`
}

func (n *AgentLLMNode) Type() string   { return "agentLLM" }
func (n *AgentLLMNode) New() types.Node { return &AgentLLMNode{} }
func (n *AgentLLMNode) Init(_ types.Config, c types.Configuration) error {
	return maps.Map2Struct(c, &n.Config)
}
func (n *AgentLLMNode) Destroy() {}

func (n *AgentLLMNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	agentID := msg.Metadata.GetValue("agent_id")
	if agentID == "" {
		ctx.TellFailure(msg, fmt.Errorf("agentLLM: no agent bound"))
		return
	}

	prompt := strings.ReplaceAll(n.Config.PromptTemplate, "{{.data}}", msg.Data.Get())
	prompt = strings.ReplaceAll(prompt, "{{.msg}}", msg.Data.Get())
	if prompt == "" {
		prompt = msg.Data.Get()
	}

	// Path 1: delegate to agent via chat task
	var delegationErr error
	if workflowTaskSvc != nil && workflowQueries != nil {
		triggeredBy := msg.Metadata.GetValue("triggered_by")
		content, err := delegateToAgent(agentID, triggeredBy, prompt)
		if err == nil {
			msg.Data.Set(content)
			ctx.TellSuccess(msg)
			return
		}
		delegationErr = err
		slog.Warn("agentLLM: delegation failed, trying fallback", "agent_id", agentID, "error", err)
	}
	// Path 2: direct LLM API call (fallback)
	apiKey := msg.Metadata.GetValue("agent_api_key")
	model := msg.Metadata.GetValue("agent_model")
	systemPrompt := msg.Metadata.GetValue("agent_system_prompt")
	baseURL := msg.Metadata.GetValue("agent_base_url")

	if apiKey == "" {
		errDetail := "no_delegation_path"
		if delegationErr != nil {
			errDetail = fmt.Sprintf("delegation_failed: %v", delegationErr)
		} else if workflowTaskSvc == nil {
			errDetail = "workflow_task_service_not_wired"
		}
		slog.Warn("agentLLM: no available execution path",
			"agent_id", agentID, "error", errDetail)
		result := map[string]any{
			"content":  fmt.Sprintf("[Agent %s] %s. Configure agent runtime or add API key to custom_env.", agentID, errDetail),
			"agent_id": agentID,
			"error":    errDetail,
		}
		resultJSON, _ := json.Marshal(result)
		msg.Data.Set(string(resultJSON))
		ctx.TellSuccess(msg)
		return
	}

	if model == "" {
		model = "gpt-4o-mini"
	}
	provider := llm.DetectProvider(model)
	resp, err := llm.Complete(context.Background(), llm.Request{
		Provider: provider, Model: model, SystemPrompt: systemPrompt,
		UserPrompt: prompt, MaxTokens: n.Config.MaxTokens,
		Temperature: n.Config.Temperature, APIKey: apiKey, BaseURL: baseURL,
	})
	if err != nil {
		ctx.TellFailure(msg, fmt.Errorf("agentLLM: LLM API failed: %w", err))
		return
	}
	result := map[string]any{
		"content": resp.Content, "model": resp.Model,
		"input_tokens": resp.InputTokens, "output_tokens": resp.OutputTokens,
		"agent_id": agentID,
	}
	resultJSON, _ := json.Marshal(result)
	msg.Data.Set(string(resultJSON))
	msg.Metadata.PutValue("last_tokens_used", fmt.Sprintf("%d", resp.InputTokens+resp.OutputTokens))
	ctx.TellSuccess(msg)
}

// delegateToAgent creates a chat session, sends the prompt, enqueues a task,
// and polls for the agent's reply.
// delegateToAgent returns the agent's raw reply content as a string.
func delegateToAgent(agentIDStr, triggeredByStr, prompt string) (string, error) {
	bgCtx := context.Background()
	q := workflowQueries
	tSvc := workflowTaskSvc

	agentUUID := parseHexUUID(agentIDStr)
	if !agentUUID.Valid {
		return "", fmt.Errorf("invalid agent_id: %s", agentIDStr)
	}

	creatorUUID := parseHexUUID(triggeredByStr)

	agent, err := q.GetAgent(bgCtx, agentUUID)
	if err != nil {
		return "", fmt.Errorf("load agent: %w", err)
	}
	if !agent.RuntimeID.Valid {
		return "", fmt.Errorf("agent has no runtime")
	}

	// Use triggered_by user as session creator; fall back to agent owner.
	if !creatorUUID.Valid {
		creatorUUID = agent.OwnerID
	}
	if !creatorUUID.Valid {
		return "", fmt.Errorf("no valid creator for chat session")
	}

	session, err := q.CreateChatSession(bgCtx, db.CreateChatSessionParams{
		WorkspaceID: agent.WorkspaceID,
		AgentID:     agentUUID,
		CreatorID:   creatorUUID,
		Title:       "workflow-llm-" + time.Now().Format("20060102-150405"),
	})
	if err != nil {
		return "", fmt.Errorf("create chat session: %w", err)
	}

	_, err = q.CreateChatMessage(bgCtx, db.CreateChatMessageParams{
		ChatSessionID: session.ID,
		Role:          "user",
		Content:       prompt,
	})
	if err != nil {
		return "", fmt.Errorf("create user message: %w", err)
	}

	task, err := tSvc.EnqueueChatTask(bgCtx, session, pgtype.UUID{})
	if err != nil {
		return "", fmt.Errorf("enqueue chat task: %w", err)
	}

	slog.Info("agentLLM: task enqueued, polling for reply",
		"agent_id", agentIDStr, "task_id", uuidStr(task.ID))

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)

		t, err := q.GetAgentTask(bgCtx, task.ID)
		if err != nil {
			continue
		}
		if t.Status != "completed" && t.Status != "failed" {
			continue
		}

		messages, err := q.ListChatMessages(bgCtx, session.ID)
		if err != nil {
			return "", fmt.Errorf("list messages: %w", err)
		}
		// Return the last assistant message content directly — no wrapping.
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" {
				return messages[i].Content, nil
			}
		}
		if t.Status == "failed" {
			reason := "unknown"
			if t.FailureReason.Valid {
				reason = t.FailureReason.String
			}
			return "", fmt.Errorf("agent task failed: %s", reason)
		}
		return "", nil
	}
	return "", fmt.Errorf("agent task timed out (2min)")
}

func parseHexUUID(s string) pgtype.UUID {
	clean := strings.ReplaceAll(s, "-", "")
	if len(clean) != 32 {
		return pgtype.UUID{}
	}
	var u pgtype.UUID
	for i := 0; i < 16; i++ {
		hi := unhex(clean[i*2])
		lo := unhex(clean[i*2+1])
		if hi < 0 || lo < 0 {
			return pgtype.UUID{}
		}
		u.Bytes[i] = byte(hi<<4 | lo)
	}
	u.Valid = true
	return u
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c - 'a' + 10)
	case c >= 'A' && c <= 'F':
		return int(c - 'A' + 10)
	default:
		return -1
	}
}

// DocGenerateNode renders markdown from template + data.
type DocGenerateNode struct {
	Config DocGenerateNodeConfig
}

type DocGenerateNodeConfig struct {
	Format          string `json:"format"`
	ContentTemplate string `json:"contentTemplate"`
}

func (n *DocGenerateNode) Type() string   { return "docGenerate" }
func (n *DocGenerateNode) New() types.Node { return &DocGenerateNode{} }
func (n *DocGenerateNode) Init(_ types.Config, c types.Configuration) error {
	return maps.Map2Struct(c, &n.Config)
}
func (n *DocGenerateNode) Destroy() {}

func (n *DocGenerateNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	content := msg.Data.Get()
	if n.Config.ContentTemplate != "" {
		content = strings.ReplaceAll(n.Config.ContentTemplate, "{{.data}}", content)
	}
	result := map[string]any{
		"format": n.Config.Format, "content": content, "length": len(content),
	}
	resultJSON, _ := json.Marshal(result)
	msg.Data.Set(string(resultJSON))
	ctx.TellSuccess(msg)
}
