+++
id = "deprecate-beads-view"
title = "Deprecate the broken beads view (b key)"
type = "task"
priority = 2
depends_on = []
labels = ["quasar", "tui"]
scope = ["internal/tui/keys.go", "internal/tui/footer.go"]
allow_scope_overlap = true
+++

## Problem

The beads view, toggled by pressing `b`, is currently broken and provides no useful information to the user. When pressed, it opens a detail panel that either shows "(no bead data yet)" or renders a bead hierarchy tree that doesn't function correctly. This creates a confusing dead-end in the UI and wastes a valuable keybinding.

The `b` key is advertised in the footer bindings for `NebulaFooterBindings` and `NebulaDetailFooterBindings`, leading users to discover and use a broken feature.

## Solution

Remove the beads keybinding and footer hint so users are no longer led to the broken view. This is a deprecation — the underlying `BeadView` struct and rendering code in `beadview.go` can remain for now (it may be fixed later), but the keybinding and all UI entry points should be removed.

### Changes

1. **`internal/tui/footer.go`** — Remove `km.Beads` from:
   - `NebulaFooterBindings()` (line 50)
   - `NebulaDetailFooterBindings()` (line 55)
   - `LoopFooterBindings()` (line 45)

2. **`internal/tui/model.go`** — In the `handleKey` method, remove or no-op the `"b"` key case that calls `handleBeadsKey()`. The simplest approach is to remove the key match so pressing `b` does nothing. Leave `handleBeadsKey()` and `updateBeadDetail()` intact as dead code for future reactivation.

3. **`internal/tui/keys.go`** — Disable the `Beads` binding by default so it doesn't appear in any help text:
   ```go
   Beads: key.NewBinding(
       key.WithKeys("b"),
       key.WithHelp("b", "beads"),
       key.WithDisabled(),
   ),
   ```

## Files

- `internal/tui/footer.go` — Remove `km.Beads` from three footer binding functions
- `internal/tui/keys.go` — Add `key.WithDisabled()` to the Beads binding
- `internal/tui/model.go` — Remove or guard the `"b"` key handler in `handleKey`

## Acceptance Criteria

- [ ] Pressing `b` in nebula mode does nothing (no panel opens, no crash)
- [ ] Pressing `b` in loop mode does nothing
- [ ] The footer in nebula table view no longer shows `b:beads`
- [ ] The footer in nebula detail view no longer shows `b:beads`
- [ ] The footer in loop mode no longer shows `b:beads`
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
