+++
id = "adaptive-layout"
title = "Adaptive layout: side panel on wide, top banner on medium/narrow"
type = "feature"
priority = 1
depends_on = ["banner-art"]
scope = ["internal/tui/model.go", "internal/tui/layout.go"]
+++

## Problem

The `Banner` component exists but isn't wired into the TUI layout. We need to integrate it into `AppModel.View()` with three adaptive modes based on terminal width, without breaking any existing functionality.

## Current View() Structure (do NOT change this logic)

```go
func (m AppModel) View() string {
    sections = append(sections, m.StatusBar.View())           // full width
    sections = append(sections, m.renderBreadcrumb())          // full width (nebula only)
    sections = append(sections, m.renderMainView())            // full width
    sections = append(sections, sep + m.Detail.View())         // full width (when active)
    sections = append(sections, m.Gate.View())                 // full width (when active)
    sections = append(sections, RenderToasts(m.Toasts, ...))   // full width
    sections = append(sections, footer.View())                 // full width
    return lipgloss.JoinVertical(lipgloss.Left, sections...)
}
```

## Solution — Three Layout Modes

### Layout A: Side Panel (width >= 120 cols)

S-B · Tall Ellipse sits in a left panel alongside the main content area. Status bar and footer span full width. The art panel sits beside the "middle" section (breadcrumb + main view + detail panel).

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│ ━━╋━━ QUASAR ━━╋━━  setup  ◆ $1.24 ▓▓▓▓░░ $5.00  2/5  03:12                  │
├────────────────────────────┬─────────────────────────────────────────────────────┤
│                            │                                                     │
│     ..·::····::·..         │  phases › setup › output                           │
│  .::··:::\|/:::··::.       │                                                     │
│.::··::.··\|/··.::··::.     │ ▸ Cycle 1                                          │
│··::. ·::\|/::· .::··::.    │   ├── ✓ coder   1.2s  $0.0031                     │
│·::. ··::.·|·.::·· .::··::  │   └── ✓ reviewer 0.8s $0.0012                     │
│·::. ··::---@---::·· .::··::│   Cycle 2                                          │
│·::. ··::.·|·.::·· .::··::  │   ├── ◆ coder   working… 3s  ⠋                   │
│··::. ·::/|\::· .::··::.    │   └── · reviewer                                   │
│.::··::..·/|\··..::··::.    │                                                     │
│··::. ·::\|/::· .::··::.    │                                                     │
│·::. ··::.·|·.::·· .::··::  │                                                     │
│·::. ··::---@---::·· .::··::│                                                     │
│·::. ··::.·|·.::·· .::··::  │                                                     │
│··::. ·::/|\::· .::··::.    │                                                     │
│.::··::..·/|\··..::··::.    │                                                     │
│  .::··:::/|\:::··::.       │ ────────────────────────────────────                │
│     ..·::····::·..         │ coder · cycle 2 · working… 3s                      │
│                            │ Agent output appears here...                        │
│  Q    U    A    S    A    R│                                                     │
│                            │                                                     │
├────────────────────────────┴─────────────────────────────────────────────────────┤
│ [j/k navigate] [⏎ drill] [esc back] [d diff] [q quit]                          │
└──────────────────────────────────────────────────────────────────────────────────┘
```

Implementation in `View()`:

```go
// Build the "middle" section: breadcrumb + main view + detail + gate + toasts.
var middle []string
// ... append breadcrumb, mainView, detail, gate, toasts as today ...
middleStr := lipgloss.JoinVertical(lipgloss.Left, middle...)

if m.Banner.Size() == BannerSB {
    // Side panel mode: join art panel horizontally with middle content.
    middleHeight := lipgloss.Height(middleStr)
    artPanel := m.Banner.SidePanelView(middleHeight)
    middleStr = lipgloss.JoinHorizontal(lipgloss.Top, artPanel, middleStr)
}

