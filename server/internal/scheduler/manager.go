package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options configure a Manager. Defaults are set in NewManager so all
// fields are optional for callers.
type Options struct {
	// RunnerID identifies this process in audit rows. Empty defaults
	// to a fresh UUID — readable enough for short-lived debugging,
	// still unique across replicas.
	RunnerID string

	// TickInterval is how often the manager wakes up to evaluate due
	// plans across all registered jobs. Should be smaller than the
	// shortest job cadence; defaults to 30 * time.Second.
	TickInterval time.Duration

	// Logger is used for structured logs. nil defaults to
	// slog.Default().
	Logger *slog.Logger
}

// Manager is the per-process scheduler. Register one or more jobs and
// call Run with a cancellable context.
type Manager struct {
	pool   *pgxpool.Pool
	opts   Options
	jobs   map[string]*JobSpec
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewManager constructs a Manager. The pool MUST point at the database
// containing the sys_cron_executions table. The manager does not start
// any goroutine until Run is called.
func NewManager(pool *pgxpool.Pool, opts Options) *Manager {
	if opts.RunnerID == "" {
		opts.RunnerID = uuid.NewString()
	}
	if opts.TickInterval <= 0 {
		opts.TickInterval = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Manager{
		pool:   pool,
		opts:   opts,
		jobs:   make(map[string]*JobSpec),
		logger: opts.Logger.With("component", "scheduler", "runner_id", opts.RunnerID),
	}
}

// Register adds a job to the manager. Must be called before Run; later
// registrations are also accepted but the new job will not tick until
// the next loop iteration.
func (m *Manager) Register(job JobSpec) error {
	if err := job.validate(); err != nil {
		return err
	}
	spec := job
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.jobs[spec.Name]; exists {
		return fmt.Errorf("scheduler: duplicate job name %q", spec.Name)
	}
	m.jobs[spec.Name] = &spec
	return nil
}

func (m *Manager) snapshot() []*JobSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*JobSpec, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j)
	}
	return out
}

// Run blocks until ctx is cancelled, ticking every Options.TickInterval.
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("scheduler starting",
		"tick_interval", m.opts.TickInterval.String(),
		"jobs", len(m.snapshot()))

	if err := m.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("scheduler tick error", "error", err)
	}

	t := time.NewTicker(m.opts.TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.logger.Info("scheduler stopped", "reason", ctx.Err())
			return ctx.Err()
		case <-t.C:
			if err := m.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				m.logger.Warn("scheduler tick error", "error", err)
			}
		}
	}
}

func (m *Manager) runOnce(ctx context.Context) error {
	now, err := dbNow(ctx, m.pool)
	if err != nil {
		return err
	}
	for _, job := range m.snapshot() {
		if err := m.runJob(ctx, job, now); err != nil {
			m.logger.Warn("job tick error",
				"job", job.Name, "error", err)
		}
	}
	return nil
}

func (m *Manager) runJob(ctx context.Context, job *JobSpec, now time.Time) error {
	scopes, err := job.Scopes(ctx, now)
	if err != nil {
		return fmt.Errorf("scheduler: scope provider for %q: %w", job.Name, err)
	}

	if affected, err := markStaleAsFailed(ctx, m.pool, job.Name, now); err != nil {
		m.logger.Warn("scheduler: mark stale failed",
			"job", job.Name, "error", err)
	} else if affected > 0 {
		m.logger.Warn("scheduler: closed out abandoned RUNNING leases",
			"job", job.Name,
			"rows", affected,
			"reentrant", job.AllowStaleReentry)
	}

	for _, scope := range scopes {
		plans, err := m.plansForTick(ctx, job, scope, now)
		if err != nil {
			m.logger.Warn("scheduler: plan computation",
				"job", job.Name, "scope", scope.String(), "error", err)
			continue
		}
		for _, planTime := range plans {
			m.processPlan(ctx, job, scope, planTime, now)
		}
	}
	return nil
}

func (m *Manager) plansForTick(
	ctx context.Context,
	job *JobSpec,
	scope Scope,
	now time.Time,
) ([]time.Time, error) {
	eligible := now.Add(-job.ScheduleDelay)
	latest := FloorPlan(eligible, job.Cadence)
	if latest.After(eligible) {
		return nil, nil
	}

	switch job.CatchUpMode {
	case CatchUpLatestOnly:
		return []time.Time{latest}, nil

	case CatchUpEveryPlan:
		info, err := latestPlan(ctx, m.pool, job.Name, scope)
		if err != nil {
			return nil, err
		}
		oldestAllowed := now.Add(-job.CatchUpWindow)
		if job.CatchUpWindow <= 0 {
			oldestAllowed = latest
		}
		var start time.Time
		switch {
		case info.Found && info.RetryEligible(now):
			start = info.PlanTime
		case info.Found:
			start = info.PlanTime.Add(job.Cadence)
		default:
			start = latest
		}
		if start.Before(oldestAllowed) {
			start = FloorPlan(oldestAllowed, job.Cadence)
			if start.Before(oldestAllowed) {
				start = start.Add(job.Cadence)
			}
		}
		var plans []time.Time
		for t := start; !t.After(latest) && len(plans) < job.MaxPlansPerTick; t = t.Add(job.Cadence) {
			plans = append(plans, t)
		}
		return plans, nil

	default:
		return nil, fmt.Errorf("scheduler: job %q: unknown catch_up_mode %v", job.Name, job.CatchUpMode)
	}
}

