+++
id = "color-theme"
title = "Rich color theme with adaptive palette and visual hierarchy"
type = "feature"
priority = 1
+++

## Problem

The current `styles.go` has a basic color palette with flat, uniform styling. The status bar, row highlights, and section borders all look similar. There's no visual hierarchy — everything is roughly the same visual weight.

## Solution

Redesign the Lip Gloss style system in `internal/tui/styles.go` for a polished, high-contrast terminal experience:

### Color Palette
- Use a cohesive palette with primary (cyan/blue), accent (gold/amber), success (green), danger (red), and muted (gray) tones
- Add `colorprofile` detection to gracefully degrade from TrueColor → 256-color → ANSI16 (lipgloss handles this, but verify the colors look good across profiles)
- Define semantic color variables: `colorPrimary`, `colorAccent`, `colorSuccess`, `colorDanger`, `colorMuted`, `colorSurface`, `colorSurfaceBright`

### Visual Hierarchy
- **Status bar**: Bold background (dark blue/indigo), white text, the most visually prominent element
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

- `internal/tui/styles.go` — Complete redesign of style variables
- `internal/tui/statusbar.go` — Use new status bar styles
- `internal/tui/loopview.go` — Use selection indicator, colored checkmarks
- `internal/tui/nebulaview.go` — Use selection indicator, status-colored icons
- `internal/tui/detailpanel.go` — Rounded border, styled title
- `internal/tui/footer.go` — Top border, better key/desc contrast

## Acceptance Criteria

- [ ] Status bar is visually dominant with background color
- [ ] Selected rows have a visible left indicator (▎)
- [ ] Done/failed/working/waiting states are clearly distinguishable at a glance
- [ ] Detail panel has rounded border
- [ ] Footer has top separator
- [ ] Colors look acceptable on both dark and light terminal backgrounds
- [ ] `go build` and `go test ./internal/tui/...` pass
