+++
id = "home-view-render"
title = "Render the landing page view with nebula list and details"
type = "feature"
priority = 2
depends_on = ["home-mode-model"]
+++

## Problem

With `ModeHome` added to the model, we need the actual View rendering — a landing page that shows all discovered nebulas with their status, phase counts, and description. The user navigates with arrow keys and presses Enter to select.

## Current State

- `AppModel.View()` dispatches to `renderMainView()` which switches on `m.Mode`
- `ModeNebula` renders `NebulaView` (phase list with status icons)
- `ModeLoop` renders `LoopView` (agent output)
- The completion overlay already has a nebula picker (`renderNebulaPicker`) — reuse its visual patterns

## Solution

### 1. Create `homeview.go`

New file `internal/tui/homeview.go` with a `HomeView` struct and rendering:

```go
type HomeView struct {
    Nebulae  []NebulaChoice
    Cursor   int
    Width    int
}
```

The view renders a styled list of nebulas. Each row shows:
```
  ▎ nebula-name          12 phases  ready
    Short description line...
```

Status indicators:
- **ready** (gray) — no state file, hasn't been run
- **in_progress** (blue) — some phases done, not all
- **done** (green) — all phases complete
- **partial** (yellow) — some done, some failed

The selected item is highlighted with `▎` indicator bar (matching existing selection pattern from `nebulaview.go`) and shows bold text.

### 2. Wire into `renderMainView()`

Add a `case ModeHome:` in `renderMainView()` that calls `m.HomeView.View()` (or inline rendering using `m.HomeNebulae` and `m.HomeCursor`).

### 3. Detail panel integration

When a nebula is selected (cursor moves), update the detail panel to show the nebula's full description and phase list. Reuse the existing `DetailPanel` by setting its content.

### 4. Status bar in home mode

The status bar should show "QUASAR" as the name, with no progress counter (since no nebula is running). Show something like "N nebulas" as the status.

### 5. Footer keybindings

Footer should show: `↑/↓ navigate  enter run  q quit`

## Files to Modify

- `internal/tui/homeview.go` — New file with `HomeView` struct and `View()` method
- `internal/tui/model.go` — Wire `ModeHome` into `renderMainView()`, update `buildFooter()`, handle status bar for home mode
- `internal/tui/styles.go` — Add any needed styles for the home view (reuse existing palette)

## Acceptance Criteria

- [ ] Landing page renders a scrollable list of discovered nebulas
- [ ] Each nebula shows name, phase count, and status with color-coding
- [ ] Selected nebula is highlighted with the `▎` indicator
- [ ] Detail panel shows the selected nebula's description
- [ ] Status bar shows "QUASAR" with nebula count
- [ ] Footer shows relevant keybindings (navigate, run, quit)
- [ ] Empty state handled gracefully ("No nebulas found in .nebulas/")
- [ ] `go build` and `go vet ./...` pass
- [ ] Tests cover `HomeView` rendering