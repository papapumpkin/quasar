+++
id = "diff-messages"
title = "Extend diff messages and bridge with structured file data"
type = "feature"
priority = 1
depends_on = ["wire-committer"]
scope = ["internal/tui/msg.go", "internal/tui/bridge.go", "internal/tui/loopview.go"]
+++

## Problem

The TUI diff messages (`MsgAgentDiff`, `MsgPhaseAgentDiff`) currently carry only a raw diff string. To show a file list and launch `git difftool`, we need structured data: git refs, parsed file stats, and the working directory.

## Solution

### Extend diff messages

**File: `internal/tui/msg.go`** (around lines 131-143)

Add fields to `MsgAgentDiff`:
```go
BaseRef string          // git ref before this cycle
HeadRef string          // git ref after this cycle
Files   []FileStatEntry // pre-parsed file list
WorkDir string          // working directory for running git difftool
```

Add the same fields to `MsgPhaseAgentDiff`.

`FileStatEntry` already exists in `diffview.go` (around line 50-54) — reuse it.

### Update bridge to populate structured fields

**File: `internal/tui/bridge.go`** (around lines 143-159)

Modify `captureGitDiff()` (or add a parallel function) to:
- Accept `baseRef, headRef string` parameters instead of hardcoding `HEAD~1..HEAD`
- Run `git diff --stat <base>..<head>` and parse into `[]FileStatEntry`
- Reuse `ComputeDiffStat()` from `diffview.go` for parsing
- Return file list + refs alongside the raw diff string
- Fall back to existing `HEAD~1..HEAD` behavior when refs are empty

The bridge receives cycle state from the loop (via `MsgAgentDone` or similar). Thread the `BaseCommitSHA` and `CycleCommits` through to the bridge so it can compute `baseRef` (previous cycle's SHA or `BaseCommitSHA`) and `headRef` (current cycle's SHA).

### Store file list on AgentEntry

**File: `internal/tui/loopview.go`** (around lines 12-21)

Add to `AgentEntry`:
```go
DiffFiles []FileStatEntry
BaseRef   string
HeadRef   string
WorkDir   string
```

Add a `SetAgentDiffFiles()` method to `LoopView` that populates these fields on the relevant `AgentEntry`.

## Files to Modify

- `internal/tui/msg.go` — Extend `MsgAgentDiff` and `MsgPhaseAgentDiff` with refs/files/workdir
- `internal/tui/bridge.go` — Accept refs, parse file stats, populate new message fields
- `internal/tui/loopview.go` — Add `DiffFiles`/`BaseRef`/`HeadRef`/`WorkDir` to `AgentEntry`, add setter

## Acceptance Criteria

- [ ] `MsgAgentDiff` and `MsgPhaseAgentDiff` carry `BaseRef`, `HeadRef`, `Files`, `WorkDir`
- [ ] Bridge populates file stats from `git diff --stat`
- [ ] `AgentEntry` stores structured diff data
- [ ] Falls back gracefully when refs are empty
- [ ] `go build` passes
- [ ] `go test ./internal/tui/...` passes
