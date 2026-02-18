+++
id = "tui-refactor-action"
title = "Add refactor action to the TUI for completed phases"
type = "feature"
priority = 2
depends_on = ["refactor-prompt"]
+++

## Problem

Users need a way to trigger a refactor pass from the TUI after a nebula completes. Currently there is no UI affordance for this.

## Solution

1. **Availability**: The refactor action should only appear when:
   - The nebula has finished (all phases done or failed)
   - The selected phase has status `Done`

2. **Trigger**: Add a keybinding (e.g., `r` for refactor) that:
   - Shows a confirmation prompt: "Refactor phase X? This will run a cleanup pass."
   - On confirm, transitions the phase to `Refactoring` status
   - Kicks off a new worker run with the refactor prompt
   - Updates the TUI to show refactor progress (reuse existing cycle view)

3. **Footer hint**: When a completed phase is selected and the nebula is done, show `r refactor` in the footer keybindings.

4. **Completion**: When the refactor loop finishes, the phase returns to `Done` with an incremented `RefactorCycles` counter. The detail panel should show "Refactored (1 pass)" or similar.

## Files

- `internal/tui/model.go` — key handler for `r`
- `internal/tui/footer.go` — conditional refactor hint
- `internal/tui/nebulaview.go` — refactor status rendering
- `internal/nebula/worker.go` — launch refactor worker
- `internal/nebula/apply.go` — orchestrate refactor run

## Acceptance Criteria

- [ ] `r` key triggers refactor on a completed phase after nebula is done
- [ ] Confirmation prompt shown before starting
- [ ] Phase shows `Refactoring` status during the pass
- [ ] Refactor uses existing coder-reviewer loop with specialized prompt
- [ ] Phase returns to `Done` after refactor completes
- [ ] Footer shows refactor hint only when applicable
- [ ] `go build` and `go test ./...` pass
