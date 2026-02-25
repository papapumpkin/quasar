+++
id = "split-hail-extract-test"
title = "Split internal/loop/hail_extract_test.go into extract and bridge tests"
type = "task"
priority = 2
scope = ["internal/loop/hail_extract_test.go", "internal/loop/hail_extract_bridge_test.go"]
+++

## Problem

`internal/loop/hail_extract_test.go` is 433 lines, exceeding the 400-line arch test limit. The file tests two concerns: direct extraction of reviewer hails and bridged discovery hail posting.

## Solution

Split along the extraction vs. bridge boundary.

### `hail_extract_test.go` keeps (~180 lines):

**Extraction tests:**
- `TestExtractReviewerHails`

### `hail_extract_bridge_test.go` (new) gets (~250 lines):

**Bridge and posting tests:**
- `TestBridgeDiscoveryHails`
- `TestExtractAndPostHails`

### Steps

1. Create `internal/loop/hail_extract_bridge_test.go` with the test package and necessary imports
2. Move `TestBridgeDiscoveryHails` and `TestExtractAndPostHails`
3. Remove moved functions and any now-unused imports from `hail_extract_test.go`
4. Verify: `go test ./internal/loop/...`

## Files

- `internal/loop/hail_extract_test.go` — remove bridge/posting tests
- `internal/loop/hail_extract_bridge_test.go` — new file with bridge/posting tests

## Acceptance Criteria

- [ ] `hail_extract_test.go` is under 400 lines
- [ ] `hail_extract_bridge_test.go` contains bridge and posting tests
- [ ] `go test ./internal/loop/...` passes
- [ ] No test coverage lost — all test functions preserved
