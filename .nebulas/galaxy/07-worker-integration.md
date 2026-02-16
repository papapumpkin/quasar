+++
id = "worker-integration"
title = "Integrate Coordinator into WorkerGroup for runtime conflict detection"
type = "feature"
priority = 1
depends_on = ["scope-validation", "channel-coordinator"]
scope = ["internal/nebula/worker.go"]
+++

## Problem

WorkerGroup currently dispatches phases directly with no runtime coordination. With the Coordinator interface and scope validation in place, we can add runtime conflict detection: when a worker finishes and broadcasts its changes, other workers check for file overlap and restart if conflicted.

## Solution

Add `Coordinator` as an optional field on `WorkerGroup`. When present, the execution flow becomes:

### Dispatch loop

1. For each ready phase (from `Graph.Ready`):
   - If phase has `Scope`, call `Coordinator.Lock(ctx, phase.Scope)` before starting
   - Call `Coordinator.Enqueue(ctx, phase.ID)` (or dispatch directly as today)
2. Worker goroutine:
   - Subscribe to broadcast channel on start
   - Run phase via `PhaseRunner`
   - On completion: get actual files edited (from git diff), broadcast `ChangeEvent`
   - Call unlock function
3. Conflict detection goroutine (per worker):
   - Listen on subscribed channel
   - For each ChangeEvent: check if any `FilesEdited` overlap with own phase's `Scope`
   - If overlap detected: log warning via UI, check if own work has touched those files
   - If real conflict: cancel own context → triggers pessimistic restart

### Backward compatibility

When `Coordinator` is nil, WorkerGroup behaves exactly as today — direct goroutine dispatch, no locking, no broadcasts. This ensures existing nebulae work unchanged.

### Phase restart protocol

When a conflict is detected:
1. Cancel the phase's context (kills the coder-reviewer loop)
2. Re-queue the phase with `Coordinator.Lock` on the contested paths
3. Update state to record the retry
4. Log via `OnProgress` callback

## Files to Modify

- `internal/nebula/worker.go` — Add Coordinator field, integrate into dispatch loop

## Acceptance Criteria

- [ ] WorkerGroup with nil Coordinator works exactly as before
- [ ] WorkerGroup with Coordinator uses Lock/Broadcast/Subscribe
- [ ] Conflict detection cancels and re-queues conflicting phase
- [ ] Phase restarts are logged via OnProgress
- [ ] State tracks retry count
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` passes
