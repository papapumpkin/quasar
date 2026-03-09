+++
id = "loop-bus-wiring"
title = "Wire streaming invoker output to the event bus through the loop"
type = "task"
priority = 2
depends_on = ["streaming-invoker"]
+++

## Problem

The coder-reviewer loop calls `Invoke()` and only publishes `EventAgentDone` after the call returns. The bus has `KindPhaseAgentOutput` and `KindAgentOutput` event kinds ready to use but nothing emits them during execution. Workers and the TUI are blind while agents run.

## Solution

When the loop invokes the coder or reviewer, wire the invoker's `OnOutput` callback to publish `bus.Event` messages in real-time.

### Approach

1. The loop already has access to the bus (via its `emit()` method or the `Bus` field added by pulsar). Confirm how the loop currently publishes events and use the same mechanism.

2. Before each `Invoke()` call in `runCoderPhase()` and `runReviewerPhase()`, set the invoker's `OnOutput` callback to a closure that:
   - Creates a `bus.Event` with `KindPhaseAgentOutput` (for nebula phases) or `KindAgentOutput` (for single-task mode)
   - Sets `PhaseID`, `Role` (coder/reviewer), `Message` (the stderr line), and `Timestamp`
   - Publishes to the bus (non-blocking — if bus is nil or full, drop silently)

3. After `Invoke()` returns, clear/reset the callback (or just let it be overwritten on the next call).

4. The callback must be lightweight — it runs in the stderr goroutine. Just construct the event and publish. No parsing, no filtering. The digester (phase 3) handles interpretation.

5. For single-task mode (`RunTask`), use `KindAgentOutput`. For nebula phase mode (`RunExistingPhase`/`RunFromCheckpoint`), use `KindPhaseAgentOutput` with the phase ID.

## Files

- `internal/loop/loop.go` — wire `OnOutput` before coder/reviewer invocations
- `internal/loop/loop_test.go` — verify bus events are published during invocation (use mock invoker that calls OnOutput)

## Acceptance Criteria

- [ ] `KindPhaseAgentOutput` events are published to the bus during coder invocation
- [ ] `KindPhaseAgentOutput` events are published to the bus during reviewer invocation
- [ ] Events carry correct `PhaseID`, `Role`, `Message`, and `Timestamp`
- [ ] When bus is nil, no panic — callback is simply not set
- [ ] No change to existing event timing (AgentStart, AgentDone still fire at the same points)
- [ ] `go test ./internal/loop/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
