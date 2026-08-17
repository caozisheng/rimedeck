package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type LabelResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func labelToResponse(l db.IssueLabel) LabelResponse {
	return LabelResponse{
		ID:          uuidToString(l.ID),
		WorkspaceID: uuidToString(l.WorkspaceID),
		Name:        l.Name,
		Color:       l.Color,
		CreatedAt:   timestampToString(l.CreatedAt),
		UpdatedAt:   timestampToString(l.UpdatedAt),
	}
}

func labelsToResponse(list []db.IssueLabel) []LabelResponse {
	out := make([]LabelResponse, len(list))
	for i, l := range list {
		out[i] = labelToResponse(l)
	}
	return out
}

type CreateLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type UpdateLabelRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

// 6-digit hex, with or without leading '#'.
var hexColorRE = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

// normalizeColor returns a canonical "#rrggbb" form or an error if invalid.
//
// LOAD-BEARING INVARIANT: LabelChip renders `style={{ backgroundColor: color }}`
// directly in the frontend. If this regex is ever relaxed to accept arbitrary
// CSS (named colors, `url(...)`, etc.), that inline style becomes an injection
// surface. Keep the regex strict.
func normalizeColor(c string) (string, error) {
	c = strings.TrimSpace(c)
	if !hexColorRE.MatchString(c) {
		return "", errors.New("color must be a 6-digit hex value like #3b82f6")
	}
	if !strings.HasPrefix(c, "#") {
		c = "#" + c
	}
	return strings.ToLower(c), nil
}

const maxLabelNameLen = 32

// validateLabelName trims and validates a label name. Returns the trimmed
// name or an error suitable for a 400 response.
func validateLabelName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if len(name) > maxLabelNameLen {
		return "", errors.New("name must be 32 characters or fewer")
	}
	// TODO(labels): consider restricting to a charset that excludes newlines,
	// tabs, and control characters. Emoji are left allowed — users can pick
	// `🐛 bug` if they want. Tracked as a follow-up so we don't gate this PR.
	return name, nil
}

// ---------------------------------------------------------------------------
// Handlers — label CRUD
// ---------------------------------------------------------------------------

func (h *Handler) ListLabels(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	query := r.URL.Query()
	source := strings.TrimSpace(query.Get("source"))
	projectRaw := strings.TrimSpace(query.Get("project_id"))
	trackerRaw := strings.TrimSpace(query.Get("tracker_id"))
	if source != "" && source != "local" && source != "gitlab" {
		writeError(w, http.StatusBadRequest, "source must be local or gitlab")
		return
	}
	if source == "local" && trackerRaw != "" {
		writeError(w, http.StatusBadRequest, "tracker_id cannot be combined with source=local")
		return
	}
	var labels []db.IssueLabel
	var err error
	if source == "" && projectRaw == "" && trackerRaw == "" {
		labels, err = h.Queries.ListLabels(r.Context(), wsUUID)
	} else {
		var projectID, trackerID pgtype.UUID
		if projectRaw != "" {
			projectID, ok = parseUUIDOrBadRequest(w, projectRaw, "project_id")
			if !ok {
				return
			}
		}
		if trackerRaw != "" {
			trackerID, ok = parseUUIDOrBadRequest(w, trackerRaw, "tracker_id")
			if !ok {
				return
			}
		}
		labels, err = h.Queries.ListLabelsFiltered(r.Context(), db.ListLabelsFilteredParams{
			WorkspaceID: wsUUID,
			Source:      pgtype.Text{String: source, Valid: source != ""},
			TrackerID:   trackerID,
			ProjectID:   projectID,
		})
	}
	if err != nil {
		slog.Warn("ListLabels failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}
	resp := labelsToResponse(labels)
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp, "total": len(resp)})
}

func (h *Handler) GetLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "label id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("GetLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get label")
		return
	}
	writeJSON(w, http.StatusOK, labelToResponse(label))
}

