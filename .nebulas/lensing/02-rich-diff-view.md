+++
id = "rich-diff-view"
title = "Enhanced diff rendering with inline annotations and stat bars"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

The current diff view (`diffview.go`) renders a basic unified diff with colored +/- lines. It works, but it's hard to quickly assess the shape and significance of changes. Developers scanning agent output want to rapidly answer: "what changed, how much, and does it look right?" Side-by-side rendering exists (`BuildSideBySidePairs`) but isn't fully wired. There's no visual summary bar, no hunk-level collapse, and no way to see which hunks are semantically important.

## Solution

Upgrade the diff rendering pipeline to be more information-dense and navigable:

1. **Diff stat bar per file.** Render a visual bar (e.g., `+++---` in green/red proportional to additions/deletions) next to each file in the file list view, similar to `git diff --stat`. Already have `FileStatEntry` — wire it to a rendered bar.

2. **Hunk headers with context.** Parse the `@@ ... @@` function/method context string and display it as a styled label above each hunk (e.g., `func (l *Loop) Run` in bold). This helps developers orient within the diff without reading every line.

3. **Collapsible hunks.** Allow toggling individual hunks open/closed with enter/space. Default: all open. This lets developers skip boilerplate hunks and focus on the interesting ones.

4. **Change-type indicators.** Annotate each file in the file list with a glyph: `M` modified, `A` added, `D` deleted, `R` renamed. Color-code them. Parse from the `diff --git` header.

5. **Side-by-side toggle.** Wire the existing `BuildSideBySidePairs` to a keybound toggle (`s` key) in the detail panel, switching between unified and side-by-side rendering.

## Files

- `internal/tui/diffview.go` — add stat bars, hunk context parsing, collapse state, side-by-side toggle
- `internal/tui/filelistview.go` — add stat bars and change-type glyphs per file
- `internal/tui/detailpanel.go` — support collapsible sections and side-by-side mode toggle
- `internal/tui/keys.go` — add keybinding for side-by-side toggle

## Acceptance Criteria

- [ ] File list shows visual stat bars (green/red proportional) and change-type glyphs (M/A/D/R)
- [ ] Hunk headers display the function/method context string when present
- [ ] Hunks can be collapsed/expanded individually via keyboard
- [ ] Side-by-side diff view toggleable with `s` key in detail panel
- [ ] Diff rendering remains fast for diffs with 50+ files
