package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// InvitationResponse is the JSON shape returned for a workspace invitation.
type InvitationResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	InviterID     string  `json:"inviter_id"`
	InviteeEmail  string  `json:"invitee_email"`
	InviteeUserID *string `json:"invitee_user_id"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	ExpiresAt     string  `json:"expires_at"`
	InviteCode    string  `json:"invite_code,omitempty"`
	// Enriched fields (present in list responses).
	InviterName   string `json:"inviter_name,omitempty"`
	InviterEmail  string `json:"inviter_email,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}

func invitationToResponse(inv db.WorkspaceInvitation) InvitationResponse {
	return InvitationResponse{
		ID:            uuidToString(inv.ID),
		WorkspaceID:   uuidToString(inv.WorkspaceID),
		InviterID:     uuidToString(inv.InviterID),
		InviteeEmail:  inv.InviteeEmail,
		InviteeUserID: uuidToPtr(inv.InviteeUserID),
		Role:          inv.Role,
		Status:        inv.Status,
		CreatedAt:     timestampToString(inv.CreatedAt),
		UpdatedAt:     timestampToString(inv.UpdatedAt),
		ExpiresAt:     timestampToString(inv.ExpiresAt),
		InviteCode:    inv.InviteCode.String,
	}
}

// ---------------------------------------------------------------------------
// CreateInvitation replaces the old "instant-add" CreateMember flow.
// POST /api/workspaces/{id}/members  (same endpoint, new behaviour)
// ---------------------------------------------------------------------------

