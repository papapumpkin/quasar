+++
id = "model-tiers"
title = "Define model tier registry with configurable thresholds"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

To route phases to different models based on complexity, we need a mapping from score ranges to model identifiers. This mapping must be configurable at the nebula manifest level so operators can swap models without code changes, while providing sensible defaults.

## Solution

Add a new file `internal/nebula/tiers.go` containing the tier registry and selection logic.

### Types

```go
// ModelTier represents a named tier with a complexity threshold and model identifier.
type ModelTier struct {
    Name      string  // human-readable name: "fast", "balanced", "heavy"
    Model     string  // model identifier passed to the invoker (e.g. "claude-haiku", "claude-sonnet")
    MaxScore  float64 // phases with score <= MaxScore are routed to this tier (exclusive upper bound for all but the last tier)
}

// TierConfig holds the ordered list of tiers and the flag to enable auto-routing.
type TierConfig struct {
    Enabled bool        `toml:"enabled"`
    Tiers   []ModelTier `toml:"tiers"`
}
```

### Defaults

When `TierConfig.Tiers` is empty (or `TierConfig` is zero-value), use built-in defaults:

```go
var DefaultTiers = []ModelTier{
    {Name: "fast",     Model: "claude-haiku",  MaxScore: 0.35},
    {Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
    {Name: "heavy",    Model: "claude-opus",   MaxScore: 1.00},
}
```

### Selection Function

```go
// SelectTier picks the first tier whose MaxScore >= the given complexity score.
// Tiers must be sorted by MaxScore ascending. If no tier matches (should not
// happen with a 1.0 ceiling), the last tier is returned as a fallback.
func SelectTier(score float64, tiers []ModelTier) ModelTier
```

### Manifest Integration

Extend the `Execution` struct in `internal/nebula/types.go` with an optional `Routing` field:

```go
Execution struct {
    // ... existing fields ...
    Routing TierConfig `toml:"routing"` // Auto-routing config. Zero-value = disabled.
}
```

This allows nebula authors to opt in:

```toml
[execution.routing]
enabled = true

[[execution.routing.tiers]]
name = "fast"
model = "claude-haiku"
max_score = 0.35

[[execution.routing.tiers]]
name = "balanced"
model = "claude-sonnet"
max_score = 0.70

[[execution.routing.tiers]]
name = "heavy"
model = "claude-opus"
max_score = 1.00
```

### Validation

Add a `ValidateRouting` function that checks:
- Tiers are sorted by `MaxScore` ascending
- The last tier has `MaxScore >= 1.0`
- No duplicate tier names
- All `Model` fields are non-empty

Wire this into the existing nebula validation path.

## Files

- `internal/nebula/tiers.go` -- `ModelTier`, `TierConfig`, `DefaultTiers`, `SelectTier`, `ValidateRouting`
- `internal/nebula/tiers_test.go` -- table-driven tests for `SelectTier` (exact boundary, below, above, fallback) and `ValidateRouting` (happy path, unsorted, missing model, duplicate names)
- `internal/nebula/types.go` -- add `Routing TierConfig` field to `Execution` struct

## Acceptance Criteria

- [ ] `SelectTier` returns the correct tier for scores at exact boundaries (0.35, 0.70, 1.0)
- [ ] `SelectTier` returns the last tier as fallback for scores above all thresholds
- [ ] `DefaultTiers` provides three tiers covering the full `[0.0, 1.0]` range
- [ ] `ValidateRouting` rejects unsorted tiers, duplicate names, and empty model fields
- [ ] `Execution.Routing` deserializes correctly from TOML with custom tiers
- [ ] `go test ./internal/nebula/...` passes
