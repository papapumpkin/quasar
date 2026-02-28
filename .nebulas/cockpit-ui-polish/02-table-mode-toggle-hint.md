+++
id = "table-mode-toggle-hint"
title = "Show v:board hint in table mode footer"
type = "task"
priority = 2
depends_on = ["deprecate-beads-view"]
labels = ["quasar", "tui"]
scope = ["internal/tui/footer.go"]
allow_scope_overlap = true
+++

## Problem

When the user is in board mode (columnar view) and presses `v` to switch to table mode, the footer changes from `CockpitFooterBindings` (which shows `v:table`) to `NebulaFooterBindings` (which does **not** show any `v:board` hint). This means the user has no way of knowing how to get back to board mode — the toggle is only discoverable in one direction.

The `buildFooter()` method in `model.go` uses `CockpitFooterBindings` when `m.BoardActive` is true (line 2138-2139), but falls through to `NebulaFooterBindings` when `m.BoardActive` is false (line 2143-2144). `NebulaFooterBindings` in `footer.go` doesn't include the `BoardToggle` binding.

## Solution

Add the `BoardToggle` binding to `NebulaFooterBindings` with the label `v:board` so users in table mode can see how to switch to board mode. The binding should only appear when the terminal is wide enough for board mode (`>= BoardMinWidth`), matching the existing guard in the `"v"` key handler.

### Approach

Modify `NebulaFooterBindings` to accept the board toggle and show it with the "board" label. Since `NebulaFooterBindings` is also used at the top level of the `buildFooter` else-if chain (when `!m.BoardActive`), this is the right place to add the hint.

1. **`internal/tui/footer.go`** — Add the `BoardToggle` binding to `NebulaFooterBindings`:
   ```go
   func NebulaFooterBindings(km KeyMap) []key.Binding {
       boardToggle := km.BoardToggle
       boardToggle.SetHelp("v", "board")
       return []key.Binding{km.Up, km.Down, km.Enter, km.Info, boardToggle, km.Pause, km.Stop, km.Quit}
   }
   ```

   The `CockpitFooterBindings` already sets `boardToggle.SetHelp("v", "table")` which is correct for the board→table direction.

2. **Conditional display** — The toggle hint should only appear if the terminal is wide enough. The simplest approach is to conditionally include it in `buildFooter()` in `model.go` by checking `m.Width >= BoardMinWidth` before using the bindings that include the toggle. Alternatively, since `NebulaFooterBindings` is called unconditionally, include the binding but disable it when the terminal is too narrow. The cleanest approach: always include it in the footer definition, and let the existing `"v"` key handler's width guard prevent the action on narrow terminals (the hint just tells users the key exists; if too narrow, the view won't switch — acceptable UX).

## Files

- `internal/tui/footer.go` — Add `BoardToggle` binding with "board" label to `NebulaFooterBindings`

## Acceptance Criteria

- [ ] In table mode (board inactive), the footer shows `v:board` hint
- [ ] In board mode, the footer continues to show `v:table` hint (existing behavior)
- [ ] Pressing `v` in table mode switches to board mode (existing behavior, unchanged)
- [ ] Pressing `v` in board mode switches to table mode (existing behavior, unchanged)
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
