+++
id = "split-hail-escalation-test"
title = "Split internal/loop/hail_escalation_test.go into escalation and post tests"
type = "task"
priority = 2
scope = ["internal/loop/hail_escalation_test.go", "internal/loop/hail_escalation_post_test.go"]
+++

## Problem

`internal/loop/hail_escalation_test.go` is 405 lines, exceeding the 400-line arch test limit. The file tests two related but separable concerns: escalation logic and hail posting/building.

## Solution

Split into escalation tests and post/build tests.

### `hail_escalation_test.go` keeps (~200 lines):

**Escalation logic tests:**
- `TestEscalateCriticalFindings`
- `TestEscalateHighRiskLowSatisfaction`

### `hail_escalation_post_test.go` (new) gets (~200 lines):

**Hail building and posting tests:**
- `TestBuildMaxCyclesHail`
- `TestExtractAndPostHailsWithEscalation`
- `TestPostMaxCyclesHail`

### Steps

1. Create `internal/loop/hail_escalation_post_test.go` with the test package and necessary imports
2. Move the 3 post/build test functions
3. Remove moved functions and any now-unused imports from `hail_escalation_test.go`
4. Verify: `go test ./internal/loop/...`

## Files

- `internal/loop/hail_escalation_test.go` — remove post/build tests
- `internal/loop/hail_escalation_post_test.go` — new file with post/build tests

## Acceptance Criteria

- [ ] `hail_escalation_test.go` is under 400 lines
- [ ] `hail_escalation_post_test.go` contains post/build test functions
- [ ] `go test ./internal/loop/...` passes
- [ ] No test coverage lost — all test functions preserved
