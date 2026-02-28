+++
id = "remove-scanning-column"
title = "Remove dead Scanning column from the board view"
type = "task"
priority = 2
depends_on = []
labels = ["quasar", "tui"]
scope = ["internal/tui/boardview.go", "internal/tui/boardview_test.go"]
+++

## Problem

The board view defines a `ColScanning` column with label "Scanning" and color `colorNebula`, but nothing ever populates it. The `statusToColumn()` function maps phase statuses to board columns and has no case that returns `ColScanning`:

```go
func statusToColumn(p PhaseEntry) BoardColumn {
    switch p.Status {
    case PhaseWorking:   return ColRunning
    case PhaseDone, PhaseSkipped: return ColDone
    case PhaseFailed:    return ColFailed
    case PhaseGate:      return ColReview
    default:             // PhaseWaiting
        if p.BlockedBy != "" { return ColBlocked }
        return ColQueued
    }
}
```

The fabric layer does set `StateScanning` briefly during the `Reevaluate()` → `Scan()` pipeline, but this is a sub-second transitional state. A dedicated board column for it wastes horizontal space — on a 7-column board at 120 cols, each column loses ~17 chars of width to the empty Scanning column.

The `visibleColumns()` method already hides Scanning at medium widths (100-139 cols), acknowledging it's low-value. At full width (140+) it appears but is always empty.

## Solution

Remove `ColScanning` from the board entirely. This means:

1. **`internal/tui/boardview.go`**:
   - Remove the `ColScanning` constant from the `BoardColumn` enum. This shifts the iota values for subsequent columns, so update `colCount` accordingly (it derives from iota so it will adjust automatically).
   - Remove the `ColScanning` entry from `columnDefs`.
   - Remove `ColScanning` from the full-width slice returned by `visibleColumns()`.
   - Remove any references to `ColScanning` in comments.
   - The medium-width merge logic that collapses `ColScanning` into `ColQueued` becomes dead code — remove it.

2. **`internal/tui/boardview_test.go`**:
   - Remove test cases and assertions that reference `ColScanning`.
   - Update column count expectations and medium-width merge tests.

### Impact

The board view goes from 7 columns to 6: Queued, Running, Review, Blocked, Done, Failed. Each column gains ~17% more width for phase names. The full-width threshold in `visibleColumns()` may need adjustment (or simplification, since there's less reason to collapse columns now).

## Files

- `internal/tui/boardview.go` — Remove `ColScanning` constant, `columnDefs` entry, `visibleColumns()` reference, and merge logic
- `internal/tui/boardview_test.go` — Update tests to remove Scanning column expectations

## Acceptance Criteria

- [ ] The board view renders with 6 columns: Queued, Running, Review, Blocked, Done, Failed
- [ ] No "Scanning" label appears in the board at any terminal width
- [ ] Phase names have more horizontal space in each column
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
