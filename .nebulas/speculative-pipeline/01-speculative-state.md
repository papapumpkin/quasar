+++
id = "speculative-state"
title = "Add speculative execution state tracking primitives"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/nebula/types.go", "internal/nebula/tracker.go", "internal/fabric/fabric.go", "internal/loop/state.go"]
+++

## Problem

The current phase execution model in Quasar is strictly sequential within a dependency chain: Phase N+1 cannot begin coding until Phase N's reviewer approves. This is correct but leaves compute idle during the review window. To enable speculative pipelining, the system needs state primitives that distinguish between confirmed and speculative execution, track which phases are running speculatively, and record enough context to roll back speculative work when a review rejects.

Today, `PhaseTracker` in `internal/nebula/tracker.go` tracks three maps — `done`, `failed`, and `inFlight` — with no concept of speculative vs. confirmed work. The fabric phase states in `internal/fabric/fabric.go` (`StateQueued`, `StateScanning`, `StateRunning`, `StateBlocked`, `StateDone`, `StateFailed`, `StateHumanDecision`) have no speculative variant. The `CycleState` in `internal/loop/state.go` has no mechanism to snapshot and restore itself for rollback purposes.

Without these foundational types, the scheduler, worker dispatch, and rollback mechanisms in subsequent phases have nothing to build on.

## Solution

### 1. Add `StateSpeculative` to fabric phase states

In `internal/fabric/fabric.go`, add a new constant alongside the existing states:

```go
const (
    StateQueued         = "QUEUED"
    StateScanning       = "SCANNING"
    StateRunning        = "RUNNING"
    StateSpeculative    = "SPECULATIVE"  // new: running ahead of confirmed dependency
    StateBlocked        = "BLOCKED"
    StateDone           = "DONE"
    StateFailed         = "FAILED"
    StateHumanDecision  = "HUMAN_DECISION_REQUIRED"
)
```

This state is set on the fabric when a phase is dispatched speculatively. It behaves like `StateRunning` for scheduling purposes but signals to the TUI and metrics that the work is tentative.

### 2. Extend `PhaseTracker` with speculative tracking

Add a `speculative` map and a `speculativeContext` map to `PhaseTracker`:

```go
type SpeculativeContext struct {
    DependsOnPhaseID string    // the phase whose review we're speculating past
    BaseCommitSHA    string    // git HEAD before speculative work began
    StartedAt        time.Time // when speculative execution began
}

type PhaseTracker struct {
    phasesByID  map[string]*PhaseSpec
    done        map[string]bool
    failed      map[string]bool
    inFlight    map[string]bool
    speculative map[string]bool              // new: tracks which in-flight phases are speculative
    specCtx     map[string]*SpeculativeContext // new: rollback context per speculative phase
}
```

Add methods:

```go
// MarkSpeculative records a phase as speculatively dispatched.
func (pt *PhaseTracker) MarkSpeculative(phaseID string, ctx *SpeculativeContext)

// IsSpeculative reports whether a phase is running speculatively.
func (pt *PhaseTracker) IsSpeculative(phaseID string) bool

// ConfirmSpeculative promotes a speculative phase to confirmed (removes from speculative map).
func (pt *PhaseTracker) ConfirmSpeculative(phaseID string)

// SpeculativeContext returns the rollback context for a speculative phase, or nil.
func (pt *PhaseTracker) SpeculativeContext(phaseID string) *SpeculativeContext

// SpeculativePhases returns the set of currently speculative phase IDs.
func (pt *PhaseTracker) SpeculativePhases() map[string]bool

// DiscardSpeculative marks a speculative phase as no longer in flight without recording it as done.
// The phase returns to eligible status for re-dispatch once conditions are met.
func (pt *PhaseTracker) DiscardSpeculative(phaseID string)
```

### 3. Add `PhaseStatusSpeculative` to nebula state

In `internal/nebula/types.go`, add a new `PhaseStatus` constant:

```go
const (
    PhaseStatusPending    PhaseStatus = "pending"
    PhaseStatusInProgress PhaseStatus = "in_progress"
    PhaseStatusSpeculative PhaseStatus = "speculative"  // new
    PhaseStatusDone       PhaseStatus = "done"
    PhaseStatusFailed     PhaseStatus = "failed"
    PhaseStatusSkipped    PhaseStatus = "skipped"
)
```

### 4. Add speculative configuration to `Execution` and `PhaseSpec`

In `internal/nebula/types.go`, add the opt-in flag:

```go
type Execution struct {
    MaxWorkers      int     `toml:"max_workers"`
    MaxReviewCycles int     `toml:"max_review_cycles"`
    MaxBudgetUSD    float64 `toml:"max_budget_usd"`
    Model           string  `toml:"model"`
    Gate            string  `toml:"gate"`
    Speculative     bool    `toml:"speculative"`  // new: enable speculative pipelining
    // ...
}
```

Also add `Speculative` as an optional per-phase override in `PhaseSpec`:

```go
type PhaseSpec struct {
    // ... existing fields ...
    Speculative *bool `toml:"speculative"` // new: per-phase override (nil = inherit from execution)
}
```

### 5. Add `NewPhaseTracker` initialization for new fields

Update `NewPhaseTracker` in `internal/nebula/tracker.go` to initialize the new maps:

```go
func NewPhaseTracker(phases []PhaseSpec, state *State) *PhaseTracker {
    pt := &PhaseTracker{
        phasesByID:  make(map[string]*PhaseSpec),
        done:        make(map[string]bool),
        failed:      make(map[string]bool),
        inFlight:    make(map[string]bool),
        speculative: make(map[string]bool),
        specCtx:     make(map[string]*SpeculativeContext),
    }
    // ... existing initialization ...
    return pt
}
```

## Files

- `internal/fabric/fabric.go` — Add `StateSpeculative` constant
- `internal/nebula/types.go` — Add `PhaseStatusSpeculative`, `SpeculativeContext` struct, `Speculative` field to `Execution` and `PhaseSpec`
- `internal/nebula/tracker.go` — Add `speculative`/`specCtx` maps, `MarkSpeculative`, `IsSpeculative`, `ConfirmSpeculative`, `DiscardSpeculative`, `SpeculativePhases`, `SpeculativeContext` methods
- `internal/nebula/parse.go` — Parse `speculative` field from TOML frontmatter and manifest
- `internal/nebula/validate.go` — Validate `speculative` field (no cycles in speculative chains)

## Acceptance Criteria

- [ ] `StateSpeculative` constant exists in `internal/fabric/fabric.go`
- [ ] `PhaseStatusSpeculative` constant exists in `internal/nebula/types.go`
- [ ] `SpeculativeContext` struct defined with `DependsOnPhaseID`, `BaseCommitSHA`, `StartedAt` fields
- [ ] `PhaseTracker` has `speculative` and `specCtx` maps initialized in constructor
- [ ] `MarkSpeculative`, `IsSpeculative`, `ConfirmSpeculative`, `DiscardSpeculative`, `SpeculativePhases`, `SpeculativeContext` methods implemented
- [ ] `Execution.Speculative` and `PhaseSpec.Speculative` fields parse from TOML
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] `go test ./internal/nebula/...` passes with new tracker tests
- [ ] Existing tests unaffected — all maps default to empty, speculative is off by default
