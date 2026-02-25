+++
id = "split-hail-relay-test"
title = "Split internal/loop/hail_relay_test.go into queue and relay tests"
type = "task"
priority = 2
scope = ["internal/loop/hail_relay_test.go", "internal/loop/hail_relay_queue_test.go"]
+++

## Problem

`internal/loop/hail_relay_test.go` is 508 lines, exceeding the 400-line arch test limit. The file tests two separable concerns: queue mechanics (unrelayed/resolved, mark relayed) and relay formatting/behavior.

## Solution

Extract queue-specific tests into a dedicated file.

### `hail_relay_queue_test.go` (new) gets (~200 lines):

**Queue mechanics tests:**
- `TestMemoryHailQueue_UnrelayedResolved`
- `TestMemoryHailQueue_MarkRelayed`

### `hail_relay_test.go` keeps (~310 lines):

**Relay formatting and behavior tests:**
- `TestFormatHailRelay`
- `TestPendingHailRelay`
- `TestOneShotRelayBehavior`
- `TestFormatHailRelay_AutoResolved`
- `TestPendingHailRelay_SweepsExpired`

### Steps

1. Create `internal/loop/hail_relay_queue_test.go` with the test package and necessary imports
2. Move `TestMemoryHailQueue_UnrelayedResolved` and `TestMemoryHailQueue_MarkRelayed`
3. Remove moved functions and any now-unused imports from `hail_relay_test.go`
4. Verify: `go test ./internal/loop/...`

## Files

- `internal/loop/hail_relay_test.go` — remove queue tests
- `internal/loop/hail_relay_queue_test.go` — new file with queue mechanics tests

## Acceptance Criteria

- [ ] `hail_relay_test.go` is under 400 lines
- [ ] `hail_relay_queue_test.go` contains queue mechanics tests
- [ ] `go test ./internal/loop/...` passes
- [ ] No test coverage lost — all test functions preserved
