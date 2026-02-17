+++
id = "nebula-extract-phases-by-id"
title = "Extract shared phasesByID helper to eliminate repeated map construction"
type = "task"
priority = 2
scope = ["internal/nebula/types.go", "internal/nebula/worker.go", "internal/nebula/apply.go", "internal/nebula/checkpoint.go", "internal/nebula/architect.go"]
+++

## Problem

Multiple files in the `nebula` package independently build `map[string]*Phase` from a `[]*Phase` slice:

1. **`worker.go`** `initPhaseState()` — builds the map to initialize worker state
2. **`apply.go`** `Apply()` — builds the map for phase lookup during apply
3. **`checkpoint.go`** `BuildCheckpoint()` — does a linear scan (`findPhase` helper) instead of a map, which is O(n) per lookup
4. **`architect.go`** `buildArchitectPrompt()` — builds a map for prompt construction

This repeated logic is error-prone (a new caller might forget to handle duplicates) and the linear scan in `checkpoint.go` is a minor performance concern for large nebulae.

## Solution

Add a shared helper function in `types.go` (where `Phase` is defined):

```go
// PhasesByID returns a map from phase ID to phase pointer for quick lookup.
func PhasesByID(phases []*Phase) map[string]*Phase {
    m := make(map[string]*Phase, len(phases))
    for _, p := range phases {
        m[p.ID] = p
    }
    return m
}
```

Then replace all four inline map constructions and the linear scan with calls to `PhasesByID`.

## Files

- `internal/nebula/types.go` — add `PhasesByID` helper
- `internal/nebula/worker.go` — use `PhasesByID` in `initPhaseState`
- `internal/nebula/apply.go` — use `PhasesByID` in `Apply`
- `internal/nebula/checkpoint.go` — replace `findPhase` linear scan with `PhasesByID`
- `internal/nebula/architect.go` — use `PhasesByID` in `buildArchitectPrompt`

## Acceptance Criteria

- [ ] Single `PhasesByID` helper exists in `types.go`
- [ ] All four call sites use the shared helper
- [ ] `findPhase` linear scan in `checkpoint.go` is removed
- [ ] `go test ./...` passes
