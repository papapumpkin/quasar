+++
id = "integration-test"
title = "Add integration tests for the home screen flow"
type = "task"
priority = 3
depends_on = ["return-to-home"]
+++

## Problem

The new home screen flow (discover → select → run → return) needs test coverage to prevent regressions.

## Solution

### 1. Unit tests for `DiscoverAllNebulae`

In `internal/tui/nebula_discover_test.go`:
- Test with a temp directory containing valid nebula subdirectories (with `nebula.toml` and phase files)
- Test with empty directory → returns empty slice
- Test with mixed valid/invalid subdirectories → only valid ones returned
- Test that `Description` field is populated from manifest

### 2. Unit tests for `HomeView` rendering

In `internal/tui/homeview_test.go`:
- Test rendering with multiple nebulas (verify all names appear)
- Test cursor highlighting (selected item has `▎` prefix)
- Test empty state rendering
- Test status color-coding

### 3. Model tests for home mode

In `internal/tui/model_controls_test.go` or a new `home_test.go`:
- Test key handling: up/down moves cursor
- Test Enter sets `SelectedNebula` and returns quit command
- Test q returns quit
- Test cursor clamping at boundaries

### 4. `NewHomeProgram` constructor test

Test that `NewHomeProgram` creates a valid program with `ModeHome`, correct initial state.

## Files to Modify

- `internal/tui/nebula_discover_test.go` — Add `TestDiscoverAllNebulae` tests
- `internal/tui/homeview_test.go` — New file with rendering tests
- `internal/tui/model_controls_test.go` or `internal/tui/home_test.go` — Key handling tests

## Acceptance Criteria

- [ ] `DiscoverAllNebulae` has tests for happy path, empty dir, and mixed valid/invalid
- [ ] `HomeView` rendering is tested (names, cursor, empty state)
- [ ] Home-mode key handling is tested (up, down, enter, q, boundary clamping)
- [ ] All tests pass with `go test ./internal/tui/...`
- [ ] `go vet ./...` passes