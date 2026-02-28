+++
id = "tycho-speculation"
title = "Teach Tycho scheduler to identify and yield speculative candidates"
type = "feature"
priority = 2
depends_on = ["speculative-state"]
scope = ["internal/tycho/tycho.go", "internal/tycho/speculative.go", "internal/tycho/tycho_test.go"]
+++

## Problem

The Tycho `Scheduler` currently only yields phases whose DAG dependencies are fully satisfied (via `EligibleResolver.ResolveEligible()`). When speculative pipelining is enabled, the scheduler needs a second resolution pass that identifies phases whose dependencies are *almost* satisfied — specifically, phases where the only unsatisfied dependency is a phase currently in the `PhaseReviewing` stage of its coder-reviewer loop. These are the speculative candidates: phases that can begin coding on the assumption the in-flight review will pass.

The `Scheduler.Eligible()` method currently returns a flat `[]string` of phase IDs. It has no concept of "eligible but speculative." The `EligibleResolver` interface defines `ResolveEligible() []string` and `AnyInFlight() bool`, neither of which distinguishes review-phase dependencies from coding-phase dependencies.

Without this, the worker dispatch loop cannot identify which phases to speculatively schedule.

## Solution

### 1. Extend `EligibleResolver` with speculative resolution

Add a new method to the `EligibleResolver` interface in `internal/tycho/tycho.go`:

```go
type EligibleResolver interface {
    ResolveEligible() []string
    AnyInFlight() bool

    // ResolveSpeculative returns phase IDs that would be eligible if their
    // sole remaining dependency (currently in review) passes. Each returned
    // ID is paired with the dependency phase ID being speculated past.
    // Implementations should only return candidates when speculative mode
    // is enabled and the dependency phase is in PhaseReviewing or later
    // (but not yet PhaseApproved).
    ResolveSpeculative() []SpeculativeCandidate
}

// SpeculativeCandidate pairs a phase ID with the dependency it speculates past.
type SpeculativeCandidate struct {
    PhaseID      string // the phase to speculatively schedule
    SpeculatesOn string // the dependency phase currently in review
}
```

### 2. Add `SpeculativeEligible` method to `Scheduler`

Create a new method on `Scheduler` that wraps the resolver's speculative resolution:

```go
// SpeculativeEligible returns phases that can be speculatively dispatched.
// A phase is speculatively eligible when its only unsatisfied dependency is
// currently in the review stage of its coder-reviewer loop. Returns nil
// when no Resolver is configured or when speculative mode is disabled.
func (s *Scheduler) SpeculativeEligible(_ context.Context) []SpeculativeCandidate {
    if s.Resolver == nil {
        return nil
    }
    return s.Resolver.ResolveSpeculative()
}
```

### 3. Implement `ResolveSpeculative` in `workerEligibleResolver`

In `internal/nebula/worker_fabric.go`, implement the new method on `workerEligibleResolver`. The logic:

1. Check if speculative mode is enabled in the nebula manifest (`wg.Nebula.Manifest.Execution.Speculative`).
2. For each phase not yet done, not in-flight, and not failed:
   - Compute its unsatisfied dependencies (deps not in `done`).
   - If exactly one unsatisfied dependency remains, and that dependency is in-flight with its loop in `PhaseReviewing` or `PhaseReviewComplete`:
     - The phase is a speculative candidate.
3. Filter through tracker (not already speculative, no scope conflicts).
4. Sort by impact score (reuse existing `scheduler.Analyzer()` scoring).

