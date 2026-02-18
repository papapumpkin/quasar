+++
id = "wire-startup"
title = "Wire splash animation into TUI startup sequence"
type = "feature"
priority = 1
depends_on = ["import-splash"]
+++

## Problem

The splash animation component exists but is not yet shown when quasar starts. It needs to be wired into the TUI startup flow so users see the binary-star animation before the main dashboard appears.

## Solution

Modify the TUI initialization to run the splash as a phase before the main model:

1. In `internal/tui/tui.go` (or wherever the Bubble Tea program is created):
   - Before entering the main TUI model, create and run the splash
   - The splash plays its animation (2 spins with ease-out deceleration)
   - Any keypress skips the splash immediately
   - When `splash.Done()` returns true, transition to the main TUI model

2. Integration pattern (from the README):
   ```go
   type app struct {
       splash  SplashModel
       main    mainModel
       booted  bool
   }
   ```
   The app delegates to splash until `Done()`, then switches to main.

3. Add a `--no-splash` flag or config option to skip the animation (for CI, scripting, etc.)

## Files

- `internal/tui/tui.go` — startup sequence modification
- `internal/tui/model.go` — may need a wrapper model or state flag
- `cmd/nebula.go` or `cmd/run.go` — flag for `--no-splash`

## Acceptance Criteria

- [ ] Splash plays on TUI startup by default
- [ ] Any keypress skips the splash
- [ ] After splash completes, main TUI loads seamlessly
- [ ] `--no-splash` flag disables the animation
- [ ] Non-TUI mode (stderr printer) is unaffected
- [ ] `go build` and `go test ./...` pass
