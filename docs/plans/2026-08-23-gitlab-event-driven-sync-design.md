# GitLab Event-Driven Synchronization Design

## Problem

GitLab write operations are durable but not prompt. Issue mutations enqueue `tracker_sync_outbox` rows, while `runTrackerImportWorker` checks the queue only once per minute. The same timer both drains local writes and schedules remote reconciliation. A user can therefore update the Kanban immediately but wait up to one minute before the first GitLab request starts.

The timer is appropriate for background convergence when RimeDeck has no local activity. It is not an appropriate trigger for user-initiated writes.

## Decision

Keep the durable outbox and local-first HTTP semantics. Add an in-process, coalescing wake signal from committed outbox producers to the sync worker.

- A local GitLab issue mutation still commits local state and its outbox intent before returning.
- After the outbox transaction commits, the handler sends a non-blocking signal to a capacity-one channel.
- The worker drains ready outbox rows immediately when signaled and continues until no ready rows remain.
- The periodic ticker schedules remote reconcile/full-reconcile work and remains a safety net for retries and lost process-local signals.
- Webhook and manual sync/retry paths also wake the worker after durable enqueue/reset because they represent explicit work, not background polling.

No GitLab REST request is added to the HTTP request path. Remote failure therefore leaves the local mutation available with its existing `pending`, `retrying`, or `failed` state.

## Components

### Wake channel

`main` creates one buffered `chan struct{}` and passes:

- its send side through `RouterOptions` into `handler.Handler`;
- its receive side to `runTrackerImportWorker`.

Handler notification is non-blocking:

```go
select {
case wake <- struct{}{}:
default:
}
```

Capacity one intentionally coalesces bursts. The outbox is the durable work ledger; the channel only says “re-check it.” No issue IDs or payloads travel through memory.

### Producer boundary

A producer wakes only after the database operation that makes work visible succeeds:

- GitLab issue create: after the transaction containing issue + `create_issue` outbox commits.
- Issue update/delete and label changes: after `enqueueGitlabWriteOp` commits.
- Batch mutations: inherited through the same helper.
- Tracker initial import, webhook ingress, manual sync, and failed-outbox retry: after enqueue/reset succeeds.

Never wake before a transaction commits. Otherwise the worker can observe an empty queue, consume the only signal, and leave the just-committed row waiting for the fallback ticker.

### Worker loop

Separate triggers by intent:

1. Startup or periodic tick: schedule due remote reconciliation, then drain ready work.
2. Wake signal: drain ready work without scheduling a background pull.
3. Context cancellation: stop promptly.

A drain calls `Tick` repeatedly while `Claimed > 0`. `ClaimReadyTrackerOutbox` retains its existing priorities and per-connection serialization, so user writes remain ahead of reconcile rows.

## Error and concurrency behavior

- Channel full: drop the signal; an earlier signal already guarantees another drain.
- Worker disabled or restarting: outbox rows remain durable; startup/periodic drain recovers them.
- Multiple rapid edits: outbox revision compression remains authoritative; wake coalescing avoids one goroutine or signal per edit.
- Retry scheduled in the future: the immediate drain stops when no row is ready; the periodic fallback later retries it.
- Multiple server processes: the in-process signal accelerates writes handled by the process hosting the worker. PostgreSQL `FOR UPDATE SKIP LOCKED` remains the correctness mechanism. Cross-process signaling is intentionally not added because this repository starts the handler and worker in the same server process.

## Verification

- A worker-loop test proves a wake triggers a drain without advancing the periodic timer.
- A burst test proves one wake drains more than one batch/tick until the ready queue is empty.
- Handler tests prove a committed GitLab issue mutation emits a wake while local issues do not.
- Existing GitLab outbox, write-operation, reconcile, and local-issue golden tests remain green.
