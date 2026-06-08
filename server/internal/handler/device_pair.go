package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// pairingCodeChars excludes ambiguous characters (O/0, I/1, L).
const pairingCodeChars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// PairingStore holds the active device pairing code. Thread-safe.
type PairingStore struct {
	mu   sync.RWMutex
	code string
}

func NewPairingStore() *PairingStore {
	s := &PairingStore{}
	s.Regenerate()
	return s
}

func (s *PairingStore) Code() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.code
}

func (s *PairingStore) Regenerate() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code = generatePairingCode(6)
	return s.code
}

func (s *PairingStore) Verify(input string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.EqualFold(strings.TrimSpace(input), s.code)
}

func generatePairingCode(length int) string {
	chars := []byte(pairingCodeChars)
	max := big.NewInt(int64(len(chars)))
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, max)
		b[i] = chars[n.Int64()]
	}
	return string(b)
}

type DevicePairRequest struct {
	Code        string `json:"code"`
	DeviceName  string `json:"device_name"`
	WorkspaceID string `json:"workspace_id"`
}

type DevicePairResponse struct {
	Token       string `json:"token"`
	WorkspaceID string `json:"workspace_id"`
}

// DevicePair validates the pairing code and issues a daemon token for the
// first workspace found (single-workspace local deployments). The caller
// (remote daemon) stores the token and uses it for all subsequent requests.
func (h *Handler) DevicePair(w http.ResponseWriter, r *http.Request) {
	if h.PairingStore == nil {
		writeError(w, http.StatusServiceUnavailable, "pairing not available")
		return
	}

	var req DevicePairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !h.PairingStore.Verify(req.Code) {
		writeError(w, http.StatusForbidden, "invalid pairing code")
		return
	}

	// Resolve workspace: use provided ID or fall back to first available.
	var wsUUID pgtype.UUID
	if req.WorkspaceID != "" {
		wsUUID = parseUUID(req.WorkspaceID)
	} else {
		ws, err := h.firstWorkspace(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "no workspace available")
			return
		}
		wsUUID = ws
	}

	rawToken, err := h.issueDaemonToken(r.Context(), wsUUID, req.DeviceName)
	if err != nil {
		slog.Warn("device pair: issue daemon token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	slog.Info("device paired", "device_name", req.DeviceName, "workspace_id", uuidToString(wsUUID))

	writeJSON(w, http.StatusOK, DevicePairResponse{
		Token:       rawToken,
		WorkspaceID: uuidToString(wsUUID),
	})
}

// issueDaemonToken generates a daemon token for the given workspace.
// Used by both DevicePair and RedeemInvitation so a joining device can
// authenticate its daemon without a separate pairing step.
func (h *Handler) issueDaemonToken(ctx context.Context, wsUUID pgtype.UUID, deviceName string) (string, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	rawToken := "mdt_" + hex.EncodeToString(rawBytes)
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	daemonID := deviceName
	if daemonID == "" {
		daemonID = fmt.Sprintf("joined-%s", rawToken[4:12])
	}

	expiresAt := pgtype.Timestamptz{Time: time.Now().Add(365 * 24 * time.Hour), Valid: true}
	if _, err := h.Queries.CreateDaemonToken(ctx, db.CreateDaemonTokenParams{
		TokenHash:   tokenHash,
		WorkspaceID: wsUUID,
		DaemonID:    daemonID,
		ExpiresAt:   expiresAt,
	}); err != nil {
		return "", fmt.Errorf("create daemon token: %w", err)
	}
	return rawToken, nil
}

func (h *Handler) firstWorkspace(ctx context.Context) (pgtype.UUID, error) {
	if h.DB == nil {
		return pgtype.UUID{}, fmt.Errorf("no db executor")
	}
	var id pgtype.UUID
	err := h.DB.QueryRow(ctx, "SELECT id FROM workspace ORDER BY created_at ASC LIMIT 1").Scan(&id)
	return id, err
}
