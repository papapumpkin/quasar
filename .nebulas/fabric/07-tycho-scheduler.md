+++
id = "tycho-scheduler"
title = "Extract DAG scheduler into internal/tycho"
type = "feature"
priority = 2
depends_on = ["fabric-rename", "discovery-cli"]
scope = ["internal/tycho/**"]
+++

## Problem

The DAG scheduling logic currently lives inside `WorkerGroup.Run` and the `worker_fabric.go` helpers. This works but couples scheduling decisions with worker lifecycle management. The design calls for a dedicated `Tycho` scheduler that observes the full system state and determines what runs where next — like Tycho Brahe, the master observer who tracked positions without theorizing.

## Solution

Create `internal/tycho` with a `Scheduler` that encapsulates DAG resolution, task dispatch, blocked task re-polling, stale detection, and hail triggering.

### Scheduler type

```go
// Scheduler observes fabric state and resolves the DAG to determine
// which tasks are eligible for execution.
type Scheduler struct {
    Fabric    fabric.Fabric
    DAG       *dag.DAG
    Poller    fabric.Poller
    Blocked   *fabric.BlockedTracker
    Pushback  *fabric.PushbackHandler
    OnHail    func(phaseID string, discovery fabric.Discovery) // callback for cockpit surfacing
}
```

### Core responsibilities

**1. Eligible task resolution**

Extract the topological sort + dependency check from `WorkerGroup`:
```go
// Eligible returns task IDs that have all DAG dependencies satisfied
// and are not currently running, blocked, or done.
func (s *Scheduler) Eligible(ctx context.Context) ([]string, error)
```

**2. Scanning gate**

Extract the polling logic from `pollEligible`:
```go
// Scan evaluates whether an eligible task can proceed by checking
// fabric state (entanglements, claims). Returns tasks that passed scanning.
func (s *Scheduler) Scan(ctx context.Context, eligible []string) ([]string, error)
```

Tasks that fail scanning transition to `StateBlocked` with the poll result recorded.

**3. Blocked task re-evaluation**

Extract from `reevaluateBlocked`:
```go
// Reevaluate re-polls all blocked tasks against current fabric state.
// Tasks whose blockers are resolved transition back to eligible.
func (s *Scheduler) Reevaluate(ctx context.Context) (unblocked []string, err error)
```

**4. Stale detection**

New capability:
```go
// StaleCheck identifies tasks and claims that appear stuck.
// - Claims older than staleClaim duration with no running task
// - Tasks with no state transition in staleTask duration
func (s *Scheduler) StaleCheck(ctx context.Context, staleClaim, staleTask time.Duration) ([]StaleItem, error)

type StaleItem struct {
    Kind    string // "claim" or "task"
    ID      string // filepath or task_id
    Age     time.Duration
    Details string
}
```

**5. Hail triggering**

When a blocked task cannot be automatically resolved (pushback handler returns `ActionEscalate`), Tycho:
- Posts a discovery of the appropriate kind
- Calls `OnHail` callback to surface in the cockpit
- Transitions the task to `StateBlocked`

### WorkerGroup refactor

`WorkerGroup` delegates to `Tycho` instead of containing the logic directly:
- `WorkerGroup.scheduler *tycho.Scheduler` replaces the inline scheduling code
- The `Run` loop calls `scheduler.Eligible` → `scheduler.Scan` → dispatch worker → on completion, `scheduler.Reevaluate`
- `boardPhaseComplete` → `scheduler.PhaseComplete` which publishes entanglements and triggers re-evaluation

This is a refactor of existing working code, not new logic. The behavior must be identical — just better organized.

## Files

- `internal/tycho/tycho.go` — `Scheduler` type with Eligible, Scan, Reevaluate, StaleCheck
- `internal/tycho/tycho_test.go` — Tests using mock Fabric and DAG
- `internal/nebula/worker_fabric.go` — Refactor to delegate to Tycho

## Acceptance Criteria

- [ ] `Scheduler.Eligible` correctly resolves DAG dependencies
- [ ] `Scheduler.Scan` gates tasks through fabric polling
- [ ] `Scheduler.Reevaluate` unblocks tasks when fabric state changes
- [ ] `Scheduler.StaleCheck` identifies stuck claims and tasks
- [ ] `OnHail` callback fires on escalation
- [ ] `WorkerGroup` delegates to Tycho — no inline scheduling logic remains
- [ ] All existing worker integration tests continue to pass
- [ ] `go test ./internal/tycho/...` passes
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` clean
