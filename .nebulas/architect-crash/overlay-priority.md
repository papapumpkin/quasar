+++
id = "overlay-priority"
title = "Fix overlay key priority mismatch"
type = "bug"
priority = 1
depends_on = ["panic-recovery"]
+++

## Bug

In `handleKey()`, the completion overlay check (`m.Overlay != nil`, line 534) comes BEFORE the architect check (`m.Architect != nil`, line 557). If `MsgNebulaDone` arrives while the architect is active, the user sees the architect overlay (it renders first in `View()`) but pressing `q` triggers `tea.Quit` via the completion overlay handler instead of being handled by the architect overlay.

## Root Cause

Two issues:

1. **Key priority**: `handleKey()` checks `m.Overlay` before `m.Architect`. Since `MsgNebulaDone` sets `m.Overlay` without clearing `m.Architect`, both are non-nil simultaneously. The completion overlay "steals" key events.

2. **Missing cleanup**: The `MsgNebulaDone` handler never cancels or clears the architect overlay. The architect goroutine continues running uselessly, and the overlay lingers behind the completion screen.

## Fix

### 1. Move architect check before completion overlay in `handleKey()`

In `internal/tui/model.go`, `handleKey()` method: move the `if m.Architect != nil` block (line ~557-560) to BEFORE the `if m.Overlay != nil` block (line ~534). This ensures the architect overlay receives key events when it's active, regardless of whether a completion overlay also exists.

### 2. Cancel and clear architect on `MsgNebulaDone`

In the `MsgNebulaDone` handler (line ~375), after setting `m.Overlay`, add:
```go
if m.Architect != nil {
    if m.Architect.CancelFunc != nil {
        m.Architect.CancelFunc()
    }
    m.Architect = nil
}
```

### 3. Add tests to `internal/tui/model_controls_test.go`

- **Test architect takes priority**: set both `m.Architect` and `m.Overlay` to non-nil, send a key event, verify the architect handler receives it (not the completion overlay)
- **Test MsgNebulaDone cleans up architect**: set `m.Architect` with a `CancelFunc`, send `MsgNebulaDone`, verify `m.Architect` is nil and the cancel function was called

## Files

- `internal/tui/model.go` — reorder checks in `handleKey()`, add cleanup in `MsgNebulaDone` handler
- `internal/tui/model_controls_test.go` — add overlay priority and cleanup tests
