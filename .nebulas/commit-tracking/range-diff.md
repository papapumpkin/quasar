+++
id = "range-diff"
title = "Add DiffRange method to GitCommitter for base..head diffs"
type = "feature"
priority = 2
depends_on = []
+++

## Problem

`nebula.GitCommitter` has `DiffLastCommit()` (`HEAD~1..HEAD`) and `Diff()` (`HEAD` vs working tree). Neither can compute a diff across an arbitrary commit range. When a phase spans multiple commits, the only way to see the full phase diff is `git diff <base>..<head>`, which no current method supports.

Similarly, `loop.CycleCommitter` has `HeadSHA()` and `CommitCycle()` but no way to produce a diff between two known SHAs — needed for showing "what changed in cycle N" (diff between cycle N-1's final SHA and cycle N's final SHA).

## Solution

1. Add `DiffRange(ctx context.Context, base, head string) (string, error)` to `nebula.GitCommitter`.

2. Add `DiffStatRange(ctx context.Context, base, head string) (string, error)` alongside it for stat-only output.

3. Implement both in `gitCommitter` using `git diff <base>..<head>` and `git diff --stat <base>..<head>`.

4. Add `DiffRange(ctx context.Context, base, head string) (string, error)` to `loop.CycleCommitter` with a matching implementation in `gitCycleCommitter`.

5. Update mocks/tests that implement these interfaces.

## Files

- `internal/nebula/git.go` — Add `DiffRange` and `DiffStatRange` to interface and implementation
- `internal/loop/git.go` — Add `DiffRange` to `CycleCommitter` interface and `gitCycleCommitter`
- `internal/nebula/git_test.go` — Test `DiffRange` and `DiffStatRange`
- `internal/loop/git_test.go` — Test `DiffRange`
- Any mock implementations of these interfaces in test files

## Acceptance Criteria

- [ ] `GitCommitter.DiffRange(ctx, baseSHA, headSHA)` returns the full diff between two commits
- [ ] `GitCommitter.DiffStatRange(ctx, baseSHA, headSHA)` returns the stat summary
- [ ] `CycleCommitter.DiffRange(ctx, baseSHA, headSHA)` returns the full diff
- [ ] Both return errors for invalid SHAs
- [ ] Nil receivers return empty string (nil-safety pattern preserved)
- [ ] All existing interface consumers updated (mocks, tests)