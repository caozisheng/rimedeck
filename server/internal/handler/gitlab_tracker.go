package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GitlabTrackerResponse is the public tracker summary. Credential material is
// intentionally absent; callers only learn whether a token is configured.
type GitlabTrackerResponse struct {
	ID                 string  `json:"id"`
	InstanceURL        string  `json:"instance_url"`
	PathWithNamespace  string  `json:"path_with_namespace"`
	WebURL             string  `json:"web_url"`
	State              string  `json:"state"`
	WebhookState       string  `json:"webhook_state"`
	LastPullAt         *string `json:"last_pull_at"`
	PendingOutboxCount int64   `json:"pending_outbox_count"`
	FailedOutboxCount  int64   `json:"failed_outbox_count"`
	TokenConfigured    bool    `json:"token_configured"`
	CanManage          bool    `json:"can_manage"`
}

func gitlabTrackerToResponse(row db.GitlabTrackerConnection, pending, failed int64, canManage bool) GitlabTrackerResponse {
	return GitlabTrackerResponse{
		ID:                 uuidToString(row.ID),
		InstanceURL:        row.InstanceUrl,
		PathWithNamespace:  row.PathWithNamespace,
		WebURL:             row.WebUrl,
		State:              row.State,
		WebhookState:       row.WebhookState,
		LastPullAt:         timestampToPtr(row.LastPullAt),
		PendingOutboxCount: pending,
		FailedOutboxCount:  failed,
		TokenConfigured:    len(row.TokenCiphertext) > 0,
		CanManage:          canManage,
	}
}

// ListProjectGitlabTrackers returns non-disabled tracker summaries for a
// project. It is member-readable; management capability is a response hint.
func (h *Handler) ListProjectGitlabTrackers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	member, _ := middleware.MemberFromContext(ctx)
	canManage := roleAllowed(member.Role, "owner", "admin")
	rows, err := h.Queries.ListGitlabTrackerConnectionsByProject(ctx, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list GitLab trackers")
		return
	}

	trackers := make([]GitlabTrackerResponse, 0, len(rows))
	for _, row := range rows {
		counts, err := h.Queries.CountTrackerOutboxByStatus(ctx, row.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to count tracker outbox")
			return
		}
		var pending, failed int64
		for _, count := range counts {
			switch count.Status {
			case "pending", "running", "retrying":
				pending += count.Cnt
			case "failed":
				failed = count.Cnt
			}
		}
		trackers = append(trackers, gitlabTrackerToResponse(row, pending, failed, canManage))
	}
	writeJSON(w, http.StatusOK, map[string]any{"trackers": trackers})
}
