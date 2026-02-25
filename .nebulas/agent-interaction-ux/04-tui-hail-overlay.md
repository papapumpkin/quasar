+++
id = "tui-hail-overlay"
title = "Build TUI hail overlay and notification badge"
type = "feature"
priority = 2
depends_on = ["ui-hail-interface"]
+++

## Problem

In TUI mode, hails need to be visible and actionable. The user should see when agents are asking for help, and be able to respond without leaving the TUI.

## Solution

### Notification Badge
Add a hail counter to the StatusBar that shows when unresolved hails exist:

```
⚡ 2:34 elapsed | 3/7 phases | $4.12 | ⚠ 2 hails
```

The badge should:
- Only appear when unresolved hails > 0
- Use yellow/orange color to draw attention
- Disappear when all hails are resolved

### Hail Overlay
Create a `HailOverlay` component (similar to the existing `GatePrompt`) that:
- Shows hail details: kind, source agent, cycle, summary, detail
- If the hail has options, present them as selectable choices (like gate approve/reject)
- If no options, provide a free-text input or a simple "Acknowledged" button
- On resolution, call `HailQueue.Resolve()` and send `MsgHailResolved`

### Navigation
- Pressing `h` from the main nebula/loop view opens the hail list
- If only one hail, go directly to the overlay
- If multiple, show a list to pick from (reuse the phase table pattern)

## Files

- `internal/tui/statusbar.go` — Add hail count to status bar rendering
- `internal/tui/hail_overlay.go` — New HailOverlay component
- `internal/tui/model.go` — Wire hail overlay into AppModel, handle MsgHailReceived/Resolved
- `internal/tui/msg.go` — Add hail-related messages if not already done

## Acceptance Criteria

- [ ] Status bar shows hail count badge when unresolved hails exist
- [ ] Pressing 'h' opens hail list/overlay
- [ ] Hail overlay shows full context and allows resolution
- [ ] Resolution flows back to HailQueue
- [ ] Overlay dismisses after resolution
- [ ] Multiple concurrent hails are navigable