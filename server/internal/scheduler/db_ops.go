package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrLeaseLost is returned by heartbeat / terminal-update primitives
// when the row is no longer owned by the calling runner.
var ErrLeaseLost = errors.New("scheduler: lease lost")

var errTerminalIgnored = ErrLeaseLost

func dbNow(ctx context.Context, pool *pgxpool.Pool) (time.Time, error) {
	var t time.Time
	if err := pool.QueryRow(ctx, "SELECT now()").Scan(&t); err != nil {
		return time.Time{}, fmt.Errorf("scheduler: read db now: %w", err)
	}
	return t.UTC(), nil
}

type claim struct {
	ID         uuid.UUID
	LeaseToken uuid.UUID
	Attempt    int
	Won        bool
	Stole      bool
	Conflicted bool
}

func tryClaim(
	ctx context.Context,
	pool *pgxpool.Pool,
	job *JobSpec,
	scope Scope,
	planTime time.Time,
	dbTime time.Time,
	runnerID string,
) (claim, error) {
	insertSQL := `
		INSERT INTO sys_cron_executions (
			job_name, scope_kind, scope_id, plan_time,
			status, attempt, max_attempts,
			runner_id, lease_token,
			heartbeat_at, stale_after,
			started_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			'RUNNING', 1, $5,
			$6, gen_random_uuid(),
			$7::timestamptz, $7::timestamptz + make_interval(secs => $8),
			$7::timestamptz, $7::timestamptz
		)
		ON CONFLICT ON CONSTRAINT uq_sys_cron_execution DO NOTHING
		RETURNING id, lease_token, attempt
	`
	staleSecs := int64(job.StaleTimeout / time.Second)
	if staleSecs <= 0 {
		staleSecs = 1
	}

	var c claim
	err := pool.QueryRow(ctx, insertSQL,
		job.Name, scope.Kind, scope.ID, planTime,
		job.MaxAttempts,
		runnerID,
		dbTime, staleSecs,
	).Scan(&c.ID, &c.LeaseToken, &c.Attempt)
	if err == nil {
		c.Won = true
		return c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return claim{}, fmt.Errorf("scheduler: claim insert: %w", err)
	}

	stealSQL := `
		UPDATE sys_cron_executions
		   SET status        = 'RUNNING',
		       attempt       = attempt + 1,
		       runner_id     = $1,
		       lease_token   = gen_random_uuid(),
		       heartbeat_at  = $2::timestamptz,
		       stale_after   = $2::timestamptz + make_interval(secs => $3),
		       started_at    = $2::timestamptz,
		       finished_at   = NULL,
		       duration_ms   = NULL,
		       next_retry_at = NULL,
		       error_code    = NULL,
		       error_msg     = NULL,
		       updated_at    = $2::timestamptz
		 WHERE job_name   = $4
		   AND scope_kind = $5
		   AND scope_id   = $6
		   AND plan_time  = $7
		   AND attempt < max_attempts
		   AND (
		        (status = 'FAILED' AND COALESCE(next_retry_at, $2::timestamptz) <= $2::timestamptz)
		        OR
		        (status = 'RUNNING' AND stale_after < $2::timestamptz AND $8)
		   )
		RETURNING id, lease_token, attempt
	`
	err = pool.QueryRow(ctx, stealSQL,
		runnerID,
		dbTime, staleSecs,
		job.Name, scope.Kind, scope.ID, planTime,
		job.AllowStaleReentry,
	).Scan(&c.ID, &c.LeaseToken, &c.Attempt)
	if err == nil {
		c.Stole = true
		return c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return claim{}, fmt.Errorf("scheduler: claim steal: %w", err)
	}

	c.Conflicted = true
	return c, nil
}

