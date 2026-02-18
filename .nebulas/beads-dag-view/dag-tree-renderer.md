+++
id = "dag-tree-renderer"
title = "Rewrite BeadView as a compact DAG/tree with inline titles"
type = "feature"
priority = 1
+++

## Problem

The current `BeadView.View()` in `internal/tui/beadview.go` renders beads as cycle-grouped rows of status icons (●/◎/✓) without showing issue titles inline. This produces an opaque view — you see colored dots but have to drill into the detail panel to understand what each bead represents. The plan view (`i`) already provides a text summary, so the beads view should focus on being a scannable issue tracker, not a second wall of text.

## Solution

Rewrite `BeadView.View()` to render a tree/DAG layout where each child bead shows its title and status on its own line. Keep the progress header (title + `[N/M resolved]` + progress bar) at the top, then render children as a tree with connector characters.

Target rendering:

```
  Fix login bug  [3/5 resolved]
  ████████████████░░░░░░░░░░░░░░░░

  ├─ ✓ Validate input sanitization
  ├─ ✓ Add error boundary
  ├─ ◎ Update unit tests
  ├─ ● Fix edge case in parser
  └─ ● Add integration test
```

Children are ordered by cycle number (ascending), then by their position within the cycle. Titles are truncated to fit the available width minus the tree prefix. No cycle sub-headers — the tree ordering itself conveys progression.

### Changes

**`internal/tui/beadview.go`**:
- Remove `cycleGroup`, `groupByCycle`, and `renderCompactCycle` — they are no longer needed.
- Rewrite `View()` to:
  1. Render the progress header (title + fraction + bar) exactly as today.
  2. Sort `v.Root.Children` by `(Cycle, index)` — stable sort so within-cycle order is preserved.
  3. Iterate children, rendering each as `  {connector} {icon} {title}` where connector is `├─` for all but the last and `└─` for the last.
  4. Truncate titles to `v.Width - prefixLen` using the existing `TruncateWithEllipsis` helper from `layout.go`.
- Keep `beadStatusIcon` as-is (it maps status → icon + style).

**`internal/tui/beadview_test.go`**:
- Update tests to assert tree-line output (`├─`, `└─`) with inline titles instead of cycle headers.
- Add a test for title truncation at narrow widths.
- Add a test verifying cycle-based ordering (cycle 1 children before cycle 2).

## Files

- `internal/tui/beadview.go` — rewrite `View()`, remove cycle-group helpers
- `internal/tui/beadview_test.go` — update all existing tests, add truncation + ordering tests

## Acceptance Criteria

- [ ] Each child bead line shows `{connector} {icon} {title}`
- [ ] Children are ordered by ascending cycle number
- [ ] Tree connectors (`├─`/`└─`) are rendered correctly
- [ ] Titles truncate with ellipsis when wider than available width
- [ ] Progress header (title, fraction, bar) is preserved
- [ ] `(no bead data yet)` and `(no child issues)` empty states are preserved
- [ ] All existing and new tests pass