+++
id = "delete-dead-code"
title = "Delete dead code: board/, worker_board.go, architect_overlay.go"
type = "task"
priority = 1
scope = ["internal/board/", "internal/nebula/worker_board.go", "internal/tui/architect_overlay.go"]
+++

## Problem

The codebase contains dead code that adds confusion and noise:

1. **`internal/board/`** — 7 source files + 6 test files (13 total). Every source file is an empty 14-byte stub containing only `package board`. The entire package was renamed to `internal/fabric/` on Feb 23, 2026 (commits 1e6cd62, 083227b). Not imported anywhere in the codebase.

2. **`internal/nebula/worker_board.go`** — Empty placeholder file with 0 symbols. Was reserved for board integration that was replaced by `worker_fabric.go`.

3. **`internal/tui/architect_overlay.go`** — Empty placeholder file with no types or functions.

## Solution

Delete all dead files. No import changes needed since nothing references them.

1. Delete the entire `internal/board/` directory (all 13 files):
   - `board.go`, `contract.go`, `llmpoller.go`, `poller.go`, `publisher.go`, `pushback.go`, `sqlite.go`
   - `publisher_test.go`, `pushback_test.go`, `poller_test.go`, `llmpoller_test.go`, `sqlite_test.go`, `integration_test.go`

2. Delete `internal/nebula/worker_board.go`

3. Delete `internal/tui/architect_overlay.go`

4. Run `go build ./...` and `go vet ./...` to verify nothing breaks.

## Files

- `internal/board/` — delete entire directory
- `internal/nebula/worker_board.go` — delete
- `internal/tui/architect_overlay.go` — delete

## Acceptance Criteria

- [ ] `internal/board/` directory no longer exists
- [ ] `internal/nebula/worker_board.go` no longer exists
- [ ] `internal/tui/architect_overlay.go` no longer exists
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
