+++
id = "polling-state"
title = "Add POLLING and BLOCKED states to worker dispatch"
type = "feature"
priority = 1
depends_on = ["board-store"]
scope = ["internal/board/poller.go", "internal/board/poller_test.go"]
+++

## Problem

The current worker dispatch in `WorkerGroup.Run` has a simple lifecycle: a phase becomes ready (deps satisfied), gets dispatched to a worker, runs its coder-reviewer loop, and completes. There's no checkpoint between "deps satisfied" and "start working" where the worker can inspect the board and decide whether it has enough context to proceed.

The conversation identified a new state: **POLLING** — where the worker ingests the current board state, checks if it has everything it needs, and either transitions to RUNNING or BLOCKED.

## Solution

Define the polling state machine and the `Poller` that manages it:

### State Transitions

```
QUEUED → POLLING → RUNNING → DONE
              ↓
           BLOCKED → (auto-retry on board change) → POLLING
              ↓
         HUMAN_DECISION (if stuck after N retries)
```

### Poller Interface

```go
// PollResult represents the outcome of a phase polling the board.
type PollResult struct {
    Decision    PollDecision
    Reason      string   // human-readable explanation
    MissingInfo []string // what's needed (for NEED_INFO)
    ConflictWith string  // phase ID causing conflict (for CONFLICT)
}

type PollDecision string

const (
    PollProceed  PollDecision = "PROCEED"   // board has everything needed
    PollNeedInfo PollDecision = "NEED_INFO" // missing contracts, can't compile
    PollConflict PollDecision = "CONFLICT"  // file or interface conflict detected
)

// Poller evaluates whether a phase has sufficient board context to proceed.
type Poller interface {
    Poll(ctx context.Context, phaseID string, snap BoardSnapshot) (PollResult, error)
}
```

### Blocked Phase Tracking

```go
// BlockedPhase tracks a phase that is waiting for more board context.
type BlockedPhase struct {
    PhaseID    string
    BlockedAt  time.Time
    RetryCount int
    LastResult PollResult
}
```

The `Poller` is called by the dispatch loop after a phase's DAG dependencies are satisfied but before it's handed to a worker goroutine. The poll is cheap — it reads the board snapshot and makes a decision. If the decision is PROCEED, dispatch continues normally. If NEED_INFO or CONFLICT, the phase enters BLOCKED and is tracked for auto-retry.

## Files

- `internal/board/poller.go` — PollResult, PollDecision, Poller interface, BlockedPhase tracker
- `internal/board/poller_test.go` — Tests for state transitions and blocked phase tracking

## Acceptance Criteria

- [ ] PollDecision has PROCEED, NEED_INFO, CONFLICT variants
- [ ] PollResult carries structured reason, missing info, and conflict details
- [ ] BlockedPhase tracks retry count and last result
- [ ] Poller interface is defined for pluggable implementations
- [ ] State constants align with board table's state column
- [ ] `go test ./internal/board/...` passes
