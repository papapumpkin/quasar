+++
id = "soft-dag"
title = "Soften DAG gate when fabric is active"
type = "feature"
priority = 1
depends_on = ["wave-scanner"]
scope = ["internal/nebula/worker_fabric.go", "internal/nebula/scheduler.go"]
+++

## Problem

`workerEligibleResolver.ResolveEligible()` calls `scheduler.ReadyTasks(done)`, which requires ALL `depends_on` phases to be in the done set before a phase is even considered for dispatch. This is a hard DAG wall — the fabric is never consulted for phases whose upstream hasn't formally completed.

But with the contract poller and wave scanner in place, the fabric can determine readiness on its own. If `spacetime-model` has published `SpacetimeManifest` to the board but its reviewer is still iterating (not formally "done"), `nebula-scanner` could already see that entanglement and start working. The hard DAG gate prevents this.

The DAG should become an **ordering hint** when fabric is active, not a hard wall. The wave scanner already walks the DAG layer by layer and prunes downstream when contracts aren't fulfilled — that's the safety mechanism. The DAG doesn't need to be a separate gate on top of that.

## Solution

### 1. Add `AllPending` to Scheduler

```go
// AllPending returns all phase IDs that are not in the done set,
// sorted by impact score (highest first). Unlike ReadyTasks, this
// does not filter by DAG dependency satisfaction — all non-complete
// phases are candidates.
func (s *Scheduler) AllPending(done map[string]bool) []string {
    var pending []string
    for _, id := range s.analyzer.DAG().Nodes() {
        if !done[id] {
            pending = append(pending, id)
        }
    }
    // Sort by impact score descending.
    scores := s.scores
    sort.Slice(pending, func(i, j int) bool {
        return scores[pending[i]] > scores[pending[j]]
    })
    return pending
}
```

### 2. Update `ResolveEligible` in `worker_fabric.go`

```go
func (r *workerEligibleResolver) ResolveEligible() []string {
    done := r.wg.tracker.Done()

    var candidates []string
    if r.wg.Fabric != nil {
        // Soft DAG: all non-done phases are candidates. The wave
        // scanner and contract poller handle ordering and safety.
        candidates = r.scheduler.AllPending(done)
    } else {
        // Hard DAG: only phases with all deps satisfied (legacy).
        candidates = r.scheduler.ReadyTasks(done)
    }

    return r.wg.tracker.FilterEligible(candidates, r.scheduler.Analyzer().DAG())
}
```

### 3. Scope filtering still applies

`FilterEligible` in `tracker.go` still checks scope conflicts and failed dependencies — those remain hard gates regardless of fabric. The change is only in which phases are *candidates* for that filtering:

- **Without fabric**: Only DAG-ready phases (deps all done)
- **With fabric**: All non-done phases (the wave scanner handles DAG-aware ordering)

### 4. Impact score ordering matters more

With soft DAG, the impact score ordering becomes the primary dispatch priority. High-impact phases (bottleneck nodes identified by PageRank + Betweenness Centrality) are dispatched first, and the wave scanner ensures they're only polled when their contracts can be checked.

### 5. Remove the `StateQueued` concept

With soft DAG, the `StateQueued` / `StateScanning` distinction collapses. All non-running phases are either:
- **Scanning** — being evaluated by the wave scanner
- **Running** — executing
- **Blocked** — contracts unfulfilled, waiting for re-evaluation
- **Done** / **Failed** / **Human_Decision**

The `StateQueued` constant can remain for backward compatibility but no phase should linger in it — phases move directly to Scanning when the dispatch loop starts.

## Files

- `internal/nebula/scheduler.go` — Add `AllPending()` method
- `internal/nebula/worker_fabric.go` — Update `ResolveEligible()` to use `AllPending` when fabric is active
- `internal/nebula/scheduler_test.go` — Tests for `AllPending`
- `internal/nebula/worker_fabric_test.go` — Tests verifying soft DAG behavior

## Acceptance Criteria

- [ ] When `wg.Fabric != nil`, `ResolveEligible` returns all non-done phases (not just DAG-ready)
- [ ] When `wg.Fabric == nil`, behavior is identical to current (hard DAG gate)
- [ ] Phases are sorted by impact score in the soft DAG path
- [ ] `FilterEligible` still filters out scope conflicts and failed-dependency phases
- [ ] The wave scanner (from previous phase) correctly handles the broader candidate set without dispatching phases whose contracts aren't fulfilled
- [ ] No phase lingers in `StateQueued` when fabric is active — all non-done phases enter the scan cycle
- [ ] Table-driven tests cover: fabric-active soft DAG, fabric-nil hard DAG, mixed scope conflicts with soft DAG
- [ ] `go vet ./...` passes
- [ ] All existing tests pass unchanged