// Final assembly: status bar + middle + footer.
sections := []string{m.StatusBar.View(), middleStr, footer.View()}
```

The key change: the middle content's `Width` is reduced by `m.Banner.SidePanelWidth()` when in side panel mode. Set this on all sub-views:
- `m.LoopView.Width = contentWidth`
- `m.NebulaView.Width = contentWidth`
- `m.Detail` width
- Breadcrumb width

Where `contentWidth = m.Width - m.Banner.SidePanelWidth()`.

### Layout B: Top Banner — S-A (width 90-119 cols)

S-A · Wide Ellipse appears as a centered section between the status bar and the main content. All existing content remains full width.

```
┌──────────────────────────────────────────────────────────────────────────┐
│ ━━╋━━ QUASAR ━━╋━━  setup  ◆ $1.24 ▓▓▓▓░░ $5.00  2/5  03:12          │
├──────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│            ..·::··::····::··::·..                                        │
│        .::··::··:.\|/.··./|\.::··::··::.                                │
│     .::··::··::·.·\|/.··./|\·.::··::··::.                              │
│   .::··::··::·.·---@---··---@---·.::··::··::.                          │
│    ::··::··::··::.·./|\.··.\|\.·.::··::··::··::                        │
│     .::··::··::·.·.|.·.··.|.·.::··::··::.                              │
│        .::··::··::··::····::··::··::··::.                                │
│            ..·::··::····::··::·..                                        │
│                                                                          │
│                Q    U    A    S    A    R                                │
│                                                                          │
├──────────────────────────────────────────────────────────────────────────┤
│ ▸ Cycle 1                                                               │
│   ├── ✓ coder   1.2s  $0.0031                                          │
│   └── ✓ reviewer 0.8s $0.0012                                          │
│   Cycle 2                                                               │
│   ├── ◆ coder   working… 3s  ⠋                                        │
│   └── · reviewer                                                        │
├──────────────────────────────────────────────────────────────────────────┤
│ [j/k navigate] [⏎ drill] [esc back] [q quit]                           │
└──────────────────────────────────────────────────────────────────────────┘
```

Implementation: insert `m.Banner.View()` as a section after the status bar, before the breadcrumb.

### Layout C: Top Banner — XS-A (width 60-89 cols)

Same as Layout B but with the smaller XS-A Pill art. Saves vertical space on narrow terminals.

```
┌──────────────────────────────────────────────────────┐
│ ━━╋━━ QUASAR ━━╋━━  setup ◆  $1.24  2/5  03:12     │
├──────────────────────────────────────────────────────┤
│                                                      │
│       .·::··::···::··::·.                            │
│    .::\|/::··::··::\|/::·:.                          │
│   ::---@---::····::---@---::                         │
│    .::/|\ ::··::··::/|\::·:.                         │
│       .·::··::···::··::·.                            │
│       ···· Q U A S A R ····                          │
│                                                      │
├──────────────────────────────────────────────────────┤
│ ▸ Cycle 1                                           │
│   ├── ✓ coder   1.2s                                │
│   └── ✓ reviewer 0.8s                               │
├──────────────────────────────────────────────────────┤
│ [j/k] [⏎] [esc] [q]                                │
└──────────────────────────────────────────────────────┘
```

### Layout D: No Art (width < 60 cols)

Existing compact layout, completely unchanged. `m.Banner.View()` returns `""`.

### Add `Banner` field to `AppModel`

In `NewAppModel()`, initialize `Banner`. On `tea.WindowSizeMsg`, update `m.Banner.Width` and `m.Banner.Height`.

### Add breakpoint constant to `layout.go`

```go
// SidePanelWidth is the minimum terminal width to show the side panel art.
SidePanelMinWidth = 120
// BannerSMinWidth is the minimum width for the S-A Wide Ellipse top banner.
BannerSMinWidth = 90
```

## Files to Modify

- `internal/tui/model.go` — Add `Banner` field, modify `View()` to assemble the three layouts, adjust sub-view widths in side panel mode
- `internal/tui/layout.go` — Add `SidePanelMinWidth`, `BannerSMinWidth` constants

## What NOT to Change

- `StatusBar.View()` — completely untouched
- `Footer` — completely untouched
- `renderMainView()` — untouched (just gets a narrower `Width` in side panel mode)
- `Detail`, `Gate`, `Toasts`, `CompletionOverlay`, `ArchitectOverlay` — all untouched
- All key handlers, message types, update logic — untouched

## Acceptance Criteria

- [ ] Width >= 120: S-B art in left side panel, all content to the right
- [ ] Width 90-119: S-A art as top banner between status bar and content
- [ ] Width 60-89: XS-A art as top banner
- [ ] Width < 60: no art, existing compact layout unchanged
- [ ] Status bar and footer always span full terminal width regardless of layout mode
- [ ] Detail panel, gate overlay, toasts render correctly in all modes
- [ ] Breadcrumb renders correctly with reduced width in side panel mode
- [ ] Window resize switches between layouts smoothly
- [ ] `go build` passes
- [ ] `go test ./internal/tui/...` passes
