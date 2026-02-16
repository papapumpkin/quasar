+++
id = "color-theme"
title = "Rich color theme with adaptive palette and visual hierarchy"
type = "feature"
priority = 1
+++

## Problem

The current `styles.go` has a basic color palette with flat, uniform styling. The status bar, row highlights, and section borders all look similar. There's no visual hierarchy — everything is roughly the same visual weight. The new breadcrumb bar and per-phase drill-down views also need styling.

## Current State

The TUI now has a working three-level navigation hierarchy (phases → phase cycles → agent output) with breadcrumb rendering, per-phase `LoopView` tracking, and `PhaseUIBridge` for phase-contextualized messages. The existing styles in `styles.go` define basic named styles (`styleStatusBar`, `styleRowSelected`, `styleRowDone`, `styleRowFailed`, `styleRowWorking`, `styleRowWaiting`, `styleRowNormal`, `styleRowGate`, `styleFooter`, `styleFooterKey`, `styleFooterSep`, `styleSectionBorder`, `styleDetailTitle`, `styleDetailDim`) but they all use simple foreground colors with no backgrounds, borders, or visual weight differentiation.

## Solution

Redesign the Lip Gloss style system in `internal/tui/styles.go` for a polished, high-contrast terminal experience:

### Color Palette
- Use a cohesive palette with primary (cyan/blue), accent (gold/amber), success (green), danger (red), and muted (gray) tones
- Add `colorprofile` detection to gracefully degrade from TrueColor → 256-color → ANSI16 (lipgloss handles this, but verify the colors look good across profiles)
- Define semantic color variables: `colorPrimary`, `colorAccent`, `colorSuccess`, `colorDanger`, `colorMuted`, `colorSurface`, `colorSurfaceBright`

### Visual Hierarchy
- **Status bar**: Bold background (dark blue/indigo), white text, the most visually prominent element
- **Breadcrumb bar**: Subtle background tint, dimmer than status bar — shows navigation path like `phases › setup › output`
- **Section headers** (Cycle N, Phase table header): Subtle background tint or underline, not as heavy as status bar
- **Selected row**: Bright foreground + subtle left-border indicator (▎or ▌) instead of just bold
- **Active/working items**: Pulsing color via spinner (already have spinner, but add colored spinner dots)
- **Completed items**: Muted green with checkmark
- **Failed items**: Red with ✗
- **Dim/waiting items**: Gray, clearly de-emphasized

### Borders & Separators
- Use `lipgloss.RoundedBorder()` for the detail panel instead of `NormalBorder()`
- Add a thin horizontal rule between status bar and content (lipgloss border on bottom only)
- Footer should have a top border separating it from content

## Files to Modify

- `internal/tui/styles.go` — Complete redesign of style variables; add breadcrumb style, semantic color constants
- `internal/tui/statusbar.go` — Use new status bar styles with background color
- `internal/tui/loopview.go` — Use selection indicator (▎), colored checkmarks/fail marks
- `internal/tui/nebulaview.go` — Use selection indicator, status-colored icons for each `PhaseStatus`
- `internal/tui/detailpanel.go` — Rounded border, styled title
- `internal/tui/footer.go` — Top border, better key/desc contrast
- `internal/tui/model.go` — Use breadcrumb style in `renderBreadcrumb()`

## Acceptance Criteria

- [ ] Status bar is visually dominant with background color
- [ ] Breadcrumb bar has its own distinct style
- [ ] Selected rows have a visible left indicator (▎)
- [ ] Done/failed/working/waiting states are clearly distinguishable at a glance
- [ ] Detail panel has rounded border
- [ ] Footer has top separator
- [ ] Colors look acceptable on both dark and light terminal backgrounds
- [ ] `go build` and `go test ./internal/tui/...` pass
