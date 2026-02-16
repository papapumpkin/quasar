+++
id = "resize-edge-cases"
title = "Graceful terminal resize and narrow-width handling"
type = "task"
priority = 3
depends_on = ["color-theme", "progress-indicators", "detail-panel"]
+++

## Problem

The TUI currently receives `tea.WindowSizeMsg` and updates width/height, but the layout doesn't adapt well to narrow terminals (< 60 columns) or very short terminals (< 15 rows). Status bar text wraps, progress bars break, and the detail panel may have zero height. The breadcrumb bar and per-phase drill-down views also need width-aware rendering.

## Current State

The model handles `tea.WindowSizeMsg` by updating `m.Width`, `m.Height`, `m.StatusBar.Width`, and `m.Detail.SetSize()`. The `detailHeight()` helper computes available height. The `renderMainView()` sets `lv.Width` or `nv.Width` before calling `View()`. The breadcrumb is rendered via `renderBreadcrumb()` with no width awareness. Per-phase `LoopView`s in `PhaseLoops` also get their width set in `renderMainView()`.

## Solution

### Minimum Size Handling
- Define minimum usable dimensions: 40 columns, 10 rows
- Below minimum: show a centered "Terminal too small" message instead of the broken layout
- At the boundary: hide optional elements (detail panel, progress bars, breadcrumb) to fit

### Adaptive Layout
- Status bar: truncate nebula name with ellipsis when terminal is narrow
- Breadcrumb: truncate phase IDs with ellipsis, hide "output" segment if too narrow
- Progress bars: switch to compact format (percentage only) below 60 columns
- Phase table: truncate phase IDs with ellipsis when they don't fit
- Footer: show abbreviated key hints below 60 columns (`↑↓` instead of `↑/k:up  ↓/j:down`)
- Detail panel: auto-collapse when terminal height < 20 rows

### Resize Transitions
- On resize, recalculate all layout dimensions
- Reflow the detail panel viewport content
- Ensure the cursor remains valid in all views: clamp `NebulaView.Cursor`, `LoopView.Cursor`, and per-phase `PhaseLoops[*].Cursor` if their list shrunk
- Propagate width to per-phase LoopViews on resize

## Files to Modify

- `internal/tui/model.go` — Minimum size check in `View()`, adaptive layout logic, cursor clamping on resize, propagate width to `PhaseLoops`
- `internal/tui/statusbar.go` — Truncation with ellipsis for narrow terminals
- `internal/tui/footer.go` — Compact mode for narrow terminals
- `internal/tui/loopview.go` — Width-aware rendering
- `internal/tui/nebulaview.go` — Truncate phase IDs with ellipsis
- `internal/tui/detailpanel.go` — Auto-collapse logic

## Acceptance Criteria

- [ ] Terminal below 40x10 shows "Terminal too small" message
- [ ] Status bar and breadcrumb truncate cleanly at narrow widths
- [ ] Footer adapts to compact mode
- [ ] Detail panel auto-collapses on short terminals
- [ ] Cursor remains valid after resize that shrinks the list (all views including per-phase)
- [ ] No panics or rendering glitches during rapid resize
- [ ] Tests for truncation helpers and layout calculations
- [ ] `go build` and `go test ./internal/tui/...` pass
