+++
id = "resize-edge-cases"
title = "Graceful terminal resize and narrow-width handling"
type = "task"
priority = 3
depends_on = ["color-theme", "progress-indicators", "detail-panel"]
+++

## Problem

The TUI currently receives `tea.WindowSizeMsg` and updates width/height, but the layout doesn't adapt well to narrow terminals (< 60 columns) or very short terminals (< 15 rows). Status bar text wraps, progress bars break, and the detail panel may have zero height.

## Solution

### Minimum Size Handling
- Define minimum usable dimensions: 40 columns, 10 rows
- Below minimum: show a centered "Terminal too small" message instead of the broken layout
- At the boundary: hide optional elements (detail panel, progress bars) to fit

### Adaptive Layout
- Status bar: truncate nebula name with ellipsis when terminal is narrow
- Progress bars: switch to compact format (percentage only) below 60 columns
- Phase table: truncate phase IDs with ellipsis when they don't fit
- Footer: show abbreviated key hints below 60 columns (`↑↓` instead of `↑/k:up  ↓/j:down`)
- Detail panel: auto-collapse when terminal height < 20 rows

### Resize Transitions
- On resize, recalculate all layout dimensions
- Reflow the detail panel viewport content
- Ensure the cursor remains valid (clamp if list shrunk)

## Files to Modify

- `internal/tui/model.go` — Minimum size check, adaptive layout logic, cursor clamping on resize
- `internal/tui/statusbar.go` — Truncation with ellipsis for narrow terminals
- `internal/tui/footer.go` — Compact mode for narrow terminals
- `internal/tui/loopview.go` — Width-aware rendering
- `internal/tui/nebulaview.go` — Truncate phase IDs
- `internal/tui/detailpanel.go` — Auto-collapse logic

## Acceptance Criteria

- [ ] Terminal below 40x10 shows "Terminal too small" message
- [ ] Status bar truncates cleanly at narrow widths
- [ ] Footer adapts to compact mode
- [ ] Detail panel auto-collapses on short terminals
- [ ] Cursor remains valid after resize that shrinks the list
- [ ] No panics or rendering glitches during rapid resize
- [ ] Tests for truncation helpers and layout calculations
- [ ] `go build` and `go test ./internal/tui/...` pass
