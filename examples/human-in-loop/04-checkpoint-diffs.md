+++
id = "checkpoint-diffs"
title = "Generate structured checkpoint diffs at phase boundaries"
type = "feature"
priority = 2
depends_on = ["git-commit-phase-boundaries"]
+++

## Problem

When a nebula pauses for human review, the human has no structured summary of what changed. They must manually run `git diff` and parse raw output. There is no connection between the diff and the phase context (which phase, how many review cycles, cost, reviewer summary).

## Solution

Create a `Checkpoint` struct that captures a structured snapshot of what a phase produced, and a renderer that formats it for the terminal.

### Checkpoint Struct

```go
// Checkpoint captures the outcome of a completed phase for human review.
type Checkpoint struct {
    PhaseID       string
    PhaseTitle    string
    NebulaName    string
    Status        PhaseStatus
    ReviewCycles  int
    CostUSD       float64
    ReviewSummary string       // From ReviewReport.Summary
    Diff          string       // Output of git diff (the phase's commit vs prior)
    FilesChanged  []FileChange // Parsed summary of changed files
}

type FileChange struct {
    Path      string
    Operation string // "added", "modified", "deleted"
    LinesAdded   int
    LinesRemoved int
}
```

### Building Checkpoints

Create `BuildCheckpoint(ctx context.Context, git GitCommitter, phaseID string, result PhaseRunnerResult, nebula *Nebula) (*Checkpoint, error)`:

- Uses `git diff HEAD~1..HEAD` to get the diff for the most recent commit (the phase's commit)
- Parses the diff to extract `FileChange` entries (use `git diff --stat` for the summary)
- Populates review info from `PhaseRunnerResult`

### Rendering

Create `RenderCheckpoint(w io.Writer, cp *Checkpoint)` that outputs a formatted block to stderr:

```
── Phase: test-script-action ──────────────────────
   Status:  done (2 review cycles, $0.12)
   Files:
     + scripts/test.sh                    (15 lines)
     + .github/actions/test/action.yml    (22 lines)
   Reviewer: "Clean implementation, follows POSIX conventions"
───────────────────────────────────────────────────
```

Use ANSI colors via `ui.Printer` patterns (green for added, red for deleted, yellow for modified).

## Files to Create

- `internal/nebula/checkpoint.go` — `Checkpoint`, `FileChange` types, `BuildCheckpoint`, `RenderCheckpoint`
- `internal/nebula/checkpoint_test.go` — Tests for parsing and rendering

## Files to Modify

- `internal/nebula/worker.go` — Build checkpoint after phase completion (before gate prompt)

## Acceptance Criteria

- [ ] `Checkpoint` struct captures phase outcome, diff, and file change summary
- [ ] `BuildCheckpoint` uses `GitCommitter.Diff` or `git diff HEAD~1..HEAD`
- [ ] `FileChange` entries parsed from git diff stat output
- [ ] `RenderCheckpoint` outputs formatted block to an `io.Writer`
- [ ] Output uses ANSI colors consistent with `ui.Printer`
- [ ] Tests verify checkpoint building from mock git output
- [ ] Tests verify rendered output format
- [ ] `go test ./internal/nebula/...` passes