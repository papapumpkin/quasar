+++
id = "worker-integration"
title = "Wire board, polling, and pushback into WorkerGroup.Run"
type = "feature"
priority = 1
depends_on = ["contract-publisher", "pushback-handler"]
scope = ["internal/nebula/worker.go", "internal/nebula/worker_board.go"]
+++

## Problem

The board store, contract publisher, polling state, and pushback handler are all independent components. They need to be wired into the existing `WorkerGroup.Run` dispatch loop so that the contract-board execution model actually runs end to end.

## Solution

Modify the `WorkerGroup` to optionally use the board when configured:

### New Fields on WorkerGroup

```go
type WorkerGroup struct {
    // ... existing fields ...
    Board     board.Board      // nil = no board (legacy behavior)
    Poller    board.Poller     // nil = skip polling (legacy behavior)
    Publisher *board.Publisher  // nil = no contract publishing
}
```

### Modified Dispatch Flow

The existing `Run` method's dispatch loop gets a polling checkpoint inserted between "deps satisfied" and "launch worker goroutine":

```
1. DAG finds ready phases (deps satisfied)
2. For each ready phase:
   a. If Board is nil: dispatch immediately (current behavior)
   b. If Board is set:
      i.   Take a board snapshot
      ii.  Call Poller.Poll(ctx, phaseID, snapshot)
      iii. If PROCEED: dispatch to worker goroutine
      iv.  If NEED_INFO/CONFLICT: hand to PushbackHandler
      v.   If ActionRetry: add to blocked set, skip dispatch
      vi.  If ActionEscalate: trigger gate signal for human decision
3. Worker goroutine runs phase (existing coder-reviewer loop)
4. On phase completion:
   a. If Publisher is set: Publisher.PublishPhase(ctx, phaseID, beforeSHA, afterSHA)
   b. Mark phase done in board
   c. Re-evaluate blocked phases: for each blocked phase, re-poll with updated snapshot
   d. Any newly PROCEED phases get dispatched
```

### Board Lifecycle

- Board is opened in `Run` before the dispatch loop starts (if configured).
- Board is closed in `Run` after all phases complete.
- The board `.db` file path is `<nebula.Dir>/<nebula.Name>.board.db`.

### Re-evaluation After Completion

The "cascading resolution" from the conversation: after every phase completion, iterate blocked phases and re-poll. This is the auto-resume mechanism. Phases that now have sufficient contracts transition from BLOCKED → POLLING → RUNNING without human intervention.

### Backward Compatibility

When `Board` is nil, the dispatch loop behaves identically to current behavior. No polling, no contracts, no blocked state. This makes the feature opt-in.

## Files

- `internal/nebula/worker_board.go` — Board-aware dispatch helpers (pollPhase, publishPhase, reevaluateBlocked)
- `internal/nebula/worker.go` — Minimal changes to Run: call board helpers at dispatch and completion points

## Acceptance Criteria

- [ ] WorkerGroup.Board/Poller/Publisher fields added (all optional, nil = disabled)
- [ ] Dispatch loop calls Poller.Poll before launching worker goroutine
- [ ] PROCEED result dispatches normally
- [ ] NEED_INFO/CONFLICT results block the phase and skip dispatch
- [ ] Phase completion triggers Publisher.PublishPhase if Publisher is set
- [ ] Phase completion triggers re-evaluation of all blocked phases
- [ ] Blocked phases that now poll PROCEED are dispatched immediately
- [ ] Escalation triggers a gate signal compatible with existing Gater
- [ ] Board=nil preserves exact current behavior (no regressions)
- [ ] `go test ./internal/nebula/...` passes
