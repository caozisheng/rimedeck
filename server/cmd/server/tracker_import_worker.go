package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/gitlabsync"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const trackerImportInterval = time.Minute

// runTrackerImportWorker drains the local outbox. The caller owns the
// context and cancels it during graceful shutdown.
func runTrackerImportWorker(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, cipher *gitlabtracker.Cipher, factory gitlabsync.ClientFactory) {
	worker := &gitlabsync.Worker{Queries: queries, TxStarter: pool, Cipher: cipher, ClientFactory: factory, BatchSize: gitlabsync.BatchSize}
	ticker := time.NewTicker(trackerImportInterval)
	defer ticker.Stop()
	for {
		if result, err := worker.Tick(ctx); err != nil {
			slog.Warn("GitLab tracker import tick failed", "error", err)
		} else if result.Claimed > 0 {
			slog.Info("GitLab tracker import tick", "claimed", result.Claimed, "success", result.Success, "retried", result.Retried, "failed", result.Failed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// trackerCipherFromEnv loads the same versioned keyring format as the
// HTTP handler. Missing keys disable the worker rather than allowing
// plaintext credentials or an infinite decrypt-failure loop.
func trackerCipherFromEnv() (*gitlabtracker.Cipher, error) {
	raw := strings.TrimSpace(os.Getenv("GITLAB_TRACKER_KEYS"))
	if raw == "" {
		return nil, os.ErrNotExist
	}
	keys := make(map[int16]string)
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(entry), "=", 2)
		if len(parts) != 2 {
			return nil, os.ErrInvalid
		}
		var version int
		if _, err := fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(parts[0]), "v"), "%d", &version); err != nil || version <= 0 || version > 32767 {
			return nil, os.ErrInvalid
		}
		keys[int16(version)] = strings.TrimSpace(parts[1])
	}
	return gitlabtracker.NewCipher(keys)
}
