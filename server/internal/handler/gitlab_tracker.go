package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
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

// ---------------------------------------------------------------------------
// Validate endpoint (Phase 2 Task 7)
// ---------------------------------------------------------------------------

// GitlabTrackerAllowedHosts is the operator-configured list of self-hosted
// GitLab hosts (comma-separated, GITLAB_ALLOWED_HOSTS). gitlab.com is
// always accepted and does not need to appear here. Exposed as a variable
// so tests can override without setenv gymnastics.
var GitlabTrackerAllowedHosts = allowedGitlabHostsFromEnv

// gitlabTrackerClientFactory builds a *gitlabtracker.RestClient for a
// given base URL + PAT. Tests replace it with a factory that points at an
// httptest server; production uses the SSRF-safe transport.
var gitlabTrackerClientFactory = defaultGitlabTrackerClientFactory

func allowedGitlabHostsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("GITLAB_ALLOWED_HOSTS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func defaultGitlabTrackerClientFactory(baseURL, token string) (*gitlabtracker.RestClient, error) {
	transport, err := gitlabtracker.NewClient(gitlabtracker.Config{
		AllowedHosts:   GitlabTrackerAllowedHosts(),
		RequestTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return gitlabtracker.NewRestClient(transport, baseURL, token), nil
}

// ValidateGitlabTrackerRequest carries the two pieces of input a caller
// needs to check before we ever persist anything. The token is held in
// memory for the duration of the request only.
type ValidateGitlabTrackerRequest struct {
	RepositoryURL string `json:"repository_url"`
	AccessToken   string `json:"access_token"`
}

// ValidateGitlabTrackerResponse is the safe summary returned on success.
// It intentionally omits the token echo and the raw project ref.
type ValidateGitlabTrackerResponse struct {
	Host              string                       `json:"host"`
	InstanceURL       string                       `json:"instance_url"`
	PathWithNamespace string                       `json:"path_with_namespace"`
	RemoteProjectID   int64                        `json:"remote_project_id"`
	WebURL            string                       `json:"web_url"`
	DefaultBranch     string                       `json:"default_branch"`
	Permissions       ValidateGitlabTrackerPermits `json:"permissions"`
}

type ValidateGitlabTrackerPermits struct {
	CanWriteIssues      bool `json:"can_write_issues"`
	CanConfigureWebhook bool `json:"can_configure_webhook"`
}

// ValidateGitlabTracker checks whether the URL/token pair maps to a
// reachable GitLab project. The endpoint is workspace-member-gated but
// stops short of a role check: validate is a read-only preflight that
// the Create Project modal calls before the operator commits to storing
// the token. Never persists anything.
func (h *Handler) ValidateGitlabTracker(w http.ResponseWriter, r *http.Request) {
	var req ValidateGitlabTrackerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.RepositoryURL = strings.TrimSpace(req.RepositoryURL)
	req.AccessToken = strings.TrimSpace(req.AccessToken)
	if req.AccessToken == "" {
		writeStructuredGitlabError(w, http.StatusBadRequest, "access_token_required", "access_token is required")
		return
	}

	parsed, err := gitlabtracker.ParseProjectURL(req.RepositoryURL, GitlabTrackerAllowedHosts())
	if err != nil {
		var ue *gitlabtracker.URLError
		if errors.As(err, &ue) {
			writeStructuredGitlabError(w, http.StatusBadRequest, ue.Code, ue.Message)
			return
		}
		writeStructuredGitlabError(w, http.StatusBadRequest, "invalid_url", err.Error())
		return
	}

	client, err := gitlabTrackerClientFactory(parsed.InstanceURL, req.AccessToken)
	if err != nil {
		writeStructuredGitlabError(w, http.StatusInternalServerError, "internal", "failed to build GitLab client")
		return
	}
	project, err := client.GetProject(r.Context(), parsed.PathWithNamespace)
	if err != nil {
		status, code, msg := mapGitlabValidationError(err)
		writeStructuredGitlabError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusOK, ValidateGitlabTrackerResponse{
		Host:              parsed.Host,
		InstanceURL:       parsed.InstanceURL,
		PathWithNamespace: project.PathWithNamespace,
		RemoteProjectID:   project.ID,
		WebURL:            project.WebURL,
		DefaultBranch:     project.DefaultBranch,
		Permissions: ValidateGitlabTrackerPermits{
			CanWriteIssues:      project.CanWriteIssues,
			CanConfigureWebhook: project.CanConfigureWebhook,
		},
	})
}

// mapGitlabValidationError translates a REST error into the (status,
// code, message) triple the frontend maps to a user-visible reason. The
// message is a safe summary — never the URL or token.
func mapGitlabValidationError(err error) (int, string, string) {
	switch {
	case errors.Is(err, gitlabtracker.ErrInvalidToken):
		return http.StatusUnauthorized, "invalid_token", "the access token was rejected by GitLab"
	case errors.Is(err, gitlabtracker.ErrPermissionDenied):
		return http.StatusForbidden, "permission_denied", "the token lacks permission to read this project"
	case errors.Is(err, gitlabtracker.ErrNotFound):
		return http.StatusNotFound, "not_found", "the project was not found; check the URL and token scope"
	}
	var rate *gitlabtracker.RateLimitedError
	if errors.As(err, &rate) {
		return http.StatusTooManyRequests, "rate_limited", "GitLab rate limited the validation request"
	}
	if errors.Is(err, gitlabtracker.ErrRemote) {
		return http.StatusBadGateway, "network", "unexpected response from GitLab"
	}
	// Every other error (dial guard rejection, TLS failure) surfaces as a
	// generic network error rather than leaking the transport detail.
	return http.StatusBadGateway, "network", "could not reach GitLab"
}

func writeStructuredGitlabError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}

// Ensure imports used only by ctx-shaped helpers stay referenced.
var _ = context.Background
