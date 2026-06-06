package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobNameRollupTaskUsageHourly is the canonical name used in audit
// rows. Stable across releases — do not rename without a migration.
const JobNameRollupTaskUsageHourly = "rollup_task_usage_hourly"

// advisoryLockKey is the int64 identifier shared by every path that
// writes to task_usage_hourly (the SQL function, the backfill tool).
const advisoryLockKey int64 = 4246

// TaskUsageHourlyJob returns the JobSpec that drives the
// task_usage_hourly rollup. The handler calls the existing
// `rollup_task_usage_hourly()` SQL function, which already holds
// advisory lock 4246 internally so a concurrent legacy pg_cron tick or
// manual call is safe.
func TaskUsageHourlyJob(pool *pgxpool.Pool) JobSpec {
	return JobSpec{
		Name:              JobNameRollupTaskUsageHourly,
		Cadence:           5 * time.Minute,
		ScheduleDelay:     5 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        25 * time.Minute,
		StaleTimeout:      30 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff: []time.Duration{
			1 * time.Minute,
			5 * time.Minute,
			15 * time.Minute,
		},
		Scopes:  StaticScopes(ScopeGlobal),
		Handler: makeTaskUsageHourlyHandler(pool),
	}
}

func makeTaskUsageHourlyHandler(pool *pgxpool.Pool) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		watermarkBefore, err := readTaskUsageWatermark(ctx, pool)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("read watermark before: %w", err)
		}

		var rows int64
		if err := pool.QueryRow(ctx, `SELECT rollup_task_usage_hourly()`).Scan(&rows); err != nil {
			return HandlerResult{}, fmt.Errorf("rollup_task_usage_hourly: %w", err)
		}

		watermarkAfter, err := readTaskUsageWatermark(ctx, pool)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("read watermark after: %w", err)
		}

		if in.Heartbeat != nil {
			_ = in.Heartbeat(ctx)
		}

		result := map[string]any{
			"advisory_lock_id": advisoryLockKey,
		}
		if !watermarkBefore.IsZero() {
			result["watermark_before"] = watermarkBefore.UTC().Format(time.RFC3339)
		}
		if !watermarkAfter.IsZero() {
			result["watermark_after"] = watermarkAfter.UTC().Format(time.RFC3339)
		}
		return HandlerResult{
			RowsAffected: rows,
			Result:       result,
		}, nil
	}
}

func readTaskUsageWatermark(ctx context.Context, pool *pgxpool.Pool) (time.Time, error) {
	var t time.Time
	err := pool.QueryRow(ctx, `
		SELECT watermark_at
		  FROM task_usage_hourly_rollup_state
		 WHERE id = 1
	`).Scan(&t)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return t, nil
}
