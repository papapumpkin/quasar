+++
id = "pushback-handler"
title = "Route pushback to auto-retry or human escalation"
type = "feature"
priority = 2
depends_on = ["poll-prompt"]
scope = ["internal/board/pushback.go", "internal/board/pushback_test.go"]
+++

## Problem

When a phase polls the board and gets NEED_INFO or CONFLICT, the system needs to decide what to do. Some pushback is transient — the missing contract will appear when an in-progress phase completes. Other pushback is permanent — the decomposition missed a dependency, or two contracts genuinely conflict. The system needs to distinguish these cases and route appropriately.

## Solution

Implement a `PushbackHandler` that manages the lifecycle of blocked phases:

### Decision Logic

**NEED_INFO pushback:**
1. Check if any currently in-progress phase could plausibly produce the missing info (by checking its scope or phase spec against the requested symbols).
2. If yes: mark as BLOCKED with auto-retry. When that phase completes and publishes contracts, re-poll.
3. If no in-progress phase could provide it: check if the missing info exists in the codebase already (it might be an existing symbol the contract publisher didn't capture).
4. If still missing: escalate to HUMAN_DECISION_REQUIRED. The decomposition likely missed a dependency.

**CONFLICT pushback:**
1. File conflict (file claimed by running phase): mark as BLOCKED, auto-retry when the claiming phase completes and releases its claims.
2. Interface conflict (contradictory contracts from different phases): escalate to HUMAN_DECISION_REQUIRED immediately. This requires human judgment.

### Auto-Retry

```go
// PushbackHandler decides how to handle phases that push back during polling.
type PushbackHandler struct {
    Board       Board
    MaxRetries  int           // max auto-retries before escalating (default: 3)
    RetryDelay  time.Duration // minimum time between retries (default: 0, immediate on board change)
}

// Handle processes a PollResult for a blocked phase and returns the action to take.
func (h *PushbackHandler) Handle(ctx context.Context, bp *BlockedPhase, inProgress []string) PushbackAction

type PushbackAction string

const (
    ActionRetry    PushbackAction = "retry"     // re-poll after board changes
    ActionEscalate PushbackAction = "escalate"  // surface to human
    ActionProceed  PushbackAction = "proceed"   // override and start anyway
)
```

### Escalation

When escalating, the handler produces a structured message for the human:

```
PHASE BLOCKED: {phaseID}
Reason: {NEED_INFO or CONFLICT}
Details: {reason from PollResult}
Retries: {count}/{max}
Suggestion: {add dependency / resolve conflict / override}
```

This integrates with the existing `Gater` / `HUMAN_DECISION_REQUIRED` mechanism — the handler doesn't implement the UI, it produces the signal.

## Files

- `internal/board/pushback.go` — PushbackHandler, PushbackAction, escalation message builder
- `internal/board/pushback_test.go` — Tests for retry logic, escalation thresholds, conflict routing

## Acceptance Criteria

- [ ] NEED_INFO with a plausible in-progress producer results in ActionRetry
- [ ] NEED_INFO with no plausible producer results in ActionEscalate after MaxRetries
- [ ] CONFLICT on file claims results in ActionRetry (wait for release)
- [ ] CONFLICT on contradictory contracts results in ActionEscalate immediately
- [ ] Retry count is tracked per blocked phase and resets on successful poll
- [ ] Escalation message includes phase ID, reason, retry count, and suggestion
- [ ] MaxRetries defaults to 3
- [ ] `go test ./internal/board/...` passes
