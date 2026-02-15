+++
id = "git-commit-phase-boundaries"
title = "Auto-commit at phase boundaries for clean diffs"
type = "feature"
priority = 1
depends_on = ["rename-task-to-phase"]
+++

## Problem

Long-running nebulae accumulate many file changes across phases. When a human reviewer is prompted, the working tree contains a messy mix of changes from the current phase and all prior phases. There is no clean baseline to `git diff` against.

## Solution

Introduce a `GitCommitter` that automatically creates a git commit after each phase completes successfully. This gives reviewers a clean diff — each phase's changes are isolated in their own commit, and `git diff HEAD` always shows only the current phase's delta.

### Interface

```go
// GitCommitter creates commits at phase boundaries.
type GitCommitter interface {
    // CommitPhase stages all changes and creates a commit for the completed phase.
    CommitPhase(ctx context.Context, nebulaName, phaseID string) error
    // Diff returns the diff of unstaged/staged changes since the last commit.
    Diff(ctx context.Context) (string, error)
}
```

### Implementation

Create a `gitCommitter` struct in a new file `internal/nebula/git.go`:

- `CommitPhase`: runs `git add -A && git commit -m "nebula(<nebula-name>): <phase-id>"` via `exec.CommandContext`
- `Diff`: runs `git diff HEAD` via `exec.CommandContext`, returns stdout
- If there are no changes to commit (clean working tree), `CommitPhase` is a no-op (not an error)
- Commit messages follow the pattern: `nebula(CI/CD Pipeline): test-script-action`

### Integration

- Add `GitCommitter` field to `WorkerGroup`
- Call `CommitPhase` in `executeTask` (now `executePhase`) after a phase completes with status `done`
- If the commit fails, log a warning but don't fail the phase — the work is done, only the commit boundary is missing
- `GitCommitter` can be nil (for tests or when git isn't available), in which case skip committing

### No-Git Environments

When `git` is not available or the working directory isn't a repo, the committer should detect this at construction time and return a nil committer (not an error). The `WorkerGroup` already handles nil `Watcher` this way.

## Files to Create

- `internal/nebula/git.go` — `GitCommitter` interface and `gitCommitter` implementation
- `internal/nebula/git_test.go` — Tests using a temp git repo

## Files to Modify

- `internal/nebula/worker.go` — Add `GitCommitter` field, call after phase completion

## Acceptance Criteria

- [ ] `GitCommitter` interface defined with `CommitPhase` and `Diff` methods
- [ ] Implementation uses `exec.CommandContext` for cancellation support
- [ ] Commit message format: `nebula(<name>): <phase-id>`
- [ ] Clean working tree is a no-op, not an error
- [ ] Non-git environments handled gracefully (nil committer)
- [ ] `WorkerGroup` calls `CommitPhase` after successful phase completion
- [ ] Failed commits logged as warnings, don't fail the phase
- [ ] Tests create a real temp git repo and verify commits are created
- [ ] `go test ./internal/nebula/...` passes