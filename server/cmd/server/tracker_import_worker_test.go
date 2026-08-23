package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/gitlabsync"
)

type scriptedTrackerOutboxDrainer struct {
	mu      sync.Mutex
	results []gitlabsync.TickResult
	called  chan struct{}
	onTick  func()
}

func (d *scriptedTrackerOutboxDrainer) Tick(context.Context) (gitlabsync.TickResult, error) {
	d.mu.Lock()
	result := gitlabsync.TickResult{}
	if len(d.results) > 0 {
		result = d.results[0]
		d.results = d.results[1:]
	}
	d.mu.Unlock()
	if d.onTick != nil {
		d.onTick()
	}
	d.called <- struct{}{}
	return result, nil
}

func (d *scriptedTrackerOutboxDrainer) setResults(results ...gitlabsync.TickResult) {
	d.mu.Lock()
	d.results = append([]gitlabsync.TickResult(nil), results...)
	d.mu.Unlock()
}

func waitForTrackerDrain(t *testing.T, called <-chan struct{}) {
	t.Helper()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tracker outbox drain")
	}
}

func TestRunTrackerImportLoop_WakeDrainsWithoutSchedulingReconcile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	drainer := &scriptedTrackerOutboxDrainer{called: make(chan struct{}, 4)}
	wake := make(chan struct{}, 1)
	periodic := make(chan time.Time)
	reconciled := make(chan struct{}, 4)

	go runTrackerImportLoop(ctx, drainer, func(context.Context) error {
		reconciled <- struct{}{}
		return nil
	}, periodic, wake)

	waitForTrackerDrain(t, drainer.called)
	select {
	case <-reconciled:
	case <-time.After(time.Second):
		t.Fatal("startup did not schedule reconciliation")
	}

	wake <- struct{}{}
	waitForTrackerDrain(t, drainer.called)
	select {
	case <-reconciled:
		t.Fatal("explicit wake scheduled background reconciliation")
	default:
	}
}

func TestRunTrackerImportLoop_OneWakeDrainsUntilQueueEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	drainer := &scriptedTrackerOutboxDrainer{called: make(chan struct{}, 8)}
	wake := make(chan struct{}, 1)
	periodic := make(chan time.Time)

	go runTrackerImportLoop(ctx, drainer, func(context.Context) error { return nil }, periodic, wake)
	waitForTrackerDrain(t, drainer.called)

	drainer.setResults(
		gitlabsync.TickResult{Claimed: 2},
		gitlabsync.TickResult{Claimed: 1},
		gitlabsync.TickResult{},
	)
	wake <- struct{}{}
	waitForTrackerDrain(t, drainer.called)
	waitForTrackerDrain(t, drainer.called)
	waitForTrackerDrain(t, drainer.called)
}

func TestRunTrackerImportLoop_PeriodicTickSchedulesBeforeDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	periodic := make(chan time.Time, 1)
	wake := make(chan struct{}, 1)
	var mu sync.Mutex
	events := make([]string, 0, 4)
	drainer := &scriptedTrackerOutboxDrainer{called: make(chan struct{}, 4)}
	drainer.onTick = func() {
		mu.Lock()
		events = append(events, "drain")
		mu.Unlock()
	}

	reconcile := func(context.Context) error {
		mu.Lock()
		events = append(events, "reconcile")
		mu.Unlock()
		return nil
	}
	go runTrackerImportLoop(ctx, drainer, reconcile, periodic, wake)
	waitForTrackerDrain(t, drainer.called)

	mu.Lock()
	events = events[:0]
	mu.Unlock()
	periodic <- time.Now()
	waitForTrackerDrain(t, drainer.called)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "reconcile" || events[1] != "drain" {
		t.Fatalf("periodic events = %v, want [reconcile drain]", events)
	}
}
