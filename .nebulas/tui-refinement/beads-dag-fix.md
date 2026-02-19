+++
id = "beads-dag-fix"
title = "Fix beads view to render DAG tree instead of wall of text"
type = "bug"
priority = 1
+++

## Problem

When pressing 'b' to open the beads view, the user sees a **wall of text filling the entire screen** instead of the structured DAG tree with progress bar that the code is designed to render.

## Investigation Findings

Thorough code tracing of the full pipeline (`handleBeadsKey` → `updateBeadDetail` → `BeadView.View()` → `DetailPanel.SetContent` → viewport) shows the rendering logic is correct in isolation — all unit tests pass with proper tree output at every depth level (DepthPhases, DepthPhaseLoop, DepthAgentOutput).

Two defensive fixes were already applied:
1. **`internal/loop/loop.go`**: Changed `truncate()` to `firstLine()` for bead child titles — `truncate()` didn't strip newlines, so multi-line finding descriptions could break the tree layout
2. **`internal/tui/beadview.go`**: Added `strings.ReplaceAll(c.Title, "\n", " ")` before truncation as a belt-and-suspenders defense

The root cause is likely a race condition or timing issue in the live TUI where:
- Agent output messages (`MsgPhaseAgentOutput`) arrive and call `updateDetailFromSelection()` concurrently with the 'b' key press
- Or bead data hasn't been populated yet when 'b' is pressed, so the detail panel shows stale content from the previous view

## Remaining Work

If the issue persists after the defensive fixes above, investigate:

1. **Bubbletea message ordering** — verify that `handleBeadsKey` mutations are not being overwritten by concurrent `MsgPhaseAgentOutput` processing
2. **Viewport initialization** — the detail panel viewport might have zero dimensions if `WindowSizeMsg` hasn't fired yet
3. **Content overflow** — if there are many findings (50+ children), the tree could visually overwhelm the viewport; consider adding a max-children limit with a "(and N more...)" indicator

## Files Modified

- `internal/loop/loop.go` — Changed `truncate()` to `firstLine()` for bead child titles
- `internal/tui/beadview.go` — Strip newlines from child titles before truncation

## Files to Investigate if Issue Persists

- `internal/tui/model.go` — Message ordering around `MsgPhaseAgentOutput` + `handleBeadsKey`
- `internal/tui/detailpanel.go` — Viewport initialization when transitioning from hidden to visible

## Acceptance Criteria

- [ ] Pressing 'b' shows a structured tree with connectors, not a wall of text
- [ ] Finding descriptions with newlines don't break the tree layout
- [ ] Status icons are color-coded: green for closed, blue for in-progress, white for open
- [ ] `go build` and `go test ./internal/tui/... ./internal/loop/...` pass
