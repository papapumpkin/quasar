+++
id = "final-verification"
title = "Final verification: full test suite, vet, and architecture check"
type = "task"
priority = 1
depends_on = ["fix-external-imports", "split-loop"]
scope = ["internal/", "cmd/"]
+++

## Problem

After all reorganization phases, we need a comprehensive verification pass to ensure nothing is broken and the new structure is consistent.

## Solution

Run the full verification checklist and fix any issues found:

### 1. Compilation check
```bash
go build ./...
```
Must succeed with zero errors.

### 2. Static analysis
```bash
go vet ./...
```
Must pass with no warnings.

### 3. Full test suite
```bash
go test ./...
```
All tests must pass. Pay special attention to:
- `internal/nebula/...` — tests for code that stayed in parent
- `internal/nebula/worker/...` — tests for code that moved to sub-package
- `internal/loop/...` — tests for consolidated/split files
- `internal/tui/...` — tests for files with updated imports
- `cmd/...` — tests for command files with updated imports

### 4. Import formatting
```bash
goimports -w internal/ cmd/
```
Ensure all import blocks are properly formatted (stdlib, external, internal groups).

### 5. Architecture test
If `internal/arch_test/` tests validate layering or import rules, run them explicitly:
```bash
go test ./internal/arch_test/...
```
The architecture tests may need updates if they check package counts, import paths, or file existence for `internal/board/` or the old nebula flat structure.

### 6. Verify no circular imports
The `go build` already catches this, but explicitly verify:
- `internal/nebula/worker` imports `internal/nebula` — OK
- `internal/nebula` does NOT import `internal/nebula/worker` — OK

### 7. Verify no dead imports
Check that no file imports packages it doesn't use (goimports handles this, but verify).

## Files

- All files in `internal/` and `cmd/` — verify compilation and tests
- `internal/arch_test/` — may need updates for new package structure

## Acceptance Criteria

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] `go test ./...` — all tests pass
- [ ] `goimports` applied, no formatting changes needed
- [ ] Architecture tests pass (update if needed for new structure)
- [ ] No circular imports exist
- [ ] No unused imports remain
