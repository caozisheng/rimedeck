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
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// pairingCodeChars excludes ambiguous characters (O/0, I/1, L).
const pairingCodeChars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// PairingStore holds the active device pairing code. Thread-safe.
// The code is single-use: a successful Verify consumes it and generates
// a new one. Failed attempts are rate-limited (max 5 per minute).
type PairingStore struct {
	mu           sync.Mutex
	code         string
	failedCount  int
	windowStart  time.Time
}

const (
	pairingMaxAttempts  = 5
	pairingWindowDuration = time.Minute
)

func NewPairingStore() *PairingStore {
	s := &PairingStore{windowStart: time.Now()}
	s.regenerateLocked()
	return s
}

func (s *PairingStore) Code() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
}

func (s *PairingStore) Regenerate() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.regenerateLocked()
}

func (s *PairingStore) regenerateLocked() string {
	s.code = generatePairingCode(6)
	s.failedCount = 0
	s.windowStart = time.Now()
	return s.code
}

// Verify checks the input against the current code. On success the code
// is consumed and a new one is generated (single-use). Failed attempts
// are rate-limited: after 5 failures within a minute, all attempts are
// rejected until the window resets.
func (s *PairingStore) Verify(input string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset window if expired.
	if time.Since(s.windowStart) > pairingWindowDuration {
		s.failedCount = 0
		s.windowStart = time.Now()
	}

	if s.failedCount >= pairingMaxAttempts {
		return false
	}

	if !strings.EqualFold(strings.TrimSpace(input), s.code) {
		s.failedCount++
		return false
	}

	// Success — consume the code and generate a new one.
	s.regenerateLocked()
	return true
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

	// Generate raw token and its hash.
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	rawToken := "mdt_" + hex.EncodeToString(rawBytes)
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	daemonID := req.DeviceName
	if daemonID == "" {
		daemonID = fmt.Sprintf("paired-%s", rawToken[4:12])
	}

	expiresAt := pgtype.Timestamptz{Time: time.Now().Add(365 * 24 * time.Hour), Valid: true}

	_, err := h.Queries.CreateDaemonToken(r.Context(), db.CreateDaemonTokenParams{
		TokenHash:   tokenHash,
		WorkspaceID: wsUUID,
		DaemonID:    daemonID,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		slog.Warn("device pair: create daemon token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	slog.Info("device paired", "daemon_id", daemonID, "workspace_id", uuidToString(wsUUID))

	// Notify frontends so ConnectRemoteDialog can detect the pairing
	// immediately, without waiting for the remote daemon to restart and
	// register (which can take 10-20 seconds).
	h.publish(protocol.EventDaemonRegister, uuidToString(wsUUID), "system", "", map[string]any{
		"action":    "paired",
		"daemon_id": daemonID,
	})

	writeJSON(w, http.StatusOK, DevicePairResponse{
		Token:       rawToken,
		WorkspaceID: uuidToString(wsUUID),
	})
}

func (h *Handler) firstWorkspace(ctx context.Context) (pgtype.UUID, error) {
	if h.DB == nil {
		return pgtype.UUID{}, fmt.Errorf("no db executor")
	}
	var id pgtype.UUID
	err := h.DB.QueryRow(ctx, "SELECT id FROM workspace ORDER BY created_at ASC LIMIT 1").Scan(&id)
	return id, err
}
