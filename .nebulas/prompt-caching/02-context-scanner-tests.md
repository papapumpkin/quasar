+++
id = "context-scanner-tests"
title = "Test the project context scanner"
type = "task"
priority = 2
depends_on = ["context-scanner"]
+++

## Problem

The context scanner must be well-tested to ensure determinism, correct tree building, and proper handling of edge cases (empty repos, missing CLAUDE.md, large repos, non-Go projects).

## Solution

Write comprehensive tests in `internal/context/scanner_test.go` and `internal/context/tree_test.go`.

### Scanner tests:
- **Determinism**: Run `Scan` twice on the same fixture, assert byte-identical output
- **With CLAUDE.md**: Fixture repo with a CLAUDE.md, verify it appears in output
- **Without CLAUDE.md**: Fixture repo without one, verify graceful handling
- **Max size cap**: Create a large fixture, verify output is truncated cleanly
- **go.mod detection**: Fixture with go.mod, verify module name extracted
- **package.json detection**: Fixture with package.json, verify project name extracted
- **Empty directory**: Edge case — empty git repo

### Tree tests:
- **Flat files**: `["a.go", "b.go"]` → correct tree
- **Nested dirs**: `["cmd/root.go", "internal/loop/loop.go"]` → correct nesting
- **Depth limit**: Deep nesting with depth=2, verify truncation
- **Sorting**: Verify alphabetical ordering of dirs and files
- **Large tree**: Many files, verify render stays compact

Use `t.TempDir()` to create fixture repos. Initialize with `git init` + `git add` to make `git ls-files` work.

## Files

- `internal/context/scanner_test.go` — scanner integration tests
- `internal/context/tree_test.go` — tree builder unit tests

## Acceptance Criteria

- [ ] All tests pass with `go test ./internal/context/...`
- [ ] Determinism test explicitly checks byte equality
- [ ] At least one test covers non-Go project detection
- [ ] Edge case for empty/missing files is covered
- [ ] Tree depth limiting is tested
