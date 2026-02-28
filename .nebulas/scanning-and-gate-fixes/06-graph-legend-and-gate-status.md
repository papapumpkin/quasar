+++
id = "graph-legend-and-gate-status"
title = "Add gate state to graph legend and PhaseStatusFromString"
type = "bug"
priority = 2
depends_on = []
labels = ["quasar", "tui"]
scope = ["internal/tui/graphview.go", "internal/tui/nebulaview.go"]
+++

## Problem

Two related gaps in how the "gate" phase state is represented:

### 1. Graph legend missing gate/blocked state

`graphLegend()` in `graphview.go:310` shows four states: queued, running, done, failed. But `phaseStatusToDAGState()` maps `PhaseGate` → `"blocked"`, which renders as magenta via `colorize()` in the DAG renderer. Users see magenta nodes for phases awaiting gate review with no legend entry explaining the color.

The `phaseStatusColor()` function maps `PhaseGate` → `colorAccent`, confirming the gate has its own distinct color — it just lacks a legend entry.

### 2. PhaseStatusFromString does not handle "gate"

`PhaseStatusFromString()` in `nebulaview.go:25` handles `"done"`, `"failed"`, `"in_progress"`, and `"skipped"`, but falls through to `PhaseWaiting` for any unrecognized string — including `"gate"`. If a nebula's saved state includes a phase at the gate (e.g., the user paused and resumed), it would incorrectly show as waiting instead of gate.

## Solution

### Changes

1. **`internal/tui/graphview.go`** — Add a gate/review entry to the legend:
   ```go
   items := []struct {
       label string
       color lipgloss.Color
   }{
       {"queued", colorPrimary},
       {"running", colorStarYellow},
       {"review", colorAccent},
       {"done", colorSuccess},
       {"failed", colorDanger},
   }
   ```
   Use `"review"` as the label (matches `ColReview` in the board) rather than `"blocked"` which is ambiguous with dependency blocking.

2. **`internal/tui/nebulaview.go`** — Add a `"gate"` case to `PhaseStatusFromString`:
   ```go
   case "gate":
       return PhaseGate
   ```

## Files

- `internal/tui/graphview.go` — Add gate/review entry to `graphLegend()`
- `internal/tui/nebulaview.go` — Add `"gate"` case to `PhaseStatusFromString()`

## Acceptance Criteria

- [ ] Graph legend displays a "review" entry with the accent color
- [ ] `PhaseStatusFromString("gate")` returns `PhaseGate`
- [ ] Existing legend entries (queued, running, done, failed) remain unchanged
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
