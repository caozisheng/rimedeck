package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

// --- JSON-RPC types for the MCP bridge ---

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any         `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP tool schema types ---

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema mcpInputSchema  `json:"inputSchema"`
}

type mcpInputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]mcpProperty    `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

type mcpProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// --- tools/call param types ---

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type triggerSOPArgs struct {
	SOPName string          `json:"sop_name"`
	Input   json.RawMessage `json:"input,omitempty"`
}

// --- MCP content types ---

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// --- Workflow summary for list responses ---

type sopSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// SOPMCPBridge handles MCP JSON-RPC requests from agent runtimes.
// Route: POST /mcp/sops/{agentId}
func (h *Handler) SOPMCPBridge(w http.ResponseWriter, r *http.Request) {
	agentIDStr := chi.URLParam(r, "agentId")

	agentUUID, ok := parseUUIDOrBadRequest(w, agentIDStr, "agentId")
	if !ok {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		})
		return
	}

	switch req.Method {
	case "tools/list":
		h.mcpToolsList(w, r, req, agentUUID)
	case "tools/call":
		h.mcpToolsCall(w, r, req, agentUUID)
	default:
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		})
	}
}

// mcpToolsList returns MCP tool definitions for all workflows assigned to the agent.
func (h *Handler) mcpToolsList(w http.ResponseWriter, r *http.Request, req jsonRPCRequest, agentUUID pgtype.UUID) {
	workflows, err := h.Queries.ListAgentWorkflows(r.Context(), agentUUID)
	if err != nil {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -1, Message: "failed to list workflows: " + err.Error()},
		})
		return
	}

	// Collect published workflow names for the trigger_sop enum.
	var sopNames []string
	for _, wf := range workflows {
		if wf.Status == "published" {
			sopNames = append(sopNames, wf.Name)
		}
	}

	var tools []mcpTool

	// list_sops tool — always available.
	tools = append(tools, mcpTool{
		Name:        "list_sops",
		Description: "List all SOPs (Standard Operating Procedures) available to this agent.",
		InputSchema: mcpInputSchema{
			Type:       "object",
			Properties: map[string]mcpProperty{},
		},
	})

	// trigger_sop tool — only when there are published workflows.
	if len(sopNames) > 0 {
		tools = append(tools, mcpTool{
			Name:        "trigger_sop",
			Description: "Trigger a Standard Operating Procedure (SOP) by name and wait for it to complete.",
			InputSchema: mcpInputSchema{
				Type: "object",
				Properties: map[string]mcpProperty{
					"sop_name": {
						Type:        "string",
						Description: "Name of the SOP to trigger.",
						Enum:        sopNames,
					},
					"input": {
						Type:        "string",
						Description: "Optional JSON input to pass to the SOP.",
					},
				},
				Required: []string{"sop_name"},
			},
		})
	}

	writeJSON(w, http.StatusOK, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": tools},
	})
}

// mcpToolsCall dispatches a tool call to the appropriate handler.
func (h *Handler) mcpToolsCall(w http.ResponseWriter, r *http.Request, req jsonRPCRequest, agentUUID pgtype.UUID) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "invalid params: " + err.Error()},
		})
		return
	}

	switch params.Name {
	case "list_sops":
		h.mcpListSOPs(w, r, req, agentUUID)
	case "trigger_sop":
		h.mcpTriggerSOP(w, r, req, agentUUID, params.Arguments)
	default:
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: fmt.Sprintf("unknown tool: %s", params.Name)},
		})
	}
}

// mcpListSOPs returns a summary of all workflows assigned to the agent.
func (h *Handler) mcpListSOPs(w http.ResponseWriter, r *http.Request, req jsonRPCRequest, agentUUID pgtype.UUID) {
	workflows, err := h.Queries.ListAgentWorkflows(r.Context(), agentUUID)
	if err != nil {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: "failed to list SOPs: " + err.Error()}}, IsError: true},
		})
		return
	}

	summaries := make([]sopSummary, 0, len(workflows))
	for _, wf := range workflows {
		summaries = append(summaries, sopSummary{
			Name:        wf.Name,
			Description: wf.Description,
			Status:      wf.Status,
		})
	}

	data, _ := json.Marshal(summaries)
	writeJSON(w, http.StatusOK, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: string(data)}}},
	})
}

// mcpTriggerSOP matches a workflow by name, triggers it synchronously, and returns the output.
func (h *Handler) mcpTriggerSOP(w http.ResponseWriter, r *http.Request, req jsonRPCRequest, agentUUID pgtype.UUID, rawArgs json.RawMessage) {
	var args triggerSOPArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: "invalid arguments: " + err.Error()}}, IsError: true},
		})
		return
	}

	if args.SOPName == "" {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: "sop_name is required"}}, IsError: true},
		})
		return
	}

	// Look up workflows assigned to this agent and find the one matching by name.
	workflows, err := h.Queries.ListAgentWorkflows(r.Context(), agentUUID)
	if err != nil {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: "failed to list workflows: " + err.Error()}}, IsError: true},
		})
		return
	}

	var matchedID pgtype.UUID
	var matchedWorkspaceID pgtype.UUID
	found := false
	for _, wf := range workflows {
		if wf.Name == args.SOPName && wf.Status == "published" {
			matchedID = wf.ID
			matchedWorkspaceID = wf.WorkspaceID
			found = true
			break
		}
	}

	if !found {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("SOP %q not found or not published", args.SOPName)}}, IsError: true},
		})
		return
	}

	// Prepare input — parse the string input as JSON, or use empty object.
	triggerInput := json.RawMessage(`{}`)
	if len(args.Input) > 0 {
		// Input comes as a JSON string in args; try to use it directly if it's
		// valid JSON, otherwise wrap it as a string value.
		var inputCheck json.RawMessage
		if json.Unmarshal(args.Input, &inputCheck) == nil {
			triggerInput = args.Input
		}
	}

	if h.WorkflowService == nil {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: "workflow engine not available"}}, IsError: true},
		})
		return
	}

	run, err := h.WorkflowService.TriggerRunSync(r.Context(), service.TriggerRunParams{
		WorkflowID:  matchedID,
		WorkspaceID: matchedWorkspaceID,
		AgentID:     agentUUID,
		Source:      "mcp",
		Input:       triggerInput,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: "failed to trigger SOP: " + err.Error()}}, IsError: true},
		})
		return
	}

	// Build output text from run result.
	output := string(run.Output)
	if output == "" {
		output = fmt.Sprintf("SOP %q completed with status: %s", args.SOPName, run.Status)
	}
	if run.Status == "failed" {
		errText := ""
		if run.Error.Valid {
			errText = run.Error.String
		}
		writeJSON(w, http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("SOP failed: %s", errText)}}, IsError: true},
		})
		return
	}

	writeJSON(w, http.StatusOK, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  mcpToolResult{Content: []mcpContent{{Type: "text", Text: output}}},
	})
}
