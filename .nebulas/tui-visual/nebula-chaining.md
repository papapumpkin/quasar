+++
id = "nebula-chaining"
title = "Jump to next nebula after completion via .nebulas/ discovery"
type = "feature"
priority = 2
depends_on = ["timer-stop-on-done"]
+++

## Problem

After a nebula finishes, the TUI just sits there. If the user has more nebulae to run (other directories in `.nebulas/`), they have to quit, find the next one, and re-launch manually. It would be nice to offer a quick jump to another nebula directly from the completion screen.

## Current State

**Nebula discovery**:
- Nebulae live in `.nebulas/<name>/` directories
- Each has a `nebula.toml` manifest and optional `nebula.state.toml`
- The state file tracks which phases are done/failed/in_progress
- `cmd/nebula.go` parses a single nebula path from CLI args

**Completion state**:
- `MsgNebulaDone` fires when all workers finish
- `m.Done = true` is set, and a completion overlay may appear
- The TUI is still running (user can press `q` to quit)

## Solution

### 1. Discover Available Nebulae

Create a helper function that scans `.nebulas/` for valid nebula directories:

```go
// internal/tui/nebula_discover.go
type NebulaChoice struct {
    Name     string // from nebula.toml [nebula] name
    Path     string // directory path
    Status   string // "ready", "in_progress", "done", "partial"
    Phases   int    // total phase count
    Done     int    // completed phases
}

func DiscoverNebulae(rootDir string) ([]NebulaChoice, error)
```

For each directory in `.nebulas/`:
- Parse `nebula.toml` to get the name and phase count
- Parse `nebula.state.toml` if it exists to determine status:
  - **ready**: no state file or all phases pending
  - **in_progress**: some phases done, some not
  - **done**: all phases done/failed/skipped
  - **partial**: has a state file but not all phases resolved
- Skip the currently-running nebula

### 2. Nebula Picker View

After completion, show a list of available nebulae the user can select:

```
┌─────────────────────────────────────────────┐
│  Nebula complete! (6/6 done, 2m34s)         │
│                                              │
│  Run another nebula?                         │
│                                              │
│  ▎ tui-visual        0/7 ready              │
│    dogfood-nebula     3/5 in_progress        │
│    auth-feature       done                   │
│                                              │
│  enter:launch  q:quit                        │
└─────────────────────────────────────────────┘
```

This could be:
- Part of the completion overlay (extend the existing overlay)
- Or a new view that replaces the main content after completion

### 3. Launch Selected Nebula

When the user selects a nebula and presses enter:
- Send a new message type `MsgLaunchNebula{Path string}`
- The main `cmd/nebula.go` would need to handle this — either by:
  - **Option A**: Returning the selected path from `tuiProgram.Run()` via the final model, then re-launching in a loop
  - **Option B**: Exec-ing a new `quasar nebula apply --auto <path>` subprocess

Option A is cleaner — the `cmd/nebula.go` `runNebula` function wraps in a loop:
```go
for {
    finalModel, tuiErr := tuiProgram.Run()
    if appModel, ok := finalModel.(tui.AppModel); ok && appModel.NextNebula != "" {
        // Re-create program for next nebula
        nextPath = appModel.NextNebula
        continue
    }
    break
}
```

### 4. Model Changes

Add to `AppModel`:
- `AvailableNebulae []NebulaChoice` — populated on `MsgNebulaDone`
- `NextNebula string` — set when user selects one (read after `Run()` returns)
- `NebulaPicker` — cursor state for the picker list

## Files to Create

- `internal/tui/nebula_discover.go` — `DiscoverNebulae()` function
- `internal/tui/nebula_discover_test.go` — Tests with temp directories

## Files to Modify

- `internal/tui/model.go` — Add `AvailableNebulae`, `NextNebula`, picker state; handle selection on done screen
- `internal/tui/msg.go` — Add `MsgLaunchNebula` if needed
- `internal/tui/overlay.go` — Extend completion overlay with nebula list (or create a new post-completion view)
- `internal/tui/keys.go` — Reuse enter key for launch
- `cmd/nebula.go` — Wrap `tuiProgram.Run()` in a loop; re-create program for next nebula

## Acceptance Criteria

- [ ] After nebula completion, available nebulae from `.nebulas/` are listed
- [ ] Each nebula shows name, phase count, and status (ready/in_progress/done)
- [ ] User can navigate the list and press enter to launch a new nebula
- [ ] Currently-completed nebula is excluded or marked as done
- [ ] Pressing `q` exits as before
- [ ] Selected nebula launches cleanly with a fresh TUI state
- [ ] `go build` and `go test ./internal/tui/...` pass
