+++
id = "fix-wrapping"
title = "Prevent status bar metrics from wrapping to a second line"
type = "bug"
priority = 1
depends_on = ["unify-bar-color"]
+++

## Problem

The right-side metrics (cost, resource indicators, elapsed time) wrap under the "QUASAR" logo/title instead of staying on a single line. The status bar's `View()` method has segment-drop logic (`dropSegments`) to prevent overflow, but it is not working correctly — the rendered output exceeds the terminal width and wraps.

Likely causes:

1. **Width accounting misses ANSI escape sequences**: `lipgloss.Width()` should handle this, but if any raw strings or partial renders sneak in, the calculated width will be wrong.

2. **The `styleStatusBar.Width(s.Width).Render(line)` wrapping**: When the outer `styleStatusBar` applies `Width(s.Width)`, lipgloss may add padding that pushes content past the terminal edge. The inner content is assembled assuming `s.Width` available characters, but the `.Padding(0, 1)` on `styleStatusBar` eats 2 characters that aren't accounted for in the gap calculation.

3. **Double-counting or under-counting**: The `View()` method subtracts `2` for "bar padding" (`availableForName` calculation at line 55), but if the outer `.Padding(0, 1)` plus the `.Width()` interaction doesn't match, content overflows.

## Solution

1. **Audit the width budget**: Ensure the inner content assembly accounts for exactly the same padding that `styleStatusBar` applies. The inner content should target `s.Width - 2` (for the 1-char left and right padding), not `s.Width`.

2. **Verify segment widths are measured after styling**: Each segment's `lipgloss.Width()` must be measured on the styled/rendered text, not on raw text. Check that `totalWidth()` and the gap calculation in `View()` are consistent.

3. **Add a safety clamp**: After assembling the final `line`, if `lipgloss.Width(line) > s.Width - 2`, truncate or drop additional segments. This prevents any edge-case wrapping.

4. **Test at various widths**: Add or update tests that verify `StatusBar.View()` never produces output wider than `s.Width` for widths from `MinWidth` down to narrow values.

## Files

- `internal/tui/statusbar.go` — `View()`, `dropSegments()`, `totalWidth()`, width budget logic
- `internal/tui/statusbar_test.go` — width-clamping tests

## Acceptance Criteria

- [ ] Status bar always renders as exactly one line, never wrapping
- [ ] `lipgloss.Width(statusBar.View()) <= s.Width` for all tested widths
- [ ] Segments are still dropped gracefully at narrow widths
- [ ] Existing status bar tests pass
- [ ] New test verifying single-line output at various widths
