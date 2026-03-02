+++
id = "consolidate-loop"
title = "Consolidate micro-files in internal/loop/"
type = "task"
priority = 2
depends_on = ["delete-dead-code"]
scope = ["internal/loop/"]
+++

## Problem

`internal/loop/` has several tiny files that don't justify their own existence:

1. **`finding_id.go`** — 20 lines, just the `FindingID()` function (SHA-256 hash of severity:description).
2. **`finding_lifecycle.go`** — 50 lines, `ApplyVerifications()` + `LifecycleSummary` struct.
3. **`finding_serialize.go`** — 25 lines, `SerializeFindings()` function.
4. **`errors.go`** — 12 lines, just two sentinel errors (`ErrMaxCycles`, `ErrBudgetExceeded`).

These three finding files total ~95 lines and form a single logical unit. The errors file has two `var` declarations that belong with the state machine constants.

## Solution

### Merge 1: Finding files → `findings.go`

Create `internal/loop/findings.go` containing the contents of:
- `finding_id.go` — `FindingID()` function and its imports
- `finding_lifecycle.go` — `LifecycleSummary` struct, `ApplyVerifications()`, `String()`, `HasUnresolved()`
- `finding_serialize.go` — `SerializeFindings()` function

Delete the three source files after merging.

Similarly, merge `finding_id_test.go`, `finding_lifecycle_test.go`, and `finding_serialize_test.go` into a single `findings_test.go`.

### Merge 2: `errors.go` → `state.go`

Move the two sentinel error variables (`ErrMaxCycles`, `ErrBudgetExceeded`) and their `errors` import into the top of `state.go`, which already defines the related `Phase` enum and `CycleState`. Delete `errors.go`.

No test file merge needed — `errors.go` has no dedicated test file; these errors are tested transitively through `loop_test.go`.

## Files

- `internal/loop/finding_id.go` — delete (merge into findings.go)
- `internal/loop/finding_lifecycle.go` — delete (merge into findings.go)
- `internal/loop/finding_serialize.go` — delete (merge into findings.go)
- `internal/loop/findings.go` — create (combined content)
- `internal/loop/finding_id_test.go` — delete (merge into findings_test.go)
- `internal/loop/finding_lifecycle_test.go` — delete (merge into findings_test.go)
- `internal/loop/finding_serialize_test.go` — delete (merge into findings_test.go)
- `internal/loop/findings_test.go` — create (combined tests)
- `internal/loop/errors.go` — delete (merge into state.go)
- `internal/loop/state.go` — add sentinel errors at top

## Acceptance Criteria

- [ ] `finding_id.go`, `finding_lifecycle.go`, `finding_serialize.go` no longer exist
- [ ] `findings.go` contains all finding-related types and functions
- [ ] `errors.go` no longer exists
- [ ] `ErrMaxCycles` and `ErrBudgetExceeded` are in `state.go`
- [ ] `go build ./internal/loop/...` succeeds
- [ ] `go test ./internal/loop/...` passes
- [ ] No duplicate imports in merged files
