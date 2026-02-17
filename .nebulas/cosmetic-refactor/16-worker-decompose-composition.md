+++
id = "worker-decompose-composition"
title = "Decompose WorkerGroup into composed collaborators"
type = "task"
priority = 2
depends_on = ["worker-gate-strategy", "unify-review-report"]
scope = ["internal/nebula/worker.go", "internal/nebula/tracker.go", "internal/nebula/progress.go", "internal/nebula/hotreload.go"]
+++

## Problem

Even after functional options and the gate strategy, `WorkerGroup` in `worker.go` (~1100 lines) remains a God Object handling:

- **Phase orchestration** — scheduling phases based on the dependency graph
- **Phase state tracking** — managing `phaseState` maps, status transitions, completion counts
- **Progress reporting** — dashboard updates, checkpoint writes, metrics collection
- **Hot-reload** — watching for file changes, dynamically adding/removing phases

These are four distinct responsibilities in one struct. The file is very difficult to navigate and reason about.

## Solution

Decompose `WorkerGroup` using composition — extract responsibilities into focused types that `WorkerGroup` delegates to:

1. **`PhaseTracker`** (new, in `internal/nebula/tracker.go`):
   - Owns `phaseState map[string]*phaseState`, completion counts, status transitions
   - Methods: `Init(phases)`, `MarkRunning(id)`, `MarkDone(id, result)`, `MarkFailed(id, err)`, `IsReady(id) bool`, `Summary() (done, failed, total int)`
   - Extracted from: `initPhaseState`, status transition logic, completion tracking scattered through `worker.go`

2. **`ProgressReporter`** (new, in `internal/nebula/progress.go`):
   - Owns dashboard, checkpoint, and metrics concerns
   - Methods: `ReportPhaseStart(phase)`, `ReportPhaseComplete(phase, result)`, `WriteCheckpoint()`, `RecordMetrics(phase, result)`
   - Extracted from: dashboard update calls, checkpoint logic, metrics recording in `worker.go`

3. **`HotReloader`** (new, in `internal/nebula/hotreload.go`):
   - Owns watcher integration and dynamic phase addition/removal
   - Methods: `Start(ctx)`, `Stop()`, `PendingChanges() []Change`
   - Extracted from: hot-add logic, watcher event handling in `worker.go`

4. **`WorkerGroup`** becomes a thin orchestrator:
   - Holds references to `PhaseTracker`, `ProgressReporter`, `HotReloader`, `Gater`
   - `Run(ctx)` method focuses purely on: pick ready phases → dispatch to workers → collect results → check gate → repeat
   - Should shrink to ~200-300 lines

5. Update `NewWorkerGroup` to construct and wire the collaborators internally. The external API doesn't change — callers still create a `WorkerGroup` and call `Run`.

## Files

- `internal/nebula/tracker.go` (new) — `PhaseTracker` type
- `internal/nebula/progress.go` (new) — `ProgressReporter` type
- `internal/nebula/hotreload.go` (new) — `HotReloader` type
- `internal/nebula/worker.go` — slim down to thin orchestrator, delegate to collaborators

## Acceptance Criteria

- [ ] `WorkerGroup` delegates to `PhaseTracker`, `ProgressReporter`, and `HotReloader`
- [ ] `worker.go` is under 400 lines
- [ ] Each new file is focused on a single responsibility
- [ ] External API of `WorkerGroup` is unchanged (`NewWorkerGroup` + `Run`)
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
