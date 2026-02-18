+++
id = "home-mode-model"
title = "Add ModeHome to AppModel for the landing page view state"
type = "feature"
priority = 1
depends_on = ["speed-up-splash"]
+++

## Problem

The TUI currently has two modes: `ModeLoop` (single bead run) and `ModeNebula` (nebula execution). There's no concept of a "home" or "landing page" mode where the user can browse and select nebulas before execution begins.

## Current State

`internal/tui/model.go`:
- `Mode` type with `ModeLoop` and `ModeNebula` constants
- `AppModel` struct holds all TUI state, including `AvailableNebulae []NebulaChoice`
- `NebulaChoice` (in `nebula_discover.go`) already has Name, Path, Status, Phases, Done fields
- `DiscoverNebulae()` discovers sibling nebulas (used for the post-completion picker)

`internal/tui/tui.go`:
- `NewNebulaProgram()` creates a program in `ModeNebula`
- No constructor for a "home" mode program

## Solution

### 1. Add `ModeHome` constant

Add `ModeHome Mode = "home"` alongside `ModeLoop` and `ModeNebula` in `model.go`.

### 2. Extend `AppModel` for home mode

Add fields to `AppModel`:
```go
HomeCursor   int              // cursor position in the home nebula list
HomeNebulae  []NebulaChoice   // discovered nebulas for the home view
HomeDir      string           // the .nebulas/ parent directory
```

### 3. Enhance `DiscoverNebulae`

The existing `DiscoverNebulae` takes a `currentDir` (the currently-running nebula dir) and discovers its siblings. For the home screen, we need a variant that discovers ALL nebulas in a `.nebulas/` directory. Add a new function:

```go
// DiscoverAllNebulae scans the given directory for valid nebula subdirectories.
// Unlike DiscoverNebulae, it does not exclude any directory.
func DiscoverAllNebulae(nebulaeDir string) ([]NebulaChoice, error)
```

This should also load the nebula description from the manifest for display.

### 4. Extend `NebulaChoice` with description

Add a `Description string` field to `NebulaChoice` so the home screen can show a summary.

### 5. Add `NewHomeProgram` constructor

In `tui.go`, add:
```go
func NewHomeProgram(nebulaeDir string, choices []NebulaChoice, noSplash bool) *Program
```

This creates the BubbleTea program in `ModeHome`, populates `HomeNebulae` and `HomeDir`, and sets up the splash.

## Files to Modify

- `internal/tui/model.go` — Add `ModeHome`, new fields, handle `ModeHome` in `Init()`
- `internal/tui/nebula_discover.go` — Add `DiscoverAllNebulae()`, add `Description` to `NebulaChoice`
- `internal/tui/tui.go` — Add `NewHomeProgram()`

## Acceptance Criteria

- [ ] `ModeHome` constant exists and is usable
- [ ] `DiscoverAllNebulae()` returns all valid nebulas in a directory with Name, Description, Status, Phases, Done
- [ ] `NebulaChoice` has a `Description` field populated from the manifest
- [ ] `NewHomeProgram()` creates a program in `ModeHome` with the splash animation
- [ ] Existing `ModeNebula` and `ModeLoop` paths are unaffected
- [ ] Tests cover `DiscoverAllNebulae()` and `ModeHome` creation
- [ ] `go build` and `go vet ./...` pass
