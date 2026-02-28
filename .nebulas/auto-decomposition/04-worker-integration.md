+++
id = "worker-integration"
title = "Wire struggle detection and decomposition into the WorkerGroup execution flow"
type = "feature"
priority = 1
depends_on = ["struggle-detection", "dag-insertion"]
+++

## Problem

The struggle detector (phase 01) and the DAG surgery (phase 03) exist as standalone units. They need to be wired into the actual execution flow so that when a phase struggles mid-run, the `WorkerGroup` pauses it, invokes the architect, applies the decomposition, and schedules the sub-phases.

The integration touches three layers:
1. **Loop layer** (`internal/loop/loop.go`): After each cycle, evaluate `StruggleSignal` and return a special result when triggered.
2. **Worker layer** (`internal/nebula/worker.go`): The `WorkerGroup` interprets the struggle result, invokes `RunDecompose`, calls `ApplyDecompositionToNebula`, and enqueues sub-phases.
3. **Configuration layer**: `StruggleConfig` must be derivable from the nebula manifest and per-phase overrides.

## Solution

### Loop Integration

Add a `StruggleConfig` field to the `Loop` struct:

```go
// In Loop struct (internal/loop/loop.go):
StruggleConfig StruggleConfig // Optional; zero value disables struggle detection.
```

Modify `runLoop` to evaluate struggle after each cycle completes (after findings are accumulated but before the next cycle begins). When `StruggleSignal.Triggered` is true:

1. Emit a new `EventStruggleDetected` hook event (add to `internal/loop/hooks.go`).
2. Return a new `TaskResult` variant that signals the caller (WorkerGroup) to decompose. Add a field:

```go
// In TaskResult (internal/loop/loop.go):
Decompose       bool           // true if the loop exited due to a struggle signal
StruggleReason  string         // human-readable reason from StruggleSignal.Reason
AllFindings     []ReviewFinding // accumulated findings at time of decomposition
```

The loop stops cleanly — it does not return an error. The caller decides what to do.

### Worker Integration

In `WorkerGroup`'s phase execution path (the function that calls `PhaseRunner.RunExistingPhase`), check the returned `TaskResult.Decompose` flag:

```go
if result.Decompose {
    subPhaseIDs, err := wg.decomposePhase(ctx, phaseID, result)
    if err != nil {
        // Log and mark phase as failed — decomposition is best-effort.
        wg.logf("decomposition failed for %s: %v", phaseID, err)
    } else {
        // Enqueue sub-phases for scheduling.
        for _, id := range subPhaseIDs {
            wg.enqueueHotAdded(id)
        }
    }
}
```

Add a new method to `WorkerGroup`:

```go
// decomposePhase invokes the architect to decompose a struggling phase and
// applies the resulting sub-phases to the DAG. It returns the IDs of the
// newly created sub-phases.
func (wg *WorkerGroup) decomposePhase(ctx context.Context, phaseID string, result *loop.TaskResult) ([]string, error)
```

This method:
1. Builds an `ArchitectRequest` with `Mode: ArchitectModeDecompose`, populating struggle context from `result`.
2. Calls `RunDecompose(ctx, wg.Runner.(agent.Invoker), req)` — note: the invoker must be accessible. If `PhaseRunner` does not expose it, pass it as a new `WorkerGroup` field (`Invoker agent.Invoker`).
3. Converts `DecomposeResult.SubPhases` into a `DecomposeOp`.
4. Acquires `wg.mu` and calls `ApplyDecompositionToNebula(wg.Nebula, liveGraph, op)`.
5. Sets fabric state: `wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateDecomposed)`.
6. Fires `wg.OnHotAdd` for each sub-phase (reusing the existing `HotAddFunc` callback pattern).
7. Updates `wg.State` to mark the original phase as decomposed (not failed).
8. Posts a hail via `wg.OnHail` if configured, notifying the human that a decomposition occurred.

### Decomposition Guard

A phase that was itself created by decomposition must not be decomposed again (to prevent infinite recursion). Add a `Decomposed bool` field to `PhaseSpec` frontmatter:

```go
// In PhaseSpec (internal/nebula/phase.go or equivalent):
Decomposed bool `toml:"decomposed,omitempty"` // true if this phase was produced by auto-decomposition
```

When building the `Loop` for a phase, set `StruggleConfig.Enabled = false` if `phase.Decomposed` is true.

### Configuration Plumbing

Add an `AutoDecompose` field to the execution section of the nebula manifest:

```go
// In the manifest Execution struct:
AutoDecompose bool `toml:"auto_decompose"` // enable auto-decomposition on struggle
```

Per-phase override via frontmatter:

```go
// In PhaseSpec:
AutoDecompose *bool `toml:"auto_decompose,omitempty"` // per-phase override (nil = inherit from manifest)
```

`WorkerGroup` resolves the effective config: if the phase has `auto_decompose` set, use it; otherwise fall back to `Execution.AutoDecompose`. If false, `StruggleConfig.Enabled` is set to false in the `Loop`.

### New WorkerGroup Field

```go
// In WorkerGroup struct:
Invoker agent.Invoker // required for architect invocations during decomposition
```

## Files

- `internal/loop/loop.go` — add `StruggleConfig` field to `Loop`; evaluate struggle in `runLoop`; add `Decompose`, `StruggleReason`, `AllFindings` fields to `TaskResult`
- `internal/loop/hooks.go` — add `EventStruggleDetected` constant to `EventKind`
- `internal/nebula/worker.go` — add `Invoker` field to `WorkerGroup`; add `decomposePhase` method; check `TaskResult.Decompose` in phase execution path
- `internal/nebula/worker_test.go` — test decomposition flow with mock invoker: struggle triggers decomposition, decomposition failure falls back to failed phase, guard prevents recursive decomposition
- `internal/nebula/manifest.go` (or equivalent) — add `AutoDecompose` to execution config
- `internal/nebula/phase.go` (or equivalent) — add `Decomposed` and `AutoDecompose` fields to `PhaseSpec`

## Acceptance Criteria

- [ ] `Loop.runLoop` evaluates `EvaluateStruggle` after each cycle when `StruggleConfig.Enabled` is true
- [ ] `TaskResult.Decompose` is set to true when a struggle signal triggers, and the loop exits cleanly
- [ ] `EventStruggleDetected` hook event is emitted before the loop exits for decomposition
- [ ] `WorkerGroup.decomposePhase` calls `RunDecompose` and `ApplyDecompositionToNebula`
- [ ] Fabric state transitions to `StateDecomposed` for the original phase
- [ ] `OnHotAdd` callback is fired for each sub-phase
- [ ] Phases with `Decomposed: true` in their spec have struggle detection disabled
- [ ] `AutoDecompose` manifest flag controls whether decomposition is attempted
- [ ] Per-phase `auto_decompose` override takes precedence over the manifest default
- [ ] Decomposition failure does not crash the worker — the phase is marked failed with a log message
- [ ] `go test ./internal/loop/... ./internal/nebula/...` passes
- [ ] `go vet ./...` reports no issues
