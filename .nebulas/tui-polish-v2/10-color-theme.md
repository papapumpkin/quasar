+++
id = "color-theme"
title = "Posting-inspired rich color theme with bordered panels"
type = "task"
priority = 2
depends_on = ["split-pane-layout"]
+++

## Problem

The current TUI uses a minimal color palette (galactic theme: muted grays, accent amber, blueshift blue, success green, danger red). Panels don't have clear visual borders — the layout relies on vertical stacking and spacing. Posting uses rich saturated colors with clearly bordered/framed panels, making each section visually distinct and the overall UI feel polished.

## Solution

Update the style system to use richer colors and bordered panels, inspired by Posting's visual language.

### Approach

1. **Bordered panels**: Every major section gets a `lipgloss.RoundedBorder()` with a colored border:
   - Sidebar: subtle muted border (it's a navigation element, not a content focus)
   - Main content area: accent-colored top border with tab indicators
   - Detail panel: blueshift border (content is informational)
   - Worker cards: already have borders — update colors to be more vibrant

2. **Panel headers**: Each bordered panel gets a labeled header rendered inline with the top border:
   ```
   ┌─ Phases ─────────────────┐
   │ ▸ glamour-markdown       │
   │   unified-diff           │
   └──────────────────────────┘
   ```
   Use `lipgloss.Border` with title placement. If lipgloss doesn't support inline titles, render the title over the top border with background color matching.

3. **Color palette refresh**: Keep the galactic theme names but richen the values:
   - `colorPrimary`: warm white (`#c0caf5` → could try `#cdd6f4` Catppuccin-style)
   - `colorAccent`: keep amber but make it warmer
   - `colorBlueshift`: richer blue for active/focused elements
   - `colorSurface`: slightly lighter dark background for panels (distinguish from terminal bg)
   - Add `colorPanelBorder`: subtle border color that's visible but not distracting
   - Add `colorFocusedBorder`: brighter border color for the currently focused panel

4. **Focus indicators**: When a panel has keyboard focus (sidebar vs main vs detail), its border brightens. Unfocused panels use `colorPanelBorder` (muted). Focused panel uses `colorFocusedBorder` (bright). This is how Posting makes it clear where your keyboard input will go.

5. **Consistent spacing**: 1-char padding inside all bordered panels. 0 gap between panels (borders serve as separators). No naked content without a border context.

6. **Tab styling**: The cockpit tabs (Board, Graph, Entanglements, Scratchpad) should look like Posting's tabs — active tab has a colored underline or filled background, inactive tabs are muted. Currently they're rendered inline; update to use a more visual tab bar.

### Colors to keep:
- `colorDanger` (red) for failures/critical hails
- `colorSuccess` (green) for done/approved
- `colorStarYellow` for warnings
- These are semantic and shouldn't change

### Theming (future):
Don't implement a full theming system now, but structure the color definitions so they're easy to swap later (they already are in `styles.go` as package vars).

## Files

- `internal/tui/styles.go` — update color values, add panel border styles, focus styles
- `internal/tui/styles_test.go` — update any tests that check style output
- `internal/tui/tabs.go` — update tab bar rendering for richer visual style
- `internal/tui/detailpanel.go` — add border + header to detail panel
- `internal/tui/sidebar.go` — add border + header (from phase 08)
- `internal/tui/model.go` — pass focus state to panels for border color switching

## Acceptance Criteria

- [ ] All major panels have rounded borders with labeled headers
- [ ] Focused panel has a visually distinct (brighter) border
- [ ] Color palette is richer — panels are visually distinct from terminal background
- [ ] Tab bar uses active/inactive visual styling (underline or fill)
- [ ] Semantic colors (danger, success, warning) unchanged
- [ ] Worker cards, status bar, and footer updated for consistency
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
