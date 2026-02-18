+++
id = "quit-confirmation"
title = "Add quit confirmation prompt for in-progress nebulas"
type = "feature"
priority = 2
+++

## Problem

Pressing 'q' immediately exits the TUI with no confirmation, even when a nebula is actively running with in-progress phases. This makes it too easy to accidentally abort a long-running nebula. The user wants a confirmation prompt before quitting.

## Current State

`internal/tui/model.go`:
- 'q' key and Ctrl+C trigger immediate exit (via `tea.Quit`)
- No confirmation overlay or intermediate state
- The existing completion overlay (`overlay.go`) shows results when a task finishes, but there's no "are you sure?" equivalent

`internal/tui/keys.go`:
- 'q' is bound at the top level (lines ~65-68)
- Ctrl+C also exits

## Solution

### 1. Add Quit Confirmation State

Add a `showQuitConfirm bool` field to the model. When 'q' is pressed and any phase has status `in_progress` or `working`:
- Set `showQuitConfirm = true`
- Show a confirmation overlay instead of immediately quitting

### 2. Confirmation Overlay

Render a centered overlay (similar to the existing completion overlay pattern):

```
  ┌─────────────────────────────────────┐
  │                                     │
  │   Are you sure you want to exit?    │
  │   Nebula has in-progress phases.    │
  │                                     │
  │   [y] Yes, exit    [n] Continue     │
  │                                     │
  └─────────────────────────────────────┘
```

- Border color: `colorAccent` (orange) — warning tone
- 'y' or 'Y': proceed with `tea.Quit`
- 'n', 'N', or Esc: dismiss the overlay and continue
- Ctrl+C should also confirm quit (force exit)

### 3. Skip Confirmation When Safe

If no phases are in-progress (all done, failed, or waiting), pressing 'q' should exit immediately without the confirmation prompt — the overlay is only needed when work would be interrupted.

## Files to Modify

- `internal/tui/model.go` — Add `showQuitConfirm` field; modify 'q' key handler to check for in-progress phases; handle y/n/Esc in the confirmation state
- `internal/tui/overlay.go` or new `internal/tui/quitconfirm.go` — Render the confirmation overlay
- `internal/tui/styles.go` — Add style for the quit confirmation overlay if needed (may reuse existing overlay styles)

## Acceptance Criteria

- [ ] Pressing 'q' during an in-progress nebula shows a confirmation overlay
- [ ] Pressing 'y' in the overlay exits the TUI
- [ ] Pressing 'n' or Esc in the overlay dismisses it and continues
- [ ] Pressing 'q' when no phases are in-progress exits immediately (no overlay)
- [ ] Ctrl+C always exits (force quit, even from confirmation overlay)
- [ ] `go build` and `go test ./internal/tui/...` pass