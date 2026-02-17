+++
id = "diff-state-fix"
title = "Fix diff view disappearing and j/k scroll lock after diff toggle"
type = "bug"
priority = 1
depends_on = ["detail-refresh"]
+++

## Bug 3: Diff view appears then disappears

When at `DepthAgentOutput` in loop mode, pressing Enter clears `ShowDiff`/`DiffFileList`/`ShowBeads`/`ShowPlan` unconditionally at the top of `drillDown()` BEFORE the depth check returns early. The diff state is wiped even though no navigation actually occurs.

## Bug 4: j/k scroll locks after diff

`handleDiffKey()` sets `ShowDiff=true` unconditionally, but `buildDiffFileList()` can return nil (no diff files available). This leaves `ShowDiff=true` with `DiffFileList=nil`, causing inconsistent state where the diff view is "on" but has no content.

## Fix

1. In `drillDown()`: Move the state-clearing block AFTER the depth/mode checks. At `DepthAgentOutput` in loop mode, return early WITHOUT clearing. For nebula mode, clear only when actually transitioning depths.

2. In `handleDiffKey()`: After `buildDiffFileList()`, if result is nil AND the selected agent has no raw diff text, reset `ShowDiff=false`.

## File

`internal/tui/model.go`
