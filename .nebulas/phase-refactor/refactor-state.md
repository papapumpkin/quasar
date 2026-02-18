+++
id = "refactor-state"
title = "Add refactor state to phase lifecycle"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Phases currently have a linear lifecycle: pending -> in_progress -> done/failed. There is no concept of a post-completion refinement pass. We need a "refactoring" state that a completed phase can enter when the user wants to improve its output.

## Solution

1. Add a `Refactoring` status to the phase state machine in `internal/nebula/types.go`:
   - New status constant alongside existing ones
   - Transition rule: only `Done` phases can move to `Refactoring`
   - When refactoring completes, phase returns to `Done`

2. Update `internal/nebula/state.go` to persist refactor state and cycle count.

3. Add a `RefactorCycles` counter to track how many refactor passes have occurred.

## Files

- `internal/nebula/types.go` — add `Refactoring` status
- `internal/nebula/state.go` — persist refactor state
- `internal/nebula/types_test.go` — test state transitions

## Acceptance Criteria

- [ ] `Refactoring` status exists and only reachable from `Done`
- [ ] State persists across checkpoint saves
- [ ] RefactorCycles counter increments per pass
- [ ] `go test ./internal/nebula/...` passes
