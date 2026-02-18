+++
id = "beads-dag-fix"
title = "Fix beads view to render DAG tree instead of wall of text"
type = "bug"
priority = 1
+++

## Problem

When pressing 'b' to open the beads view, the user sees a **wall of text** — some blue, some white — instead of the structured DAG tree with progress bar that the code is designed to render. The code in `beadview.go` creates a proper tree structure with `├─` / `└─` connectors, status icons (`●` open, `◎` in-progress, `✓` closed), and a progress bar, but this is not what actually displays.

## Current State

`internal/tui/beadview.go`:
- `BeadView` struct with `View()` method that renders a tree
- Parent task title at top with progress bar (`█` filled, `░` empty, `[N/M resolved]`)
- Tree connectors: `├─` and `└─` for child beads
- Status icons: `●` (white, open), `◎` (blue, in-progress), `✓` (green, closed)
- Styles: `styleBeadOpen`, `styleBeadInProgress`, `styleBeadClosed`, `styleBeadTitle`

`internal/tui/model.go`:
- 'b' key toggles bead view in the detail panel
- Bead data comes from `beads.Client` interface

Possible causes of the wall-of-text bug:
1. The bead data being passed to `BeadView` may be raw/unstructured instead of the expected tree format
2. The `View()` method may be receiving a flat list and rendering each item as a full line without tree structure
3. The bead data may include full descriptions/comments rather than just titles, causing verbose output
4. There may be an error in how the model populates the `BeadView` — it might be showing raw CLI output instead of parsed beads

## Solution

### 1. Investigate the Data Pipeline

Trace the flow from the 'b' key press through to rendering:
- How does `model.go` populate `BeadView` with bead data?
- What does `beads.Client` return — structured data or raw text?
- Is the `BeadView.View()` method actually being called, or is some fallback raw-text rendering happening instead?

### 2. Fix the Rendering

Depending on root cause:
- If raw CLI output is shown instead of parsed data: fix the parser to extract structured bead info
- If flat list is rendered without tree structure: fix the tree-building logic
- If descriptions/comments are included: filter to show only title, status, and ID per bead
- If `View()` is not being called: fix the model routing to use `BeadView.View()` for the 'b' key

### 3. Verify Tree Output

The expected output should look like:
```
  Fix authentication bug  [2/5 resolved]
  ████░░░░░░░

  ├─ ✓ Missing session validation
  ├─ ✓ Token expiry edge case
  ├─ ● Race condition in refresh flow
  ├─ ● Login redirect breaks on subdomain
  └─ ● Session cleanup doesn't fire
```

## Files to Modify

- `internal/tui/beadview.go` — Fix rendering to produce structured DAG tree output
- `internal/tui/model.go` — Fix the data pipeline that feeds bead data to `BeadView`
- Possibly `internal/beads/` — If the bead client returns unstructured data that needs parsing

## Acceptance Criteria

- [ ] Pressing 'b' shows a structured tree with connectors (`├─` / `└─`), not a wall of text
- [ ] Each bead shows: status icon + title (not full description or raw output)
- [ ] Progress bar at the top shows `[N/M resolved]` count
- [ ] Status icons are color-coded: green for closed, blue for in-progress, white for open
- [ ] `go build` and `go test ./internal/tui/...` pass