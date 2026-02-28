+++
id = "cleanup-deprecated"
title = "Remove deprecated internal/context package"
type = "task"
priority = 3
depends_on = ["project-scanner"]
scope = ["internal/context/**"]
+++

## Problem

`internal/context/` contains only deprecation notices pointing to `internal/snapshot/`. It shadows Go's stdlib `context` package, which can cause import confusion and IDE autocompletion issues. Now that `internal/snapshot/` exists (from phase 1), the deprecated package should be removed.

## Solution

1. Delete `internal/context/scanner.go` and `internal/context/tree.go`
2. Delete the `internal/context/` directory
3. Delete `internal/context/scanner_test.go` and `internal/context/tree_test.go` if they exist
4. Verify no imports reference `internal/context` (it was never used outside the package)
5. Run `go build ./...` and `go vet ./...` to confirm clean removal

## Files

- `internal/context/scanner.go` — Delete
- `internal/context/tree.go` — Delete
- `internal/context/scanner_test.go` — Delete (if exists)
- `internal/context/tree_test.go` — Delete (if exists)

## Acceptance Criteria

- [ ] `internal/context/` directory no longer exists
- [ ] No Go files import the removed package
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` clean
