+++
id = "unified-diff"
title = "Switch diff view to unified top/bottom layout with red/blue coloring"
type = "task"
priority = 1
depends_on = []
+++

## Problem

The current diff view uses a side-by-side layout (`BuildSideBySidePairs` + `renderSideLine`) which compresses each side to half the terminal width, making code hard to read. The colors are green (add) and red (remove) — functional but doesn't match the Claude Code convention of red (original/removed) and blue (changed/added).

## Solution

Replace the side-by-side diff rendering with a unified (top/bottom) layout where removed lines appear in red and added lines in blue, matching Claude Code's diff style.

### Approach

1. In `diffview.go`, add a new `RenderUnifiedDiffView` function (or modify `RenderDiffView`) that renders diffs in unified format:
   - File header: `── path/to/file.go ──────────`
   - Removed lines: red foreground, prefixed with `-`, showing old line number
   - Added lines: blue foreground, prefixed with `+`, showing new line number
   - Context lines: muted foreground, showing both line numbers
   - Each line gets the full terminal width (not split in half)

2. Update the color styles in `styles.go`:
   - `styleDiffRemove`: keep red foreground (`colorDanger`) — this is correct
   - `styleDiffAdd`: change from green (`colorSuccess`) to blue (`colorBlueshift` or a new `colorDiffAdd` if the existing blue doesn't look right)
   - Optionally add subtle background tinting (faint red bg for removes, faint blue bg for adds) if it looks good in practice

3. The unified layout format per hunk:
   ```
   @@ -10,5 +10,7 @@
     10 │  unchanged context line
   - 11 │  old line that was removed
   - 12 │  another removed line
   + 11 │  new replacement line
   + 12 │  another new line
   + 13 │  extra added line
     13 │  more context
   ```

4. Keep `BuildSideBySidePairs` and related code — they're used in tests and could be useful later. Just change the default rendering path.

5. The stat summary at the top (`renderDiffStat`) stays the same — just update its colors to match (blue for additions instead of green).

## Files

- `internal/tui/diffview.go` — add unified rendering, keep side-by-side code
- `internal/tui/styles.go` — update `styleDiffAdd` color to blue
- `internal/tui/diffview_test.go` — add tests for unified rendering

## Acceptance Criteria

- [ ] Diff renders in unified format (full width per line, not split)
- [ ] Removed lines are red with `-` prefix
- [ ] Added lines are blue with `+` prefix
- [ ] Context lines are muted
- [ ] Line numbers display correctly
- [ ] Stat summary colors updated (blue for additions)
- [ ] File headers are clear and full-width
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
