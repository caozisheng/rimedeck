package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/workflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// --- Response structs ---

type WorkflowResponse struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspace_id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Icon        string           `json:"icon"`
	Category    string           `json:"category"`
	Graph       json.RawMessage  `json:"graph"`
	Status      string           `json:"status"`
	Version     int32            `json:"version"`
	CreatedBy   *string          `json:"created_by"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
	PublishedAt *string          `json:"published_at"`
}

// WorkflowSummaryResponse is the list-endpoint shape: everything WorkflowResponse
// has except `graph`. Graph payloads can be large and are unnecessary for list views.
type WorkflowSummaryResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Icon        string  `json:"icon"`
	Category    string  `json:"category"`
	Status      string  `json:"status"`
	Version     int32   `json:"version"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	PublishedAt *string `json:"published_at"`
}

// AgentWorkflowSummary is the narrow shape used for workflows embedded in
// an Agent payload, mirroring AgentSkillSummary.
type AgentWorkflowSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Category    string `json:"category"`
	Status      string `json:"status"`
}

type WorkflowRunResponse struct {
	ID             string          `json:"id"`
	SopID          string          `json:"sop_id"`
	WorkspaceID    string          `json:"workspace_id"`
	AgentID        string          `json:"agent_id"`
	Source         string          `json:"source"`
	TriggerInput   json.RawMessage `json:"trigger_input"`
	Status         string          `json:"status"`
	TotalNodes     int32           `json:"total_nodes"`
	CompletedNodes int32           `json:"completed_nodes"`
	CurrentNodeID  *string         `json:"current_node_id"`
	Output         json.RawMessage `json:"output"`
	Error          *string         `json:"error"`
	IssueID        *string         `json:"issue_id"`
	AutopilotRunID *string         `json:"autopilot_run_id"`
	TriggeredBy    *string         `json:"triggered_by"`
	StartedAt      *string         `json:"started_at"`
	CompletedAt    *string         `json:"completed_at"`
	CreatedAt      string          `json:"created_at"`
	TotalTokens    int64           `json:"total_tokens"`
	TotalCost      *string         `json:"total_cost"`

	// Embedded only in the single-run detail endpoint.
	NodeExecutions []WorkflowNodeExecutionResponse `json:"node_executions,omitempty"`
}

type WorkflowNodeExecutionResponse struct {
	ID          string          `json:"id"`
	RunID       string          `json:"run_id"`
	NodeID      string          `json:"node_id"`
	NodeType    string          `json:"node_type"`
	Status      string          `json:"status"`
	Inputs      json.RawMessage `json:"inputs"`
	Outputs     json.RawMessage `json:"outputs"`
	Error       *string         `json:"error"`
	StartedAt   *string         `json:"started_at"`
	CompletedAt *string         `json:"completed_at"`
	TokensUsed  int64           `json:"tokens_used"`
	DurationMs  int32           `json:"duration_ms"`
}

// --- Request structs ---

type CreateWorkflowRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Icon        string          `json:"icon"`
	Category    string          `json:"category"`
	Graph       json.RawMessage `json:"graph"`
}

type UpdateWorkflowRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Icon        *string         `json:"icon"`
	Category    *string         `json:"category"`
	Graph       json.RawMessage `json:"graph"`
}

type TriggerWorkflowRunRequest struct {
	AgentID string          `json:"agent_id"`
	Input   json.RawMessage `json:"input"`
}

type SetAgentWorkflowsRequest struct {
	SopIDs []string `json:"sop_ids"`
}

type AddAgentWorkflowsRequest struct {
	SopIDs []string `json:"sop_ids"`
}

// --- Helpers ---

// defaultWorkflowGraph is the empty RuleGo DSL used when no graph is provided.
var defaultWorkflowGraph = json.RawMessage(`{"ruleChain":{},"metadata":{"firstNodeIndex":0,"nodes":[],"connections":[]}}`)

func toWorkflowResponse(w db.Sop) WorkflowResponse {
	graph := json.RawMessage(w.Graph)
	if len(graph) == 0 {
		graph = defaultWorkflowGraph
	}
	return WorkflowResponse{
		ID:          uuidToString(w.ID),
		WorkspaceID: uuidToString(w.WorkspaceID),
		Name:        w.Name,
		Description: w.Description,
		Icon:        w.Icon,
		Category:    w.Category,
		Graph:       graph,
		Status:      w.Status,
		Version:     w.Version,
		CreatedBy:   uuidToPtr(w.CreatedBy),
		CreatedAt:   timestampToString(w.CreatedAt),
		UpdatedAt:   timestampToString(w.UpdatedAt),
		PublishedAt: timestampToPtr(w.PublishedAt),
	}
}

