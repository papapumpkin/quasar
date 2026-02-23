+++
id = "tab-system"
title = "Tab navigation framework"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/tui/tabs.go", "internal/tui/tabs_test.go"]
+++

## Problem

The cockpit mockup shows three top-level views accessible via tabs: **Board** (task board), **Contracts** (interface agreements), and **Scratchpad** (shared notes). The current TUI has no tab navigation — it uses depth-based drill-down (Phases -> PhaseLoop -> AgentOutput) but no lateral switching between peer views.

## Solution

Create a `TabBar` component and a `CockpitTab` enum that manages lateral view switching within nebula mode.

```go
type CockpitTab int

const (
    TabBoard     CockpitTab = iota  // Task board (default)
    TabContracts                     // Interface contracts
    TabScratchpad                    // Shared notes
)
```

The `TabBar` renders a horizontal row of tab labels below the status bar, styled like:
```
  [1] board  [2] contracts  [3] scratchpad
```

The active tab is highlighted with `colorMagenta` (matching the logo accent) and bold. Inactive tabs are `colorMuted`. The tab bar is a single-line Lip Gloss render.

**Keybindings:**
- `Tab` cycles forward through tabs
- `Shift+Tab` cycles backward
- `1`, `2`, `3` jumps directly to a tab

The `AppModel` gains an `ActiveTab CockpitTab` field. In `View()`, the active tab determines which content view renders below the tab bar:
- `TabBoard` → `BoardView` + `WorkerCards` (phases 1-2)
- `TabContracts` → `ContractView` (phase 6)
- `TabScratchpad` → `ScratchpadView` (phase 7)

Until the contract and scratchpad views are built (later phases), tabs 2 and 3 render a placeholder: `"(coming soon)"` in `colorMuted`.

The tab bar only appears in nebula mode when the board view is active (not during splash, home view, or loop mode). The existing depth-based navigation (Enter/Esc drill-down) continues to work within each tab.

## Files

- `internal/tui/tabs.go` — `TabBar` component, `CockpitTab` enum, tab rendering and key handling
- `internal/tui/tabs_test.go` — Tests for tab cycling, direct jump, and render output

## Acceptance Criteria

- [ ] Tab bar renders below the status bar with three labeled tabs
- [ ] Active tab is visually distinct (bold + accent color)
- [ ] `Tab` / `Shift+Tab` cycles tabs, `1`/`2`/`3` jumps directly
- [ ] `AppModel.ActiveTab` controls which content view renders
- [ ] Tab bar only appears in nebula mode at `DepthPhases` level
- [ ] Placeholder content shown for unimplemented tabs
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
