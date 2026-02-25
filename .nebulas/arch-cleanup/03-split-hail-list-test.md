+++
id = "split-hail-list-test"
title = "Split internal/tui/hail_list_test.go into component and model tests"
type = "task"
priority = 2
scope = ["internal/tui/hail_list_test.go", "internal/tui/hail_list_model_test.go"]
+++

## Problem

`internal/tui/hail_list_test.go` is 521 lines, exceeding the 400-line arch test limit. The file contains two distinct test categories: unit tests for the `hailListOverlay` component and integration tests for the `appModel`'s hail list behavior.

## Solution

Split along the component vs. model integration boundary.

### `hail_list_test.go` keeps (~200 lines):

**Overlay component unit tests:**
- `TestNewHailListOverlay`
- `TestHailListNavigation`
- `TestHailListSelected`
- `TestHailListView`

**Test helpers:**
- `makeTestHails`
- `makeModelWithHailList`

### `hail_list_model_test.go` (new) gets (~320 lines):

**Model integration tests:**
- `TestAppModelMsgHailReceivedTracking`
- `TestAppModelMsgHailResolved`
- `TestAppModelHKeyOpensHailList`
- `TestAppModelHailListKeyHandling`
- `TestAppModelViewWithHailList`

Note: The new file will need its own imports and may reference `makeTestHails` / `makeModelWithHailList` from the same package (same `_test` package, so this is fine).

### Steps

1. Create `internal/tui/hail_list_model_test.go` with `package tui` (or the existing test package) and necessary imports
2. Move the 5 model integration test functions
3. Remove moved functions and any now-unused imports from `hail_list_test.go`
4. Verify: `go test ./internal/tui/...`

## Files

- `internal/tui/hail_list_test.go` — remove model integration tests
- `internal/tui/hail_list_model_test.go` — new file with model integration tests

## Acceptance Criteria

- [ ] `hail_list_test.go` is under 400 lines
- [ ] `hail_list_model_test.go` contains all model integration tests
- [ ] `go test ./internal/tui/...` passes
- [ ] No test coverage lost — all test functions preserved
