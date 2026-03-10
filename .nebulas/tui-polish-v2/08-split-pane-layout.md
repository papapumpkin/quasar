+++
id = "split-pane-layout"
title = "Posting-inspired split pane layout with sidebar and resizable panels"
type = "feature"
priority = 2
depends_on = []
+++

## Problem

The current TUI layout is strictly vertical: status bar → tab bar → banner → main view → detail panel → bottom bar → footer. There's no sidebar, no horizontal splits, and no way to see multiple views simultaneously. Posting's layout (left sidebar + right area split into top/bottom panels) is far more information-dense and navigable.

## Solution

Restructure the TUI layout into a Posting-inspired split-pane design with three main regions:

### Target Layout

```
┌─────────────────────────────────────────────────────────────┐
│ ◈ quasar  nebula: my-nebula  ━━━━━░░░ 3/8  $2.41    4m 12s│  ← status bar
├────────────┬────────────────────────────────────────────────┤
│            │ [Board] [Graph] [Entanglements] [Scratchpad]  │  ← tabs (right only)
│  PHASES    │                                               │
│            │  Queued    Running    Review    Done    Failed │
│  ▸ phase-1 │  ○ ph-4    ● ph-2    ◉ ph-5   ✓ ph-1  ✗ ph-3│  ← board view
│    phase-2 │  ○ ph-6    ● ph-7              ✓ ph-8        │
│    phase-3 │           ┌────────────────────────┐          │
│    phase-4 │  WORKERS  │ q-1: ph-2             │          │  ← worker cards
│    phase-5 │           │ coding · cycle 2/5    │          │
│    phase-6 │           │ $0.42 · 1m 23s        │          │
│    phase-7 │           └────────────────────────┘          │
│    phase-8 │                                               │
│            ├───────────────────────────────────────────────-│
│            │ ── detail panel ──                             │  ← detail (bottom right)
│            │ role: coder  cycle: 2  cost: $0.21            │
│            │                                               │
│            │ ## Changes                                     │
│            │ Refactored the invoker to use...              │
├────────────┴────────────────────────────────────────────────┤
│ tokens 42.1k | cost $2.41 | elapsed 4m 12s | progress ████░ 3/8 │  ← bottom bar
├─────────────────────────────────────────────────────────────┤
│ ↑/↓ navigate  ←/→ columns  enter drill  d diff  ? help    │  ← footer
└─────────────────────────────────────────────────────────────┘
```

### Approach

1. **Left sidebar — Phase list**: A persistent vertical list of all phases with status icons and truncated titles. This replaces the need for the board to show phase names (the board becomes a compact visual status). The sidebar:
   - Shows all phases with status icon (○ queued, ● running, ✓ done, ✗ failed, ◉ review)
   - Current selection highlighted
   - Phase title wraps to 2 lines if sidebar is wide enough
   - Scrollable when phases exceed viewport height
   - Width: ~20-25% of terminal, min 20 chars, max 35 chars

2. **Right top — Main content area**: The board view, graph view, entanglement view, or scratchpad — whichever tab is active. Worker cards render inline here when phases are running.

3. **Right bottom — Detail panel**: Agent output, diff view, bead tracker — contextual to the selected phase. This is the existing detail panel, just repositioned.

4. **Horizontal split ratio**: The right area splits ~60/40 (main/detail) by default. When detail is collapsed, main takes full height.

5. **Implementation**: Use `lipgloss.JoinHorizontal` for the sidebar + right split, and `lipgloss.JoinVertical` within the right area for top/bottom split. Height calculation:
   ```
   totalHeight = terminal height - statusBar - bottomBar - footer
   sidebarHeight = totalHeight
   rightTopHeight = totalHeight * 0.6  (or totalHeight if detail collapsed)
   rightBottomHeight = totalHeight * 0.4
   ```

6. **Responsive**: On terminals narrower than 100 cols, collapse the sidebar and fall back to the current vertical layout. On very wide terminals (>200), the sidebar can expand to show more of the phase title.

7. **Navigation**: ←/→ moves focus between sidebar and right area. When sidebar is focused, ↑/↓ navigates phases. When right area is focused, existing navigation applies (board movement, scroll, etc.). Tab key can also toggle focus.

## Files

- `internal/tui/model.go` — restructure `View()` to compose sidebar + right panes
- `internal/tui/sidebar.go` — new file, phase list sidebar component
- `internal/tui/sidebar_test.go` — tests
- `internal/tui/layout.go` — add split-pane layout constants and helpers

## Acceptance Criteria

- [ ] Three-region layout: left sidebar + right top (main) + right bottom (detail)
- [ ] Phase sidebar shows all phases with status icons, scrollable
- [ ] Focus navigation between sidebar and right area via ←/→ or Tab
- [ ] Selecting a phase in sidebar updates the right panels
- [ ] Falls back to current vertical layout on narrow terminals (<100 cols)
- [ ] Detail panel collapse still works (right area becomes single pane)
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
