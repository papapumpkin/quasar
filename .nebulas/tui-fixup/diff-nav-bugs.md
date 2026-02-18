+++
id = "diff-nav-bugs"
title = "Fix diff view navigation bugs: multi-file nav and state corruption"
type = "bug"
priority = 1
depends_on = []
+++

## Problem

Three related diff navigation bugs:

1. **d on cycle/phase corrupts UI**: Pressing `d` while focused on a cycle or phase node (rather than agent output) occasionally ruins the TUI state, leaving the view in an inconsistent rendering state.

2. **Multi-file diff navigation broken**: When opening a diff with many changed files, the user cannot navigate between files in the file list. Only the first file in the list is accessible.

3. **Navigation locks after exiting diff**: After opening a diff from the file list and then exiting it, j/k navigation in the main view stops responding. The nebula continues to run but the UI is unresponsive to movement keys.

## Solution

Investigate `internal/tui/model.go`, `internal/tui/diffview.go`, and `internal/tui/filelistview.go`:

1. **d on cycle/phase**: Guard `handleDiffKey()` so it only activates when the focused node actually has diff data. If the node has no diff content, `d` should be a no-op rather than toggling diff state.

2. **Multi-file navigation**: Check that the file list view's key handler properly passes j/k (or up/down) to the list model when `ShowDiff` is true and `DiffFileList` is active. The viewport may be capturing keys before the file list gets them.

3. **Navigation lock after diff exit**: When exiting the diff view (Escape/q), ensure all focus state is properly restored — the main list cursor, the active depth, and any captured key handlers must be released.

## Files

- `internal/tui/model.go` — key dispatch, state transitions
- `internal/tui/diffview.go` — diff rendering and key handling
- `internal/tui/filelistview.go` — file list navigation
- `internal/tui/keys.go` — key binding definitions

## Acceptance Criteria

- [ ] Pressing `d` on a cycle/phase with no diff data is a no-op
- [ ] Can navigate between all files in a multi-file diff
- [ ] Navigation works normally after exiting a diff view
- [ ] `go build` and `go test ./internal/tui/...` pass
