+++
id = "dag-insertion"
title = "Hot-insert remediation phases into the live DAG and wire into WorkerGroup"
type = "feature"
priority = 1
depends_on = ["failure-analyzer", "remediation-architect"]
labels = ["quasar", "auto-healing", "reliability"]
scope = ["internal/nebula/healing.go", "internal/nebula/worker.go", "internal/nebula/hotreload.go"]
allow_scope_overlap = true
+++

## Problem

After the architect generates a remediation `PhaseSpec`, it must be inserted into the live DAG so that:

1. The remediation phase becomes a new node in the DAG
2. All phases that previously depended on the failed phase now depend on the remediation phase instead
3. The remediation phase itself has no unmet dependencies (the failed phase's own dependencies are already complete)
4. The `WorkerGroup` picks up the new phase for execution in the current or next wave

Today, `HotReloader.handlePhaseAdded` handles file-system-triggered hot-adds by calling `dag.AddNode` + `dag.AddEdge` and signaling via the `hotAdded` channel. But the healing flow is not triggered by file changes — it is triggered programmatically from within `WorkerGroup` when a phase execution fails.

## Solution

### DAG surgery helper

Add a function in `internal/nebula/healing.go`:

```go
// InsertRemediationPhase adds a remediation phase into the live DAG, rewiring
// edges so that dependents of the failed phase now depend on the remediation
// phase. The remediation phase has no dependencies of its own (the failed
// phase's deps are already satisfied).
//
// Returns the list of phase IDs whose dependency edges were rewired.
func InsertRemediationPhase(d *dag.DAG, failedID string, remediation *PhaseSpec) ([]string, error)
```

The algorithm:

1. `d.AddNode(remediation.ID, remediation.Priority)` — insert the new node
2. Collect `dependents` = all nodes where `failedID` appears in their dependency set. Use the DAG's reverse adjacency: iterate `d.Descendants(failedID)` is too broad (transitive); instead use the reverse map directly. Since the DAG's `reverse` field is unexported, add a new exported method to `dag.DAG`:

```go
// DirectDependents returns the IDs of nodes that directly depend on the given node.
func (d *DAG) DirectDependents(id string) []string
```

3. For each direct dependent:
   - `d.RemoveEdge(dependent, failedID)` — remove the edge to the failed phase
   - `d.AddEdge(dependent, remediation.ID)` — add edge to the remediation phase
4. Return the list of rewired dependent IDs

### Healing attempt tracking

Add a `healAttempts` map to the existing state tracked alongside `WorkerGroup`:

```go
// healAttempts tracks how many times healing has been attempted for each phase.
// Key: original failed phase ID, Value: number of attempts.
healAttempts map[string]int
```

This lives as a field on `WorkerGroup` (initialized in `Run`).

### WorkerGroup integration

Modify `WorkerGroup.recordResult` (or the call site that handles failed phases) to trigger healing when a phase fails. The flow within the existing `executePhase` / result-recording path:

```go
// After recording a failure:
if wg.healingPolicy.Enabled && wg.shouldHeal(phaseID, err, taskResult, cycleState) {
    go wg.attemptHealing(ctx, phaseID, err, taskResult, cycleState)
}
```

The `attemptHealing` method:

1. Call `AnalyzeFailure(phaseID, err, taskResult, cycleState)` to get `*FailureDiagnosis`
2. Check `wg.healingPolicy.CanHeal(diag, wg.healAttempts[phaseID])`
3. If not healable, return (log the reason)
4. Increment `wg.healAttempts[phaseID]`
5. Call `BuildRemediationRequest(diag, wg.SnapshotNebula(), failedSpec)`
6. Call `RunArchitect(ctx, wg.Runner.Invoker(), req)` to generate the remediation phase
7. Call `FinalizeRemediationSpec(result, diag, failedSpec)`
8. Acquire `wg.mu`, call `InsertRemediationPhase(wg.liveGraph, phaseID, &result.PhaseSpec)`
9. Register the new phase in `wg.tracker.PhasesByIDMap()` and `wg.State.Phases`
10. Signal `wg.hotReload.HotAdded()` channel with the new phase ID (or use `OnHotAdd` callback directly)
11. Fire `wg.OnHotAdd(result.PhaseSpec.ID, result.PhaseSpec.Title, result.PhaseSpec.DependsOn)`

### Config surface

Add `HealingPolicy` to the nebula execution config. In `nebula.toml`:

```toml
[execution]
healing = false          # master switch
healing_max_attempts = 1 # per-phase
healing_budget_reserve = 10.0  # USD reserved for healing
```

Parse these into `HealingPolicy` during nebula loading in `internal/nebula/types.go` (add fields to `ExecutionConfig`).

### Passing loop state to the worker

Currently `WorkerResult` contains `PhaseID`, `BeadID`, `Err`, and `Report`. To pass the `CycleState` and `TaskResult` needed for failure analysis, either:

- **Option A**: Add optional `TaskResult *loop.TaskResult` and `CycleState *loop.CycleState` fields to `WorkerResult`. These are only populated on failure.
- **Option B**: Have the `PhaseRunner` return them via a richer result type.

Use **Option A** — it is the smallest change. The `PhaseRunner` implementation already has access to both values; it just needs to propagate them on error.

Add to `WorkerResult`:

```go
// Populated on failure for healing analysis. Nil on success.
TaskResult *loop.TaskResult
CycleState *loop.CycleState
```

## Files

- `internal/dag/dag.go` — add `DirectDependents` method
- `internal/dag/dag_test.go` — test `DirectDependents`
- `internal/nebula/healing.go` — add `InsertRemediationPhase`
- `internal/nebula/healing_test.go` — test DAG rewiring (diamond graph, linear chain, no dependents)
- `internal/nebula/worker.go` — add `healAttempts` field, `attemptHealing` method, call site in failure path
- `internal/nebula/types.go` — add `TaskResult`/`CycleState` fields to `WorkerResult`; add healing fields to `ExecutionConfig`
- `internal/nebula/hotreload.go` — ensure programmatic hot-add (not just file-triggered) works via `HotAdded()` channel

## Acceptance Criteria

- [ ] `dag.DirectDependents` returns only immediate dependents, not transitive
- [ ] `InsertRemediationPhase` adds the node, rewires edges, and does not create a cycle
- [ ] `InsertRemediationPhase` returns an error if the failed phase ID does not exist in the DAG
- [ ] `WorkerGroup` calls `attemptHealing` only when `HealingPolicy.Enabled` is true
- [ ] `healAttempts` prevents more than `MaxAttempts` healing invocations per phase
- [ ] The remediation phase is picked up by the worker pool's next wave dispatch
- [ ] `HealingPolicy` fields are parsed from `nebula.toml` `[execution]` section
- [ ] `WorkerResult` carries `TaskResult` and `CycleState` on failure for diagnosis
- [ ] `go test ./internal/nebula/...` and `go test ./internal/dag/...` pass
- [ ] `go vet ./...` clean
