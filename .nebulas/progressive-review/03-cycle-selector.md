+++
id = "cycle-selector"
title = "Implement pure function mapping cycle number to strictness tier"
type = "task"
priority = 2
depends_on = ["strictness-tiers"]
labels = ["quasar", "reviewer", "cost-optimization"]
+++

## Problem

There is no logic that maps a cycle number to a `ReviewStrictness` tier. The loop in `internal/loop/loop.go` iterates `for cycle := 1; cycle <= l.MaxCycles; cycle++` and always passes `l.ReviewPrompt` (a single static string) to `reviewerAgent()`. We need a pure function that, given the current cycle and the maximum cycle count, returns the correct `ReviewStrictness`.

## Solution

Add a `StrictnessForCycle(cycle, maxCycles int) ReviewStrictness` function in `internal/agent/reviewer.go`. The function uses the tier boundaries described in the feature spec:

- Cycles 1-2: `StrictnessLenient`
- Cycles 3-4: `StrictnessStandard`
- Cycles 5+: `StrictnessStrict`

The function should also handle the edge case where `maxCycles` is very low (e.g., 1 or 2). When the total cycle budget is small, every cycle matters, so the function should compress the tiers: if `maxCycles <= 2`, start at `StrictnessStandard`; if `maxCycles == 1`, use `StrictnessStrict` (no room for leniency).

```go
// StrictnessForCycle returns the appropriate reviewer strictness for the given
// cycle within a loop of maxCycles total. When maxCycles is small, tiers are
// compressed so the reviewer does not waste the limited budget on lenient passes.
func StrictnessForCycle(cycle, maxCycles int) ReviewStrictness {
    if maxCycles <= 1 {
        return StrictnessStrict
    }
    if maxCycles <= 2 {
        if cycle <= 1 {
            return StrictnessStandard
        }
        return StrictnessStrict
    }
    // Standard tier boundaries for maxCycles >= 3.
    switch {
    case cycle <= 2:
        return StrictnessLenient
    case cycle <= 4:
        return StrictnessStandard
    default:
        return StrictnessStrict
    }
}
```

This is a pure function with no dependencies — it takes two ints and returns a tier. This makes it trivially testable with table-driven tests.

### Tests

Add `internal/agent/reviewer_test.go` (or extend it if it exists) with a table-driven test:

```go
func TestStrictnessForCycle(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name      string
        cycle     int
        maxCycles int
        want      ReviewStrictness
    }{
        {"cycle 1 of 5", 1, 5, StrictnessLenient},
        {"cycle 2 of 5", 2, 5, StrictnessLenient},
        {"cycle 3 of 5", 3, 5, StrictnessStandard},
        {"cycle 4 of 5", 4, 5, StrictnessStandard},
        {"cycle 5 of 5", 5, 5, StrictnessStrict},
        {"cycle 6 of 7", 6, 7, StrictnessStrict},
        // Compressed tiers for small maxCycles.
        {"cycle 1 of 1", 1, 1, StrictnessStrict},
        {"cycle 1 of 2", 1, 2, StrictnessStandard},
        {"cycle 2 of 2", 2, 2, StrictnessStrict},
        // Edge: maxCycles 3 — lenient still gets one cycle.
        {"cycle 1 of 3", 1, 3, StrictnessLenient},
        {"cycle 2 of 3", 2, 3, StrictnessLenient},
        {"cycle 3 of 3", 3, 3, StrictnessStandard},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got := StrictnessForCycle(tt.cycle, tt.maxCycles)
            if got != tt.want {
                t.Errorf("StrictnessForCycle(%d, %d) = %v, want %v",
                    tt.cycle, tt.maxCycles, got, tt.want)
            }
        })
    }
}
```

## Files

- `internal/agent/reviewer.go` — Add `StrictnessForCycle(cycle, maxCycles int) ReviewStrictness` function
- `internal/agent/reviewer_test.go` — Add table-driven `TestStrictnessForCycle` with subtests

## Acceptance Criteria

- [ ] `StrictnessForCycle` is an exported function in package `agent`
- [ ] It returns `StrictnessLenient` for cycles 1-2 when `maxCycles >= 3`
- [ ] It returns `StrictnessStandard` for cycles 3-4 when `maxCycles >= 3`
- [ ] It returns `StrictnessStrict` for cycles 5+ when `maxCycles >= 3`
- [ ] When `maxCycles == 1`, it returns `StrictnessStrict`
- [ ] When `maxCycles == 2`, cycle 1 returns `StrictnessStandard`, cycle 2 returns `StrictnessStrict`
- [ ] The function is pure — no side effects, no dependencies on global state
- [ ] Table-driven tests cover all boundary conditions including small `maxCycles` values
- [ ] `go test ./internal/agent/...` passes
