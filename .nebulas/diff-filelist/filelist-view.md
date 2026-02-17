+++
id = "filelist-view"
title = "FileListView component and model integration"
type = "feature"
priority = 1
depends_on = ["diff-messages"]
scope = ["internal/tui/filelistview.go", "internal/tui/model.go"]
+++

## Problem

When the user presses `d` to view a diff, the TUI currently calls `RenderDiffView()` which re-parses and re-renders the entire side-by-side diff on every frame, killing performance for large diffs. We need a lightweight replacement.

## Solution

### New FileListView component

**File: `internal/tui/filelistview.go`** (new)

A simple navigable list of changed files, rendered as short styled strings:

```
  ▸ internal/tui/model.go       | +42 -15
    internal/tui/diffview.go    | +8  -3
    internal/loop/loop.go       | +20 -2

  ↵ open diff · d close
```

Struct:
```go
type FileListView struct {
    Files   []FileStatEntry
    Cursor  int
    Width   int
    BaseRef string
    HeadRef string
    WorkDir string
}
```

Methods:
- `View() string` — trivially cheap: just N short styled strings with cursor indicator
- `MoveUp()` — move cursor up, wrapping at top
- `MoveDown()` — move cursor down, wrapping at bottom
- `SelectedFile() FileStatEntry` — return the file at cursor position
- Render additions in green (`+N`), deletions in red (`-N`)
- Show `▸` indicator on selected row
- Show "(no changes)" when `Files` is empty

### Replace RenderDiffView calls in model.go

**File: `internal/tui/model.go`**

Add `DiffFileList *FileListView` field to `AppModel`.

Replace the hot path (around lines 1048-1051 and 1120-1123):
```go
// OLD:
body := RenderDiffView(agent.Diff, m.Width-4)
// NEW:
body := m.DiffFileList.View()
```

When `ShowDiff` is toggled on (around line 720):
- Create `m.DiffFileList` from `agent.DiffFiles`
- Populate `BaseRef`, `HeadRef`, `WorkDir` from the agent entry

When `ShowDiff` is toggled off:
- Set `m.DiffFileList = nil`

Route `j/k` (or `↑/↓`) keys to `m.DiffFileList.MoveUp()/MoveDown()` when `ShowDiff` is active.

### Fallback

If `agent.DiffFiles` is empty but `agent.Diff` is non-empty (no-git fallback), fall back to existing `RenderDiffView` for small diffs or show "(diff not available as file list)".

## Files to Create/Modify

- `internal/tui/filelistview.go` — **NEW**: `FileListView` struct + rendering
- `internal/tui/model.go` — Add `DiffFileList` field, replace `RenderDiffView` hot path, route keys

## Acceptance Criteria

- [ ] `FileListView` renders a navigable file list with cursor
- [ ] Additions shown in green, deletions in red
- [ ] Empty state shows "(no changes)"
- [ ] `model.go` uses `FileListView` instead of `RenderDiffView` when diff is shown
- [ ] Cursor navigation works (up/down/wrap)
- [ ] Falls back to old `RenderDiffView` when no structured file data is available
- [ ] `go build` passes
- [ ] `go test ./internal/tui/...` passes
