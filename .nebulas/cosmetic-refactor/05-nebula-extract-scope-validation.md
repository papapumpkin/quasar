+++
id = "nebula-extract-scope-validation"
title = "Extract scope overlap validation into its own file"
type = "task"
priority = 3
scope = ["internal/nebula/validate.go", "internal/nebula/scope.go"]
+++

## Problem

`internal/nebula/validate.go` contains ~150 lines of scope overlap validation logic across 9 helper functions: `validateScopeOverlaps`, `scopesOverlap`, `patternsOverlap`, `dirContains`, `isGlob`, `globsOverlap`, `globDirPrefix`, `globSuffixesOverlap`, `globRepresentative`.

These are purely about path/glob matching and have nothing to do with the rest of nebula validation (checking required fields, dependency cycles, etc.). Mixing them in one file makes `validate.go` harder to navigate and the scope logic harder to test in isolation.

## Solution

Extract all scope-related functions into a new file `internal/nebula/scope.go`:

1. Move these functions to `scope.go`: `validateScopeOverlaps`, `scopesOverlap`, `patternsOverlap`, `dirContains`, `isGlob`, `globsOverlap`, `globDirPrefix`, `globSuffixesOverlap`, `globRepresentative`.
2. Keep them in the `nebula` package (same package, just a different file).
3. `validate.go` continues to call `validateScopeOverlaps` — no API change.
4. If there are tests for scope validation in `validate_test.go`, move those to `scope_test.go`.

## Files

- `internal/nebula/validate.go` — remove scope-related functions
- `internal/nebula/scope.go` (new) — scope overlap validation functions
- `internal/nebula/scope_test.go` (new, if applicable) — moved scope tests

## Acceptance Criteria

- [ ] All scope-related helpers live in `scope.go`
- [ ] `validate.go` is noticeably shorter and focused on structural validation
- [ ] No exported API changes
- [ ] `go test ./...` passes
