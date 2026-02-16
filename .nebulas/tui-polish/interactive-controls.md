+++
id = "interactive-controls"
title = "Keyboard shortcuts for pause, resume, stop, and retry"
type = "feature"
priority = 1
depends_on = ["color-theme"]
+++

## Problem

The TUI supports navigation (j/k, enter/esc for drill-down, q) and gate decisions, but has no way to pause, stop, or retry execution. Users must create PAUSE/STOP files manually. The `keys.go` defines `Pause`, `Stop`, and `Retry` bindings but they aren't wired up in `model.go`'s `handleKey`.

## Current State

The model already has:
- `handleKey` dispatching to `moveUp/moveDown/drillDown/drillUp` based on `ViewDepth`
- `handleGateKey` for gate prompt overlay
- `buildFooter()` switching between `LoopFooterBindings`, `NebulaFooterBindings`, `NebulaDetailFooterBindings`, and `GateFooterBindings`
- `NebulaDir` is NOT yet on `AppModel` — needs to be added

The nebula `Watcher` already monitors for PAUSE/STOP intervention files at runtime.

## Solution

Wire the keyboard shortcuts to actual execution control by writing intervention files that the nebula Watcher already monitors:

### Pause/Resume (`p` key)
- On first press: write a `PAUSE` file to the nebula directory (same mechanism `handlePause` already reads)
- Toggle behavior: if paused, pressing `p` again removes the PAUSE file to resume
- Update status bar to show "PAUSED" indicator when paused
- Send a `MsgPauseToggled{Paused bool}` message for UI state
- Only active at `DepthPhases` in nebula mode (not when drilled into a phase or in loop mode)

### Stop (`s` key)
- Write a `STOP` file to the nebula directory
- Show confirmation in the status bar: "Stopping..."
- The existing `handleStop` in worker.go picks it up
- Only active in nebula mode at `DepthPhases` (not loop mode, where there's only one task)

### Retry (`r` key)
- Only meaningful when viewing a failed phase in nebula mode (at `DepthPhases` or `DepthPhaseLoop`)
- Sends a signal to retry the selected phase (reset phase status in state, re-dispatch)
- Requires the selected phase to be in `PhaseFailed` state

### Implementation
- Add `Paused bool`, `Stopping bool`, `NebulaDir string` fields to `AppModel`
- In `handleKey`, add cases for `m.Keys.Pause`, `m.Keys.Stop`, `m.Keys.Retry` — check `m.Mode` and `m.Depth` before acting
- Write intervention files via `os.WriteFile`/`os.Remove` (the path is `filepath.Join(m.NebulaDir, "PAUSE")` etc.)
- Add new message types: `MsgPauseToggled`, `MsgStopRequested`
- Update `buildFooter()` to conditionally enable pause/stop/retry based on mode, depth, and state
- Pass `NebulaDir` into `AppModel` from `cmd/nebula.go` via `tui.NewProgramRaw` options or a post-init message

## Files to Modify

- `internal/tui/model.go` — Add `Paused`, `Stopping`, `NebulaDir` fields; wire key handlers; handle new messages
- `internal/tui/msg.go` — Add `MsgPauseToggled`, `MsgStopRequested`
- `internal/tui/keys.go` — Ensure pause/stop/retry bindings are enabled/disabled based on mode context
- `internal/tui/statusbar.go` — Show "PAUSED" / "STOPPING" indicators
- `internal/tui/footer.go` — `buildFooter()` already switches on mode/depth; update to show pause/stop when at `DepthPhases`
- `internal/tui/tui.go` — Accept `NebulaDir` option for intervention file paths
- `cmd/nebula.go` — Pass nebula dir into TUI (e.g. via a `MsgNebulaInit` field or `AppModel` setter)

## Acceptance Criteria

- [ ] `p` toggles pause (writes/removes PAUSE file) when at phase table level
- [ ] `s` writes STOP file in nebula mode at phase table level
- [ ] Status bar shows PAUSED/STOPPING state
- [ ] Footer shows relevant controls based on mode and drill-down depth
- [ ] Controls are disabled in loop mode and when drilled into agent output
- [ ] Retry works on failed phases
- [ ] Tests for pause toggle logic
- [ ] `go build` and `go test ./...` pass
