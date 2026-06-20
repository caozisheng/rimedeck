package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/llm"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WorkflowService manages workflow execution via the embedded RuleGo engine.
type WorkflowService struct {
	Queries   *db.Queries
	TxStarter interface{ Begin(ctx context.Context) (pgx.Tx, error) }
	Bus       *events.Bus
	TaskSvc   *TaskService

	mu      sync.Mutex
	engines map[string]types.RuleEngine // runID → engine (for cancellation)
}

// Package-level refs for AgentLLMNode to access services (RuleGo nodes
// can't receive dependency injection — they're created by the registry).
var workflowTaskSvc *TaskService
var workflowQueries *db.Queries

// NewWorkflowService creates a WorkflowService and registers custom nodes.
func NewWorkflowService(
	queries *db.Queries,
	txStarter interface{ Begin(ctx context.Context) (pgx.Tx, error) },
	bus *events.Bus,
) *WorkflowService {
	workflowQueries = queries

	for _, node := range []types.Node{
		&AgentLLMNode{},
		&DocGenerateNode{},
		&WebScrapeNode{},
		&RssFetchNode{},
		&SpreadsheetNode{},
	} {
		if err := rulego.Registry.Register(node); err != nil {
			slog.Warn("workflow node registration failed (may be duplicate)", "type", node.Type(), "error", err)
		} else {
			slog.Info("workflow node registered", "type", node.Type())
		}
	}

	return &WorkflowService{
		Queries:   queries,
		TxStarter: txStarter,
		Bus:       bus,
		engines:   make(map[string]types.RuleEngine),
	}
}

// SetTaskService wires TaskService after construction (avoids circular init).
func (s *WorkflowService) SetTaskService(taskSvc *TaskService) {
	s.TaskSvc = taskSvc
	workflowTaskSvc = taskSvc
}

// TriggerRunParams contains everything needed to start a workflow execution.
type TriggerRunParams struct {
	WorkflowID     pgtype.UUID
	WorkspaceID    pgtype.UUID
	AgentID        pgtype.UUID
	Source         string
	Input          json.RawMessage
	TriggeredBy    pgtype.UUID
	IssueID        pgtype.UUID
	AutopilotRunID pgtype.UUID
}

// TriggerRun creates a workflow_run record and starts asynchronous DAG execution.
func (s *WorkflowService) TriggerRun(ctx context.Context, p TriggerRunParams) (db.WorkflowRun, error) {

	wf, err := s.Queries.GetWorkflow(ctx, p.WorkflowID)
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("load workflow: %w", err)
	}

	totalNodes := countGraphNodes(wf.Graph)

	run, err := s.Queries.CreateWorkflowRun(ctx, db.CreateWorkflowRunParams{
		WorkflowID:   p.WorkflowID,
		WorkspaceID:  p.WorkspaceID,
		AgentID:      p.AgentID,
		Source:       p.Source,
		TriggerInput: p.Input,
		Status:       "running",
		TotalNodes:   int32(totalNodes),
		TriggeredBy:  p.TriggeredBy,
	})
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("create run: %w", err)
	}

	go s.executeRun(run, wf, p.AgentID)

	return run, nil
}

