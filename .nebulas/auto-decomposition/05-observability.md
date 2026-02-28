+++
id = "observability"
title = "Surface decomposition events through TUI messages, hails, and metrics"
type = "feature"
priority = 2
depends_on = ["worker-integration"]
+++

## Problem

When a phase is auto-decomposed, the user needs visibility into what happened and why. The existing observability channels — TUI messages (`internal/tui/msg.go`), hails (`internal/loop/hail.go`), and metrics (`internal/nebula/metrics.go`) — must be extended to cover decomposition events. Without this, a decomposition would appear as a silent phase disappearance followed by mysterious new phases appearing in the DAG.

## Solution

Extend the three observability layers to report decomposition events with full context.

### TUI Messages

Add new message types to `internal/tui/msg.go`:

```go
// MsgPhaseStruggleDetected is sent when the struggle detector triggers for a phase.
type MsgPhaseStruggleDetected struct {
    PhaseID string
    Cycle   int     // cycle at which struggle was detected
    Score   float64 // composite struggle score
    Reason  string  // human-readable summary
}

// MsgPhaseDecomposed is sent after a phase is successfully decomposed into sub-phases.
type MsgPhaseDecomposed struct {
    PhaseID     string   // original phase that was decomposed
    SubPhaseIDs []string // IDs of the replacement sub-phases
    CostSoFar   float64  // budget consumed before decomposition
}

// MsgPhaseDecomposeFailed is sent when decomposition was attempted but failed.
type MsgPhaseDecomposeFailed struct {
    PhaseID string
    Err     string // error message
}
```

These messages follow the existing pattern: the `WorkerGroup` (or its progress callback) sends them via the TUI's `Program.Send()`. The TUI model handles them to update phase status displays.

The TUI should render `MsgPhaseStruggleDetected` as a warning toast (similar to `MsgPhaseError`) showing the phase ID, cycle number, and reason. `MsgPhaseDecomposed` should update the phase list to show the original phase as "decomposed" and add the sub-phases. `MsgPhaseDecomposeFailed` should render as an error toast.

### Hails

When decomposition triggers, post a hail to `HailQueue` with kind `HailDecisionNeeded` so the human is notified. This happens in `WorkerGroup.decomposePhase` (from phase 04).

```go
hail := loop.Hail{
    PhaseID:    phaseID,
    Cycle:      result.CyclesUsed,
    SourceRole: "system",
    Kind:       loop.HailDecisionNeeded,
    Summary:    fmt.Sprintf("Phase %q was auto-decomposed into %d sub-phases", phaseID, len(subPhaseIDs)),
    Detail:     result.StruggleReason,
}
```

If the gate mode is `"approve"`, decomposition should wait for human approval before proceeding. This is achieved by posting the hail and blocking on its resolution before calling `ApplyDecompositionToNebula`. For `"review"` and `"trust"` gate modes, decomposition proceeds immediately and the hail is informational only.

For the `"watch"` gate mode, the hail is posted but the decomposition is blocked until the human explicitly approves (same as `"approve"`).

### Metrics

Extend `PhaseMetrics` in `internal/nebula/metrics.go` to track decomposition:

```go
// Add to PhaseMetrics:
Decomposed      bool     // true if this phase was decomposed
DecomposeReason string   // struggle reason that triggered decomposition
SubPhaseIDs     []string // IDs of sub-phases created by decomposition
```

Extend `Metrics` with aggregate counters:

```go
// Add to Metrics:
TotalDecompositions int // count of phases that were auto-decomposed
```

The `WorkerGroup` updates these fields after a successful decomposition:
1. Set `pm.Decomposed = true`, `pm.DecomposeReason`, and `pm.SubPhaseIDs` on the original phase's `PhaseMetrics`.
2. Increment `Metrics.TotalDecompositions`.

### Progress Callback

The existing `WorkerGroup.OnProgress` (`ProgressFunc`) callback fires on phase state changes. Decomposition events should trigger a progress update so the TUI's progress bar and phase count reflect the new total. After adding sub-phases, call:

```go
if wg.OnProgress != nil {
    wg.OnProgress(phaseID, "decomposed", 0) // cost delta is 0 for the decomposition itself
}
```

The TUI model should handle this status string to render the decomposed state correctly.

### Dashboard Integration

If `WorkerGroup.Dashboard` is non-nil, the decomposition event should trigger a dashboard refresh. The `MsgPhaseDecomposed` message (sent through the TUI program) will naturally cause the dashboard model to re-render the phase list, picking up the removed original and newly added sub-phases.

## Files

- `internal/tui/msg.go` — add `MsgPhaseStruggleDetected`, `MsgPhaseDecomposed`, `MsgPhaseDecomposeFailed`
- `internal/nebula/metrics.go` — add `Decomposed`, `DecomposeReason`, `SubPhaseIDs` fields to `PhaseMetrics`; add `TotalDecompositions` field to `Metrics`
- `internal/nebula/worker.go` — emit TUI messages, post hails, update metrics, and fire progress callbacks during decomposition (within `decomposePhase` from phase 04)
- `internal/tui/msg_test.go` (or equivalent) — verify new message types are constructible and carry correct fields
- `internal/nebula/metrics_test.go` — verify decomposition counters increment correctly

## Acceptance Criteria

- [ ] `MsgPhaseStruggleDetected` carries phase ID, cycle, score, and reason
- [ ] `MsgPhaseDecomposed` carries original phase ID, sub-phase IDs, and cost consumed
- [ ] `MsgPhaseDecomposeFailed` carries phase ID and error message
- [ ] A hail with kind `HailDecisionNeeded` is posted when decomposition occurs
- [ ] For `"approve"` and `"watch"` gate modes, decomposition blocks on hail resolution
- [ ] For `"review"` and `"trust"` gate modes, the hail is informational (non-blocking)
- [ ] `PhaseMetrics.Decomposed` is set to true on the original phase after decomposition
- [ ] `PhaseMetrics.SubPhaseIDs` lists all created sub-phase IDs
- [ ] `Metrics.TotalDecompositions` increments by 1 per decomposition event
- [ ] `OnProgress` callback fires with a `"decomposed"` status for the original phase
- [ ] `go test ./internal/tui/... ./internal/nebula/...` passes
- [ ] `go vet ./...` reports no issues
