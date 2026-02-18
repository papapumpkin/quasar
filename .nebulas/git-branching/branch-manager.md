+++
id = "branch-manager"
title = "Create BranchManager for nebula-scoped git branches"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Nebulas currently commit directly to whatever branch happens to be checked out. There is no isolation between nebula work and the main branch, and no mechanism to enforce that all workers operate on the same branch.

## Solution

Create a `BranchManager` type in `internal/nebula/branch.go` that encapsulates git branch operations for nebula-scoped branching.

### Type and Interface

```go
// BranchManager manages git branches for nebula execution.
type BranchManager struct {
    dir    string // working directory
    branch string // the target branch name (e.g., "nebula/statusbar-regression")
}
```

### Methods

- `NewBranchManager(ctx context.Context, dir, nebulaName string) (*BranchManager, error)` — creates manager with branch name `nebula/<nebulaName>`. Returns error if not in a git repo or git unavailable. Does NOT create/checkout the branch yet.

- `CreateOrCheckout(ctx context.Context) error` — if the branch exists, check it out. If not, create it from the current HEAD and check it out. This handles both fresh starts and resumptions.

- `EnsureBranch(ctx context.Context) error` — verifies the current branch matches the expected branch name. Returns a descriptive error if on the wrong branch. This is the enforcement mechanism.

- `Branch() string` — returns the target branch name (e.g., `nebula/statusbar-regression`).

### Implementation Notes

- Branch name format: `nebula/<nebulaName>` (e.g., `nebula/statusbar-regression`).
- Use `git rev-parse --abbrev-ref HEAD` to check current branch.
- Use `git branch --list <name>` to check if branch exists.
- Use `git checkout -b <name>` to create, `git checkout <name>` to switch.
- Follow the nil-safe pattern from `gitCycleCommitter` — methods on nil receiver are no-ops.
- All methods accept `context.Context` and use `exec.CommandContext`.

### Tests

Write tests in `internal/nebula/branch_test.go` using a temporary git repo (created with `git init` in `t.TempDir()`). Test:
- Creating a branch on a fresh repo
- Checking out an existing branch (resume case)
- `EnsureBranch` succeeds when on correct branch
- `EnsureBranch` fails when on wrong branch

## Files

- `internal/nebula/branch.go` — new file with `BranchManager` type
- `internal/nebula/branch_test.go` — new file with tests

## Acceptance Criteria

- [ ] `BranchManager` type exists with `CreateOrCheckout`, `EnsureBranch`, and `Branch` methods
- [ ] `CreateOrCheckout` creates a new branch if it doesn't exist
- [ ] `CreateOrCheckout` checks out an existing branch on resume
- [ ] `EnsureBranch` returns nil when on the correct branch
- [ ] `EnsureBranch` returns an error when on the wrong branch
- [ ] All tests pass: `go test ./internal/nebula/...`
- [ ] `go vet ./internal/nebula/...` passes