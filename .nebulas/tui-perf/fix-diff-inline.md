+++
id = "fix-diff-inline"
title = "Show inline per-file diff instead of launching external difftool"
type = "bug"
priority = 1
+++

## Problem

`openDiffTool()` in `internal/tui/model.go` uses `tea.ExecProcess` to launch
`git difftool --no-prompt`. This suspends the BubbleTea event loop. If the tool
exits instantly (no difftool configured, or exits with error), the TUI fails to
properly resume — causing a flash and freeze.

## Solution

Replace the external-tool launch with inline diff rendering. When the user
presses Enter on a file in the diff file list, parse the agent's raw unified
diff, extract the matching `FileDiff`, and render it in the detail panel using
the existing `renderFileDiff()` function.

Add a `RenderSingleFileDiff(raw, path, width)` helper to `diffview.go` that
parses a raw diff string and renders just the named file's diff.

## Files

- `internal/tui/model.go` — rewrite `openDiffTool()` to show inline diff
- `internal/tui/diffview.go` — add `RenderSingleFileDiff` helper

## Acceptance Criteria

- [ ] Pressing Enter on a file in the diff list shows that file's diff inline
- [ ] No external process is launched (no `tea.ExecProcess`)
- [ ] Pressing Esc or navigating away returns to the file list view
- [ ] `go build` and `go test ./internal/tui/...` pass
