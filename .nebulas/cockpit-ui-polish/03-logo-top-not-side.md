+++
id = "logo-top-not-side"
title = "Move logo from side panel to top banner to stop truncating phase names"
type = "task"
priority = 1
depends_on = []
labels = ["quasar", "tui"]
scope = ["internal/tui/banner.go", "internal/tui/layout.go"]
allow_scope_overlap = true
+++

## Problem

When the terminal is >= 120 columns wide (`SidePanelMinWidth`), the banner renders in `BannerSB` mode — a 48-column side panel containing the tall S-B quasar art (44 cols art + 2 padding each side). This permanently consumes 48 columns from the content area, leaving only `width - 48` for phase names, board columns, and detail panels.

On a typical 120-col terminal, the content area is only 72 columns. In board mode with multiple columns (Queued, Running, Review, Done, etc.), each column gets roughly 10-14 chars — far too narrow for phase titles like "Implement hero section with CTA buttons", which get severely truncated.

The side panel mode was designed for visual flair, but it directly harms usability by stealing nearly 40% of the horizontal space from the most important content.

## Solution

Remove the `BannerSB` side panel mode entirely. Instead, treat wide terminals the same as medium terminals — show the S-A Wide Ellipse as a top banner (or the XS-A Pill for less vertical overhead). This gives phase names and board columns the full terminal width.

### Changes

1. **`internal/tui/banner.go`** — Modify `Size()` to never return `BannerSB`. Wide terminals (`>= SidePanelMinWidth`) should fall through to `BannerS` instead:
   ```go
   func (b Banner) Size() BannerSize {
       switch {
       case b.Width >= BannerSMinWidth:
           return BannerS
       case b.Width >= CompactWidth:
           return BannerXS
       default:
           return BannerNone
       }
   }
   ```

   The `SidePanelView()` and `SidePanelWidth()` methods can remain but will now always return empty/zero since `Size()` never returns `BannerSB`. They become dead code that can be cleaned up later.

2. **`internal/tui/model.go`** — In `View()`, the side panel join logic (lines ~1937-1942) is now unreachable since `Size()` never returns `BannerSB`, but it can be left as-is (the guard `if m.Banner.Size() == BannerSB` will never be true). Optionally remove this block for clarity.

3. **`internal/tui/layout.go`** — The `SidePanelMinWidth` constant becomes unused. It can be removed or kept for documentation. If removed, also remove it from `banner.go`'s `Size()` switch.

### Visual Impact

- **Wide terminals (>= 120 cols)**: Get the S-A Wide Ellipse top banner (52 cols × 10 rows) centered at the top, plus the full terminal width for content below. Net gain: +48 columns for phase names.
- **Medium terminals (90-119 cols)**: Unchanged — already show S-A top banner.
- **Narrow terminals (60-89 cols)**: Unchanged — already show XS-A Pill top banner.
- **Very narrow (< 60 cols)**: Unchanged — no banner.

## Files

- `internal/tui/banner.go` — Change `Size()` to skip `BannerSB`, returning `BannerS` for wide terminals
- `internal/tui/model.go` — Optionally remove the now-dead side panel join block in `View()`
- `internal/tui/layout.go` — Optionally remove the `SidePanelMinWidth` constant

## Acceptance Criteria

- [ ] On a 120+ column terminal, the logo appears as a centered top banner (not a side panel)
- [ ] Phase names in board mode have the full terminal width available
- [ ] Phase names in table mode have the full terminal width available
- [ ] The banner still renders correctly on 90-119 col terminals (S-A variant)
- [ ] The banner still renders correctly on 60-89 col terminals (XS-A variant)
- [ ] No banner on terminals < 60 cols wide
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