func markStaleAsFailed(
	ctx context.Context,
	pool *pgxpool.Pool,
	jobName string,
	dbTime time.Time,
) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET status      = 'FAILED',
		       finished_at = $2,
		       error_code  = 'stale_timeout',
		       error_msg   = 'lease expired without heartbeat',
		       updated_at  = $2
		 WHERE job_name    = $1
		   AND status      = 'RUNNING'
		   AND stale_after < $2
	`, jobName, dbTime)
	if err != nil {
		return 0, fmt.Errorf("scheduler: mark stale failed: %w", err)
	}
	return tag.RowsAffected(), nil
}

func heartbeat(
	ctx context.Context,
	pool *pgxpool.Pool,
	id, leaseToken uuid.UUID,
	staleTimeout time.Duration,
) error {
	staleSecs := int64(staleTimeout / time.Second)
	if staleSecs <= 0 {
		staleSecs = 1
	}
	tag, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET heartbeat_at = now(),
		       stale_after  = now() + make_interval(secs => $3),
		       updated_at   = now()
		 WHERE id          = $1
		   AND lease_token = $2
		   AND status      = 'RUNNING'
	`, id, leaseToken, staleSecs)
	if err != nil {
		return fmt.Errorf("scheduler: heartbeat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func finishSuccess(
	ctx context.Context,
	pool *pgxpool.Pool,
	id, leaseToken uuid.UUID,
	dbTime time.Time,
	durationMs int64,
	res HandlerResult,
) error {
	resultJSON, err := encodeResult(res.Result)
	if err != nil {
		return err
	}

	tag, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET status        = 'SUCCESS',
		       finished_at   = $3,
		       duration_ms   = $4,
		       rows_affected = $5,
		       result        = $6::jsonb,
		       error_code    = NULL,
		       error_msg     = NULL,
		       updated_at    = $3
		 WHERE id            = $1
		   AND lease_token   = $2
		   AND status        = 'RUNNING'
	`, id, leaseToken, dbTime, durationMs, res.RowsAffected, resultJSON)
	if err != nil {
		return fmt.Errorf("scheduler: finish success: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errTerminalIgnored
	}
	return nil
}

func finishFailure(
	ctx context.Context,
	pool *pgxpool.Pool,
	id, leaseToken uuid.UUID,
	dbTime time.Time,
	durationMs int64,
	errorCode, errorMsg string,
	nextRetryAt time.Time,
) error {
	var nextRetry pgtype.Timestamptz
	if !nextRetryAt.IsZero() {
		nextRetry = pgtype.Timestamptz{Time: nextRetryAt, Valid: true}
	}

	if errorCode == "" {
		errorCode = "handler_error"
	}
	if len(errorMsg) > 4000 {
		errorMsg = errorMsg[:4000]
	}

	tag, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET status        = 'FAILED',
		       finished_at   = $3,
		       duration_ms   = $4,
		       next_retry_at = $5,
		       error_code    = $6,
		       error_msg     = $7,
		       updated_at    = $3
		 WHERE id            = $1
		   AND lease_token   = $2
		   AND status        = 'RUNNING'
	`, id, leaseToken, dbTime, durationMs, nextRetry, errorCode, errorMsg)
	if err != nil {
		return fmt.Errorf("scheduler: finish failure: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errTerminalIgnored
	}
	return nil
}

func encodeResult(in map[string]any) (string, error) {
	if len(in) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("scheduler: marshal result: %w", err)
	}
	if len(b) > 16*1024 {
		return "", fmt.Errorf("scheduler: result payload too large (%d bytes); keep it small or use logs", len(b))
	}
	return string(b), nil
}

type latestPlanInfo struct {
	Found       bool
	PlanTime    time.Time
	Status      string
	Attempt     int
	MaxAttempts int
	NextRetryAt time.Time
}

func (i latestPlanInfo) RetryEligible(now time.Time) bool {
	if !i.Found {
		return false
	}
	if i.Status != "FAILED" {
		return false
	}
	if i.Attempt >= i.MaxAttempts {
		return false
	}
	if i.NextRetryAt.IsZero() {
		return true
	}
	return !i.NextRetryAt.After(now)
}

func latestPlan(
	ctx context.Context,
	pool *pgxpool.Pool,
	jobName string,
	scope Scope,
) (latestPlanInfo, error) {
	var info latestPlanInfo
	var nextRetry pgtype.Timestamptz
	err := pool.QueryRow(ctx, `
		SELECT plan_time, status, attempt, max_attempts, next_retry_at
		  FROM sys_cron_executions
		 WHERE job_name   = $1
		   AND scope_kind = $2
		   AND scope_id   = $3
		 ORDER BY plan_time DESC
		 LIMIT 1
	`, jobName, scope.Kind, scope.ID).Scan(
		&info.PlanTime, &info.Status,
		&info.Attempt, &info.MaxAttempts,
		&nextRetry,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return info, nil
		}
		return info, fmt.Errorf("scheduler: read latest plan: %w", err)
	}
	info.Found = true
	info.PlanTime = info.PlanTime.UTC()
	if nextRetry.Valid {
		info.NextRetryAt = nextRetry.Time.UTC()
	}
	return info, nil
}
