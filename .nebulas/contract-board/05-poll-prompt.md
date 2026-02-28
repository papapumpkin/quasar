+++
id = "poll-prompt"
title = "LLM polling prompt and structured response parser"
type = "feature"
priority = 2
depends_on = ["contract-model", "polling-state"]
scope = ["internal/board/llmpoller.go", "internal/board/llmpoller_test.go"]
+++

## Problem

The Poller interface needs a concrete implementation. The polling decision — "does this phase have enough context from the board to proceed?" — requires understanding both the phase's requirements and the available contracts. This is a semantic judgment best handled by a single LLM call with a structured prompt.

## Solution

Implement `LLMPoller` that satisfies the `Poller` interface by making one LLM call per poll:

### Prompt Template

```
You are evaluating whether a coding task has sufficient context to begin.

## Your Task
{phase.Body}

## Available Context (Board State)
{RenderSnapshot(boardSnapshot)}

## Instructions
Based on the task description and the available contracts/interfaces on the board, determine if you can proceed.

Respond with EXACTLY ONE of these decisions:

PROCEED — You have all the interfaces, types, and function signatures needed to write compilable code. Minor uncertainty about implementation details is fine — proceed with your best judgment and the reviewer will catch issues.

NEED_INFO — You literally cannot write compilable code without a missing type, interface, or function signature. Specify exactly what's missing.

CONFLICT — A contract on the board contradicts another contract, or a file you need to modify is actively claimed by a running phase. Specify the conflict.

IMPORTANT: Only respond NEED_INFO or CONFLICT for structural blockers. If you're unsure about a return type or implementation detail but could make a reasonable assumption, respond PROCEED.

Your response (PROCEED, NEED_INFO, or CONFLICT followed by explanation):
```

### Response Parser

Parse the LLM response into a `PollResult`:
1. Extract the first word as the decision (PROCEED / NEED_INFO / CONFLICT)
2. The remainder is the reason
3. For NEED_INFO, extract bulleted items as `MissingInfo`
4. For CONFLICT, extract the conflicting phase ID or file as `ConflictWith`

### LLMPoller Implementation

```go
// LLMPoller uses an LLM call to evaluate board readiness for a phase.
type LLMPoller struct {
    Invoker agent.Invoker
    Phases  map[string]*PhaseSpec // phase specs for body lookup
}

func (p *LLMPoller) Poll(ctx context.Context, phaseID string, snap BoardSnapshot) (PollResult, error)
```

The poll call uses the same `agent.Invoker` as the main loop, keeping model configuration consistent. The prompt is ~500 tokens; the response is ~50 tokens. Total cost per poll is negligible.

## Files

- `internal/board/llmpoller.go` — LLMPoller struct, prompt builder, response parser
- `internal/board/llmpoller_test.go` — Tests with mock Invoker, covers PROCEED/NEED_INFO/CONFLICT parsing

## Acceptance Criteria

- [ ] LLMPoller satisfies the Poller interface
- [ ] Prompt includes the phase body and rendered board snapshot
- [ ] Prompt explicitly instructs to only block on structural issues, not uncertainty
- [ ] Response parser correctly extracts PROCEED, NEED_INFO, CONFLICT decisions
- [ ] NEED_INFO responses populate MissingInfo with specific items
- [ ] CONFLICT responses populate ConflictWith with the conflicting phase or file
- [ ] Malformed LLM responses default to PROCEED (fail-open, not fail-closed)
- [ ] `go test ./internal/board/...` passes
