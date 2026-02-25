+++
id = "update-exceptions"
title = "Update arch test exceptions after file splits"
type = "task"
priority = 2
depends_on = ["split-msg", "split-bridge", "split-hail-list-test", "split-hail-escalation-test", "split-hail-extract-test", "split-hail-relay-test"]
scope = ["internal/arch_test/size_test.go"]
+++

## Problem

After phases 1-6 split the 6 oversized files, the arch test exceptions in `internal/arch_test/size_test.go` need updating:

1. Files that are now under 400 lines should be **removed** from `lineCountExceptions`
2. Stale line counts in `lineCountExceptions` that have drifted from actual values need correcting
3. `packageFileCountExceptions["tui"]` must be bumped from 34 to 36 (2 new production files: `msg_phase.go` and `bridge_phase.go`)

Note: The new test files (`hail_list_model_test.go`, `hail_escalation_post_test.go`, `hail_extract_bridge_test.go`, `hail_relay_queue_test.go`) do not affect `packageFileCountExceptions` since that map counts non-test `.go` files only.

## Solution

### Step 1: Remove split files from exceptions

The following files were over 400 lines and should now be under the limit after splitting. Remove their entries from `lineCountExceptions`:

- `internal/tui/msg.go` (was 402)
- `internal/tui/bridge.go` (was 428)
- `internal/tui/hail_list_test.go` (was 521)
- `internal/loop/hail_escalation_test.go` (was 405)
- `internal/loop/hail_extract_test.go` (was 433)
- `internal/loop/hail_relay_test.go` (was 508)

Note: These files were NOT in the exceptions map before (that's why the arch test was failing). Confirm they don't appear; if they were added as a stopgap, remove them.

### Step 2: Audit stale line counts

Run `wc -l` on every file in `lineCountExceptions` and update any entries whose recorded count differs from the actual count. The exception map should reflect reality so progress toward compliance is visible.

### Step 3: Bump package file count for tui

Update `packageFileCountExceptions`:
```go
"tui": 36, // TODO: split into tui/views, tui/bridge, tui/overlay sub-packages
```

(from 34 to 36 — adding `msg_phase.go` and `bridge_phase.go`)

### Step 4: Verify

```bash
go test ./internal/arch_test/...
go test ./...
```

All tests must pass.

## Files

- `internal/arch_test/size_test.go` — update `lineCountExceptions` and `packageFileCountExceptions`

## Acceptance Criteria

- [ ] No entries in `lineCountExceptions` for files that are now under 400 lines
- [ ] All line counts in `lineCountExceptions` match actual `wc -l` values
- [ ] `packageFileCountExceptions["tui"]` is 36
- [ ] `go test ./internal/arch_test/...` passes
- [ ] `go test ./...` passes
