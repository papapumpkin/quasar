+++
id = "board-phase-names"
title = "Fix phase name truncation in board view"
type = "bug"
priority = 2
depends_on = []
+++

## Problem

Phase names in the board view get truncated too aggressively. The `columnWidth()` function caps each column at 30 characters max, and after subtracting 4 for icon + spacing, only 26 chars remain for the title. With many columns visible, the width shrinks further. Phase titles like "Wire streaming invoker output to the event bus" become unreadable.

## Solution

Improve how phase names are displayed in the board view to maximize readability.

### Approach

1. **Increase max column width**: Bump `workerCardMaxWidth` equivalent for board columns. The current cap in `columnWidth()` is `w > 30 → w = 30`. On wide terminals (>180 cols) there's plenty of room — raise the cap to 40 or remove it and let the layout use available space.

2. **Show title on hover/selection**: When a phase is selected (cursor on it), show the full title in a tooltip-style line below the board, or in the detail panel header. This is the main fix — truncation is fine for non-selected items, but the selected phase should always show its full name.

3. **Prefer title over ID**: `renderBoardEntry` currently tries `p.Title` first, then falls back to `p.ID`. Verify this is working — if `Title` is empty for some phases, that's the root cause of unreadable entries (IDs like `streaming-invoker` are less descriptive than titles).

4. **Two-line entries on wide terminals**: When column width permits (>35), render each phase entry on two lines: icon + truncated title on line 1, phase ID in muted text on line 2. This doubles the information density without requiring wider columns.

## Files

- `internal/tui/boardview.go` — adjust `columnWidth()` max, improve `renderBoardEntry()`, add selected-phase full title display
- `internal/tui/boardview_test.go` — update tests for new layout behavior

## Acceptance Criteria

- [ ] Phase names are more readable in the board view on standard terminal widths (120-200 cols)
- [ ] Selected phase shows its full title somewhere visible (below board or in detail panel)
- [ ] Column widths use available space better on wide terminals
- [ ] Layout still degrades gracefully on narrow terminals (<100 cols)
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