func (h *Handler) CreateLabel(w http.ResponseWriter, r *http.Request) {
	var req CreateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, err := validateLabelName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	color, err := normalizeColor(req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	label, err := h.Queries.CreateLabel(r.Context(), db.CreateLabelParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        name,
		Color:       color,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a label with that name already exists")
			return
		}
		slog.Warn("CreateLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create label")
		return
	}
	resp := labelToResponse(label)
	h.publish(protocol.EventLabelCreated, workspaceID, "member", userID, map[string]any{"label": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	var req UpdateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	idUUID, ok := parseUUIDOrBadRequest(w, id, "label id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	params := db.UpdateLabelParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}
	if req.Name != nil {
		name, err := validateLabelName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Color != nil {
		color, err := normalizeColor(*req.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Color = pgtype.Text{String: color, Valid: true}
	}

	// Branch on pgx.ErrNoRows directly from the UPDATE — the WHERE clause
	// already enforces (id, workspace_id), so a missing row means either the
	// label doesn't exist or it's not in this workspace. Dropping the prior
	// GetLabel precheck removes a TOCTOU window and saves a round-trip.
	label, err := h.Queries.UpdateLabel(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a label with that name already exists")
			return
		}
		slog.Warn("UpdateLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update label")
		return
	}
	resp := labelToResponse(label)
	h.publish(protocol.EventLabelUpdated, workspaceID, "member", userID, map[string]any{"label": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, id, "label id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	// DeleteLabel is :one RETURNING id — ErrNoRows means the label wasn't in
	// this workspace (404). Any other error is a real 500.
	if _, err := h.Queries.DeleteLabel(r.Context(), db.DeleteLabelParams{
		ID: idUUID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("DeleteLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete label")
		return
	}
	h.publish(protocol.EventLabelDeleted, workspaceID, "member", userID, map[string]any{"label_id": uuidToString(idUUID)})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Handlers — issue↔label attach/detach
// ---------------------------------------------------------------------------

type AttachLabelRequest struct {
	LabelID string `json:"label_id"`
}

// listLabelsForIssueSafe reads the attached-label list and handles the error
// by logging + returning nil. Callers use this after a successful attach/detach
// mutation: if the read fails, the mutation is already committed, so returning
// nil → clients refetch via query invalidation, and we skip broadcasting an
// empty list that would incorrectly overwrite every subscriber's optimistic
// state.
func (h *Handler) listLabelsForIssueSafe(r *http.Request, issueID, workspaceID pgtype.UUID) ([]db.IssueLabel, bool) {
	labels, err := h.Queries.ListVisibleLabelsByIssue(r.Context(), db.ListVisibleLabelsByIssueParams{
		IssueID:     issueID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		slog.Warn("ListVisibleLabelsByIssue failed after mutation", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issueID))...)
		return nil, false
	}
	return labels, true
}

// ListLabelsForIssue returns the labels currently attached to an issue.
func (h *Handler) ListLabelsForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	// Authorize via the issue — if it's not in this workspace, the caller
	// shouldn't see its labels.
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	labels, err := h.Queries.ListVisibleLabelsByIssue(r.Context(), db.ListVisibleLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("ListLabelsForIssue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": labelsToResponse(labels)})
}

// AttachLabel attaches a label to an issue.
func (h *Handler) AttachLabel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req AttachLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LabelID == "" {
		writeError(w, http.StatusBadRequest, "label_id is required")
		return
	}

	// Both the issue and label must belong to this workspace.
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	labelID, ok := parseUUIDOrBadRequest(w, req.LabelID, "label_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: labelID, WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("GetLabel in AttachLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to attach label")
		return
	}

	if err := h.Queries.AttachLabelToIssue(r.Context(), db.AttachLabelToIssueParams{
		IssueID:     issue.ID,
		LabelID:     labelID,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		slog.Warn("AttachLabelToIssue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to attach label")
		return
	}

	// Read the updated label list; on read failure, the attach is already
	// committed — return success without a labels body (clients refetch via
	// query invalidation) and skip the broadcast so we don't overwrite every
	// subscriber's optimistic state with an incorrect empty list.
	labels, ok2 := h.listLabelsForIssueSafe(r, issue.ID, issue.WorkspaceID)
	if !ok2 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	resp := labelsToResponse(labels)
	h.enqueueGitlabSetLabelsIfNeeded(r, issue, labels)
	h.publish(protocol.EventIssueLabelsChanged, uuidToString(issue.WorkspaceID), "member", userID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"labels":   resp,
	})
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp})
}

// DetachLabel removes a label from an issue.
func (h *Handler) DetachLabel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	labelID := chi.URLParam(r, "labelId")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify both issue and label belong to this workspace before detaching
	// (mirror of AttachLabel). Without this, a crafted request with a foreign
	// labelID would no-op and return 200 — "silent success" is worse than an
	// explicit 404.
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	labelUUID, ok := parseUUIDOrBadRequest(w, labelID, "label id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: labelUUID, WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("GetLabel in DetachLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to detach label")
		return
	}

	if err := h.Queries.DetachLabelFromIssue(r.Context(), db.DetachLabelFromIssueParams{
		IssueID:     issue.ID,
		LabelID:     labelUUID,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		slog.Warn("DetachLabelFromIssue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to detach label")
		return
	}

	labels, ok2 := h.listLabelsForIssueSafe(r, issue.ID, issue.WorkspaceID)
	if !ok2 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	resp := labelsToResponse(labels)
	h.enqueueGitlabSetLabelsIfNeeded(r, issue, labels)
	h.publish(protocol.EventIssueLabelsChanged, uuidToString(issue.WorkspaceID), "member", userID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"labels":   resp,
	})
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp})
}

