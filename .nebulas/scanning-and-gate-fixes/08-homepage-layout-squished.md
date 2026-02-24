+++
id = "homepage-layout-squished"
title = "Fix homepage layout: logo cut off at top, everything squished"
type = "bug"
priority = 1
depends_on = []
labels = ["quasar", "tui"]
scope = ["internal/tui/model.go"]
allow_scope_overlap = true
+++

## Problem

The homepage (nebula selector) has two layout issues:

1. **Logo cut off at the top** — the banner art gets clipped because the total rendered height exceeds the terminal height. BubbleTea renders top-to-bottom, so overflow pushes the top of the output (including the banner) off the visible terminal area.

2. **Everything looks squished** — the nebula list and detail panel compete for limited vertical space after the banner, status bar, bottom bar, and footer all take their share.

### Root Cause

`homeMainHeight()` at `model.go:1803` undercounts the chrome:

```go
func (m AppModel) homeMainHeight() int {
    chrome := 3  // status bar (1) + spacing (1) + footer (1)
    if bv := m.Banner.View(); bv != "" {
        chrome += lipgloss.Height(bv)
    }
    if m.showDetailPanel() && m.Height >= DetailCollapseHeight {
        chrome++              // separator line
        chrome += m.detailHeight()
    }
    h := m.Height - chrome
    // ...
}
```

**Missing from the chrome count:**
- **BottomBar** (1 line) — `StatusBar.BottomBar()` always renders in home mode since `Width > 0`. This extra line is never subtracted from the available height.

**Compounding factors:**
- The S-A banner (`artSA`) is 11 lines tall. On a typical 40-50 line terminal, the banner alone consumes ~25% of vertical space.
- The detail panel takes 40% of remaining height (`mainH * 2 / 5` from `detailHeight()`), and is on by default in home mode (`m.ShowPlan = true` at line 152).
- Combined: status(1) + spacing(1) + banner(11) + bottom bar(1) + footer(1) + detail panel(~15) = ~30 lines of chrome, leaving only 10-20 lines for the actual nebula list on a 40-50 row terminal.

## Solution

### Changes

1. **`internal/tui/model.go`** — Fix `homeMainHeight()` to account for the bottom bar:
   ```go
   func (m AppModel) homeMainHeight() int {
       // Fixed chrome: status bar (1) + spacing (1) + bottom bar (1) + footer (1).
       chrome := 4
       // Banner.
       if bv := m.Banner.View(); bv != "" {
           chrome += lipgloss.Height(bv)
       }
       // Detail panel.
       if m.showDetailPanel() && m.Height >= DetailCollapseHeight {
           chrome++ // separator line
           chrome += m.detailHeight()
       }
       h := m.Height - chrome
       if h < 3 {
           h = 3
       }
       return h
   }
   ```

2. **`internal/tui/model.go`** — Suppress the banner on the homepage when the terminal is short (e.g., `Height < 35`), to give the nebula list more room. This can be done by checking height in `View()` before rendering the banner section, or by having `Banner.Size()` consider height as well as width.

   A simpler approach: skip banner rendering in `View()` when the available content area would be too cramped:
   ```go
   // Top banner — skip if terminal height is too short to avoid squishing content.
   if m.Height >= 35 {
       if bannerView := m.Banner.View(); bannerView != "" {
           sections = append(sections, bannerView)
       }
   }
   ```

3. **`internal/tui/model.go`** — Consider reducing `detailHeight()` for home mode or raising the `DetailCollapseHeight` threshold so the detail panel auto-hides on shorter terminals. Currently `DetailCollapseHeight = 20`, which is too low — the detail panel renders even at 20 rows where it would consume ~7 lines, leaving almost nothing for content.

## Files

- `internal/tui/model.go` — Fix `homeMainHeight()` chrome count, add height guard for banner, improve detail panel collapse threshold for home mode

## Acceptance Criteria

- [ ] The banner logo is fully visible on the homepage (not clipped at top)
- [ ] The nebula list is not squished — sufficient rows visible for browsing
- [ ] At terminal heights below ~35 rows, the banner gracefully hides to preserve content space
- [ ] The detail panel collapses at reasonable terminal heights instead of squishing the list
- [ ] The bottom bar is correctly accounted for in height calculations
- [ ] Layout looks correct at common terminal sizes (80x24, 120x40, 200x50)
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
