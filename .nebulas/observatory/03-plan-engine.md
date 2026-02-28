+++
id = "plan-engine"
title = "Plan engine combining DAG + static contracts"
type = "feature"
priority = 1
depends_on = ["static-scanner"]
scope = ["internal/nebula/plan_engine.go"]
+++

## Problem

The existing `BuildPlan` in `internal/nebula/plan.go` only determines bead create/update/close/skip actions — it's about bead lifecycle, not execution strategy. And the DAG engine in `internal/dag/` computes topological order, waves, and tracks — but knows nothing about entanglements.

To support a terraform-style plan, we need an engine that combines:
1. DAG structure (dependency order, waves, tracks, impact scores)
2. Static contracts (what each phase produces/consumes)
3. Conflict detection (scope overlaps, missing producers, disputed entanglements)

into a single `ExecutionPlan` that can be rendered, diffed, and gated before apply.

## Solution

### 1. New file: `internal/nebula/plan_engine.go`

```go
// PlanEngine combines DAG analysis with static entanglement contracts
// to produce a pre-execution plan that shows the full contract graph,
// identifies risks, and gates execution.
type PlanEngine struct {
    Scanner *fabric.StaticScanner
}

// ExecutionPlan is the output of the plan engine — a complete picture
// of what will happen during apply.
type ExecutionPlan struct {
    Name        string                    // nebula name
    Waves       []dag.Wave                // execution wave ordering
    Tracks      []dag.Track               // independent parallel tracks
    Contracts   []fabric.PhaseContract    // per-phase produces/consumes
    Report      *fabric.ContractReport    // fulfilled/missing/conflicting
    ImpactOrder []string                  // phases sorted by impact score
    Risks       []PlanRisk                // aggregated risks
    Stats       PlanStats                 // summary statistics
}

type PlanRisk struct {
    Severity string // "error", "warning", "info"
    PhaseID  string
    Message  string
}

type PlanStats struct {
    TotalPhases     int
    TotalWaves      int
    TotalTracks     int
    ParallelFactor  int     // max concurrent phases possible
    FulfilledContracts int
    MissingContracts   int
    Conflicts          int
    EstimatedCost      float64 // from budget limits
}

// Plan runs static analysis and produces an ExecutionPlan without
// executing any phases.
func (pe *PlanEngine) Plan(n *Nebula) (*ExecutionPlan, error)
```

### 2. Plan computation flow

1. Build DAG from phases (reuse `NewScheduler`)
2. Compute waves, tracks, and impact scores
3. Run `StaticScanner.Scan()` on all phases
4. Run `ResolveContracts()` to check completeness
5. Aggregate risks:
   - Missing contracts -> error
   - Scope overlaps without `allow_scope_overlap` -> error
   - Single-track bottleneck with `max_workers > 1` -> warning
   - Phase with no produces and no consumes -> info (leaf node)
6. Compute stats
7. Return `ExecutionPlan`

### 3. Plan diffing

When a plan has been run before (stored as `.nebulas/<name>/<name>.plan.json`), the engine can diff against the previous plan to show what changed:

```go
// Diff compares two plans and returns human-readable changes.
func Diff(old, new *ExecutionPlan) []PlanChange

type PlanChange struct {
    Kind    string // "added", "removed", "changed"
    Subject string // phase ID or contract description
    Detail  string
}
```

### 4. Structured output

The `ExecutionPlan` should be JSON-serializable so agents can consume it:

```go
// Save writes the plan to a JSON file.
func (ep *ExecutionPlan) Save(path string) error

// LoadPlan reads a previously saved plan.
func LoadPlan(path string) (*ExecutionPlan, error)
```

## Files

- `internal/nebula/plan_engine.go` — `PlanEngine`, `ExecutionPlan`, `Plan()`, `Diff()`, `Save()`/`LoadPlan()`
- `internal/nebula/plan_engine_test.go` — Tests for plan computation, risk detection, diffing

## Acceptance Criteria

- [ ] `PlanEngine.Plan()` produces an `ExecutionPlan` with waves, tracks, contracts, and risks
- [ ] Missing contracts are surfaced as error-severity risks
- [ ] Scope overlaps without `allow_scope_overlap` are surfaced as errors
- [ ] Plan is JSON-serializable and can be saved/loaded
- [ ] Diffing two plans produces meaningful change descriptions
- [ ] Stats accurately summarize the plan (phase count, parallel factor, contract counts)
- [ ] `go vet ./...` passes
- [ ] Table-driven tests cover: simple linear chain, diamond dependency, parallel tracks, missing contracts, scope conflicts
