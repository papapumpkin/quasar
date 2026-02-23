+++
id = "migrate-consumers"
title = "Replace nebula.Graph usage with dag.DAG across all nebula consumers"
priority = 2
depends_on = ["expose-dag-caps"]
scope = ["internal/nebula/**"]
+++

## Problem

Six files in `internal/nebula/` depend on `nebula.Graph` (including direct access to its unexported `adjacency`/`reverse` fields):

- `worker.go` — constructs both `NewGraph` and `NewScheduler` from the same phases (two sources of truth)
- `tracker.go` — `FilterEligible` and `hasFailedDep` access `graph.adjacency[phaseID]` directly
- `dashboard.go` — `isBlocked` and `phaseSuffix` access `graph.adjacency[phaseID]` directly
- `scope.go` — `validateScopeOverlaps` constructs a `NewGraph` just for `Connected` checks
- `parallelism.go` — `EffectiveParallelism` takes `*Graph` for `Connected` checks
- `hotreload.go` — stores `liveGraph *Graph`, uses `AddNode`, `AddEdge`, `RemoveEdge`, `Ready`, `Sort`
- `validate.go` — `ValidateHotAdd` and `rollbackHotAdd` mutate graph via `AddNode`, `AddEdge`, `Sort`, and direct field access

## Solution

Replace all `*Graph` parameters and fields with `*dag.DAG` (accessed via `scheduler.Analyzer().DAG()` where the scheduler exists, or constructed directly for validation-only contexts).

### Per-file changes

**`worker.go`**
- Remove `graph := NewGraph(wg.Nebula.Phases)`. Use the `*dag.DAG` from `scheduler.Analyzer().DAG()` instead.
- Pass `*dag.DAG` to `tracker.FilterEligible`, `hotReload.InitLiveState`, `gatePlan`.

**`tracker.go`**
- Change `FilterEligible(ready []string, graph *Graph)` to `FilterEligible(ready []string, d *dag.DAG)`.
- Change `hasFailedDep(phaseID string, graph *Graph)` to use `d.DepsFor(phaseID)` instead of `graph.adjacency[phaseID]`.

**`dashboard.go`**
- Change `isBlocked(phaseID string, graph *Graph)` to use `d.DepsFor(phaseID)`.
- Change `phaseSuffix(phaseID string, graph *Graph, ...)` similarly.

**`scope.go`**
- Change `validateScopeOverlaps` to accept `*dag.DAG` and use `d.Connected(a.ID, b.ID)`.

**`parallelism.go`**
- Change `EffectiveParallelism` and `WaveParallelism` to accept `*dag.DAG` and use `d.Connected(a.ID, b.ID)`.

**`hotreload.go`**
- Replace `liveGraph *Graph` with `liveDAG *dag.DAG`.
- Replace `graph.AddNode` → `d.AddNodeIdempotent`, `graph.AddEdge` → `d.AddEdge`, `graph.RemoveEdge` → `d.RemoveEdge`, `graph.Ready` → `d.Ready`.

**`validate.go`**
- Change `ValidateHotAdd` and `rollbackHotAdd` to work with `*dag.DAG`, using `AddNodeIdempotent`, `AddEdge`, `RemoveEdge`, `TopologicalSort` (replaces `Sort`).
- Replace direct `graph.adjacency`/`graph.reverse` manipulation in `rollbackHotAdd` with `d.RemoveEdge` calls.

### Wave type migration
- `nebula.Wave` is used in `worker.go` (gatePlan), `parallelism.go`, `plan.go`, and TUI code.
- After phase 1, `dag.DAG.ComputeWaves()` returns `dag.Wave`. Either alias `nebula.Wave = dag.Wave` or update all references.

## Files

- `internal/nebula/worker.go` — remove `NewGraph`, use DAG from scheduler
- `internal/nebula/tracker.go` — change `*Graph` to `*dag.DAG`, use `DepsFor`
- `internal/nebula/dashboard.go` — change `*Graph` to `*dag.DAG`, use `DepsFor`
- `internal/nebula/scope.go` — change `*Graph` to `*dag.DAG`, use `Connected`
- `internal/nebula/parallelism.go` — change `*Graph` to `*dag.DAG`, use `Connected`
- `internal/nebula/hotreload.go` — replace `liveGraph` with `*dag.DAG`
- `internal/nebula/validate.go` — change `ValidateHotAdd`/`rollbackHotAdd` to use `*dag.DAG`
- `internal/nebula/plan.go` — update `Wave` type references if needed
- Any test files that construct `nebula.Graph` — migrate to `dag.DAG`

## Acceptance Criteria

- [ ] Zero references to `nebula.Graph` remain in the codebase
- [ ] No direct access to `adjacency` or `reverse` map fields from outside `dag` package
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes (all existing tests)
- [ ] `go vet ./...` clean