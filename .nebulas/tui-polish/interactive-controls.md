+++
id = "interactive-controls"
title = "Keyboard shortcuts for pause, resume, stop, and retry"
type = "feature"
priority = 1
depends_on = ["color-theme"]
+++

## Problem

The TUI currently supports navigation (j/k, enter, esc, q) and gate decisions, but has no way to pause, stop, or retry execution. Users must create PAUSE/STOP files manually. The `keys.go` defines `Pause`, `Stop`, and `Retry` bindings but they aren't wired up in `model.go`.

## Solution

Wire the keyboard shortcuts to actual execution control by writing intervention files that the nebula Watcher already monitors:

### Pause/Resume (`p` key)
- On first press: write a `PAUSE` file to the nebula directory (same mechanism `handlePause` already reads)
- Toggle behavior: if paused, pressing `p` again removes the PAUSE file to resume
- Update status bar to show "PAUSED" indicator when paused
- Send a `MsgPauseToggled{Paused bool}` message for UI state

### Stop (`s` key)
- Write a `STOP` file to the nebula directory
- Show confirmation in the status bar: "Stopping…"
- The existing `handleStop` in worker.go picks it up
- Only active in nebula mode (not loop mode, where there's only one task)

### Retry (`r` key)
- Only meaningful when viewing a failed phase in nebula mode
- Sends a signal to retry the selected phase
- Requires the phase to be in failed state

### Implementation
- Add `Paused bool`, `NebulaDir string` fields to `AppModel`
- In `handleKey`, when `p`/`s`/`r` are pressed, write intervention files via `os.WriteFile`/`os.Remove`
- Add new message types: `MsgPauseToggled`, `MsgStopRequested`
- Update footer to show active controls based on mode and state
- Pass `NebulaDir` into `AppModel` from `cmd/nebula.go`

## Files to Modify

- `internal/tui/model.go` — Add fields, wire key handlers, new messages
- `internal/tui/msg.go` — Add `MsgPauseToggled`, `MsgStopRequested`
- `internal/tui/keys.go` — Ensure pause/stop/retry bindings are enabled in correct modes
- `internal/tui/statusbar.go` — Show "PAUSED" / "STOPPING" indicators
- `internal/tui/footer.go` — Update footer hints dynamically
- `internal/tui/tui.go` — Accept `NebulaDir` option for intervention file paths
- `cmd/nebula.go` — Pass nebula dir into TUI

## Acceptance Criteria

- [ ] `p` toggles pause (writes/removes PAUSE file)
- [ ] `s` writes STOP file in nebula mode
- [ ] Status bar shows PAUSED/STOPPING state
- [ ] Footer updates to show relevant controls
- [ ] Controls are disabled in loop mode where they don't apply
- [ ] Tests for pause toggle logic
- [ ] `go build` and `go test ./...` pass
