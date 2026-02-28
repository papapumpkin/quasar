+++
id = "complexity-scorer"
title = "Implement phase complexity scorer from structural signals"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Quasar currently assigns the same model to every phase regardless of complexity. A one-line config tweak and a multi-file architectural refactor both consume the same expensive model. We need a deterministic scoring function that quantifies phase complexity from signals already available in the `PhaseSpec` and DAG at resolution time.

## Solution

Add a new file `internal/nebula/complexity.go` containing a `ComplexityScore` function and supporting types.

### Signals

Four orthogonal signals, each normalized to `[0.0, 1.0]`:

| Signal | Source | Interpretation |
|--------|--------|----------------|
| **Scope breadth** | `len(phase.Scope)` | More glob patterns = wider blast radius |
| **Body length** | `len(phase.Body)` (rune count) | Longer descriptions correlate with more nuanced work |
| **Dependency depth** | `len(dag.Ancestors(phase.ID))` via `*dag.DAG` | Deep dependency chains imply coordination complexity |
| **Task type weight** | `phase.Type` mapped to a weight | `"bug"` and `"task"` are lighter; `"feature"` is heavier |

### Types

```go
// ComplexitySignals holds the raw inputs used to compute a complexity score.
type ComplexitySignals struct {
    ScopeCount    int    // len(phase.Scope)
    BodyLength    int    // len([]rune(phase.Body))
    DepthCount    int    // len(dag.Ancestors(phase.ID))
    TaskType      string // phase.Type
}

// ComplexityResult holds the computed score and the contributing signal weights.
type ComplexityResult struct {
    Score         float64            // composite score in [0.0, 1.0]
    Signals       ComplexitySignals  // raw inputs for traceability
    Contributions map[string]float64 // per-signal weighted contribution
}
```

### Scoring Function

```go
// ScoreComplexity computes a composite complexity score for a phase.
// The score is in [0.0, 1.0] where 0 is trivial and 1 is maximally complex.
func ScoreComplexity(signals ComplexitySignals) ComplexityResult
```

Weighted sum with saturation clamps:

- **Scope breadth (weight 0.25)**: `min(scopeCount / 10.0, 1.0)` -- saturates at 10 patterns.
- **Body length (weight 0.35)**: `min(bodyLength / 3000.0, 1.0)` -- saturates at 3000 runes.
- **Dependency depth (weight 0.25)**: `min(depthCount / 8.0, 1.0)` -- saturates at 8 ancestors.
- **Task type (weight 0.15)**: `{"task": 0.3, "bug": 0.4, "feature": 0.8}` with fallback `0.5`.

The final score is the weighted sum of these four normalized values.

### Helper

```go
// BuildComplexitySignals extracts signals from a PhaseSpec and a DAG.
// The DAG parameter may be nil (depth contribution becomes 0).
func BuildComplexitySignals(phase *PhaseSpec, d *dag.DAG) ComplexitySignals
```

This function reads `phase.Scope`, `phase.Body`, `phase.Type`, and calls `d.Ancestors(phase.ID)` to compute depth. If `d` is nil, `DepthCount` is 0.

## Files

- `internal/nebula/complexity.go` -- `ComplexitySignals`, `ComplexityResult`, `ScoreComplexity`, `BuildComplexitySignals`
- `internal/nebula/complexity_test.go` -- table-driven tests covering edge cases (empty phase, max saturation, nil DAG, unknown type)

## Acceptance Criteria

- [ ] `ScoreComplexity` returns a value in `[0.0, 1.0]` for all valid inputs
- [ ] Each signal clamps correctly at its saturation point
- [ ] Unknown task types fall back to the default weight (0.5)
- [ ] `BuildComplexitySignals` handles a nil `*dag.DAG` without panicking
- [ ] `ComplexityResult.Contributions` map contains all four signal keys
- [ ] `go test ./internal/nebula/...` passes with at least 8 test cases covering boundary conditions
