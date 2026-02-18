+++
id = "diff-jk-scroll"
title = "j/k should scroll diff content instead of exiting the diff view"
type = "bug"
priority = 1
depends_on = []
+++

## Problem

When viewing a single file's diff (at `DepthAgentOutput` with `ShowDiff` active), pressing `j` or `k` navigates the diff file list instead of scrolling the diff content. This feels like the diff "exits" because the detail panel immediately switches to a different file's diff or summary.

The root cause is in `model.go:599-610`: when `m.ShowDiff && m.DiffFileList != nil`, j/k (`Keys.Up`/`Keys.Down`) are intercepted by the file list navigation handler before they can reach the detail panel scroll handler at lines 615-635. There is no distinction between "browsing the file list" vs "reading a file's diff."

Mouse/trackpad scrolling works because it targets the viewport directly, bypassing the key handler.

### Key binding conflict

`k` is also bound to `Keys.Skip` (`keys.go:82`), matching the same physical key as `Keys.Up`. While `Skip` is only used in gate mode, this dual binding could cause subtle matching issues with `key.Matches`.

## Solution

Track whether the user has "entered" a single file diff (e.g., after pressing Enter on a file in the file list). When in this "file diff reading" state:

1. Route `j`/`k` to `m.Detail.Update(msg)` to scroll the diff content
2. `Esc` returns to the file list (existing `drillUp` already handles dismissing `ShowDiff`)
3. Also route `PageUp`/`PageDown`/`Home`/`End` to the detail panel for fast navigation

One approach: add a `DiffFileOpen bool` field to `AppModel`. Set it `true` in `showFileDiff()`, clear it when `Esc` is pressed or when the file list is re-navigated. In the key handler, check `DiffFileOpen` before deciding whether j/k targets the file list or the detail panel.

## Files

- `internal/tui/model.go` — key handler logic (lines 597-660), `showFileDiff` method, `AppModel` struct
- `internal/tui/keys.go` — `Skip` binding overlapping with `Up` on `k`

## Acceptance Criteria

- [ ] After opening a file's diff (Enter on file list), j/k scroll the diff content
- [ ] Esc returns from single-file diff to the file list
- [ ] File list navigation still works with j/k when not viewing a single file's diff
- [ ] PageUp/PageDown/Home/End work for scrolling within a file's diff
- [ ] Mouse/trackpad scrolling continues to work
- [ ] `go build` and `go test ./internal/tui/...` pass