func toWorkflowSummaryResponse(row db.ListWorkflowsByWorkspaceRow) WorkflowSummaryResponse {
	return WorkflowSummaryResponse{
		ID:          uuidToString(row.ID),
		WorkspaceID: uuidToString(row.WorkspaceID),
		Name:        row.Name,
		Description: row.Description,
		Icon:        row.Icon,
		Category:    row.Category,
		Status:      row.Status,
		Version:     row.Version,
		CreatedBy:   uuidToPtr(row.CreatedBy),
		CreatedAt:   timestampToString(row.CreatedAt),
		UpdatedAt:   timestampToString(row.UpdatedAt),
		PublishedAt: timestampToPtr(row.PublishedAt),
	}
}

func toWorkflowSummaryFromStatusRow(row db.ListWorkflowsByWorkspaceAndStatusRow) WorkflowSummaryResponse {
	return WorkflowSummaryResponse{
		ID:          uuidToString(row.ID),
		WorkspaceID: uuidToString(row.WorkspaceID),
		Name:        row.Name,
		Description: row.Description,
		Icon:        row.Icon,
		Category:    row.Category,
		Status:      row.Status,
		Version:     row.Version,
		CreatedBy:   uuidToPtr(row.CreatedBy),
		CreatedAt:   timestampToString(row.CreatedAt),
		UpdatedAt:   timestampToString(row.UpdatedAt),
		PublishedAt: timestampToPtr(row.PublishedAt),
	}
}

func toWorkflowSummaryFromAgentRow(row db.ListAgentWorkflowsRow) WorkflowSummaryResponse {
	return WorkflowSummaryResponse{
		ID:          uuidToString(row.ID),
		WorkspaceID: uuidToString(row.WorkspaceID),
		Name:        row.Name,
		Description: row.Description,
		Icon:        row.Icon,
		Category:    row.Category,
		Status:      row.Status,
		Version:     row.Version,
		CreatedBy:   uuidToPtr(row.CreatedBy),
		CreatedAt:   timestampToString(row.CreatedAt),
		UpdatedAt:   timestampToString(row.UpdatedAt),
		PublishedAt: timestampToPtr(row.PublishedAt),
	}
}

func numericToPtr(n pgtype.Numeric) *string {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	s := strconv.FormatFloat(f.Float64, 'f', -1, 64)
	return &s
}

func toWorkflowRunResponse(r db.SopRun) WorkflowRunResponse {
	return WorkflowRunResponse{
		ID:             uuidToString(r.ID),
		SopID:          uuidToString(r.SopID),
		WorkspaceID:    uuidToString(r.WorkspaceID),
		AgentID:        uuidToString(r.AgentID),
		Source:         r.Source,
		TriggerInput:   json.RawMessage(r.TriggerInput),
		Status:         r.Status,
		TotalNodes:     r.TotalNodes,
		CompletedNodes: r.CompletedNodes,
		CurrentNodeID:  textToPtr(r.CurrentNodeID),
		Output:         json.RawMessage(r.Output),
		Error:          textToPtr(r.Error),
		IssueID:        uuidToPtr(r.IssueID),
		AutopilotRunID: uuidToPtr(r.AutopilotRunID),
		TriggeredBy:    uuidToPtr(r.TriggeredBy),
		StartedAt:      timestampToPtr(r.StartedAt),
		CompletedAt:    timestampToPtr(r.CompletedAt),
		CreatedAt:      timestampToString(r.CreatedAt),
		TotalTokens:    r.TotalTokens,
		TotalCost:      numericToPtr(r.TotalCost),
	}
}

func toNodeExecutionResponse(e db.SopNodeExecution) WorkflowNodeExecutionResponse {
	return WorkflowNodeExecutionResponse{
		ID:          uuidToString(e.ID),
		RunID:       uuidToString(e.RunID),
		NodeID:      e.NodeID,
		NodeType:    e.NodeType,
		Status:      e.Status,
		Inputs:      json.RawMessage(e.Inputs),
		Outputs:     json.RawMessage(e.Outputs),
		Error:       textToPtr(e.Error),
		StartedAt:   timestampToPtr(e.StartedAt),
		CompletedAt: timestampToPtr(e.CompletedAt),
		TokensUsed:  e.TokensUsed,
		DurationMs:  e.DurationMs,
	}
}

