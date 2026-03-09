+++
id = "activity-digester"
title = "Haiku-powered per-phase activity digester as a bus subscriber"
type = "task"
priority = 2
depends_on = ["loop-bus-wiring"]
+++

## Problem

Raw stderr lines from claude are noisy — tool permission prompts, thinking indicators, file paths, etc. A developer watching multiple phases needs a one-line summary per phase: "editing internal/loop/loop.go", "running tests", "reviewing diff for config package". This needs to be cheap, async, and not block execution.

## Solution

A new bus subscriber that accumulates recent `KindPhaseAgentOutput` events per phase, periodically sends the batch to Haiku for a one-line digest, and publishes the result as a new bus event.

### Approach

1. **New package**: `internal/digester/` with a `Digester` struct.

2. **Digester lifecycle**:
   - Subscribes to the bus for `KindPhaseAgentOutput` and `KindAgentOutput` events
   - Maintains a per-phase ring buffer of recent stderr lines (last ~20 lines)
   - On a configurable interval (default 8 seconds), if new lines have arrived since last digest:
     - Sends the buffered lines to Haiku with a system prompt: "Summarize what this AI coding agent is currently doing in one short sentence. Be specific about file names and actions. No preamble."
     - Publishes the result as a new `KindPhaseActivitySummary` event (add this Kind to `bus.go`)
   - Runs in its own goroutine, stopped via context cancellation

3. **Haiku invocation**:
   - Use the existing `claude.Invoker` with a lightweight agent config: model=`claude-haiku-4-5-20251001`, effort=`low`, max budget per call ~$0.001
   - Or if simpler, use a direct API call via the Anthropic SDK. But prefer reusing the existing invoker to avoid adding dependencies.
   - If the Haiku call fails or times out (5s), skip — the next interval will try again

4. **New bus event kind**: Add `KindPhaseActivitySummary Kind = "phase.activity.summary"` to `bus.go`. The `Message` field carries the one-line summary. `PhaseID` identifies which phase.

5. **Integration**: The digester is created and started alongside other bus subscribers in the nebula engine or CLI adapter. For single-task mode, it works the same way with `KindAgentOutput`.

### Cost control

- Haiku is ~$0.25/M input tokens, ~$1.25/M output tokens
- 20 stderr lines ≈ 500 tokens input, ~20 tokens output ≈ $0.0002 per digest
- At 8-second intervals over a 5-minute phase = ~37 digests = ~$0.007 per phase
- Negligible relative to the Opus/Sonnet costs of the actual agent work

## Files

- `internal/bus/bus.go` — add `KindPhaseActivitySummary` and `KindActivitySummary` event kinds
- `internal/digester/digester.go` — new digester subscriber
- `internal/digester/digester_test.go` — tests with mock bus and mock invoker

## Acceptance Criteria

- [ ] Digester subscribes to agent output events and batches per phase
- [ ] Periodically invokes Haiku to produce a one-line activity summary
- [ ] Publishes `KindPhaseActivitySummary` events to the bus
- [ ] Gracefully handles Haiku failures (skip and retry next interval)
- [ ] Does not block or slow down phase execution
- [ ] Cleans up goroutines on context cancellation
- [ ] `go test ./internal/digester/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
