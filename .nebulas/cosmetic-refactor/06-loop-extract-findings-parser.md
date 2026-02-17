+++
id = "loop-extract-findings-parser"
title = "Extract review findings parser from loop.go into its own file"
type = "task"
priority = 2
scope = ["internal/loop/loop.go", "internal/loop/parse.go"]
+++

## Problem

`internal/loop/loop.go` (567 lines) mixes core loop orchestration with text-parsing utilities for review findings. The following functions are purely about parsing reviewer output and have no dependency on the `Loop` struct:

- `ParseReviewFindings(text string) []string`
- `parseIssueBlock(lines []string) string`
- `collectContinuationLines(lines []string, start int) (string, int)`
- `isApproved(findings []string) bool`
- `truncate(s string, max int) string`

These are standalone pure functions that clutter the main loop file and make it harder to follow the orchestration flow.

## Solution

Extract these functions into a new file `internal/loop/parse.go`:

1. Create `internal/loop/parse.go` in the `loop` package.
2. Move `ParseReviewFindings`, `parseIssueBlock`, `collectContinuationLines`, `isApproved`, and `truncate` into it.
3. Move the associated `issuePrefixes` var as well.
4. No signature changes — these are all package-internal or already exported.
5. If there are dedicated tests for these parsers in `loop_test.go`, consider moving them to `parse_test.go` for co-location (optional but recommended).

## Files

- `internal/loop/loop.go` — remove parsing functions
- `internal/loop/parse.go` (new) — findings parser, approval checker, truncate
- `internal/loop/parse_test.go` (new, optional) — moved parser tests

## Acceptance Criteria

- [ ] `loop.go` no longer contains any text-parsing functions
- [ ] `parse.go` contains all moved functions, compiles, and is in package `loop`
- [ ] `go test ./internal/loop/...` passes (critical — this package has comprehensive tests)
