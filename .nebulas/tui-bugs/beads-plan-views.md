+++
id = "beads-plan-views"
title = "Fix beads/plan/diff view mutual exclusivity"
type = "bug"
priority = 1
depends_on = ["detail-refresh"]
+++

## Bug 5: Beads view shows wrong content

`handleInfoKey()` doesn't clear `ShowBeads` when toggling plan on, so both views can be "on" simultaneously, causing `updateDetailFromSelection()` to show beads (which takes precedence) instead of the plan.

## Bug 6: Plan view broken

Same mutual exclusivity issue — `handleInfoKey()` doesn't dismiss `ShowDiff`/`DiffFileList` either.

## Fix

1. In `handleInfoKey()`: When toggling plan ON, dismiss `ShowBeads`, `ShowDiff`, and `DiffFileList`.
2. In `handleDiffKey()`: When toggling diff ON, dismiss `ShowPlan` and `ShowBeads`.
3. `handleBeadsKey()` already dismisses `ShowPlan`/`ShowDiff` — verified correct.

## File

`internal/tui/model.go`