func (h *Handler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	role, valid := normalizeMemberRole(req.Role)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid member role")
		return
	}
	if role == "owner" {
		writeError(w, http.StatusBadRequest, "cannot invite as owner")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Code-based invitation (no email required).
	if email == "" {
		code := generatePairingCode(6)
		inv, err := h.Queries.CreateInvitationWithCode(r.Context(), db.CreateInvitationWithCodeParams{
			WorkspaceID: requester.WorkspaceID,
			InviterID:   requester.UserID,
			InviteeEmail: "",
			Role:        role,
			InviteCode:  pgtype.Text{String: code, Valid: true},
		})
		if err != nil {
			slog.Warn("create code invitation failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create invitation")
			return
		}
		slog.Info("code invitation created", append(logger.RequestAttrs(r), "invitation_id", uuidToString(inv.ID), "workspace_id", workspaceID, "code", code, "role", role)...)
		resp := invitationToResponse(inv)
		userID := requestUserID(r)
		h.publish(protocol.EventInvitationCreated, uuidToString(requester.WorkspaceID), "member", userID, map[string]any{"invitation": resp})
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Email-based invitation (legacy flow).

	// Check if the user is already a member.
	existingUser, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err == nil {
		_, memberErr := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      existingUser.ID,
			WorkspaceID: requester.WorkspaceID,
		})
		if memberErr == nil {
			writeError(w, http.StatusConflict, "user is already a member")
			return
		}
	}

	if err := h.Queries.ExpireStalePendingInvitations(r.Context(), db.ExpireStalePendingInvitationsParams{
		WorkspaceID:  requester.WorkspaceID,
		InviteeEmail: email,
	}); err != nil {
		slog.Warn("expire stale invitations failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}

	_, err = h.Queries.GetPendingInvitationByEmail(r.Context(), db.GetPendingInvitationByEmailParams{
		WorkspaceID:  requester.WorkspaceID,
		InviteeEmail: email,
	})
	if err == nil {
		writeError(w, http.StatusConflict, "invitation already pending for this email")
		return
	}

	var inviteeUserID pgtype.UUID
	if existingUser.ID.Valid {
		inviteeUserID = existingUser.ID
	}

	inv, err := h.Queries.CreateInvitation(r.Context(), db.CreateInvitationParams{
		WorkspaceID:   requester.WorkspaceID,
		InviterID:     requester.UserID,
		InviteeEmail:  email,
		InviteeUserID: inviteeUserID,
		Role:          role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "invitation already pending for this email")
			return
		}
		slog.Warn("create invitation failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}

	slog.Info("invitation created", append(logger.RequestAttrs(r), "invitation_id", uuidToString(inv.ID), "workspace_id", workspaceID, "email", email, "role", role)...)

	resp := invitationToResponse(inv)

	userID := requestUserID(r)
	eventPayload := map[string]any{"invitation": resp}
	var workspaceName string
	if ws, err := h.Queries.GetWorkspace(r.Context(), requester.WorkspaceID); err == nil {
		workspaceName = ws.Name
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventInvitationCreated, uuidToString(requester.WorkspaceID), "member", userID, eventPayload)

	h.Analytics.Capture(analytics.TeamInviteSent(
		uuidToString(requester.UserID),
		uuidToString(requester.WorkspaceID),
		email,
		"email",
	))

	if h.EmailService != nil && workspaceName != "" {
		inviterName := email
		if inviter, err := h.Queries.GetUser(r.Context(), requester.UserID); err == nil {
			inviterName = inviter.Name
		}
		invID := uuidToString(inv.ID)
		go func() {
			if err := h.EmailService.SendInvitationEmail(email, inviterName, workspaceName, invID); err != nil {
				slog.Warn("failed to send invitation email", "email", email, "error", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, resp)
}

// ---------------------------------------------------------------------------
// ListWorkspaceInvitations — pending invitations for a workspace (admin view).
// GET /api/workspaces/{id}/invitations
// ---------------------------------------------------------------------------

func (h *Handler) ListWorkspaceInvitations(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListPendingInvitationsByWorkspace(r.Context(), workspaceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}

	resp := make([]InvitationResponse, len(rows))
	for i, row := range rows {
		resp[i] = InvitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			InviterID:     uuidToString(row.InviterID),
			InviteeEmail:  row.InviteeEmail,
			InviteeUserID: uuidToPtr(row.InviteeUserID),
			Role:          row.Role,
			Status:        row.Status,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
			InviterName:   row.InviterName,
			InviterEmail:  row.InviterEmail,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// RevokeInvitation — admin cancels a pending invitation.
// DELETE /api/workspaces/{id}/invitations/{invitationId}
// ---------------------------------------------------------------------------

func (h *Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	invitationID := chi.URLParam(r, "invitationId")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	invitationUUID, ok := parseUUIDOrBadRequest(w, invitationID, "invitation id")
	if !ok {
		return
	}

	inv, err := h.Queries.GetInvitation(r.Context(), invitationUUID)
	if err != nil || uuidToString(inv.WorkspaceID) != uuidToString(workspaceUUID) || inv.Status != "pending" {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}

	if err := h.Queries.RevokeInvitation(r.Context(), inv.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke invitation")
		return
	}

	slog.Info("invitation revoked", "invitation_id", invitationID, "workspace_id", workspaceID)

	userID := requestUserID(r)
	h.publish(protocol.EventInvitationRevoked, uuidToString(workspaceUUID), "member", userID, map[string]any{
		"invitation_id":   uuidToString(inv.ID),
		"invitee_email":   inv.InviteeEmail,
		"invitee_user_id": uuidToPtr(inv.InviteeUserID),
	})

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// GetMyInvitation — get a single invitation by ID (for the invite accept page).
// GET /api/invitations/{id}
// ---------------------------------------------------------------------------

func (h *Handler) GetMyInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	invitationUUID, ok := parseUUIDOrBadRequest(w, invitationID, "invitation id")
	if !ok {
		return
	}
	inv, err := h.Queries.GetInvitation(r.Context(), invitationUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}

	// Verify the invitation belongs to the current user.
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != inv.InviteeEmail && uuidToString(inv.InviteeUserID) != userID {
		writeError(w, http.StatusForbidden, "invitation does not belong to you")
		return
	}

	resp := invitationToResponse(inv)

	// Enrich with workspace name and inviter name.
	if ws, err := h.Queries.GetWorkspace(r.Context(), inv.WorkspaceID); err == nil {
		resp.WorkspaceName = ws.Name
	}
	if inviter, err := h.Queries.GetUser(r.Context(), inv.InviterID); err == nil {
		resp.InviterName = inviter.Name
		resp.InviterEmail = inviter.Email
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// ListMyInvitations — current user's pending invitations across all workspaces.
// GET /api/invitations
// ---------------------------------------------------------------------------

func (h *Handler) ListMyInvitations(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	rows, err := h.Queries.ListPendingInvitationsForUser(r.Context(), db.ListPendingInvitationsForUserParams{
		InviteeUserID: user.ID,
		InviteeEmail:  user.Email,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}

	resp := make([]InvitationResponse, len(rows))
	for i, row := range rows {
		resp[i] = InvitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			InviterID:     uuidToString(row.InviterID),
			InviteeEmail:  row.InviteeEmail,
			InviteeUserID: uuidToPtr(row.InviteeUserID),
			Role:          row.Role,
			Status:        row.Status,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
			WorkspaceName: row.WorkspaceName,
			InviterName:   row.InviterName,
			InviterEmail:  row.InviterEmail,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// AcceptInvitation — user accepts a pending invitation.
// POST /api/invitations/{id}/accept
// ---------------------------------------------------------------------------

func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	invitationUUID, ok := parseUUIDOrBadRequest(w, invitationID, "invitation id")
	if !ok {
		return
	}
	inv, err := h.Queries.GetInvitation(r.Context(), invitationUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}

	// Verify the invitation belongs to the current user.
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != inv.InviteeEmail && uuidToString(inv.InviteeUserID) != userID {
		writeError(w, http.StatusForbidden, "invitation does not belong to you")
		return
	}

	if inv.Status != "pending" {
		writeError(w, http.StatusBadRequest, "invitation is not pending")
		return
	}

	// Check expiry.
	if inv.ExpiresAt.Valid && inv.ExpiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusGone, "invitation has expired")
		return
	}

	// Use a transaction: mark accepted + create member atomically.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	accepted, err := qtx.AcceptInvitation(r.Context(), inv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}

	member, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: accepted.WorkspaceID,
		UserID:      user.ID,
		Role:        accepted.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "you are already a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}

	// Accepting an invite marks the invitee as onboarded. The web /
	// desktop workspace layout has a hard onboarded_at gate; without
	// this mark, an invitee landing on their first workspace would be
	// redirected back to /onboarding to fill out a questionnaire for a
	// workspace someone else already set up. Atomic with CreateMember so
	// `member` and `onboarded_at` can never disagree. COALESCE in
	// MarkUserOnboarded keeps the call idempotent for users joining
	// additional workspaces after their first.
	firstOnboardingCompletion := !user.OnboardedAt.Valid
	onboardedUser, err := qtx.MarkUserOnboarded(r.Context(), user.ID)
	if err != nil {
		slog.Warn("accept invitation: mark user onboarded failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(accepted.WorkspaceID))...)
		writeError(w, http.StatusInternalServerError, "failed to mark user onboarded")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}

	slog.Info("invitation accepted", "invitation_id", invitationID, "user_id", userID, "workspace_id", uuidToString(accepted.WorkspaceID))

	wsID := uuidToString(accepted.WorkspaceID)
	memberResp := memberWithUserResponse(member, user)

	// Broadcast member:added so existing clients update their member lists.
	eventPayload := map[string]any{"member": memberResp}
	if ws, err := h.Queries.GetWorkspace(r.Context(), accepted.WorkspaceID); err == nil {
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventMemberAdded, wsID, "member", userID, eventPayload)

	// Notify the workspace about the acceptance.
	h.publish(protocol.EventInvitationAccepted, wsID, "member", userID, map[string]any{
		"invitation_id": uuidToString(accepted.ID),
		"member":        memberResp,
	})

	// days_since_invite rounds down to whole days so the funnel segments
	// "accepted same day" cleanly from "accepted later". inv.CreatedAt is
	// the invitation row's insertion time so this is safe to compute here.
	var daysSinceInvite int64
	if inv.CreatedAt.Valid {
		daysSinceInvite = int64(time.Since(inv.CreatedAt.Time).Hours() / 24)
	}
	h.Analytics.Capture(analytics.TeamInviteAccepted(
		userID,
		wsID,
		daysSinceInvite,
	))
	if firstOnboardingCompletion {
		onboardedAt := ""
		if onboardedUser.OnboardedAt.Valid {
			onboardedAt = onboardedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		h.Analytics.Capture(analytics.OnboardingCompleted(
			userID,
			wsID,
			analytics.OnboardingPathInviteAccept,
			onboardedAt,
			onboardedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, memberResp)
}

// ---------------------------------------------------------------------------
// DeclineInvitation — user declines a pending invitation.
// POST /api/invitations/{id}/decline
// ---------------------------------------------------------------------------

func (h *Handler) DeclineInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	invitationUUID, ok := parseUUIDOrBadRequest(w, invitationID, "invitation id")
	if !ok {
		return
	}
	inv, err := h.Queries.GetInvitation(r.Context(), invitationUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}

	// Verify the invitation belongs to the current user.
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != inv.InviteeEmail && uuidToString(inv.InviteeUserID) != userID {
		writeError(w, http.StatusForbidden, "invitation does not belong to you")
		return
	}

	if inv.Status != "pending" {
		writeError(w, http.StatusBadRequest, "invitation is not pending")
		return
	}

	declined, err := h.Queries.DeclineInvitation(r.Context(), inv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decline invitation")
		return
	}

	slog.Info("invitation declined", "invitation_id", invitationID, "user_id", userID)

	wsID := uuidToString(declined.WorkspaceID)
	h.publish(protocol.EventInvitationDeclined, wsID, "member", userID, map[string]any{
		"invitation_id": uuidToString(declined.ID),
		"invitee_email": declined.InviteeEmail,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// RedeemInvitation — redeem a short invite code (public, no auth required).
// POST /api/invitations/redeem
// ---------------------------------------------------------------------------

func (h *Handler) RedeemInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code       string `json:"code"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	inv, err := h.Queries.GetPendingInvitationByCode(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusNotFound, "invitation not found or expired")
		return
	}

	if inv.ExpiresAt.Valid && inv.ExpiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusGone, "invitation has expired")
		return
	}

	// Create a local user for the redeemer (or find existing by device name).
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = "Remote User"
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redeem invitation")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	// Create a new user with a placeholder email derived from the invite code.
	placeholderEmail := strings.ToLower(code) + "@local.rimedeck"
	user, err := qtx.CreateUser(r.Context(), db.CreateUserParams{
		Name:  deviceName,
		Email: placeholderEmail,
	})
	if err != nil {
		if isUniqueViolation(err) {
			// User already exists — look them up.
			user, err = qtx.GetUserByEmail(r.Context(), placeholderEmail)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to resolve user")
				return
			}
		} else {
			slog.Warn("redeem invitation: create user failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create user")
			return
		}
	}

	accepted, err := qtx.AcceptInvitation(r.Context(), inv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}

	member, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: accepted.WorkspaceID,
		UserID:      user.ID,
		Role:        accepted.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "already a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}

	if _, err := qtx.MarkUserOnboarded(r.Context(), user.ID); err != nil {
		slog.Warn("redeem invitation: mark user onboarded failed", "error", err)
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redeem invitation")
		return
	}

	// Issue a daemon token so the remote daemon can register against
	// this server and share its compute.
	var daemonToken string
	{
		rawBytes := make([]byte, 32)
		if _, err := rand.Read(rawBytes); err == nil {
			raw := "mdt_" + hex.EncodeToString(rawBytes)
			hash := sha256.Sum256([]byte(raw))
			did := deviceName
			if did == "" {
				did = fmt.Sprintf("joined-%s", raw[4:12])
			}
			if _, err := h.Queries.CreateDaemonToken(r.Context(), db.CreateDaemonTokenParams{
				TokenHash:   hex.EncodeToString(hash[:]),
				WorkspaceID: accepted.WorkspaceID,
				DaemonID:    did,
				ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(365 * 24 * time.Hour), Valid: true},
			}); err == nil {
				daemonToken = raw
			}
		}
	}

	slog.Info("invitation redeemed via code", "code", code, "user_id", uuidToString(user.ID), "workspace_id", uuidToString(accepted.WorkspaceID))

	wsID := uuidToString(accepted.WorkspaceID)
	memberResp := memberWithUserResponse(member, user)
	h.publish(protocol.EventMemberAdded, wsID, "member", uuidToString(user.ID), map[string]any{"member": memberResp})

	resp := map[string]any{
		"member":       memberResp,
		"workspace_id": wsID,
		"user_id":      uuidToString(user.ID),
	}
	if daemonToken != "" {
		resp["token"] = daemonToken
	}
	writeJSON(w, http.StatusOK, resp)
}
