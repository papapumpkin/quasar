+++
id = "gate-mode-implementation"
title = "Implement gate mode pause-and-prompt behavior"
type = "feature"
priority = 1
depends_on = ["gate-mode-types-config", "checkpoint-diffs"]
+++

## Problem

Gate mode types and checkpoint diffs exist, but nothing actually pauses execution or prompts the human. The `WorkerGroup` runs phases to completion without consulting anyone.

## Solution

Implement the gate behavior in the worker loop. After a phase completes, check the resolved gate mode and either continue, pause for input, or stream the diff.

### Gater Interface

```go
// Gater handles human interaction at phase boundaries.
type Gater interface {
    // Prompt displays the checkpoint and waits for a human decision.
    // Returns the chosen action.
    Prompt(ctx context.Context, cp *Checkpoint) (GateAction, error)
}

type GateAction string

const (
    GateActionAccept GateAction = "accept" // Continue to next phase
    GateActionReject GateAction = "reject" // Mark phase as failed, stop
    GateActionRetry  GateAction = "retry"  // Re-run this phase
    GateActionSkip   GateAction = "skip"   // Skip remaining phases, stop nebula
)
```

### Terminal Gater

Implement `terminalGater` that reads from stdin:

```
   [a]ccept  [r]eject  [e]retry  [s]kip
   > _
```

- Reads a single character (no enter required if terminal supports raw mode, otherwise read a line)
- In non-TTY environments (piped stdin), default to `accept` with a warning logged to stderr
- Timeout: if `ctx` is cancelled, return `GateActionSkip`

### Worker Integration

In `executePhase` (after commit + checkpoint):

1. Resolve gate mode via `ResolveGate(manifest, phase)`
2. If `trust`: continue (no prompt)
3. If `review`: render checkpoint, call `Gater.Prompt`, act on response
4. If `approve`: same as review (the plan-level approval is handled in phase 06)
5. If `watch`: render checkpoint but don't block (log and continue)

Handle `GateAction` responses:
- `accept`: continue normally
- `reject`: set phase status to `failed`, stop the nebula
- `retry`: re-queue the phase for execution
- `skip`: stop the nebula gracefully (mark remaining phases as skipped)

### WorkerGroup Changes

Add `Gater` field to `WorkerGroup`. If nil, behave as `trust` mode (backward compatible).

## Files to Create

- `internal/nebula/gate.go` — `Gater` interface, `GateAction` type, `terminalGater` implementation

## Files to Modify

- `internal/nebula/worker.go` — Add `Gater` field, integrate gate logic after phase completion
- `internal/nebula/types.go` — Add `PhaseStatusSkipped` constant if not already present

## Acceptance Criteria

- [ ] `Gater` interface with `Prompt` method defined
- [ ] `terminalGater` reads user input from stdin
- [ ] Non-TTY environments degrade gracefully (auto-accept with warning)
- [ ] `trust` mode skips prompting entirely
- [ ] `review` mode renders checkpoint and blocks for input
- [ ] `watch` mode renders checkpoint without blocking
- [ ] `accept`, `reject`, `retry`, `skip` actions handled correctly
- [ ] `WorkerGroup.Gater` is nil-safe (defaults to trust)
- [ ] Context cancellation causes graceful skip
- [ ] `go test ./internal/nebula/...` passes