func (m *Manager) processPlan(
	ctx context.Context,
	job *JobSpec,
	scope Scope,
	planTime time.Time,
	now time.Time,
) {
	c, err := tryClaim(ctx, m.pool, job, scope, planTime, now, m.opts.RunnerID)
	if err != nil {
		m.logger.Warn("scheduler claim error",
			"job", job.Name, "scope", scope.String(),
			"plan_time", planTime.Format(time.RFC3339), "error", err)
		return
	}
	if !c.Won && !c.Stole {
		return
	}

	m.runClaimed(ctx, job, scope, planTime, c)
}

func (m *Manager) runClaimed(
	ctx context.Context,
	job *JobSpec,
	scope Scope,
	planTime time.Time,
	c claim,
) {
	log := m.logger.With(
		"job", job.Name,
		"scope", scope.String(),
		"plan_time", planTime.Format(time.RFC3339),
		"attempt", c.Attempt,
		"execution_id", c.ID.String())

	if c.Stole {
		log.Info("scheduler stole stale lease")
	} else {
		log.Info("scheduler claimed plan")
	}

	runCtx, cancel := context.WithTimeout(ctx, job.RunTimeout)
	defer cancel()

	hbCtx, hbCancel := context.WithCancel(context.Background())
	defer hbCancel()
	hbDone := make(chan struct{})
	go m.runHeartbeats(hbCtx, hbDone, job, c, log)

	start := time.Now()
	res, handlerErr := func() (out HandlerResult, retErr error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("scheduler handler panic", "panic", r)
				retErr = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
			}
		}()
		return job.Handler(runCtx, HandlerInput{
			Job:      job,
			Scope:    scope,
			PlanTime: planTime,
			Attempt:  c.Attempt,
			RunnerID: m.opts.RunnerID,
			Heartbeat: func(ctx context.Context) error {
				return heartbeat(ctx, m.pool, c.ID, c.LeaseToken, job.StaleTimeout)
			},
		})
	}()
	duration := time.Since(start)

	hbCancel()
	<-hbDone

	dur := duration.Milliseconds()
	dbTime, dberr := dbNow(context.Background(), m.pool)
	if dberr != nil {
		dbTime = time.Now().UTC()
	}

	if handlerErr != nil {
		nextRetry := time.Time{}
		if c.Attempt < job.MaxAttempts {
			delay := job.retryDelay(c.Attempt)
			nextRetry = dbTime.Add(delay)
		}
		errCode := classifyError(handlerErr)
		if err := finishFailure(context.Background(), m.pool, c.ID, c.LeaseToken,
			dbTime, dur, errCode, handlerErr.Error(), nextRetry); err != nil {
			if errors.Is(err, ErrLeaseLost) {
				log.Warn("scheduler: terminal FAILED ignored, lease was stolen",
					"duration_ms", dur, "error", handlerErr.Error())
				return
			}
			log.Error("scheduler: write terminal FAILED",
				"duration_ms", dur, "handler_error", handlerErr.Error(), "error", err)
			return
		}
		log.Warn("scheduler: handler failed",
			"duration_ms", dur,
			"error_code", errCode,
			"error", handlerErr.Error(),
			"will_retry", c.Attempt < job.MaxAttempts)
		return
	}

	if err := finishSuccess(context.Background(), m.pool, c.ID, c.LeaseToken,
		dbTime, dur, res); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			log.Warn("scheduler: terminal SUCCESS ignored, lease was stolen",
				"duration_ms", dur)
			return
		}
		log.Error("scheduler: write terminal SUCCESS",
			"duration_ms", dur, "error", err)
		return
	}
	log.Info("scheduler: handler succeeded",
		"duration_ms", dur,
		"rows_affected", res.RowsAffected)
}

func (m *Manager) runHeartbeats(
	ctx context.Context,
	done chan<- struct{},
	job *JobSpec,
	c claim,
	log *slog.Logger,
) {
	defer close(done)
	t := time.NewTicker(job.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			hbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := heartbeat(hbCtx, m.pool, c.ID, c.LeaseToken, job.StaleTimeout)
			cancel()
			if errors.Is(err, ErrLeaseLost) {
				log.Warn("scheduler: lease lost during heartbeat, runner should stop")
				return
			}
			if err != nil {
				log.Warn("scheduler: heartbeat error", "error", err)
			}
		}
	}
}

func classifyError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "run_timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrLeaseLost):
		return "lease_lost"
	case errors.Is(err, ErrHandlerPanic):
		return "handler_panic"
	default:
		return "handler_error"
	}
}

// ErrHandlerPanic wraps a panic value recovered from a job handler.
var ErrHandlerPanic = errors.New("scheduler: handler panic")
