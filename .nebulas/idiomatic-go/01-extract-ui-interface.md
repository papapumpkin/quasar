+++
id = "extract-ui-interface"
title = "Extract UI interface from concrete *ui.Printer for testability"
type = "task"
priority = 1
depends_on = []
+++

## Problem

`loop.Loop` depends directly on `*ui.Printer`, a concrete struct. This makes the loop untestable without a real printer and violates interface-driven design — callers should depend on behavior, not implementation.

The `beads` package already demonstrates the correct pattern: `BeadsClient` is an interface in `types.go`, and `*Client` satisfies it. The UI layer should follow the same approach.

## Solution

Extract a `UI` interface from `*ui.Printer` containing only the methods that `loop.Loop` and `nebula.WorkerGroup` actually call. Then update `Loop.UI` and any other consumers to accept the interface instead of the concrete type.

## Files to Modify

- `internal/ui/ui.go` — Define a `UI` interface above the `Printer` struct with the methods used by `loop` and `nebula`
- `internal/loop/loop.go` — Change `UI *ui.Printer` to `UI ui.UI` (the new interface)
- `internal/loop/loop_test.go` — Add a `mockUI` struct that satisfies the interface for test use

## Acceptance Criteria

- [ ] A `UI` interface is defined in `internal/ui/ui.go`
- [ ] `*Printer` satisfies the `UI` interface (compile-time check: `var _ UI = (*Printer)(nil)`)
- [ ] `Loop.UI` field type is `ui.UI`, not `*ui.Printer`
- [ ] Existing code in `cmd/run.go` continues to work (passes `ui.New()` which returns `*Printer`)
- [ ] A `mockUI` or `noopUI` is available for tests
- [ ] `go vet ./...` and `go test ./...` pass
