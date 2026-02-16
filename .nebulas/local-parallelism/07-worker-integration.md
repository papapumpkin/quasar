+++
id = "worker-integration"
title = "Integrate Coordinator and effective parallelism into WorkerGroup"
type = "feature"
priority = 1
depends_on = ["scope-validation", "channel-coordinator", "effective-parallelism"]
scope = ["internal/nebula/worker.go"]
+++

## Problem

WorkerGroup currently dispatches phases with a flat `max_workers` semaphore and no runtime coordination. With scope validation, the Coordinator interface, and effective parallelism computation in place, we can add both smart worker capping and runtime conflict detection.

## Solution

### 1. Wave-based effective parallelism (Layer 1)

Replace the flat semaphore with per-wave sizing:

1. At the start of `Run`, compute waves via `ComputeWaves()`
2. For each wave, call `EffectiveParallelism()` to get the true max useful workers
3. Resize the semaphore to `min(max_workers, effective_parallelism)` before dispatching the wave
4. This is purely static — no Coordinator required, works with nil Coordinator

### 2. Coordinator integration (optional)

Add `Coordinator` as an optional field on `WorkerGroup`. When present, the execution flow becomes:

#### Dispatch loop

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

- `Coordinator` nil → no locking, no broadcasts (exactly as today)
- Effective parallelism is always active (it only reduces unnecessary worker spawns, never changes behavior)

### Phase restart protocol

When a conflict is detected:
1. Cancel the phase's context (kills the coder-reviewer loop)
2. Re-queue the phase with `Coordinator.Lock` on the contested paths
3. Update state to record the retry
4. Log via `OnProgress` callback

## Files to Modify

- `internal/nebula/worker.go` — Add Coordinator field, wave-based semaphore sizing, integrate dispatch loop

## Acceptance Criteria

- [ ] WorkerGroup with nil Coordinator works exactly as before
- [ ] Semaphore is capped at effective parallelism per wave (not flat max_workers)
- [ ] Linear dependency chain uses 1 worker regardless of max_workers setting
- [ ] Wide wave with non-overlapping scopes uses full max_workers
- [ ] Wide wave with overlapping scopes reduces effective workers
- [ ] WorkerGroup with Coordinator uses Lock/Broadcast/Subscribe
- [ ] Conflict detection cancels and re-queues conflicting phase
- [ ] Phase restarts are logged via OnProgress
- [ ] State tracks retry count
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` passes