// enqueueGitlabSetLabelsIfNeeded pushes a canonical full desired label set.
func (h *Handler) enqueueGitlabSetLabelsIfNeeded(r *http.Request, issue db.Issue, labels []db.IssueLabel) {
	if issue.SourceType != "gitlab" || !issue.TrackerConnectionID.Valid {
		return
	}
	names := canonicalGitlabLabelNames(issue, labels)
	payload, err := json.Marshal(map[string]any{"labels": names})
	if err != nil {
		slog.Warn("enqueue set_labels payload marshal failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		return
	}
	if err := h.enqueueGitlabWriteOp(r.Context(), issue.ID, issue.WorkspaceID, issue.TrackerConnectionID, "set_labels", payload, false); err != nil {
		slog.Warn("enqueue set_labels outbox failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
	}
}

func canonicalGitlabLabelNames(issue db.Issue, labels []db.IssueLabel) []string {
	ordinary := make([]string, 0, len(labels))
	for _, label := range labels {
		if label.SourceType != "gitlab" || label.MappingKind != string(gitlabtracker.MappingNone) {
			continue
		}
		if !label.GitlabTrackerConnectionID.Valid || uuidToString(label.GitlabTrackerConnectionID) != uuidToString(issue.TrackerConnectionID) {
			continue
		}
		ordinary = append(ordinary, label.Name)
	}
	return gitlabtracker.CanonicalLabels(issue.Status, issue.Priority, ordinary)
}

func (h *Handler) syncStoredMappedLabels(ctx context.Context, issue db.Issue, desired []string) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
DELETE FROM issue_to_label itl
USING issue_label l
WHERE itl.issue_id=$1 AND itl.label_id=l.id
  AND l.gitlab_tracker_connection_id=$2 AND l.mapping_kind <> 'none'`, issue.ID, issue.TrackerConnectionID); err != nil {
		return err
	}
	for _, name := range desired {
		if gitlabtracker.ClassifyLabel(name) == gitlabtracker.MappingNone {
			continue
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO issue_to_label(issue_id,label_id)
SELECT $1,id FROM issue_label
WHERE gitlab_tracker_connection_id=$2 AND mapping_kind <> 'none' AND LOWER(name)=LOWER($3)
ON CONFLICT DO NOTHING`, issue.ID, issue.TrackerConnectionID, name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
