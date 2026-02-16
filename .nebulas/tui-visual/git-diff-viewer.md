+++
id = "git-diff-viewer"
title = "Side-by-side git diff viewer in detail panel"
type = "feature"
priority = 2
depends_on = ["phase-tree-polish"]
max_review_cycles = 5
max_budget_usd = 30.0
+++

## Problem

When reviewing what a coder agent changed, users currently see raw agent output text in the detail panel. There's no way to see the actual file changes (git diff) in a structured, readable format. Users want to understand *what changed* at a glance, and side-by-side diff is preferred over inline for this use case.

## Current State

**Detail panel** (`internal/tui/detailpanel.go`):
- Wraps a `viewport.Model` for scrollable content
- `SetContent(title, content string)` sets raw text content
- Used to display agent output when drilled into a cycle/agent
- No concept of structured content types — everything is plain text

**Agent output flow**:
- `MsgAgentOutput` / `MsgPhaseAgentOutput` carry raw output strings
- Stored in `AgentEntry.Output` in the `LoopView`
- Rendered as-is in the viewport

**Git integration**:
- The coder agent runs `claude -p` which makes git commits
- After each agent invocation, the working directory has new commits
- `git diff` / `git show` can retrieve the changes

## Solution

### 1. Capture Git Diffs After Agent Runs

Add diff capture to the bridge layer. After each coder agent completes:
- Run `git diff HEAD~1..HEAD --stat` and `git diff HEAD~1..HEAD` to capture what the coder changed
- Store the diff alongside the agent output
- Send a new message type `MsgAgentDiff` / `MsgPhaseAgentDiff` with the diff content

Implementation in `internal/tui/bridge.go`:
- Add `captureGitDiff()` helper that runs `git diff` via `exec.Command`
- Call it in `AgentDone()` for the coder role
- The bridge already has access to `tea.Program.Send()`

### 2. Side-by-Side Diff Rendering

Create a new `internal/tui/diffview.go` that parses unified diff format and renders side-by-side:

```
 ┌─ handler.go ───────────────────────────────────────────────────┐
 │  10  func Login(w http.ResponseWriter  │  10  func Login(w http.ResponseWriter  │
 │  11    // validate input               │  11    // validate input               │
 │  12 -  token := generateToken()        │  12 +  token, err := generateToken()   │
 │  13                                    │  13 +  if err != nil {                  │
 │                                        │  14 +    http.Error(w, "fail", 500)     │
 │                                        │  15 +    return                         │
 │                                        │  16 +  }                                │
 │  14    w.Header().Set(...)             │  17    w.Header().Set(...)             │
 └────────────────────────────────────────────────────────────────┘
```

Key design decisions:
- Parse unified diff hunks into left (old) and right (new) line pairs
- Removed lines: red background on left, blank on right
- Added lines: blank on left, green background on right
- Context lines: shown on both sides, muted
- File headers: styled with the file path
- Line numbers: muted gray, left-aligned in each column
- Column separator: thin vertical line in muted color

### 3. Detail Panel Mode Switching

Extend the detail panel to support two content modes:
- **Text mode** (current): raw agent output
- **Diff mode** (new): side-by-side diff view

Add a keybinding `d` to toggle between output and diff views when an agent row is selected. The footer should show "d:diff" when output is visible and "d:output" when diff is visible.

### 4. Diff Stats Summary

At the top of the diff view, show a compact summary:
```
  3 files changed, 24 insertions(+), 8 deletions(-)
  handler.go | 12 +++---
  auth.go    |  8 ++++
  test.go    |  4 ++--
```

This gives a quick overview before the user scrolls into the actual diffs.

## Files to Create

- `internal/tui/diffview.go` — `DiffView` struct with unified diff parser and side-by-side renderer
- `internal/tui/diffview_test.go` — Tests for diff parsing and rendering

## Files to Modify

- `internal/tui/msg.go` — Add `MsgAgentDiff` and `MsgPhaseAgentDiff` message types
- `internal/tui/bridge.go` — Add `captureGitDiff()` helper; call in `AgentDone()`
- `internal/tui/model.go` — Handle diff messages; add `d` key toggle; store diffs in model
- `internal/tui/keys.go` — Add `Diff` key binding (`d`)
- `internal/tui/footer.go` — Show "d:diff"/"d:output" toggle hint
- `internal/tui/styles.go` — Add diff-specific styles: `styleDiffAdd`, `styleDiffRemove`, `styleDiffContext`, `styleDiffHeader`, `styleDiffLineNum`
- `internal/tui/detailpanel.go` — Support rendering `DiffView` as an alternative to plain text

## Acceptance Criteria

- [ ] After a coder agent finishes, its git diff is automatically captured
- [ ] Pressing `d` on an agent row toggles between output and diff view
- [ ] Diff is rendered side-by-side with removed (red) on left, added (green) on right
- [ ] Context lines shown on both sides in muted style
- [ ] Diff stat summary shown at top of diff view
- [ ] File headers clearly delineate which file's changes follow
- [ ] Diff view is scrollable (uses viewport)
- [ ] Line numbers shown in muted gray
- [ ] Footer shows "d:diff" or "d:output" toggle hint
- [ ] `go build` and `go test ./internal/tui/...` pass
