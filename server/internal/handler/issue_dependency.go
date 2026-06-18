package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// IssueDependencyResponse is the JSON response for an issue dependency edge.
type IssueDependencyResponse struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	IssueID          string `json:"issue_id"`
	DependsOnIssueID string `json:"depends_on_issue_id"`
	Type             string `json:"dep_type"`
	CreatedBy        string `json:"created_by,omitempty"`
	CreatedAt        string `json:"created_at"`
}

func dependencyToResponse(d db.IssueDependency) IssueDependencyResponse {
	return IssueDependencyResponse{
		ID:               uuidToString(d.ID),
		WorkspaceID:      uuidToString(d.WorkspaceID),
		IssueID:          uuidToString(d.IssueID),
		DependsOnIssueID: uuidToString(d.DependsOnIssueID),
		Type:             d.Type,
		CreatedBy:        uuidToString(d.CreatedBy),
		CreatedAt:        timestampToString(d.CreatedAt),
	}
}

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

// CreateDependencyRequest is the body for POST /api/issues/{id}/dependencies.
type CreateDependencyRequest struct {
	TargetIssueID string `json:"target_issue_id"`
	DepType       string `json:"dep_type"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// CreateIssueDependency handles POST /api/issues/{id}/dependencies.
func (h *Handler) CreateIssueDependency(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	var req CreateDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DepType != "blocks" && req.DepType != "relates_to" {
		writeError(w, http.StatusBadRequest, "dep_type must be \"blocks\" or \"relates_to\"")
		return
	}

	targetUUID, err := util.ParseUUID(req.TargetIssueID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid target_issue_id")
		return
	}

	// Verify the target issue exists in the same workspace.
	ctx := r.Context()
	_, err = h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          targetUUID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "target issue not found in this workspace")
		return
	}

	// Cycle detection for "blocks" edges.
	if req.DepType == "blocks" {
		if h.detectCycle(ctx, issue.ID, targetUUID) {
			writeError(w, http.StatusBadRequest, "adding this dependency would create a cycle")
			return
		}
	}

	// Resolve actor for created_by.
	var createdBy pgtype.UUID
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	if actorType == "member" {
		if parsed, err := util.ParseUUID(actorID); err == nil {
			createdBy = parsed
		}
	}

	dep, err := h.Queries.CreateIssueDependency(ctx, db.CreateIssueDependencyParams{
		IssueID:          issue.ID,
		DependsOnIssueID: targetUUID,
		Type:             req.DepType,
		WorkspaceID:      issue.WorkspaceID,
		CreatedBy:        createdBy,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "dependency already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create dependency")
		return
	}

	writeJSON(w, http.StatusCreated, dependencyToResponse(dep))
}

// DeleteIssueDependency handles DELETE /api/issues/{id}/dependencies/{depId}.
func (h *Handler) DeleteIssueDependency(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	depID := chi.URLParam(r, "depId")
	depUUID, ok := parseUUIDOrBadRequest(w, depID, "dependency id")
	if !ok {
		return
	}

	err := h.Queries.DeleteIssueDependency(r.Context(), db.DeleteIssueDependencyParams{
		ID:          depUUID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete dependency")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListIssueDependencies handles GET /api/issues/{id}/dependencies.
func (h *Handler) ListIssueDependencies(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	ctx := r.Context()
	deps, err := h.Queries.ListDependenciesByIssue(ctx, db.ListDependenciesByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dependencies")
		return
	}

	issueIDStr := uuidToString(issue.ID)
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)

	var blocks []IssueResponse
	var blockedBy []IssueResponse
	var relatesTo []IssueResponse
	raw := make([]IssueDependencyResponse, 0, len(deps))

	for _, d := range deps {
		raw = append(raw, dependencyToResponse(d))

		// Determine the "other" issue ID to load.
		var otherID pgtype.UUID
		depIssueIDStr := uuidToString(d.IssueID)
		depTargetIDStr := uuidToString(d.DependsOnIssueID)

		switch {
		case d.Type == "blocks" && depIssueIDStr == issueIDStr:
			// This issue blocks the other → put other in "blocks" list.
			otherID = d.DependsOnIssueID
		case d.Type == "blocks" && depTargetIDStr == issueIDStr:
			// The other issue blocks this one → put other in "blocked_by" list.
			otherID = d.IssueID
		default:
			// relates_to — the other issue is whichever is not the current one.
			if depIssueIDStr == issueIDStr {
				otherID = d.DependsOnIssueID
			} else {
				otherID = d.IssueID
			}
		}

		other, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          otherID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			// Skip if the other issue can't be loaded (deleted, etc.).
			continue
		}
		resp := issueToResponse(other, prefix)

		switch {
		case d.Type == "blocks" && depIssueIDStr == issueIDStr:
			blocks = append(blocks, resp)
		case d.Type == "blocks" && depTargetIDStr == issueIDStr:
			blockedBy = append(blockedBy, resp)
		default:
			relatesTo = append(relatesTo, resp)
		}
	}

	if blocks == nil {
		blocks = []IssueResponse{}
	}
	if blockedBy == nil {
		blockedBy = []IssueResponse{}
	}
	if relatesTo == nil {
		relatesTo = []IssueResponse{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"blocks":     blocks,
		"blocked_by": blockedBy,
		"relates_to": relatesTo,
		"raw":        raw,
	})
}

// GetProjectDependencyGraph handles GET /api/projects/{id}/dependency-graph.
func (h *Handler) GetProjectDependencyGraph(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	ctx := r.Context()

	// Verify project exists in this workspace.
	_, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID:          projectUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// 1. Fetch root issues that belong to this project.
	rootRows, err := h.Queries.ListIssues(ctx, db.ListIssuesParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectUUID,
		Limit:       1000,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project issues")
		return
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)

	// Collect root issue IDs for child lookup
	parentIDs := make([]pgtype.UUID, 0, len(rootRows))
	issueIDSet := make(map[string]bool, len(rootRows)*2)

	nodes := make([]IssueResponse, 0, len(rootRows)*2)
	for _, issue := range rootRows {
		nodes = append(nodes, issueListRowToResponse(issue, prefix))
		id := uuidToString(issue.ID)
		issueIDSet[id] = true
		parentIDs = append(parentIDs, issue.ID)
	}

	// 2. Fetch ALL children of root issues (sub-tasks may lack project_id).
	if len(parentIDs) > 0 {
		children, err := h.Queries.ListChildrenByParents(ctx, db.ListChildrenByParentsParams{
			WorkspaceID: wsUUID,
			ParentIds:   parentIDs,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list child issues")
			return
		}
		for _, child := range children {
			id := uuidToString(child.ID)
			if !issueIDSet[id] {
				nodes = append(nodes, issueToResponse(child, prefix))
				issueIDSet[id] = true
			}
		}
	}

	// 3. Fetch explicit dependency edges. Use raw query to get all deps
	//    among the collected issue IDs, since children may not have project_id.
	allIDs := make([]pgtype.UUID, 0, len(issueIDSet))
	for _, node := range nodes {
		if uid, err := util.ParseUUID(node.ID); err == nil {
			allIDs = append(allIDs, uid)
		}
	}

	edges := make([]IssueDependencyResponse, 0, len(nodes))

	// Fetch deps for each issue and deduplicate
	depSeen := make(map[string]bool)
	for _, uid := range allIDs {
		deps, err := h.Queries.ListDependenciesByIssue(ctx, db.ListDependenciesByIssueParams{
			IssueID:     uid,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			continue
		}
		for _, d := range deps {
			did := uuidToString(d.ID)
			fromID := uuidToString(d.IssueID)
			toID := uuidToString(d.DependsOnIssueID)
			if !depSeen[did] && issueIDSet[fromID] && issueIDSet[toID] {
				depSeen[did] = true
				edges = append(edges, dependencyToResponse(d))
			}
		}
	}

	// 4. Synthesize parent→child edges from parent_issue_id relationships.
	for _, node := range nodes {
		if node.ParentIssueID != nil && *node.ParentIssueID != "" && issueIDSet[*node.ParentIssueID] {
			edges = append(edges, IssueDependencyResponse{
				ID:               "parent-" + *node.ParentIssueID + "-" + node.ID,
				WorkspaceID:      uuidToString(wsUUID),
				IssueID:          *node.ParentIssueID,
				DependsOnIssueID: node.ID,
				Type:             "parent",
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"edges": edges,
	})
}

// GetParentDependencyGraph handles GET /api/issues/{id}/dependency-graph.
func (h *Handler) GetParentDependencyGraph(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	ctx := r.Context()

	// List children of this parent issue.
	children, err := h.Queries.ListChildIssues(ctx, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list child issues")
		return
	}

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	nodes := make([]IssueResponse, len(children))
	for i, child := range children {
		nodes[i] = issueToResponse(child, prefix)
	}

	// Fetch dependency edges among children of this parent.
	deps, err := h.Queries.ListDependenciesByParent(ctx, db.ListDependenciesByParentParams{
		ParentIssueID: issue.ID,
		WorkspaceID:   issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dependencies")
		return
	}

	edges := make([]IssueDependencyResponse, len(deps))
	for i, d := range deps {
		edges[i] = dependencyToResponse(d)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"edges": edges,
	})
}

// ---------------------------------------------------------------------------
// Cycle detection
// ---------------------------------------------------------------------------

// detectCycle performs a BFS from toIssueID following "blocks" edges. If we can
// reach fromIssueID, adding fromIssueID → toIssueID would create a cycle.
// Max depth 100 as a safety bound.
func (h *Handler) detectCycle(ctx context.Context, fromIssueID, toIssueID pgtype.UUID) bool {
	visited := make(map[[16]byte]bool)
	queue := []pgtype.UUID{toIssueID}
	visited[toIssueID.Bytes] = true

	for depth := 0; depth < 100 && len(queue) > 0; depth++ {
		current := queue[0]
		queue = queue[1:]

		// Follow outgoing "blocks" edges from the current node.
		targets, err := h.Queries.ListBlockDependenciesFromIssue(ctx, current)
		if err != nil {
			continue
		}

		for _, target := range targets {
			if target.Bytes == fromIssueID.Bytes {
				return true
			}
			if !visited[target.Bytes] {
				visited[target.Bytes] = true
				queue = append(queue, target)
			}
		}
	}

	return false
}