```go
func (r *workerEligibleResolver) ResolveSpeculative() []tycho.SpeculativeCandidate {
    if !r.wg.Nebula.Manifest.Execution.Speculative {
        return nil
    }

    done := r.wg.tracker.Done()
    inFlight := r.wg.tracker.InFlight()
    failed := r.wg.tracker.Failed()
    speculative := r.wg.tracker.SpeculativePhases()
    dagGraph := r.scheduler.Analyzer().DAG()

    var candidates []tycho.SpeculativeCandidate
    for _, phase := range r.wg.Nebula.Phases {
        id := phase.ID
        if done[id] || inFlight[id] || failed[id] || speculative[id] {
            continue
        }

        // Find unsatisfied deps.
        var unsatisfied []string
        for _, dep := range dagGraph.DepsFor(id) {
            if !done[dep] {
                unsatisfied = append(unsatisfied, dep)
            }
        }

        // Exactly one unsatisfied dep, and it must be in review.
        if len(unsatisfied) != 1 {
            continue
        }
        depID := unsatisfied[0]
        if !inFlight[depID] {
            continue
        }

        // Check if the dep's loop is in reviewing phase.
        if r.wg.isPhaseReviewing(depID) {
            candidates = append(candidates, tycho.SpeculativeCandidate{
                PhaseID:      id,
                SpeculatesOn: depID,
            })
        }
    }
    return candidates
}
```

### 4. Add review-phase detection to `WorkerGroup`

The `WorkerGroup` needs a way to check if a phase's coder-reviewer loop is currently in the review stage. Add a registration mechanism:

```go
// phaseLoopState tracks the current loop phase of an in-flight phase.
// Updated by the PhaseUIBridge or directly by the loop adapter.
type phaseLoopState struct {
    Phase loop.Phase
}

// RegisterPhaseState records the current loop phase for an in-flight phase.
func (wg *WorkerGroup) RegisterPhaseState(phaseID string, phase loop.Phase)

// isPhaseReviewing reports whether the given phase is in PhaseReviewing or PhaseReviewComplete.
func (wg *WorkerGroup) isPhaseReviewing(phaseID string) bool
```

The `PhaseUIBridge` (or the loop adapter) calls `RegisterPhaseState` whenever the loop transitions. This is a lightweight update — just storing the current `loop.Phase` enum value per phase ID in a `sync.Map` or mutex-protected map on the `WorkerGroup`.

### 5. Tests

Create `internal/tycho/speculative_test.go` with table-driven tests:

- **Test single-dep-in-review**: Phase B depends on A. A is in `PhaseReviewing`. B should be a speculative candidate.
- **Test multi-dep**: Phase C depends on A and B. Only A is in review. C should NOT be a candidate (two unsatisfied deps).
- **Test dep-not-reviewing**: Phase B depends on A. A is in `PhaseCoding`. B should NOT be a candidate.
- **Test already-speculative**: Phase B is already marked speculative. B should not appear again.
- **Test speculative-disabled**: `Speculative = false`. No candidates returned.

## Files

- `internal/tycho/tycho.go` — Extend `EligibleResolver` interface with `ResolveSpeculative() []SpeculativeCandidate`, add `SpeculativeCandidate` struct, add `Scheduler.SpeculativeEligible()` method
- `internal/tycho/speculative_test.go` — Table-driven tests for speculative candidate resolution
- `internal/nebula/worker_fabric.go` — Implement `ResolveSpeculative` on `workerEligibleResolver`
- `internal/nebula/worker.go` — Add `phaseLoopStates` map to `WorkerGroup`, add `RegisterPhaseState`, `isPhaseReviewing` methods

## Acceptance Criteria

- [ ] `EligibleResolver` interface has `ResolveSpeculative() []SpeculativeCandidate` method
- [ ] `SpeculativeCandidate` struct defined with `PhaseID` and `SpeculatesOn` fields
- [ ] `Scheduler.SpeculativeEligible()` delegates to resolver and returns nil when resolver is nil
- [ ] `workerEligibleResolver.ResolveSpeculative` returns candidates only when `Speculative = true`
- [ ] Candidates have exactly one unsatisfied dependency that is currently in review
- [ ] Already-speculative phases are excluded from candidate list
- [ ] `WorkerGroup.RegisterPhaseState` and `isPhaseReviewing` work correctly
- [ ] All new code has test coverage in `speculative_test.go`
- [ ] `go test ./internal/tycho/...` passes
- [ ] `go test ./internal/nebula/...` passes
- [ ] Existing `EligibleResolver` implementations still compile (interface is backward-compatible or updated)
