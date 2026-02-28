+++
id = "resolve-integration"
title = "Integrate complexity scoring and tier selection into ResolveExecution"
type = "feature"
priority = 2
depends_on = ["complexity-scorer", "model-tiers"]
+++

## Problem

The scoring and tier subsystems exist independently. They need to be wired into the existing `ResolveExecution` function in `internal/nebula/config.go` so that when auto-routing is enabled and no explicit model override is set, the complexity score determines the model.

Currently, `ResolveExecution` picks the first non-empty model from the cascade: phase -> nebula -> global -> default. Auto-routing should insert between "nebula" and "global" in that cascade, but only when:
1. `Execution.Routing.Enabled` is `true` in the nebula manifest
2. The phase does not have an explicit `Model` override (`phase.Model == ""`)
3. The nebula-level `Execution.Model` does not override (it sets a blanket model for all phases)

If the nebula sets `Execution.Model`, that is an intentional blanket override and auto-routing must not fight it.

## Solution

### Extend `ResolveExecution` Signature

The function needs access to the `TierConfig` and the DAG for depth computation. Add a new options struct to avoid breaking the existing signature:

```go
// RoutingContext carries the optional data needed for adaptive model routing.
// A nil *RoutingContext disables auto-routing (backward compatible).
type RoutingContext struct {
    Routing TierConfig
    DAG     *dag.DAG // may be nil; depth signal becomes 0
}

// ResolveExecution merges config from phase -> nebula -> global, picking the
// first non-zero value. When routing is enabled and no explicit model is set,
// complexity scoring selects the model tier.
func ResolveExecution(globalCycles int, globalBudget float64, globalModel string, neb *Execution, phase *PhaseSpec, rc *RoutingContext) ResolvedExecution
```

### Resolution Logic

After applying the existing cascade (phase -> nebula -> global -> default), add a final step:

```go
// Auto-routing: if enabled, no explicit model was set at any level, and we
// have a RoutingContext, score the phase and select a tier.
if rc != nil && rc.Routing.Enabled && r.Model == "" {
    signals := BuildComplexitySignals(phase, rc.DAG)
    result := ScoreComplexity(signals)
    tiers := rc.Routing.Tiers
    if len(tiers) == 0 {
        tiers = DefaultTiers
    }
    tier := SelectTier(result.Score, tiers)
    r.Model = tier.Model
    r.RoutedTier = tier.Name
    r.ComplexityScore = result.Score
}
```

### Extend `ResolvedExecution`

Add routing metadata fields to `ResolvedExecution` in `internal/nebula/config.go`:

```go
ResolvedExecution struct {
    MaxReviewCycles int
    MaxBudgetUSD    float64
    Model           string
    RoutedTier      string  // non-empty when auto-routing selected the model
    ComplexityScore float64 // 0 when auto-routing was not applied
}
```

### Update Call Site

In `internal/nebula/worker_exec.go`, the `executePhase` method calls `ResolveExecution`. Update it to pass the `RoutingContext`:

```go
rc := &RoutingContext{
    Routing: wg.Nebula.Manifest.Execution.Routing,
    DAG:     scheduler.Analyzer().DAG(), // captured at WorkerGroup level
}
exec := ResolveExecution(wg.GlobalCycles, wg.GlobalBudget, wg.GlobalModel, &wg.Nebula.Manifest.Execution, phase, rc)
```

The `WorkerGroup` already has access to the DAG through the scheduler built in `Run()`. Store a reference to it (e.g. `wg.dag`) during `Run()` initialization so `executePhase` can use it.

### Precedence Summary

1. Phase-level `Model` (highest -- explicit pin)
2. Nebula-level `Execution.Model` (blanket override)
3. **Auto-routed model** (new -- from complexity scoring)
4. Global `--model` flag / `QUASAR_MODEL` env
5. Built-in default (empty string -- invoker picks)

## Files

- `internal/nebula/config.go` -- update `ResolveExecution` signature, add `RoutingContext`, extend `ResolvedExecution`
- `internal/nebula/config_test.go` -- add tests for auto-routing (enabled/disabled, explicit override wins, nil context, nil DAG)
- `internal/nebula/worker_exec.go` -- pass `RoutingContext` to `ResolveExecution`
- `internal/nebula/worker.go` -- store DAG reference on `WorkerGroup` during `Run()`

## Acceptance Criteria

- [ ] Auto-routing only activates when `Routing.Enabled` is `true` and no explicit model is set
- [ ] Explicit phase-level `Model` always wins over auto-routing
- [ ] Nebula-level `Execution.Model` always wins over auto-routing
- [ ] `ResolvedExecution.RoutedTier` is non-empty only when auto-routing selected the model
- [ ] Passing `nil` for `*RoutingContext` is backward compatible (no auto-routing)
- [ ] Existing `ResolveExecution` tests still pass after signature change
- [ ] At least 4 new test cases covering the routing integration
- [ ] `go test ./internal/nebula/...` passes
