+++
id = "stdlib-string-helpers"
title = "Replace custom contains/searchSubstring with strings.Contains"
type = "task"
priority = 3
depends_on = ["table-driven-tests"]
+++

## Problem

`internal/loop/loop_test.go` defines custom `contains` and `searchSubstring` helpers (lines 139-150) that reimplements `strings.Contains`. Similarly, `report_test.go` calls `contains` from the same package. This is unnecessary complexity when the stdlib provides the exact same function.

## Solution

Delete `contains` and `searchSubstring` from `loop_test.go`. Replace all calls with `strings.Contains`. Add `"strings"` to the import block.

## Files to Modify

- `internal/loop/loop_test.go` — Delete `contains`/`searchSubstring`, use `strings.Contains`
- `internal/loop/report_test.go` — Update `contains(...)` calls to `strings.Contains(...)`

## Acceptance Criteria

- [ ] No custom `contains` or `searchSubstring` functions remain
- [ ] All call sites use `strings.Contains`
- [ ] `go test ./internal/loop/...` passes
