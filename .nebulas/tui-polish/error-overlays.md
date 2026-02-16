+++
id = "error-overlays"
title = "Error and completion overlay screens"
type = "feature"
priority = 3
depends_on = ["color-theme", "interactive-controls"]
+++

## Problem

When the loop or nebula finishes (success, error, max cycles, budget exceeded), the TUI just sets `Done = true` but doesn't communicate the outcome visually. The user has to quit and read stderr. Similarly, errors during execution are added to a messages slice but aren't surfaced prominently.

## Solution

### Completion Overlay
- When `MsgLoopDone` or `MsgNebulaDone` arrives, show a centered overlay:
  - **Success**: Green border, checkmark, "Task complete" with total cost and duration
  - **Max cycles**: Yellow border, warning icon, "Max cycles reached (N)" with suggestion
  - **Budget exceeded**: Red border, "Budget exceeded ($X / $Y)"
  - **Error**: Red border, error message
  - **Nebula results**: Summary table of phase outcomes (done/failed/skipped counts)
- Overlay has "Press q to exit" hint at bottom
- The underlying view is still visible (dimmed) behind the overlay

### Error Toast
- When `MsgError` arrives during execution (not at completion), show a brief toast notification
- Toast appears at the bottom of the screen above the footer for 5 seconds, then fades
- Red background, white text, shows the error message
- Multiple errors queue up

### Implementation
- New file: `internal/tui/overlay.go` — shared overlay rendering (centered box on dimmed background)
- Add `Overlay *CompletionOverlay` field to `AppModel`
- Add `Toasts []Toast` with auto-dismiss via `tea.Tick`

## Files to Modify

- `internal/tui/overlay.go` — New file: CompletionOverlay and Toast types with rendering
- `internal/tui/model.go` — Add overlay/toast fields, handle completion messages, toast auto-dismiss
- `internal/tui/msg.go` — Add `MsgToastExpired{ID int}` for auto-dismiss

## Acceptance Criteria

- [ ] Completion overlay appears on task/nebula completion
- [ ] Overlay shows appropriate icon, color, and message for each outcome
- [ ] Underlying view is visible but dimmed behind overlay
- [ ] Error toasts appear briefly during execution
- [ ] Toasts auto-dismiss after 5 seconds
- [ ] `q` exits from the completion overlay
- [ ] Tests for overlay rendering
- [ ] `go build` and `go test ./internal/tui/...` pass
