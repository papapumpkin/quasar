+++
id = "cycle-committer"
title = "CycleCommitter interface and git implementation"
type = "feature"
priority = 1
scope = ["internal/loop/git.go"]
+++

## Problem

There's no mechanism to create git commits at coder-cycle boundaries. Without per-cycle commits, we have no clean git refs to pass to `git difftool <base>..<head> -- <file>`.

## Solution

Create a new file `internal/loop/git.go` with:

### Interface (consumed by loop)

```go
// CycleCommitter creates git commits at coder-cycle boundaries.
type CycleCommitter interface {
    CommitCycle(ctx context.Context, label string, cycle int) (sha string, err error)
    HeadSHA(ctx context.Context) (string, error)
}
```

### Implementation

A `gitCycleCommitter` struct that shells out to git:

- `CommitCycle`: runs `git add -A` then `git commit --allow-empty -m "quasar: <label> cycle-<N>"`, then `git rev-parse HEAD` to return the SHA. If nothing to commit (clean tree), still return current HEAD SHA.
- `HeadSHA`: runs `git rev-parse HEAD`.
- All commands use `exec.CommandContext(ctx, ...)`.

### Factory

```go
// NewCycleCommitter returns a CycleCommitter if the working directory is a git repo, or nil otherwise.
func NewCycleCommitter(dir string) CycleCommitter
```

Follow the nil-safe pattern from `nebula.NewGitCommitter` — detect git repo via `git rev-parse --git-dir`, return nil if not a git repo. All methods on a nil receiver should be no-ops.

### Tests

Add `internal/loop/git_test.go` with tests using a temp git repo:
- Test `CommitCycle` creates a commit with the expected message format
- Test `HeadSHA` returns valid SHA
- Test factory returns nil for non-git directory
- Test nil receiver safety

## Files to Create/Modify

- `internal/loop/git.go` — **NEW**: interface + implementation + factory
- `internal/loop/git_test.go` — **NEW**: tests

## Acceptance Criteria

- [ ] `CycleCommitter` interface defined with `CommitCycle` and `HeadSHA`
- [ ] `gitCycleCommitter` impl shells out to git correctly
- [ ] `NewCycleCommitter` returns nil for non-git dirs
- [ ] Nil receiver methods are no-ops (no panics)
- [ ] Tests pass: `go test ./internal/loop/...`
- [ ] `go build` passes
