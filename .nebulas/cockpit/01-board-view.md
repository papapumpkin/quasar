+++
id = "board-view"
title = "Columnar board view component"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/tui/boardview.go", "internal/tui/boardview_test.go"]
+++

## Problem

The current `NebulaView` renders phases as a flat table — rows of status, title, cost, cycles. This works but gives no spatial sense of workflow progress. The cockpit mockup shows a columnar board where tasks flow left-to-right through states: **Queued -> Running -> Review -> Done/Failed**. Each column groups phases by their current state, making it immediately obvious where work is stuck.

## Solution

Create a new `BoardView` Bubble Tea component that consumes the same `[]PhaseEntry` data as `NebulaView` but renders it as columns. The component:

1. Partitions `PhaseEntry` items into four buckets based on `PhaseStatus`:
   - **Queued**: `PhaseWaiting` (includes blocked phases)
   - **Running**: `PhaseWorking`
   - **Review**: `PhaseGate`
   - **Done**: `PhaseDone`, `PhaseSkipped`
   - **Failed**: `PhaseFailed` (rendered in the Done column with red styling)

2. Renders each bucket as a Lip Gloss column with a styled header. Columns are laid out horizontally using `lipgloss.JoinHorizontal`.

3. Each phase entry in a column shows:
   - Status icon (reuse existing `iconDone`, `iconWorking`, `iconWaiting`, etc.)
   - Phase title (truncated to column width)
   - Cursor selection via arrow keys (highlight row)

4. Supports the same keyboard navigation as `NebulaView` — arrow keys to move between phases, Enter to drill down into `DepthPhaseLoop`.

5. Column widths are proportional to terminal width: Queued 20%, Running 25%, Review 25%, Done 30%. On narrow terminals (< `BoardMinWidth`, say 100 cols), fall back to the existing table view entirely.

Use the existing galactic color palette: column headers use `colorPrimary` for Queued, `colorAccent` for Running, `colorBlueshift` for Review, `colorSuccess` for Done. Dim phases use `colorMuted`.

```go
type BoardView struct {
    phases   []PhaseEntry
    cursor   int
    width    int
    height   int
}
```

The `BoardView` does not own phase data — `AppModel` feeds it the same `NebulaView.Phases` slice and calls `SetPhaseStatus`/`SetPhaseCost` etc. through the existing message handlers.

## Files

- `internal/tui/boardview.go` — New `BoardView` component
- `internal/tui/boardview_test.go` — Tests for column partitioning, cursor navigation, and rendering

## Acceptance Criteria

- [ ] `BoardView` partitions phases into correct columns based on `PhaseStatus`
- [ ] Cursor navigation works across columns (left/right moves between columns, up/down within a column)
- [ ] Column headers are styled with the galactic palette
- [ ] Phase entries show status icon and title
- [ ] View degrades to empty columns gracefully when no phases are in a state
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
