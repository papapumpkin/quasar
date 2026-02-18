+++
id = "return-to-home"
title = "Return to home screen after nebula completion"
type = "feature"
priority = 2
depends_on = ["cmd-entry-point"]
+++

## Problem

After a nebula finishes execution and the status report overlay is shown, the user should be able to return to the home screen instead of quitting entirely. Currently, the completion overlay offers a next-nebula picker, but the flow is: pick → re-launch same TUI in ModeNebula → run. We want: dismiss overlay → back to home screen with refreshed status.

## Current State

`cmd/nebula_apply.go`:
- After `tuiProgram.Run()` returns, checks `appModel.NextNebula` — if set, re-launches
- If not set, checks `appModel.DoneErr` and exits

`internal/tui/overlay.go`:
- `CompletionOverlay` shows results + nebula picker
- Enter on a nebula sets `m.NextNebula` and sends `tea.Quit`

`internal/tui/model.go`:
- Overlay dismissal currently quits the program

## Solution

### 1. New model field: `ReturnToHome bool`

When the user dismisses the completion overlay (presses Escape or a "back" key), instead of quitting, set `ReturnToHome = true` and `tea.Quit`. The outer loop in `cmd/tui.go` reads this and loops back to the home screen.

### 2. Completion overlay changes

Add a keybinding hint to the overlay: `esc back to home  enter run next  q quit`

- **Escape/b**: Set `ReturnToHome = true`, quit TUI
- **Enter** (on nebula): Set `NextNebula`, quit TUI (existing behavior)
- **q**: Quit entirely

### 3. Outer loop in `cmd/tui.go`

```go
for {
    // 1. Discover nebulas, show home screen
    // 2. User selects → run nebula
    // 3. After completion:
    //    - if ReturnToHome → continue loop (re-discover)
    //    - if NextNebula → run that nebula, then back to step 3
    //    - else → break (quit)
}
```

### 4. Re-discovery refreshes status

When returning to home, `DiscoverAllNebulae()` is called again, so nebula statuses reflect the latest state (the just-completed nebula now shows "done" or "partial").

## Files to Modify

- `internal/tui/model.go` — Add `ReturnToHome` field, handle Escape in overlay dismissal
- `internal/tui/overlay.go` — Add "back to home" keybinding hint to footer
- `cmd/tui.go` — Implement the return-to-home loop

## Acceptance Criteria

- [ ] Pressing Escape on the completion overlay returns to home screen
- [ ] Home screen shows updated nebula statuses after execution
- [ ] Enter on a nebula in the overlay still launches it directly (existing behavior preserved)
- [ ] q from the overlay exits the program entirely
- [ ] Multiple run→return→run cycles work without state leaks
- [ ] `go build` and `go vet ./...` pass