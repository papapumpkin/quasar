+++
id = "strategy-config"
title = "Define optimization strategies and configuration"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/nebula/strategy.go", "internal/nebula/types.go"]
+++

## Problem

The adaptive controller needs a configurable objective function. Different nebulae have different priorities — a CI pipeline wants speed, a budget-constrained exploration wants cost efficiency, and a critical refactor wants quality.

## Solution

Create `internal/nebula/strategy.go` with strategy types and add config support to the manifest.

### Strategy type

```go
type ConcurrencyStrategy string

const (
    // StrategySpeed maximizes parallelism, backs off only on conflicts.
    StrategySpeed ConcurrencyStrategy = "speed"
    // StrategyCost starts conservative, increases only when phases complete
    // fast with high satisfaction.
    StrategyCost ConcurrencyStrategy = "cost"
    // StrategyQuality caps at graph width, prioritizes review satisfaction.
    StrategyQuality ConcurrencyStrategy = "quality"
    // StrategyBalanced uses AIMD — additive increase on clean waves,
    // multiplicative decrease on conflicts.
    StrategyBalanced ConcurrencyStrategy = "balanced"
)
```

### Manifest config

Add to `Execution` struct in `types.go`:

```go
Strategy ConcurrencyStrategy `toml:"strategy"` // "" = balanced (default)
```

### Strategy parameters

Each strategy maps to a set of controller parameters:

```go
// StrategyParams holds the tuning knobs for the feedback controller.
type StrategyParams struct {
    InitialWorkers   int     // starting concurrency (0 = effective parallelism)
    AdditiveIncrease int     // workers to add per clean wave
    MultiplicativeDecrease float64 // factor to reduce by on conflict (e.g. 0.5)
    ConflictThreshold int   // conflicts per wave before triggering decrease
    SatisfactionFloor string // minimum satisfaction to allow increase (e.g. "medium")
    CostCeiling       float64 // max $/phase before triggering decrease (0 = no limit)
}

func DefaultParams(strategy ConcurrencyStrategy) StrategyParams
```

### Validation

Add strategy validation to `Validate()` — unknown strategy values produce a `ValidationError`.

## Files to Modify

- `internal/nebula/strategy.go` — New file: strategy types, params, defaults
- `internal/nebula/types.go` — Add Strategy field to Execution
- `internal/nebula/validate.go` — Validate strategy field

## Acceptance Criteria

- [ ] Four strategy constants defined
- [ ] DefaultParams returns sensible defaults per strategy
- [ ] Empty strategy defaults to balanced
- [ ] Unknown strategy value fails validation
- [ ] `go build ./...` compiles
- [ ] `go vet ./...` passes
