+++
id = "worker-gate-strategy"
title = "Apply strategy pattern to gate mode logic"
type = "task"
priority = 2
depends_on = ["worker-functional-options"]
scope = ["internal/nebula/gate.go", "internal/nebula/worker.go"]
+++

## Problem

Gate mode logic (`trust`, `review`, `approve`, `watch`) is handled via switch statements and conditionals scattered across `worker.go`. When a phase completes, the worker checks the gate mode and decides whether to prompt for approval, pause for review, auto-proceed, etc. This logic is interleaved with orchestration code, making both harder to follow.

Adding a new gate mode requires finding and updating every switch/if chain that branches on the gate string.

## Solution

Apply the strategy pattern by defining a `Gater` interface with one implementation per mode:

1. In `internal/nebula/gate.go`, define:
   ```go
   // Gater decides whether to proceed after a phase completes.
   type Gater interface {
       // ShouldProceed is called after a phase finishes. It returns true if
       // execution should continue, or false to pause/stop. It may block
       // for user input (e.g. approve mode).
       ShouldProceed(ctx context.Context, phase *Phase, result *PhaseRunnerResult) (bool, error)
   }
   ```

2. Implement concrete strategies:
   ```go
   // trustGater always proceeds.
   type trustGater struct{}

   func (trustGater) ShouldProceed(_ context.Context, _ *Phase, _ *PhaseRunnerResult) (bool, error) {
       return true, nil
   }

   // reviewGater pauses for human review but doesn't require explicit approval.
   type reviewGater struct{ ... }

   // approveGater blocks until explicit user approval.
   type approveGater struct{ ... }

   // watchGater logs progress but doesn't block.
   type watchGater struct{ ... }
   ```

3. Add a factory function:
   ```go
   // NewGater returns the Gater for the given mode string.
   func NewGater(mode string) (Gater, error) { ... }
   ```

4. In `WorkerGroup`, replace the `Gate string` field with `Gater Gater`. Update the `WithGate` option from the previous phase to accept a `Gater` or to call `NewGater` internally.

5. Replace all gate-related switch/if chains in `worker.go` with calls to `wg.Gater.ShouldProceed(...)`.

## Files

- `internal/nebula/gate.go` — define `Gater` interface, implement all four strategies, add `NewGater` factory
- `internal/nebula/worker.go` — replace `Gate string` field with `Gater Gater`, replace conditional gate logic with strategy calls

## Acceptance Criteria

- [ ] `Gater` interface defined with `ShouldProceed` method
- [ ] Four implementations: `trustGater`, `reviewGater`, `approveGater`, `watchGater`
- [ ] No gate-mode switch/if chains remain in `worker.go`
- [ ] `WorkerGroup` uses `Gater` field instead of `Gate string`
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
