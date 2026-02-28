+++
id = "size-guards"
title = "Enforce package file count and per-file LOC limits"
type = "task"
priority = 2
depends_on = ["arch-foundation"]
scope = ["internal/arch_test/size_test.go"]
+++

## Problem

Packages that grow too large become hard to navigate, test, and reason about. Individual files that balloon past a few hundred lines often signal mixed responsibilities. Without automated limits, these thresholds are only caught during code review — if at all.

## Solution

Create `internal/arch_test/size_test.go` that enforces two size guardrails:

1. **Per-package file count**: No internal package should have more than **20** non-test `.go` files. Exceeding this signals the package should be split.
2. **Per-file line count**: No single `.go` file (including test files) should exceed **400** lines. Exceeding this signals the file should be decomposed.

### Thresholds

```go
const (
    maxFilesPerPackage = 20
    maxLinesPerFile    = 400
)
```

### Exclusions

- `board` package (dead code)
- `arch_test` package itself
- Generated files (check for `// Code generated` header on line 1)

### Test functions

`TestPackageFileCount(t *testing.T)`:
- For each internal package, count non-test `.go` files
- If count exceeds `maxFilesPerPackage`, fail with: `"package %s has %d .go files (limit: %d); consider splitting"`
- Subtests per package

`TestFileLineCount(t *testing.T)`:
- For each internal package, scan ALL `.go` files (including test files)
- For each file, count lines via `lineCount` helper
- If count exceeds `maxLinesPerFile`, fail with: `"%s has %d lines (limit: %d); consider decomposing"`
- Subtests per file

### Handling current violations

If any packages or files currently exceed the thresholds, there are two options:

1. **Preferred**: The thresholds are generous enough that current code fits. Verify during implementation.
2. **Fallback**: If specific files/packages exceed limits, add them to an explicit exceptions list with a TODO comment. The exception should include the current count so progress toward compliance is visible.

```go
var sizeExceptions = map[string]int{
    // "internal/tui/model.go": 450, // TODO: split into model_init.go and model_update.go
}
```

## Files

- `internal/arch_test/size_test.go` — size guardrail tests (new file)

## Acceptance Criteria

- [ ] `TestPackageFileCount` catches any package with > 20 non-test `.go` files
- [ ] `TestFileLineCount` catches any file with > 400 lines
- [ ] Adding a 401-line file fails the test
- [ ] Any current violations are documented as exceptions with TODOs
- [ ] Generated files are excluded from line count checks
- [ ] `go test ./internal/arch_test/...` passes
