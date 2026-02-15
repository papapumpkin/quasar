+++
id = "table-driven-tests"
title = "Convert repetitive tests to table-driven pattern with t.Run"
type = "task"
priority = 2
depends_on = []
+++

## Problem

Several test files use repetitive individual test functions where table-driven tests would be clearer and more maintainable:

- `internal/loop/loop_test.go` — `TestParseReviewFindings_Approved`, `_SingleIssue`, `_MultipleIssues`, `_NoSeverity`, `_MultilineDescription` are five separate functions testing the same function with different inputs
- `internal/loop/report_test.go` — `TestParseReviewReport_Full`, `_NeedsHumanReview`, `_Missing`, `_MediumValues` are four separate functions

Note that `TestIsApproved` and `TestBuildArgs_OptionalFlags` already use table-driven patterns correctly — follow their style.

## Solution

Consolidate each group into a single table-driven test using `t.Run` for subtests and `t.Parallel()` where safe:

```go
func TestParseReviewFindings(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        wantLen  int
        // ...
    }{...}
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            // ...
        })
    }
}
```

## Files to Modify

- `internal/loop/loop_test.go` — Consolidate `TestParseReviewFindings_*` into one table-driven test
- `internal/loop/report_test.go` — Consolidate `TestParseReviewReport_*` into one table-driven test

## Acceptance Criteria

- [ ] Each group of related tests is one function with `t.Run` subtests
- [ ] Subtests use `t.Parallel()` where safe (no shared mutable state)
- [ ] All existing test cases are preserved — no coverage loss
- [ ] `go test ./internal/loop/...` passes
