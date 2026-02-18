+++
id = "remove-architect-overlay"
title = "Remove edit phase and add phase functionality entirely"
type = "task"
priority = 2
+++

## Problem

The architect overlay feature — triggered by 'n' (new phase) and 'e' (edit/refactor phase) — adds complexity that is not wanted in the application. This functionality should be completely removed, including all supporting code, key bindings, styles, and message types.

## Current State

`internal/tui/architect_overlay.go`:
- Full multi-step overlay for creating and editing phases
- Input flow: input → working → preview → confirm
- Dependency picker with cycle detection
- Architect invocation via the agent system

`internal/tui/model.go`:
- 'n' key bound to create new phase (ModeNebula, DepthPhases only)
- 'e' key bound to edit/refactor a working or waiting phase
- Message handling for architect responses
- State management for architect overlay visibility

`internal/tui/keys.go`:
- Key bindings for 'n' and 'e' in the phase view context

`internal/tui/styles.go`:
- Styles for the architect overlay (if any dedicated ones exist)

`internal/tui/footer.go`:
- Footer key hints showing 'n' for new phase and 'e' for edit phase

## Solution

### 1. Remove architect_overlay.go

Delete the entire file. This removes the overlay component.

### 2. Remove Key Bindings

In `keys.go` and `model.go`, remove:
- 'n' key handler for new phase creation
- 'e' key handler for phase editing/refactoring
- Any related key binding definitions

### 3. Remove Message Types

Remove any message types specific to the architect overlay (e.g., architect response messages, architect input messages) from wherever they're defined.

### 4. Remove Footer Hints

In `footer.go`, remove the key hints for 'n' (new phase) and 'e' (edit phase).

### 5. Remove Model State

In `model.go`, remove:
- Fields tracking architect overlay state (visibility, current step, input buffer)
- Logic routing messages to the architect overlay
- Import of architect-related packages if they become unused

### 6. Clean Up Styles

Remove any styles in `styles.go` that were exclusively used by the architect overlay.

### 7. Clean Up Dead Code

After removing the overlay, run `go vet ./...` to identify any dead code or unused imports. Remove them.

## Files to Modify

- `internal/tui/architect_overlay.go` — **Delete entirely**
- `internal/tui/model.go` — Remove architect state, key handlers, message routing
- `internal/tui/keys.go` — Remove 'n' and 'e' key bindings
- `internal/tui/footer.go` — Remove 'n' and 'e' key hints
- `internal/tui/styles.go` — Remove architect-only styles (if any)
- Any other files that reference the architect overlay

## Acceptance Criteria

- [ ] `architect_overlay.go` is deleted
- [ ] 'n' and 'e' keys no longer trigger any phase creation/editing
- [ ] Footer no longer shows hints for new/edit phase
- [ ] No dead code or unused imports remain
- [ ] `go build` and `go vet ./...` pass
- [ ] `go test ./...` passes (update/remove tests that reference architect overlay)