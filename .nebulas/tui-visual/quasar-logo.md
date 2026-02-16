+++
id = "quasar-logo"
title = "Design Quasar ASCII logo for TUI and README"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Quasar has no visual identity. The TUI status bar just shows "QUASAR" in plain styled text, and the README has no logo or banner. A distinctive ASCII art logo would make the TUI feel polished and give the project recognizable branding.

## Current State

The status bar in `internal/tui/statusbar.go` renders:
```
 QUASAR  nebula: TUI Polish  4/10 done  $1.24  5m36s
```
where "QUASAR" is rendered with `styleStatusLabel` (cyan bold foreground, no background). The README (`README.md`) has a plain `# Quasar` h1 heading with no visual branding.

Key files:
- `internal/tui/statusbar.go` — `StatusBar.View()` renders the top line
- `internal/tui/styles.go` — `styleStatusLabel` and color palette
- `README.md` — project documentation

## Solution

### 1. ASCII Logo Design

Design a compact ASCII art logo that evokes a quasar — a bright core with relativistic jets or radiating energy. It should:
- Be 1-3 lines tall (for TUI header, compactness matters)
- Work in monospace terminals at 80+ columns
- Use Unicode box-drawing or block characters if they improve the look
- Be visually distinctive but not overwhelming

Example directions (pick the best):
```
  ✦ QUASAR ✦
```
```
 ━━╋━━ QUASAR ━━╋━━
```
```
 ◈ Q U A S A R
```

The logo should feel "cosmic" — think radiating light, symmetry, energy.

### 2. TUI Integration

- Add a `Logo() string` function in a new `internal/tui/logo.go` file that returns the styled ASCII logo
- Use it in `StatusBar.View()` to replace the plain `styleStatusLabel.Render("QUASAR")` text
- The logo should use the primary color (`colorPrimary` / cyan) with possible accent highlights
- Must fit on a single status bar line (the status bar is 1 line tall)

### 3. README Integration

- Add the ASCII logo to the top of `README.md` as a code block or raw text
- Place it between the h1 heading and the description
- Keep it simple — the README version can be wider/taller than the TUI version

## Files to Create

- `internal/tui/logo.go` — `Logo() string` function returning styled logo text

## Files to Modify

- `internal/tui/statusbar.go` — Use `Logo()` instead of plain "QUASAR" text in `View()`
- `README.md` — Add ASCII logo banner near the top

## Acceptance Criteria

- [ ] `Logo()` returns a visually appealing single-line styled quasar logo
- [ ] Status bar uses the logo instead of plain "QUASAR" text
- [ ] README has the ASCII logo near the top
- [ ] Logo looks good on dark terminal backgrounds
- [ ] `go build` and `go test ./internal/tui/...` pass
