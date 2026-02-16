+++
id = "worker-integration"
title = "Integrate wave-based effective parallelism into WorkerGroup"
type = "feature"
priority = 1
depends_on = ["scope-validation", "effective-parallelism"]
scope = ["internal/nebula/worker.go"]
+++

## Problem

WorkerGroup dispatches phases with a flat `max_workers` semaphore for all waves. This wastes workers on narrow waves and doesn't account for scope-serialized phases. Runtime file locking is handled by agentmail (agents claim files via MCP tools during execution), but the orchestrator should still avoid spawning more workers than are useful.

## Solution

Replace the flat semaphore with per-wave sizing using the `EffectiveParallelism` function.

### Dispatch loop changes

In `WorkerGroup.Run`:

1. At the start, compute waves via `ComputeWaves()`
2. Before dispatching each wave's phases, compute `EffectiveParallelism()` for the wave
3. Resize the semaphore to `min(max_workers, effective_parallelism)`
4. Log the per-wave worker count via stderr: "Wave 1: 3 workers (effective parallelism: 3/4)"

### Backward compatibility

- Phases without `Scope` fields don't affect effective parallelism (unscoped phases are unrestricted)
- If all phases are unscoped, behavior is identical to today (semaphore = max_workers)
- No dependency on agentmail or any external system — this is purely static analysis

### No runtime locking here

Runtime file coordination (locking, conflict detection, change broadcasting) is delegated to agentmail. The orchestrator's job is limited to:
1. Static scope validation at plan time (Validate)
2. Smart worker count per wave (EffectiveParallelism)

Agents self-coordinate during execution via agentmail MCP tools.

## Files to Modify

- `internal/nebula/worker.go` — Per-wave semaphore sizing using EffectiveParallelism

## Acceptance Criteria

- [ ] Semaphore capped at effective parallelism per wave
- [ ] Linear dependency chain uses 1 worker regardless of max_workers
- [ ] Wide wave with non-overlapping scopes uses full max_workers
- [ ] Wide wave with overlapping scopes reduces effective workers
- [ ] Unscoped phases: behavior identical to current
- [ ] Per-wave worker count logged to stderr
- [ ] Existing tests pass unchanged
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` passes
