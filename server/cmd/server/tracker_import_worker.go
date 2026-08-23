package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/gitlabsync"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const trackerImportInterval = time.Minute

// Reconcile cadences match design §9.3.
const (
	incrementalReconcileInterval = 5 * time.Minute
	fullReconcileInterval        = 6 * time.Hour
)

type trackerOutboxDrainer interface {
	Tick(context.Context) (gitlabsync.TickResult, error)
}

// runTrackerImportLoop separates explicit local work from periodic remote
// reconciliation. The wake channel is only a hint; the outbox remains the
// durable source of truth and the periodic trigger is the recovery path for
// signals lost during process restarts or transient failures.
func runTrackerImportLoop(
	ctx context.Context,
	drainer trackerOutboxDrainer,
	schedule func(context.Context) error,
	periodic <-chan time.Time,
	wake <-chan struct{},
) {
	if err := schedule(ctx); err != nil {
		slog.Warn("GitLab tracker reconcile schedule failed", "error", err)
	}
	drainTrackerOutbox(ctx, drainer)

	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			drainTrackerOutbox(ctx, drainer)
		case <-periodic:
			if err := schedule(ctx); err != nil {
				slog.Warn("GitLab tracker reconcile schedule failed", "error", err)
			}
			drainTrackerOutbox(ctx, drainer)
		}
	}
}

func drainTrackerOutbox(ctx context.Context, drainer trackerOutboxDrainer) {
	for {
		result, err := drainer.Tick(ctx)
		if err != nil {
			slog.Warn("GitLab tracker import tick failed", "error", err)
			return
		}
		if result.Claimed == 0 {
			return
		}
		slog.Info("GitLab tracker import tick", "claimed", result.Claimed, "success", result.Success, "retried", result.Retried, "failed", result.Failed)
	}
}

// runTrackerImportWorker drains the local outbox. The caller owns the
// context and cancels it during graceful shutdown.
func runTrackerImportWorker(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, cipher *gitlabtracker.Cipher, factory gitlabsync.ClientFactory, taskService *service.TaskService, wake <-chan struct{}) {
	worker := &gitlabsync.Worker{Queries: queries, TxStarter: pool, Cipher: cipher, ClientFactory: factory, BatchSize: gitlabsync.BatchSize}
	worker.OnImportedNote = taskService.HandleImportedGitlabNote
	ticker := time.NewTicker(trackerImportInterval)
	defer ticker.Stop()
	runTrackerImportLoop(ctx, worker, func(ctx context.Context) error {
		return scheduleTrackerReconcile(ctx, queries)
	}, ticker.C, wake)
}

// scheduleTrackerReconcile enqueues at most one pending/retrying row for
// each scheduled pull kind. Without this guard, the minute ticker can flood
// one connection with reconcile/full_reconcile rows while a worker is
// retrying, starving user writes behind the per-connection claim gate.
func scheduleTrackerReconcile(ctx context.Context, queries *db.Queries) error {
	trackers, err := queries.ListActiveTrackersForReconcile(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, tracker := range trackers {
		if !tracker.LastPullAt.Valid || now.Sub(tracker.LastPullAt.Time) >= incrementalReconcileInterval {
			if err := enqueueScheduledTrackerOp(ctx, queries, tracker.WorkspaceID, tracker.ID, "reconcile"); err != nil {
				slog.Warn("GitLab reconcile enqueue failed", "tracker", tracker.ID, "error", err)
			}
		}
		if !tracker.LastFullReconcileAt.Valid || now.Sub(tracker.LastFullReconcileAt.Time) >= fullReconcileInterval {
			if err := enqueueScheduledTrackerOp(ctx, queries, tracker.WorkspaceID, tracker.ID, "full_reconcile"); err != nil {
				slog.Warn("GitLab full reconcile enqueue failed", "tracker", tracker.ID, "error", err)
			}
		}
	}
	return nil
}

func enqueueScheduledTrackerOp(ctx context.Context, queries *db.Queries, workspaceID, trackerID pgtype.UUID, operation string) error {
	return queries.EnqueueScheduledTrackerOutbox(ctx, db.EnqueueScheduledTrackerOutboxParams{
		WorkspaceID: workspaceID, TrackerID: trackerID, Operation: operation,
		Payload: []byte("{}"), IdempotencyKey: newSchedulerUUID(),
	})
}

// newSchedulerUUID mints a fresh idempotency key for scheduler-enqueued
// outbox rows. Uses the same pattern as the handler package's newRandomUUID.
func newSchedulerUUID() pgtype.UUID {
	id := uuid.New()
	var out pgtype.UUID
	copy(out.Bytes[:], id[:])
	out.Valid = true
	return out
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
