+++
id = "external-difftool"
title = "Launch external difftool via tea.ExecProcess"
type = "feature"
priority = 1
depends_on = ["filelist-view"]
scope = ["internal/tui/model.go", "internal/tui/msg.go", "internal/tui/keys.go", "internal/tui/footer.go"]
+++

## Problem

Once the user sees the file list, they need a way to view the actual diff for a selected file. Rendering it in the TUI is the performance problem we're solving. Instead, delegate to the user's configured `git difftool`.

## Solution

### Open diff with tea.ExecProcess

**File: `internal/tui/model.go`**

When Enter is pressed while `DiffFileList` is active and a file is selected:

```go
file := m.DiffFileList.SelectedFile()
cmd := exec.Command("git", "difftool", "--no-prompt",
    m.DiffFileList.BaseRef+".."+m.DiffFileList.HeadRef,
    "--", file.Path)
cmd.Dir = m.DiffFileList.WorkDir
return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
    return MsgDiffToolDone{Err: err}
})
```

`tea.ExecProcess` suspends the TUI, runs the difftool (nvimdiff, VS Code, meld — whatever the user configured via `git config diff.tool`), and resumes the TUI when the tool exits. For GUI tools (VS Code, meld) it returns immediately since the process detaches.

### New message type

**File: `internal/tui/msg.go`**

```go
// MsgDiffToolDone is sent when an external difftool process exits.
type MsgDiffToolDone struct{ Err error }
```

Handle in `Update`: if `Err != nil`, show a brief error in the status bar or footer. If `Err == nil`, just resume normally.

### Update keybindings

**File: `internal/tui/keys.go`**

Add an `OpenDiff` key binding (Enter when in diff file list context). Make sure it doesn't conflict with existing Enter behavior (drill-down). The key should only be active when `ShowDiff` is true.

### Update footer

**File: `internal/tui/footer.go`**

When `ShowDiff` is active, update the footer to show:
```
[↑↓ navigate] [⏎ open diff] [d close]
```

### Edge cases

- If `BaseRef` or `HeadRef` are empty (no-git case), disable the Enter key and show "(external diff not available)" in the footer
- If `git difftool` is not configured, the command will fail — show the error from `MsgDiffToolDone`
- If `Files` is empty, Enter should be a no-op

## Files to Modify

- `internal/tui/model.go` — Handle Enter key in diff list, handle `MsgDiffToolDone`, launch `tea.ExecProcess`
- `internal/tui/msg.go` — Add `MsgDiffToolDone`
- `internal/tui/keys.go` — Add `OpenDiff` binding
- `internal/tui/footer.go` — Show diff-mode footer hints when `ShowDiff` is active

## Acceptance Criteria

- [ ] Enter on a selected file launches `git difftool --no-prompt <base>..<head> -- <file>`
- [ ] TUI suspends during difftool execution and resumes cleanly
- [ ] `MsgDiffToolDone` error is displayed to user
- [ ] Footer shows diff-mode keybindings when diff list is active
- [ ] Enter is disabled when no refs are available
- [ ] `go build` passes
- [ ] `go test ./internal/tui/...` passes
