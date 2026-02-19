+++
id = "nebula-scanner"
title = "Auto-scan nebulas and git history to populate the catalog"
type = "feature"
priority = 1
depends_on = ["spacetime-model"]
+++

## Problem

The catalog should populate itself with zero manual effort. By scanning `.nebulas/*/nebula.toml` for structure and git history for timeline data, we can auto-derive sequence, status, dates, branches, and areas touched.

## Solution

### Scanner Pipeline

The scanner runs in three stages:

#### 1. Nebula Discovery

Walk `.nebulas/*/nebula.toml` and parse each manifest:
- Extract name, description, default type, labels
- Count total phases by parsing `*.md` files with `+++` frontmatter
- Derive category from the dominant phase type (feature, bugfix, etc.)

#### 2. Git Archaeology

For each discovered nebula, query git to derive timeline data:
- **Branch**: look for `nebula/<name>` branch pattern
- **Created**: first commit on the nebula branch (or first commit touching `.nebulas/<name>/`)
- **Completed**: merge commit to main (if exists)
- **Status**: has merge commit → completed; has branch → in_progress; only files → planned
- **Areas touched**: from the merge diff, extract which `internal/*/` and `cmd/` packages were modified
- **Packages added vs modified**: packages that didn't exist before the nebula vs ones that did

Use `exec.CommandContext` for git operations, keeping it testable via an interface:

```go
// GitQuerier abstracts git operations for testability.
type GitQuerier interface {
    FirstCommitOnBranch(ctx context.Context, branch string) (time.Time, error)
    MergeCommitToMain(ctx context.Context, branch string) (time.Time, error)
    DiffPackages(ctx context.Context, base, head string) (added, modified []string, err error)
    BranchExists(ctx context.Context, name string) (bool, error)
}
```

#### 3. Sequence Assignment

Order nebulas by their created timestamp. Assign sequence numbers 1, 2, 3, ...
If a nebula has no git history yet (just files), it gets the next available sequence.

#### 4. Relationship Inference

- **`enables`**: if nebula B's `nebula.toml` mentions `requires_nebulae = ["A"]`, then A enables B
- **`builds_on`**: inverse of enables
- **Area overlap**: nebulas that touch the same packages are related (store as a hint, not a hard relationship)

### Merge Strategy

When re-scanning:
1. Load existing `spacetime.toml`
2. Run the scanner
3. For each entry: auto-derived fields update, manual fields (`summary`, `lessons`, manual `enables` overrides) are preserved
4. New nebulas get appended
5. Removed nebulas get marked as `abandoned` (not deleted — history matters)

## Files

- `internal/relativity/scanner.go` — discovery, git archaeology, sequence assignment
- `internal/relativity/git.go` — GitQuerier interface + CLI implementation
- `internal/relativity/merge.go` — merge strategy for auto + manual data
- `internal/relativity/scanner_test.go` — tests with mock GitQuerier

## Acceptance Criteria

- [ ] Scanner discovers all nebulas from `.nebulas/*/nebula.toml`
- [ ] Phase count is accurate per nebula
- [ ] Git history correctly derives created/completed dates
- [ ] Status inference works: planned, in_progress, completed
- [ ] Sequence assignment is stable across re-scans
- [ ] Manual annotations survive re-scanning
- [ ] Removed nebulas become `abandoned`, not deleted
- [ ] `go test ./internal/relativity/...` passes