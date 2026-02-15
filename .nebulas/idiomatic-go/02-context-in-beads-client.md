+++
id = "context-in-beads"
title = "Add context.Context to BeadsClient interface methods"
type = "task"
priority = 1
depends_on = []
+++

## Problem

`beads.BeadsClient` methods (`Create`, `Update`, `Close`, `AddComment`, `Show`) do not accept `context.Context`. This means bead operations cannot be cancelled when the user sends SIGINT, and there is no way to enforce timeouts on CLI subprocess calls.

The `agent.Invoker` interface already follows the correct pattern — `Invoke(ctx context.Context, ...)`.

## Solution

Add `context.Context` as the first parameter to every `BeadsClient` method and the underlying `*Client` implementation. Use `exec.CommandContext` instead of `exec.Command` in `Client.run()`.

## Files to Modify

- `internal/beads/types.go` — Add `context.Context` as first param to every `BeadsClient` method signature
- `internal/beads/beads.go` — Update `run()` and all methods to accept and propagate `ctx`; switch to `exec.CommandContext`
- `internal/loop/loop.go` — Pass `ctx` to all `l.Beads.*` calls
- `internal/nebula/worker.go` — Pass `ctx` to bead calls if any exist

## Acceptance Criteria

- [ ] Every `BeadsClient` method accepts `context.Context` as its first parameter
- [ ] `Client.run()` uses `exec.CommandContext(ctx, ...)` instead of `exec.Command`
- [ ] All call sites in `loop` and `nebula` pass their `ctx` through
- [ ] `go vet ./...` and `go test ./...` pass