func (h *Handler) loadWorkflowForUser(w http.ResponseWriter, r *http.Request, id string) (db.Sop, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Sop{}, false
	}

	workflowUUID, ok := parseUUIDOrBadRequest(w, id, "workflow id")
	if !ok {
		return db.Sop{}, false
	}

	workflow, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID:          workflowUUID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return workflow, false
	}
	return workflow, true
}

// canManageWorkflow checks whether the current user can update or delete a workflow.
// The workflow creator or workspace owner/admin can manage any workflow.
func (h *Handler) canManageWorkflow(w http.ResponseWriter, r *http.Request, workflow db.Sop) bool {
	wsID := uuidToString(workflow.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "workflow not found", "owner", "admin", "member")
	if !ok {
		return false
	}
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	isCreator := workflow.CreatedBy.Valid && uuidToString(workflow.CreatedBy) == requestUserID(r)
	if !isAdmin && !isCreator {
		writeError(w, http.StatusForbidden, "only the workflow creator or workspace owner/admin can manage this workflow")
		return false
	}
	return true
}

// countGraphNodes extracts the node count from a RuleGo DSL graph JSON blob.
// Returns 0 when the graph cannot be parsed or has no nodes.
func countGraphNodes(graph []byte) int32 {
	var g struct {
		Metadata struct {
			Nodes []json.RawMessage `json:"nodes"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(graph, &g); err != nil {
		return 0
	}
	return int32(len(g.Metadata.Nodes))
}

// --- Workflow CRUD ---

func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	statusFilter := r.URL.Query().Get("status")

	if statusFilter != "" {
		rows, err := h.Queries.ListWorkflowsByWorkspaceAndStatus(r.Context(), db.ListWorkflowsByWorkspaceAndStatusParams{
			WorkspaceID: parseUUID(workspaceID),
			Status:      statusFilter,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list workflows")
			return
		}
		resp := make([]WorkflowSummaryResponse, len(rows))
		for i, row := range rows {
			resp[i] = toWorkflowSummaryFromStatusRow(row)
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	rows, err := h.Queries.ListWorkflowsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}

	resp := make([]WorkflowSummaryResponse, len(rows))
	for i, row := range rows {
		resp[i] = toWorkflowSummaryResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workflow, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toWorkflowResponse(workflow))
}

func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(creatorID)

	var req CreateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	graph := req.Graph
	if len(graph) == 0 {
		graph = defaultWorkflowGraph
	}
	category := req.Category
	if category == "" {
		category = "general"
	}

	workflow, err := h.Queries.CreateWorkflow(r.Context(), db.CreateWorkflowParams{
		WorkspaceID: workspaceUUID,
		Name:        req.Name,
		Description: req.Description,
		Icon:        req.Icon,
		Category:    category,
		Graph:       graph,
		Status:      "draft",
		CreatedBy:   creatorUUID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a workflow with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create workflow: "+err.Error())
		return
	}
	resp := toWorkflowResponse(workflow)
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventWorkflowCreated, workspaceID, actorType, actorID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workflow, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageWorkflow(w, r, workflow) {
		return
	}

	var req UpdateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWorkflowParams{
		ID: parseUUID(id),
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Icon != nil {
		params.Icon = pgtype.Text{String: *req.Icon, Valid: true}
	}
	if req.Category != nil {
		params.Category = pgtype.Text{String: *req.Category, Valid: true}
	}
	if len(req.Graph) > 0 {
		params.Graph = req.Graph
	}

	workflow, err := h.Queries.UpdateWorkflow(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a workflow with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update workflow: "+err.Error())
		return
	}
	resp := toWorkflowResponse(workflow)
	wsID := h.resolveWorkspaceID(r)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsID)
	h.publish(protocol.EventWorkflowUpdated, wsID, actorType, actorID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workflow, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageWorkflow(w, r, workflow) {
		return
	}

	if err := h.Queries.DeleteWorkflow(r.Context(), db.DeleteWorkflowParams{
		ID:          workflow.ID,
		WorkspaceID: workflow.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(workflow.WorkspaceID))
	h.publish(protocol.EventWorkflowDeleted, uuidToString(workflow.WorkspaceID), actorType, actorID, map[string]any{"workflow_id": uuidToString(workflow.ID)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) PublishWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workflow, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageWorkflow(w, r, workflow) {
		return
	}

	published, err := h.Queries.PublishWorkflow(r.Context(), db.PublishWorkflowParams{
		ID:          workflow.ID,
		WorkspaceID: workflow.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to publish workflow")
		return
	}

	// Snapshot the current graph as a version history entry.
	userID := requestUserID(r)
	publisherUUID := parseUUID(userID)
	_, _ = h.Queries.CreateWorkflowVersion(r.Context(), db.CreateWorkflowVersionParams{
		SopID:       published.ID,
		Version:     published.Version,
		Graph:       published.Graph,
		PublishedBy: publisherUUID,
	})

	resp := toWorkflowResponse(published)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(workflow.WorkspaceID))
	h.publish(protocol.EventWorkflowPublished, uuidToString(workflow.WorkspaceID), actorType, actorID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusOK, resp)
}

// --- Workflow Runs ---

func (h *Handler) TriggerWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workflow, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req TriggerWorkflowRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Resolve agent_id: explicit from request, or auto-pick from mounted agents.
	var agentUUID pgtype.UUID
	if req.AgentID != "" {
		var agentOK bool
		agentUUID, agentOK = parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
		if !agentOK {
			return
		}
		// Validate agent exists in the same workspace.
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          agentUUID,
			WorkspaceID: workflow.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "agent not found in workspace")
			return
		}
	} else {
		// Auto-pick: use the first agent that has this workflow mounted.
		rows, err := h.Queries.ListAgentWorkflowsByWorkspace(r.Context(), workflow.WorkspaceID)
		if err == nil {
			for _, row := range rows {
				if row.ID == workflow.ID {
					agentUUID = row.AgentID
					break
				}
			}
		}
		if !agentUUID.Valid {
			writeError(w, http.StatusBadRequest, "no agent mounted on this workflow — attach an agent first, or pass agent_id explicitly")
			return
		}
	}

	triggerInput := req.Input
	if len(triggerInput) == 0 {
		triggerInput = json.RawMessage(`{}`)
	}

	// Use WorkflowService to start the RuleGo engine.
	os.WriteFile("C:/Users/zisheng/workflow_debug.log", []byte(fmt.Sprintf("WorkflowService nil? %v\n", h.WorkflowService == nil)), 0644)
	if h.WorkflowService != nil {
		run, err := h.WorkflowService.TriggerRun(r.Context(), service.TriggerRunParams{
			WorkflowID:  workflow.ID,
			WorkspaceID: workflow.WorkspaceID,
			AgentID:     agentUUID,
			Source:      "api",
			Input:       triggerInput,
			TriggeredBy: parseUUID(userID),
		})
		if err != nil {
			os.WriteFile("C:/Users/zisheng/workflow_debug.log", []byte(fmt.Sprintf("TriggerRun error: %v\n", err)), 0644)
			writeError(w, http.StatusInternalServerError, "failed to trigger workflow: "+err.Error())
			return
		}
		os.WriteFile("C:/Users/zisheng/workflow_debug.log", []byte(fmt.Sprintf("TriggerRun OK: runID=%s\n", uuidToString(run.ID))), 0644)
		actorType, actorID := h.resolveActor(r, userID, uuidToString(workflow.WorkspaceID))
		h.publish("workflow_run:started", uuidToString(workflow.WorkspaceID), actorType, actorID, map[string]any{
			"run_id":      uuidToString(run.ID),
			"workflow_id": uuidToString(workflow.ID),
		})
		writeJSON(w, http.StatusCreated, toWorkflowRunResponse(run))
		return
	}

	os.WriteFile("C:/Users/zisheng/workflow_debug.log", []byte("FALLBACK: WorkflowService is nil!\n"), 0644)
	totalNodes := countGraphNodes(workflow.Graph)
	run, err := h.Queries.CreateWorkflowRun(r.Context(), db.CreateWorkflowRunParams{
		SopID:       workflow.ID,
		WorkspaceID:  workflow.WorkspaceID,
		AgentID:      agentUUID,
		Source:       "api",
		TriggerInput: triggerInput,
		Status:       "pending",
		TotalNodes:   totalNodes,
		TriggeredBy:  parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow run: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toWorkflowRunResponse(run))
}

func (h *Handler) ListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workflow, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}

	offset := int32(0)
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			offset = int32(n)
		}
	}

	runs, err := h.Queries.ListWorkflowRuns(r.Context(), db.ListWorkflowRunsParams{
		SopID:      workflow.ID,
		Limit:      50,
		Offset:     offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow runs")
		return
	}

	resp := make([]WorkflowRunResponse, len(runs))
	for i, run := range runs {
		resp[i] = toWorkflowRunResponse(run)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}

	runIDStr := chi.URLParam(r, "runId")
	runUUID, ok := parseUUIDOrBadRequest(w, runIDStr, "run id")
	if !ok {
		return
	}

	run, err := h.Queries.GetWorkflowRun(r.Context(), runUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow run not found")
		return
	}

	executions, err := h.Queries.ListNodeExecutionsByRun(r.Context(), runUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list node executions")
		return
	}

	resp := toWorkflowRunResponse(run)
	resp.NodeExecutions = make([]WorkflowNodeExecutionResponse, len(executions))
	for i, e := range executions {
		resp.NodeExecutions[i] = toNodeExecutionResponse(e)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}

	runIDStr := chi.URLParam(r, "runId")
	runUUID, ok := parseUUIDOrBadRequest(w, runIDStr, "run id")
	if !ok {
		return
	}

	// Update DB status to cancelled.
	if err := h.Queries.CancelWorkflowRun(r.Context(), runUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel workflow run")
		return
	}

	// Stop the RuleGo engine if still running.
	if h.WorkflowService != nil {
		h.WorkflowService.CancelRun(r.Context(), runIDStr)
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Agent-Workflow junction ---

func (h *Handler) ListAgentWorkflows(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	workflows, err := h.Queries.ListAgentWorkflows(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent workflows")
		return
	}

	resp := make([]WorkflowSummaryResponse, len(workflows))
	for i, row := range workflows {
		resp[i] = toWorkflowSummaryFromAgentRow(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SetAgentWorkflows(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req SetAgentWorkflowsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	workflowUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SopIDs, "sop_ids")
	if !ok {
		return
	}
	if !h.validateWorkflowIDsInWorkspace(w, r, agent, workflowUUIDs) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	if err := qtx.RemoveAllAgentWorkflows(r.Context(), agent.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear agent workflows")
		return
	}

	for _, wfID := range workflowUUIDs {
		if err := qtx.AddAgentWorkflow(r.Context(), db.AddAgentWorkflowParams{
			AgentID:    agent.ID,
			SopID:      wfID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent workflow: "+err.Error())
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	h.writeUpdatedAgentWorkflows(w, r, agent)
}

func (h *Handler) AddAgentWorkflows(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req AddAgentWorkflowsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	workflowUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SopIDs, "sop_ids")
	if !ok {
		return
	}
	if !h.validateWorkflowIDsInWorkspace(w, r, agent, workflowUUIDs) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	for _, wfID := range workflowUUIDs {
		if err := qtx.AddAgentWorkflow(r.Context(), db.AddAgentWorkflowParams{
			AgentID:    agent.ID,
			SopID:      wfID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent workflow: "+err.Error())
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	h.writeUpdatedAgentWorkflows(w, r, agent)
}

func (h *Handler) validateWorkflowIDsInWorkspace(w http.ResponseWriter, r *http.Request, agent db.Agent, workflowUUIDs []pgtype.UUID) bool {
	seen := map[string]struct{}{}
	for _, wfID := range workflowUUIDs {
		key := uuidToString(wfID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
			ID:          wfID,
			WorkspaceID: agent.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "workflow not found")
			return false
		}
	}
	return true
}

func (h *Handler) writeUpdatedAgentWorkflows(w http.ResponseWriter, r *http.Request, agent db.Agent) {
	workflows, err := h.Queries.ListAgentWorkflows(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent workflows")
		return
	}

	resp := make([]WorkflowSummaryResponse, len(workflows))
	for i, row := range workflows {
		resp[i] = toWorkflowSummaryFromAgentRow(row)
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(agent.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(agent.WorkspaceID), actorType, actorID, map[string]any{"agent_id": uuidToString(agent.ID), "workflows": resp})
	writeJSON(w, http.StatusOK, resp)
}

// --- Workflow Templates ---

// ListWorkflowTemplates returns all built-in workflow templates.
func (h *Handler) ListWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := workflow.ListTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list templates: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

// GetWorkflowTemplate returns a single template's metadata and full graph.
func (h *Handler) GetWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateId")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "template id is required")
		return
	}

	// Find the metadata entry.
	templates, err := workflow.ListTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list templates: "+err.Error())
		return
	}
	var meta *workflow.TemplateMeta
	for i := range templates {
		if templates[i].ID == templateID {
			meta = &templates[i]
			break
		}
	}
	if meta == nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}

	graph, err := workflow.LoadTemplate(templateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load template: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":             meta.ID,
		"name":           meta.Name,
		"name_zh":        meta.NameZh,
		"category":       meta.Category,
		"description":    meta.Description,
		"description_zh": meta.DescriptionZh,
		"node_count":     meta.NodeCount,
		"tags":           meta.Tags,
		"graph":          json.RawMessage(graph),
	})
}

type cloneWorkflowTemplateRequest struct {
	TemplateID string `json:"template_id"`
	Name       string `json:"name"`
}

// CloneWorkflowTemplate creates a new workflow pre-populated with a built-in
// template's graph. Accepts template_id from the URL path ({templateId}) or
// from the JSON body (template_id field); the URL path takes precedence.
func (h *Handler) CloneWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(creatorID)

	var req cloneWorkflowTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Body may be empty when template_id comes from URL.
		req = cloneWorkflowTemplateRequest{}
	}

	// URL path param takes precedence over body field.
	if urlID := chi.URLParam(r, "templateId"); urlID != "" {
		req.TemplateID = urlID
	}
	if req.TemplateID == "" {
		writeError(w, http.StatusBadRequest, "template_id is required")
		return
	}

	graph, err := workflow.LoadTemplate(req.TemplateID)
	if err != nil {
		writeError(w, http.StatusNotFound, "template not found: "+req.TemplateID)
		return
	}

	// Derive name: use the request name, or fall back to the template's
	// ruleChain.name field.
	name := req.Name
	if name == "" {
		var chain struct {
			RuleChain struct {
				Name string `json:"name"`
			} `json:"ruleChain"`
		}
		if err := json.Unmarshal(graph, &chain); err == nil && chain.RuleChain.Name != "" {
			name = chain.RuleChain.Name
		} else {
			name = req.TemplateID
		}
	}

	// Extract category and icon from the template graph.
	var tmplMeta struct {
		RuleChain struct {
			Configuration struct {
				Category string `json:"category"`
			} `json:"configuration"`
			AdditionalInfo struct {
				Description string `json:"description"`
				Icon        string `json:"icon"`
			} `json:"additionalInfo"`
		} `json:"ruleChain"`
	}
	_ = json.Unmarshal(graph, &tmplMeta)

	wf, err := h.Queries.CreateWorkflow(r.Context(), db.CreateWorkflowParams{
		WorkspaceID: workspaceUUID,
		Name:        name,
		Description: tmplMeta.RuleChain.AdditionalInfo.Description,
		Icon:        tmplMeta.RuleChain.AdditionalInfo.Icon,
		Category:    tmplMeta.RuleChain.Configuration.Category,
		Graph:       graph,
		Status:      "draft",
		CreatedBy:   creatorUUID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a workflow with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create workflow: "+err.Error())
		return
	}

	resp := toWorkflowResponse(wf)
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventWorkflowCreated, workspaceID, actorType, actorID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// --- Workflow Import ---

// ImportWorkflowResponse is the response for the import endpoint.
type ImportWorkflowResponse struct {
	Workflow WorkflowResponse       `json:"workflow"`
	Warnings []workflow.ImportWarning `json:"warnings"`
	Source   string                  `json:"source"`
}

// ImportWorkflow reads a raw n8n JSON or Dify YAML body, auto-detects the
// format, converts it to RuleGo DSL, and creates a new workflow from the result.
func (h *Handler) ImportWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(creatorID)

	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20)) // 2 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "empty request body")
		return
	}

	result, err := workflow.AutoImport(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Extract name, description, icon, category from the converted chain.
	var chainMeta struct {
		RuleChain struct {
			Name          string `json:"name"`
			Configuration struct {
				Category string `json:"category"`
			} `json:"configuration"`
			AdditionalInfo struct {
				Description string `json:"description"`
				Icon        string `json:"icon"`
			} `json:"additionalInfo"`
		} `json:"ruleChain"`
	}
	_ = json.Unmarshal(result.Chain, &chainMeta)

	name := chainMeta.RuleChain.Name
	if name == "" {
		name = "Imported Workflow"
	}

	category := chainMeta.RuleChain.Configuration.Category
	if category == "" {
		category = "general"
	}

	wf, err := h.Queries.CreateWorkflow(r.Context(), db.CreateWorkflowParams{
		WorkspaceID: workspaceUUID,
		Name:        name,
		Description: chainMeta.RuleChain.AdditionalInfo.Description,
		Icon:        chainMeta.RuleChain.AdditionalInfo.Icon,
		Category:    category,
		Graph:       result.Chain,
		Status:      "draft",
		CreatedBy:   creatorUUID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a workflow with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create workflow: "+err.Error())
		return
	}

	resp := ImportWorkflowResponse{
		Workflow: toWorkflowResponse(wf),
		Warnings: result.Warnings,
		Source:   result.Source,
	}
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventWorkflowCreated, workspaceID, actorType, actorID, map[string]any{"workflow": resp.Workflow})
	writeJSON(w, http.StatusCreated, resp)
}
// --- Workflow Credentials ---

// WorkflowCredentialResponse is the public shape for a credential. The `value`
// field is only populated on single-get, never on list.
type WorkflowCredentialResponse struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"workspace_id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	CredentialType string          `json:"credential_type"`
	Value          json.RawMessage `json:"value,omitempty"`
	CreatedBy      *string         `json:"created_by"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

type CreateWorkflowCredentialRequest struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	CredentialType string          `json:"credential_type"`
	Value          json.RawMessage `json:"value"`
}

type UpdateWorkflowCredentialRequest struct {
	Name        *string         `json:"name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Value       json.RawMessage `json:"value,omitempty"`
}

func toCredentialResponse(c db.SopCredential, includeValue bool) WorkflowCredentialResponse {
	resp := WorkflowCredentialResponse{
		ID:             uuidToString(c.ID),
		WorkspaceID:    uuidToString(c.WorkspaceID),
		Name:           c.Name,
		Description:    c.Description,
		CredentialType: c.CredentialType,
		CreatedBy:      uuidToPtr(c.CreatedBy),
		CreatedAt:      timestampToString(c.CreatedAt),
		UpdatedAt:      timestampToString(c.UpdatedAt),
	}
	if includeValue {
		resp.Value = c.Value
	}
	return resp
}

func toCredentialListResponse(row db.ListWorkflowCredentialsRow) WorkflowCredentialResponse {
	return WorkflowCredentialResponse{
		ID:             uuidToString(row.ID),
		WorkspaceID:    uuidToString(row.WorkspaceID),
		Name:           row.Name,
		Description:    row.Description,
		CredentialType: row.CredentialType,
		CreatedBy:      uuidToPtr(row.CreatedBy),
		CreatedAt:      timestampToString(row.CreatedAt),
		UpdatedAt:      timestampToString(row.UpdatedAt),
	}
}

func (h *Handler) ListWorkflowCredentials(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListWorkflowCredentials(r.Context(), workspaceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list credentials")
		return
	}

	resp := make([]WorkflowCredentialResponse, len(rows))
	for i, row := range rows {
		resp[i] = toCredentialListResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateWorkflowCredential(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req CreateWorkflowCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Value) == 0 {
		req.Value = json.RawMessage(`{}`)
	}
	if req.CredentialType == "" {
		req.CredentialType = "api_key"
	}

	cred, err := h.Queries.CreateWorkflowCredential(r.Context(), db.CreateWorkflowCredentialParams{
		WorkspaceID:    workspaceUUID,
		Name:           req.Name,
		Description:    req.Description,
		CredentialType: req.CredentialType,
		Value:          req.Value,
		CreatedBy:      parseUUID(creatorID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a credential with this name already exists")
			return
		}
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "invalid credential_type")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create credential")
		return
	}
	writeJSON(w, http.StatusCreated, toCredentialResponse(cred, false))
}

func (h *Handler) UpdateWorkflowCredential(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	credID := chi.URLParam(r, "credId")
	credUUID, ok := parseUUIDOrBadRequest(w, credID, "credential id")
	if !ok {
		return
	}

	// Verify the credential belongs to the workspace.
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetWorkflowCredential(r.Context(), db.GetWorkflowCredentialParams{
		ID:          credUUID,
		WorkspaceID: workspaceUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}

	var req UpdateWorkflowCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWorkflowCredentialParams{
		ID: credUUID,
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if len(req.Value) > 0 {
		params.Value = req.Value
	}

	updated, err := h.Queries.UpdateWorkflowCredential(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a credential with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update credential")
		return
	}
	writeJSON(w, http.StatusOK, toCredentialResponse(updated, false))
}

func (h *Handler) DeleteWorkflowCredential(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	credID := chi.URLParam(r, "credId")
	credUUID, ok := parseUUIDOrBadRequest(w, credID, "credential id")
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	if err := h.Queries.DeleteWorkflowCredential(r.Context(), db.DeleteWorkflowCredentialParams{
		ID:          credUUID,
		WorkspaceID: workspaceUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete credential")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Workflow Versions ---

type WorkflowVersionResponse struct {
	ID          string          `json:"id"`
	SopID       string          `json:"sop_id"`
	Version     int32           `json:"version"`
	Graph       json.RawMessage `json:"graph"`
	PublishedBy *string         `json:"published_by"`
	PublishedAt string          `json:"published_at"`
}

func toVersionResponse(v db.SopVersion) WorkflowVersionResponse {
	return WorkflowVersionResponse{
		ID:          uuidToString(v.ID),
		SopID:       uuidToString(v.SopID),
		Version:     v.Version,
		Graph:       v.Graph,
		PublishedBy: uuidToPtr(v.PublishedBy),
		PublishedAt: timestampToString(v.PublishedAt),
	}
}

func (h *Handler) ListWorkflowVersions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workflow, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}

	versions, err := h.Queries.ListWorkflowVersions(r.Context(), workflow.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}

	resp := make([]WorkflowVersionResponse, len(versions))
	for i, v := range versions {
		resp[i] = toVersionResponse(v)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RollbackWorkflowVersion(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workflow, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageWorkflow(w, r, workflow) {
		return
	}

	versionStr := chi.URLParam(r, "version")
	versionNum, err := strconv.Atoi(versionStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid version number")
		return
	}

	ver, err := h.Queries.GetWorkflowVersion(r.Context(), db.GetWorkflowVersionParams{
		SopID:      workflow.ID,
		Version:    int32(versionNum),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}

	// Update the workflow's graph to the version's graph.
	updated, err := h.Queries.UpdateWorkflow(r.Context(), db.UpdateWorkflowParams{
		ID:    workflow.ID,
		Graph: ver.Graph,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rollback workflow")
		return
	}

	resp := toWorkflowResponse(updated)
	wsID := uuidToString(workflow.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsID)
	h.publish(protocol.EventWorkflowUpdated, wsID, actorType, actorID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusOK, resp)
}

// --- Workflow Export ---

// ExportWorkflow exports a workflow in the requested format.
// GET /api/workflows/{id}/export?format=n8n|dify|json
func (h *Handler) ExportWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	switch format {
	case "json":
		// Return raw RuleGo DSL graph as JSON download.
		out, err := json.MarshalIndent(json.RawMessage(wf.Graph), "", "  ")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal graph")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+wf.Name+".json\"")
		w.WriteHeader(http.StatusOK)
		w.Write(out)

	case "n8n":
		out, err := workflow.ExportN8n(wf.Graph, wf.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to export as n8n: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+wf.Name+"_n8n.json\"")
		w.WriteHeader(http.StatusOK)
		w.Write(out)

	case "dify":
		out, err := workflow.ExportDify(wf.Graph, wf.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to export as Dify: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+wf.Name+"_dify.yaml\"")
		w.WriteHeader(http.StatusOK)
		w.Write(out)

	default:
		writeError(w, http.StatusBadRequest, "unsupported export format: "+format+"; use json, n8n, or dify")
	}
}

// --- Workflow Stats ---

// WorkflowStatsResponse is the shape returned by the stats endpoint.
type WorkflowStatsResponse struct {
	TotalRuns     int64  `json:"total_runs"`
	CompletedRuns int64  `json:"completed_runs"`
	FailedRuns    int64  `json:"failed_runs"`
	TotalTokens   int64  `json:"total_tokens"`
	AvgDurationMs int64  `json:"avg_duration_ms"`
	LastRunAt     string `json:"last_run_at"`
}

// GetWorkflowStats returns aggregated run statistics for a workflow.
// GET /api/workflows/{id}/stats
func (h *Handler) GetWorkflowStats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowForUser(w, r, id)
	if !ok {
		return
	}

	row, err := h.Queries.GetWorkflowStats(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get workflow stats")
		return
	}

	resp := WorkflowStatsResponse{
		TotalRuns:     row.TotalRuns,
		CompletedRuns: row.CompletedRuns,
		FailedRuns:    row.FailedRuns,
		TotalTokens:   toInt64(row.TotalTokens),
		AvgDurationMs: toInt64(row.AvgDurationMs),
		LastRunAt:     toTimestampString(row.LastRunAt),
	}

	writeJSON(w, http.StatusOK, resp)
}

// toInt64 extracts an int64 from interface{} values produced by sqlc for
// aggregate expressions (coalesce/sum/avg that produce interface{}).
func toInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case pgtype.Numeric:
		if !n.Valid {
			return 0
		}
		f, _ := n.Float64Value()
		return int64(f.Float64)
	default:
		return 0
	}
}

// toTimestampString converts a pgx timestamp (pgtype.Timestamptz or time.Time)
// to an ISO-8601 string, or "" if null.
func toTimestampString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case pgtype.Timestamptz:
		if !t.Valid {
			return ""
		}
		return t.Time.Format(time.RFC3339)
	case time.Time:
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}
