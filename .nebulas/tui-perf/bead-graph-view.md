+++
id = "bead-graph-view"
title = "Compact bead graph visualization"
type = "feature"
priority = 2
depends_on = []
+++

## Problem

The current `BeadView` (`internal/tui/beadview.go`) renders a verbose text tree
with IDs, titles, statuses, severities, and cycle groupings. This takes ~20+
lines for a typical task. Users want a high-level graph/pipeline view showing
work status at a glance without reading all the text.

## Solution

Redesign `BeadView.View()` to render a compact graph layout:

```
  auth-middleware                    [3/5 resolved]
  ████████░░░░

  Cycle 1  ✓✓✓     Cycle 2  ✓◎●     Cycle 3  ●●
```

Key design:
- Top line: task title + progress fraction
- Progress bar showing resolved/total ratio
- Per-cycle compact row: just status icons (✓◎●) with cycle label
- Color-coded: green=closed, blue=in_progress, white=open
- No bead IDs, no full titles, no severity text
- Total footprint: ~4-6 lines vs current ~20+ lines

## Files

- `internal/tui/beadview.go` — rewrite `View()` to render graph layout
- `internal/tui/beadview_test.go` — update tests for new rendering

## Acceptance Criteria

- [ ] Bead view shows compact graph with progress bar and status icons
- [ ] Empty state and root-only state render correctly
- [ ] All existing bead test scenarios pass with updated assertions
- [ ] `go build` and `go test ./internal/tui/...` pass
