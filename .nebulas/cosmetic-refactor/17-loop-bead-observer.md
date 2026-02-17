+++
id = "loop-bead-observer"
title = "Decouple bead tracking from loop orchestration with an observer hook"
type = "task"
priority = 2
depends_on = ["loop-extract-bead-error-helper"]
scope = ["internal/loop/loop.go", "internal/loop/hooks.go", "internal/loop/bead_hook.go", "internal/loop/loop_test.go", "cmd/run.go"]
+++

## Problem

After phase `loop-extract-bead-error-helper`, bead calls are consolidated into helpers, but the loop still directly invokes bead operations at every lifecycle point: cycle start, agent start, agent done, review result, task success, task failure. The `Loop` struct has a hard dependency on `beads.Client` and the orchestration logic is littered with tracking side-effects.

This coupling means:
- The loop can't run without a beads client (even in tests, a mock is required)
- Adding a new tracking integration (e.g., metrics, webhooks) requires modifying loop.go
- It's hard to see the core orchestration flow through the tracking noise

## Solution

Introduce an observer/hook pattern that decouples tracking from orchestration:

1. Define lifecycle events and a hook interface in `internal/loop/hooks.go`:
   ```go
   // Event represents a lifecycle event in the coder-reviewer loop.
   type Event struct {
       Kind      EventKind
       Cycle     int
       Agent     string // "coder" or "reviewer"
       Result    *agent.InvocationResult
       Findings  []string
       Err       error
   }

   type EventKind int

   const (
       EventCycleStart EventKind = iota
       EventAgentStart
       EventAgentDone
       EventReviewComplete
       EventTaskSuccess
       EventTaskFailed
   )

   // Hook receives lifecycle events. Implementations must not block.
   type Hook interface {
       OnEvent(ctx context.Context, event Event)
   }

   // HookFunc adapts a plain function to the Hook interface.
   type HookFunc func(ctx context.Context, event Event)

   func (f HookFunc) OnEvent(ctx context.Context, event Event) { f(ctx, event) }
   ```

2. Create a bead hook implementation in `internal/loop/bead_hook.go`:
   ```go
   // BeadHook translates loop events into bead operations.
   type BeadHook struct {
       Beads   beads.Client
       BeadID  string
       UI      UI  // for error logging
   }

   func (h *BeadHook) OnEvent(ctx context.Context, event Event) {
       switch event.Kind {
       case EventCycleStart:
           // Add cycle start comment
       case EventAgentDone:
           // Add agent result comment
       case EventTaskSuccess:
           // Close bead
       // ...
       }
   }
   ```

3. Update `Loop` struct:
   - Add `Hooks []Hook` field
   - Replace direct bead calls with `l.emit(ctx, Event{...})`
   - Add a small `emit` helper that fans out to all hooks
   - `Beads` field becomes optional (only needed if `BeadHook` is registered)

4. At the call site (`cmd/run.go`), wire the `BeadHook`:
   ```go
   loop := &loop.Loop{
       Hooks: []loop.Hook{
           &loop.BeadHook{Beads: beadsCli, BeadID: id, UI: ui},
       },
       // ...
   }
   ```

5. Tests that don't care about bead tracking can simply omit hooks — no mock needed.

## Files

- `internal/loop/hooks.go` (new) — `Event`, `EventKind`, `Hook` interface, `HookFunc`
- `internal/loop/bead_hook.go` (new) — `BeadHook` implementation
- `internal/loop/loop.go` — remove direct bead calls, add `Hooks` field and `emit` helper
- `cmd/run.go` — wire `BeadHook` into the loop
- `internal/loop/loop_test.go` — simplify tests that currently mock beads just for side-effects

## Acceptance Criteria

- [ ] `Loop` struct no longer directly calls `beads.Client` methods
- [ ] `Hook` interface exists with at least `BeadHook` implementation
- [ ] Loop emits events at all lifecycle points previously covered by bead calls
- [ ] Tests that don't test bead behavior can omit hooks entirely
- [ ] `go test ./internal/loop/...` passes
- [ ] `go vet ./...` passes
