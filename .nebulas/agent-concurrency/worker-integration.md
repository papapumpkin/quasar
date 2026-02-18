+++
id = "worker-integration"
title = "Integrate lock manager and sync protocol into the worker pipeline"
type = "feature"
priority = 3
depends_on = ["sync-protocol"]
+++

## Problem

The lock manager and sync protocol exist as standalone components but need to be wired into the nebula worker execution pipeline so that parallel workers actually use them.

## Solution

1. **Worker startup**: Before a worker begins a phase, it:
   - Expands the phase's scope to concrete file paths
   - Acquires locks via the LockManager
   - If lock acquisition fails (conflict), the worker waits or skips to the next available phase

2. **Worker completion**: After a phase finishes:
   - Commit changes
   - Broadcast change notification via sync protocol
   - Release all file locks

3. **Worker failure**: If a worker crashes or times out:
   - Stale lock detection cleans up after the configured timeout
   - Other workers can proceed once locks are released

4. **Apply orchestrator**: Update `internal/nebula/apply.go` to:
   - Inject the LockManager and SyncProtocol into workers
   - Use lock availability to influence phase scheduling (prefer unlocked phases)

## Files

- `internal/nebula/worker.go` — add lock acquire/release around phase execution
- `internal/nebula/apply.go` — inject concurrency dependencies, scheduling logic
- `internal/nebula/worker_test.go` — integration tests

## Acceptance Criteria

- [ ] Workers acquire locks before starting and release after completing
- [ ] Parallel workers with non-overlapping scope run concurrently
- [ ] Workers with conflicting scope are serialized
- [ ] Lock cleanup happens on worker failure
- [ ] `go test -race ./internal/nebula/...` passes