// executeRun runs the RuleGo engine for a single workflow run.
func (s *WorkflowService) executeRun(run db.WorkflowRun, wf db.Workflow, agentID pgtype.UUID) {
	runID := uuidStr(run.ID)
	ctx := context.Background()
	// Debug: write to file since slog may not show in desktop app

	// Catch panics so a crashing node doesn't leak goroutines or locks.
	defer func() {
		if r := recover(); r != nil {
			s.failRun(ctx, run.ID, fmt.Sprintf("internal error: %v", r))
		}
	}()

	completedCount := 0
	hasFailedNode := false
	totalTokens := int64(0)

	debugCallback := func(ruleChainId string, flowType string, nodeId string, msg types.RuleMsg, relationType string, err error) {
		msgData := msg.Data.Get()
		if len(msgData) > 10000 {
			msgData = msgData[:10000] + "...(truncated)"
		}
		dataJSON := json.RawMessage(fmt.Sprintf(`{"data":%q}`, msgData))

		if flowType == "IN" {
			// Node starting — create execution record with inputs.
			exec, createErr := s.Queries.CreateNodeExecution(ctx, db.CreateNodeExecutionParams{
				RunID:    run.ID,
				NodeID:   nodeId,
				NodeType: relationType,
				Status:   "running",
			})
			if createErr == nil {
				_ = s.Queries.UpdateNodeExecution(ctx, db.UpdateNodeExecutionParams{
					ID:      exec.ID,
					Status:  "running",
					Inputs:  dataJSON,
					Outputs: nil,
					Error:   pgtype.Text{},
				})
			}
			return
		}

		if flowType == "OUT" || err != nil {
			completedCount++
			status := "completed"
			var errText pgtype.Text
			if err != nil {
				status = "failed"
				hasFailedNode = true
				errText = pgtype.Text{String: err.Error(), Valid: true}
			}

			// Update existing running record with outputs, or create new one.
			executions, _ := s.Queries.ListNodeExecutionsByRun(ctx, run.ID)
			updated := false
			for _, e := range executions {
				if e.NodeID == nodeId && e.Status == "running" {
					_ = s.Queries.UpdateNodeExecution(ctx, db.UpdateNodeExecutionParams{
						ID:      e.ID,
						Status:  status,
						Inputs:  e.Inputs,
						Outputs: dataJSON,
						Error:   errText,
					})
					updated = true
					break
				}
			}
			if !updated {
				exec, _ := s.Queries.CreateNodeExecution(ctx, db.CreateNodeExecutionParams{
					RunID: run.ID, NodeID: nodeId, NodeType: relationType, Status: status,
				})
				_ = s.Queries.UpdateNodeExecution(ctx, db.UpdateNodeExecutionParams{
					ID: exec.ID, Status: status, Outputs: dataJSON, Error: errText,
				})
			}

			// Update run progress.
			_ = s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
				ID:             run.ID,
				Status:         "running",
				CurrentNodeID:  pgtype.Text{String: nodeId, Valid: true},
				CompletedNodes: int32(completedCount),
				Output:         nil,
				Error:          errText,
				TotalTokens:    totalTokens,
				TotalCost:      pgtype.Numeric{},
			})

			if tokStr := msg.Metadata.GetValue("last_tokens_used"); tokStr != "" {
				if tok, parseErr := strconv.ParseInt(tokStr, 10, 64); parseErr == nil {
					totalTokens += tok
				}
			}

			if s.Bus != nil {
				s.Bus.Publish(events.Event{
					Type:        "workflow_run:node_completed",
					WorkspaceID: uuidStr(run.WorkspaceID),
					Payload: map[string]any{
						"run_id": runID, "node_id": nodeId, "status": status,
						"completed_nodes": completedCount, "total_nodes": run.TotalNodes,
						"error": errText.String,
					},
				})
			}
		}
	}

	// Force debugMode on the rule chain so OnDebug fires for every node,
	// enabling per-node progress tracking. Also patch the graph in-memory
	// (does not modify the DB copy).
	graphWithDebug := forceDebugMode(wf.Graph)
	// Use rulego.NewConfig (not types.NewConfig) — it sets Parser, Registry, Cache defaults.
	config := rulego.NewConfig(types.WithOnDebug(debugCallback))
	engine, err := rulego.New(runID, graphWithDebug, rulego.WithConfig(config))
	if err != nil {
		s.failRun(ctx, run.ID, fmt.Sprintf("engine init: %v", err))
		return
	}

	slog.Info("workflow engine created, starting execution",
		"run_id", runID,
		"total_nodes", run.TotalNodes,
		"graph_size", len(wf.Graph),
	)

	// Track for cancellation.
	s.mu.Lock()
	s.engines[runID] = engine
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.engines, runID)
		s.mu.Unlock()
		engine.Stop(ctx)
	}()

	// Build message metadata with Agent context.
	metaData := types.NewMetadata()
	metaData.PutValue("agent_id", uuidStr(agentID))
	metaData.PutValue("run_id", runID)
	metaData.PutValue("workspace_id", uuidStr(run.WorkspaceID))
	metaData.PutValue("triggered_by", uuidStr(run.TriggeredBy))

	// Load agent record for model, instructions, and custom_env (API keys).
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Warn("agentLLM: failed to load agent, LLM calls will use mock",
			"agent_id", uuidStr(agentID), "error", err)
	} else {
		// Inject model
		if agent.Model.Valid && agent.Model.String != "" {
			metaData.PutValue("agent_model", agent.Model.String)
		}

		// Build system prompt from instructions + skills
		var systemPrompt strings.Builder
		if agent.Instructions != "" {
			systemPrompt.WriteString(agent.Instructions)
			systemPrompt.WriteString("\n\n")
		}
		skills, _ := s.Queries.ListAgentSkills(ctx, agentID)
		for _, skill := range skills {
			fmt.Fprintf(&systemPrompt, "## Skill: %s\n\n%s\n\n", skill.Name, skill.Content)
		}
		if systemPrompt.Len() > 0 {
			metaData.PutValue("agent_system_prompt", systemPrompt.String())
		}

		// Extract API key and base URL from custom_env, then fall back to system env vars.
		var customEnv map[string]string
		if len(agent.CustomEnv) > 0 {
			_ = json.Unmarshal(agent.CustomEnv, &customEnv)
		}
		if customEnv == nil {
			customEnv = make(map[string]string)
		}
		provider := llm.DetectProvider(metaData.GetValue("agent_model"))
		apiKey := llm.ExtractAPIKey(customEnv, provider)
		// Fallback: check system environment variables if agent custom_env has no key.
		if apiKey == "" {
			for _, envName := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
				if v := os.Getenv(envName); v != "" {
					apiKey = v
					break
				}
			}
		}
		if apiKey != "" {
			metaData.PutValue("agent_api_key", apiKey)
		}
		baseURL := llm.ExtractBaseURL(customEnv, provider)
		if baseURL == "" {
			for _, envName := range []string{"ANTHROPIC_BASE_URL", "OPENAI_BASE_URL"} {
				if v := os.Getenv(envName); v != "" {
					baseURL = v
					break
				}
			}
		}
		if baseURL != "" {
			metaData.PutValue("agent_base_url", baseURL)
		}

		// Check if agent's runtime is online.
		if agent.RuntimeID.Valid {
			rt, rtErr := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
			if rtErr == nil && rt.Status == "online" {
				metaData.PutValue("agent_runtime_online", "true")
			}
		}
	}


	inputStr := "{}"
	if run.TriggerInput != nil {
		inputStr = string(run.TriggerInput)
	}

	msg := types.NewMsg(0, "WORKFLOW_TRIGGER", types.JSON, metaData, inputStr)

	// Execute with a 5-minute timeout to prevent indefinite hangs.
	done := make(chan struct{})
	go func() {
		engine.OnMsgAndWait(msg, types.WithEndFunc(func(_ types.RuleContext, outMsg types.RuleMsg, endErr error) {
			if endErr != nil {
				slog.Error("workflow execution ended with error", "run_id", runID, "error", endErr)
			} else {
				slog.Info("workflow execution completed", "run_id", runID, "output_length", outMsg.Data.Len())
			}
		}))
		close(done)
	}()

	select {
	case <-done:
		slog.Info("workflow execution done", "run_id", runID, "completed_nodes", completedCount)
	case <-time.After(5 * time.Minute):
		slog.Warn("workflow execution timed out", "run_id", runID)
		s.failRun(ctx, run.ID, "execution timed out after 5 minutes")
		return
	}

	// Mark run as completed (or failed if any node errored out).
	// Skip only if already cancelled by user.
	currentRun, err := s.Queries.GetWorkflowRun(ctx, run.ID)
	if err == nil && currentRun.Status == "cancelled" {
		return
	}

	finalStatus := "completed"
	if hasFailedNode {
		finalStatus = "failed"
	}
	outputData := msg.Data.Get()
	_ = s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:             run.ID,
		Status:         finalStatus,
		CurrentNodeID:  pgtype.Text{},
		CompletedNodes: int32(completedCount),
		Output:         json.RawMessage(outputData),
		Error:          pgtype.Text{},
		TotalTokens:    totalTokens,
		TotalCost:      pgtype.Numeric{},
	})

	if s.Bus != nil {
		s.Bus.Publish(events.Event{
			Type:        "workflow_run:completed",
			WorkspaceID: uuidStr(run.WorkspaceID),
			Payload:     map[string]any{"run_id": runID, "status": finalStatus},
		})
	}
}

