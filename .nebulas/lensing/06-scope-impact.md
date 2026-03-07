+++
id = "scope-impact"
title = "File scope and impact visualization"
type = "feature"
priority = 2
depends_on = ["work-summary"]
+++

## Problem

When multiple agents work concurrently, developers need to understand the blast radius of changes. Which files are being touched by which phases? Are there overlapping modifications? The entanglement view handles contract-level conflicts, but there's no view showing the raw file-level picture of what's changing across the fleet.

## Solution

Add a file impact view that shows the aggregate footprint of all agent work.

1. **Impact tab on the cockpit.** Add a new cockpit tab (or sub-view within scratchpad) that lists all files touched across all active/completed phases. Group by phase, show change type (M/A/D) and diff stat per file. Highlight files touched by multiple phases in yellow/red.

2. **Scope overlay on phase detail.** When viewing a phase in the detail panel, show a "scope" section listing all files the phase has claimed or modified. Include the diff stat for each file.

3. **Cross-phase overlap warnings.** When two phases modify the same file, surface a warning indicator. This is lighter than a full entanglement — it's a quick "heads up, these phases are touching the same file" signal.

4. **Wire file claims from worker cards.** Worker cards already track `FileClaims` — aggregate these into a global file map on the `AppModel` and use it for the impact view.

## Files

- `internal/tui/impactview.go` — new view rendering aggregate file impact across phases
- `internal/tui/model.go` — add file impact map, wire to cockpit tab or scratchpad
- `internal/tui/tabs.go` — add impact tab to cockpit tab selector
- `internal/tui/detailpanel.go` — add scope section for phase file list
- `internal/tui/workercard.go` — expose `FileClaims` for aggregation

## Acceptance Criteria

- [ ] New cockpit tab or view shows all files touched across phases with change type and stat
- [ ] Files touched by multiple phases highlighted with overlap warning
- [ ] Phase detail panel includes scope section listing modified files
- [ ] File impact data derived from existing worker card claims and diff data
- [ ] View updates in real time as agents claim and modify files
