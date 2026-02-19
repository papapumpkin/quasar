+++
id = "home-key-handling"
title = "Handle keyboard input in home mode (navigate, select, quit)"
type = "feature"
priority = 2
depends_on = ["home-view-render"]
+++

## Problem

The home view needs keyboard navigation: up/down to browse nebulas, Enter to select one for execution, and q to quit. The `handleKey` method in `model.go` currently only handles `ModeLoop` and `ModeNebula`.

## Current State

`internal/tui/model.go`:
- `handleKey(msg tea.KeyMsg)` is the central key dispatch
- For `ModeNebula` at `DepthPhases`, it calls `m.moveUp()`, `m.moveDown()`, `m.drillDown()`, etc.
- `m.Keys` is a `KeyMap` from `keys.go` with standard bindings

## Solution

### 1. Add home-mode key handling in `handleKey`

When `m.Mode == ModeHome`:
- **Up/k**: Move cursor up in the nebula list (`m.HomeCursor--`, clamped)
- **Down/j**: Move cursor down (`m.HomeCursor++`, clamped)
- **Enter**: Select the nebula under cursor — set a field like `m.SelectedNebula` and send a `tea.Quit` command so the outer loop in `cmd/` can pick it up and launch execution
- **q**: Quit the program
- **?/i**: Toggle detail panel showing nebula description and phase list

### 2. Update detail panel on cursor move

When cursor changes, call `m.updateHomeDetail()` to populate the detail panel with the selected nebula's info (description, phase names, status breakdown).

### 3. Add `SelectedNebula` field

Add `SelectedNebula string` to `AppModel` — set to the nebula's `Path` when the user presses Enter. The outer command loop reads this after `tea.Program.Run()` returns to know which nebula to launch.

## Files to Modify

- `internal/tui/model.go` — Add home-mode case in `handleKey()`, add `SelectedNebula` field, add `updateHomeDetail()` method
- `internal/tui/keys.go` — Ensure keybindings cover home mode (may reuse existing bindings)

## Acceptance Criteria

- [ ] Up/down (and j/k) navigate the nebula list
- [ ] Enter selects a nebula and exits the TUI (setting `SelectedNebula`)
- [ ] q quits the program cleanly
- [ ] Detail panel updates when cursor moves
- [ ] Cursor is clamped to valid range
- [ ] `go build` and `go vet ./...` pass