// CancelRun cancels a running workflow.
func (s *WorkflowService) CancelRun(ctx context.Context, runID string) {
	s.mu.Lock()
	engine, ok := s.engines[runID]
	s.mu.Unlock()
	if ok {
		engine.Stop(ctx)
	}
}

func (s *WorkflowService) failRun(ctx context.Context, runID pgtype.UUID, errMsg string) {
	_ = s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:     runID,
		Status: "failed",
		Error:  pgtype.Text{String: errMsg, Valid: true},
	})
}

// countGraphNodes parses the RuleGo DSL JSON and returns the number of nodes.
func countGraphNodes(graph []byte) int {
	var g struct {
		Metadata struct {
			Nodes []json.RawMessage `json:"nodes"`
		} `json:"metadata"`
	}
	if json.Unmarshal(graph, &g) != nil {
		return 0
	}
	return len(g.Metadata.Nodes)
}

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// forceDebugMode patches the RuleGo DSL JSON to set debugMode=true on the
// ruleChain level, which overrides all nodes' debugMode so OnDebug fires
// for every node during execution.
func forceDebugMode(graph []byte) []byte {
	var g map[string]json.RawMessage
	if json.Unmarshal(graph, &g) != nil {
		return graph
	}
	var chain map[string]json.RawMessage
	if json.Unmarshal(g["ruleChain"], &chain) != nil {
		return graph
	}
	chain["debugMode"] = json.RawMessage(`true`)
	chainBytes, _ := json.Marshal(chain)
	g["ruleChain"] = chainBytes
	result, _ := json.Marshal(g)
	return result
}
