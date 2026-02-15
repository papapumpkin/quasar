+++
id = "consume-watch-events"
title = "Consume watcher events in WorkerGroup.Run"
type = "task"
priority = 2
depends_on = []
+++

## Problem

`WorkerGroup.Watcher` is set but never read. The `Run` method uses a polling loop (`for ctx.Err() == nil`) that checks for eligible tasks, but never consults the `Watcher.Changes` channel. The `--watch` flag is a no-op.

## Solution

Refactor the main loop in `WorkerGroup.Run` (`internal/nebula/worker.go`) to multiplex between:
1. Task completion signals (existing behavior)
2. `Watcher.Changes` channel (new behavior, only when `wg.Watcher != nil`)

When `Watcher` is nil, behavior must be identical to today.

### Key changes

- Replace the blocking `wgSync.Wait()` with a channel-based completion signal so we can `select` on both task completions and watch events.
- When a `Change` arrives, call a new method `wg.handleChange(change)` that dispatches based on `change.Kind`.
- The `handleChange` method is a stub in this task — subsequent tasks implement the actual handling for each `ChangeKind`.

### Design

```go
// In the main loop, replace:
//   wgSync.Wait()
// With select on:
//   case <-taskDone:    // a worker finished
//   case change := <-watchCh:  // file changed
//   case <-ctx.Done():  // shutdown
```

Use a `taskDone` channel that each goroutine sends on when it finishes (in addition to `wgSync.Done()`). When `wg.Watcher` is nil, set `watchCh` to a nil channel (never selected).

## Files to Modify

- `internal/nebula/worker.go` — Refactor `Run` loop, add `handleChange` stub

## Acceptance Criteria

- [ ] `WorkerGroup.Run` selects on `Watcher.Changes` when watcher is non-nil
- [ ] When `Watcher` is nil, `Run` behaves identically to before (nil channel is never selected)
- [ ] `handleChange` method exists and logs the change kind (stub for now)
- [ ] Existing tests still pass (`go test ./internal/nebula/...`)
- [ ] No data races under `-race`
