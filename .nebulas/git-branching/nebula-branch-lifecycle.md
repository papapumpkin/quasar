+++
id = "nebula-branch-lifecycle"
title = "Wire BranchManager into nebula apply and enforce branch during execution"
type = "feature"
priority = 1
depends_on = ["branch-manager"]
+++

## Problem

Even with a `BranchManager` type, the nebula apply flow doesn't use it. Workers and their spawned loops can still commit to the wrong branch. Branch management needs to be integrated at two points:

1. **Startup**: create/checkout the nebula branch before any worker runs.
2. **Execution**: verify the branch before every commit (both cycle-level and phase-level).

## Solution

### 1. Branch creation at nebula apply startup

In `cmd/nebula_apply.go`, after the plan is applied and before workers start (`wg.Run`), create and checkout the nebula branch:

```go
branchMgr, err := nebula.NewBranchManager(ctx, workDir, n.Manifest.Nebula.Name)
if err != nil {
    // Log warning, don't fail — might not be a git repo
}
if branchMgr != nil {
    if err := branchMgr.CreateOrCheckout(ctx); err != nil {
        return fmt.Errorf("failed to create nebula branch: %w", err)
    }
}
```

This goes just before the `useTUI` branch (line ~131), after `git := loop.NewCycleCommitter(ctx, workDir)`.

### 2. Branch enforcement in the committers

**Phase-level commits** (`internal/nebula/git.go` — `gitCommitter`):

Add a `branch string` field to `gitCommitter`. Before committing in `CommitPhase`, call `EnsureBranch`:

```go
type gitCommitter struct {
    dir    string
    branch string // expected branch; empty = no enforcement
}
```

In `CommitPhase`, before `git add`:
```go
if g.branch != "" {
    currentBranch := getCurrentBranch(ctx, g.dir)
    if currentBranch != g.branch {
        return fmt.Errorf("branch mismatch: expected %q, on %q", g.branch, currentBranch)
    }
}
```

Update `NewGitCommitter` to accept the branch name (or add a `WithBranch` option).

**Cycle-level commits** (`internal/loop/git.go` — `gitCycleCommitter`):

Same pattern — add a `branch string` field. Before committing in `CommitCycle`, verify the branch.

Update `NewCycleCommitter` to accept the branch name (or pass it from the nebula apply setup). For standalone `quasar run`, pass `""` (no enforcement).

### 3. Wiring through the adapters

In `cmd/nebula_apply.go`, when constructing the `CycleCommitter` and `GitCommitter`, pass the branch name:

```go
branchName := ""
if branchMgr != nil {
    branchName = branchMgr.Branch()
}
git := loop.NewCycleCommitterWithBranch(ctx, workDir, branchName)
// ... later ...
wg.Committer = nebula.NewGitCommitterWithBranch(ctx, workDir, branchName)
```

The existing `NewCycleCommitter` (no branch) remains for backward compatibility with `quasar run`.

### 4. WorkerGroup receives committer

The `WorkerGroup` already has a `Committer` field. Currently it's set... let me check. Looking at the code, `wg.Committer` is set somewhere in `nebula_apply.go` or defaults. We need to make sure it's set with the branch-aware committer.

Actually, looking at the existing code, `NewWorkerGroup` or the option functions set `wg.Committer`. Ensure the committer is created with the branch name and passed via an option.

## Files

- `cmd/nebula_apply.go` — add branch creation at startup, pass branch to committers
- `internal/nebula/git.go` — add branch field to `gitCommitter`, verify before commit
- `internal/loop/git.go` — add branch field to `gitCycleCommitter`, verify before commit
- `cmd/nebula_adapters.go` — ensure branch-aware git is threaded to adapters

## Acceptance Criteria

- [ ] Running `nebula apply --auto` creates a `nebula/<name>` branch (or checks it out if it already exists)
- [ ] Phase commits (`gitCommitter.CommitPhase`) verify the current branch before committing
- [ ] Cycle commits (`gitCycleCommitter.CommitCycle`) verify the current branch before committing
- [ ] Standalone `quasar run` is unaffected (no branch enforcement when branch is empty)
- [ ] `go build -o quasar .` succeeds
- [ ] `go vet ./...` passes