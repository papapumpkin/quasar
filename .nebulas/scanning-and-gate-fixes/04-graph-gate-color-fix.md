+++
id = "graph-gate-color-fix"
title = "Fix completed phases showing purple instead of green in graph view"
type = "bug"
priority = 1
depends_on = []
labels = ["quasar", "tui"]
scope = ["internal/tui/model.go"]
allow_scope_overlap = true
+++

## Problem

When a nebula runs with `gate = "review"`, completed phases display as purple in the graph view instead of green. On the home page (before running), the same phases show correctly as green.

### Root Cause

The `resolveGate` method in `model.go` (line 1487) updates `NebulaView.SetPhaseStatus` for the accepted/rejected/retried phase but **does not** update `Graph.SetPhaseStatus`. The graph stays stuck on `PhaseGate`, which maps through:

1. `phaseStatusToDAGState(PhaseGate)` → `"blocked"` (graphview.go:350)
2. `colorize(text, id, "blocked")` → `ansi.Magenta` (dagrender.go:206)

So the graph permanently shows the gate color (magenta/purple) instead of transitioning to done (green).

### Evidence

The `MsgPhaseTaskComplete` handler at line 322-323 correctly updates **both** views:
```go
m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseDone)
m.Graph.SetPhaseStatus(msg.PhaseID, PhaseDone)
```

But `resolveGate` at line 1494-1503 only updates `NebulaView`:
```go
case nebula.GateActionAccept:
    m.NebulaView.SetPhaseStatus(phaseID, PhaseDone)
// ← missing: m.Graph.SetPhaseStatus(phaseID, PhaseDone)
```

## Solution

Add `m.Graph.SetPhaseStatus(phaseID, ...)` calls to `resolveGate` for each gate action, mirroring the `NebulaView` updates:

```go
func (m *AppModel) resolveGate(action nebula.GateAction) {
    if m.Gate != nil {
        phaseID := m.Gate.PhaseID
        m.Gate.Resolve(action)
        m.Gate = nil

        switch action {
        case nebula.GateActionAccept:
            m.NebulaView.SetPhaseStatus(phaseID, PhaseDone)
            m.Graph.SetPhaseStatus(phaseID, PhaseDone)
        case nebula.GateActionReject:
            m.NebulaView.SetPhaseStatus(phaseID, PhaseFailed)
            m.Graph.SetPhaseStatus(phaseID, PhaseFailed)
        case nebula.GateActionRetry:
            m.NebulaView.SetPhaseStatus(phaseID, PhaseWorking)
            m.Graph.SetPhaseStatus(phaseID, PhaseWorking)
        case nebula.GateActionSkip:
            m.NebulaView.SetPhaseStatus(phaseID, PhaseSkipped)
            m.Graph.SetPhaseStatus(phaseID, PhaseSkipped)
        }
    }
}
```

## Files

- `internal/tui/model.go` — Add `m.Graph.SetPhaseStatus` calls to `resolveGate` for all four gate actions

## Acceptance Criteria

- [ ] After accepting a phase at the gate, the graph view shows it as green (done)
- [ ] After rejecting a phase at the gate, the graph view shows it as red (failed)
- [ ] After retrying a phase at the gate, the graph view shows it as yellow (running)
- [ ] After skipping a phase at the gate, the graph view shows it as green (done/skipped)
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
