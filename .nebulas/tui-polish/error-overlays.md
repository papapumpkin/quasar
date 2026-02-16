+++
id = "error-overlays"
title = "Error and completion overlay screens"
type = "feature"
priority = 3
depends_on = ["color-theme", "interactive-controls"]
+++

## Problem

When the loop or nebula finishes (success, error, max cycles, budget exceeded), the TUI just sets `Done = true` and `DoneErr` but doesn't communicate the outcome visually. The user has to quit and read stderr. Similarly, errors during execution are added to a `Messages []string` slice but aren't surfaced prominently. Phase-level errors (`MsgPhaseError`) are also just appended to messages.

## Current State

The model handles `MsgLoopDone{Err}` and `MsgNebulaDone{Results, Err}` by setting `m.Done = true` and `m.DoneErr`. Phase errors (`MsgPhaseError`) set `PhaseFailed` status on the `NebulaView` and append to `m.Messages`. General errors (`MsgError`) and info (`MsgInfo`) just append to `m.Messages`. The `Messages` slice is never rendered in the view — it's effectively invisible.

## Solution

### Completion Overlay
- When `MsgLoopDone` or `MsgNebulaDone` arrives, show a centered overlay:
  - **Success**: Green border, checkmark, "Task complete" with total cost and duration
  - **Max cycles**: Yellow border, warning icon, "Max cycles reached (N)" with suggestion
  - **Budget exceeded**: Red border, "Budget exceeded ($X / $Y)"
  - **Error**: Red border, error message
  - **Nebula results**: Summary table of phase outcomes (done/failed/skipped counts from `MsgNebulaDone.Results`)
- Overlay has "Press q to exit" hint at bottom
- The underlying view (at whatever drill-down depth the user was at) is still visible (dimmed) behind the overlay

### Error Toast
- When `MsgError` or `MsgPhaseError` arrives during execution (not at completion), show a brief toast notification
- Toast appears at the bottom of the screen above the footer for 5 seconds, then fades
- Red background, white text, shows the error message (phase errors prefixed with `[phaseID]`)
- Multiple errors queue up

### Implementation
- New file: `internal/tui/overlay.go` — shared overlay rendering (centered box on dimmed background)
- Add `Overlay *CompletionOverlay` field to `AppModel`
- Add `Toasts []Toast` with auto-dismiss via `tea.Tick`
- Completion overlay takes precedence over normal key handling (only `q` to exit)
- Gate overlay and completion overlay should not conflict (gate resolves before completion)

## Files to Modify

- `internal/tui/overlay.go` — New file: CompletionOverlay and Toast types with rendering
- `internal/tui/model.go` — Add overlay/toast fields; handle `MsgLoopDone`/`MsgNebulaDone` to create overlay; handle `MsgError`/`MsgPhaseError` to create toasts; toast auto-dismiss; overlay key handling
- `internal/tui/msg.go` — Add `MsgToastExpired{ID int}` for auto-dismiss

## Acceptance Criteria

- [ ] Completion overlay appears on task/nebula completion
- [ ] Overlay shows appropriate icon, color, and message for each outcome
- [ ] Nebula completion overlay shows phase result summary (done/failed/skipped)
- [ ] Underlying view is visible but dimmed behind overlay
- [ ] Error toasts appear briefly during execution (both `MsgError` and `MsgPhaseError`)
- [ ] Toasts auto-dismiss after 5 seconds
- [ ] `q` exits from the completion overlay
- [ ] Tests for overlay rendering
- [ ] `go build` and `go test ./internal/tui/...` pass
