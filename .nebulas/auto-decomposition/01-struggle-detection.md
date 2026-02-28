+++
id = "struggle-detection"
title = "Implement struggle signal detection in the coder-reviewer loop"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Quasar's coder-reviewer loop (`internal/loop/loop.go`) currently runs until it either converges (reviewer approves), exhausts `MaxCycles`, or blows the budget. There is no mechanism to detect that a phase is *stuck* — cycling without meaningful progress. Symptoms include:

- The pre-reviewer filter (`Loop.Filter`) failing on the same check multiple consecutive cycles (`CycleState.FilterCheckName` repeating).
- The reviewer producing the same findings cycle after cycle (`CycleState.AllFindings` containing near-duplicate `ReviewFinding.Description` entries across consecutive cycles).
- The budget burn rate exceeding a threshold relative to remaining cycles (spending too fast for the remaining work).

When these signals co-occur, the phase is struggling and should be paused for decomposition rather than burning through the remaining budget.

## Solution

Add a new file `internal/loop/struggle.go` containing the detection logic, and a `StruggleSignal` type that the loop can evaluate after each cycle.

### Types

```go
// StruggleSignal represents the result of analyzing a CycleState for struggle indicators.
type StruggleSignal struct {
    Triggered       bool    // true if the combined score exceeds the threshold
    Score           float64 // composite struggle score in [0.0, 1.0]
    FilterRepeat    int     // count of consecutive cycles failing the same filter check
    FindingOverlap  float64 // ratio of findings in the current cycle that duplicate a prior cycle's findings
    BudgetBurnRate  float64 // ratio of budget consumed per cycle vs remaining budget
    Reason          string  // human-readable summary of why the signal triggered
}

// StruggleConfig holds tunable thresholds for struggle detection.
type StruggleConfig struct {
    Enabled              bool    // master switch; false = never trigger
    MinCyclesBeforeCheck int     // do not evaluate until this many cycles have run (default: 2)
    FilterRepeatThreshold int   // consecutive same-filter failures to flag (default: 2)
    FindingOverlapThreshold float64 // overlap ratio above which findings are considered stuck (default: 0.6)
    BudgetBurnThreshold float64 // fraction of budget per cycle that is considered excessive (default: 0.3)
    CompositeThreshold  float64 // combined score above which decomposition triggers (default: 0.6)
}
```

### Detection Function

```go
// EvaluateStruggle analyzes a CycleState for struggle signals.
// It returns a StruggleSignal indicating whether the phase should be paused for decomposition.
// The function is pure — it reads CycleState fields but does not mutate them.
func EvaluateStruggle(state *CycleState, cfg StruggleConfig) StruggleSignal
```

**Filter repeat detection**: Track the `FilterCheckName` field across cycles. This requires adding a `FilterHistory []string` field to `CycleState` that accumulates each cycle's `FilterCheckName`. Count trailing consecutive identical entries.

**Finding overlap detection**: Compare `CycleState.Findings` (current cycle) against `CycleState.AllFindings` from prior cycles. Two findings overlap when their `Description` fields have a Jaccard similarity above 0.8 on whitespace-tokenized words. Compute the ratio of overlapping current-cycle findings to total current-cycle findings.

**Budget burn rate**: Compute `CycleState.TotalCostUSD / float64(CycleState.Cycle)` as the per-cycle burn, then compare it against `CycleState.MaxBudgetUSD / float64(CycleState.MaxCycles)` (the ideal even burn). The ratio of actual-to-ideal gives the burn rate signal.

**Composite score**: Weighted sum of the three normalized signals:
- Filter repeat: weight 0.35 — `min(float64(filterRepeat) / float64(cfg.FilterRepeatThreshold), 1.0)`
- Finding overlap: weight 0.40 — `min(findingOverlap / cfg.FindingOverlapThreshold, 1.0)`
- Budget burn: weight 0.25 — `min(burnRate / cfg.BudgetBurnThreshold, 1.0)`

Trigger when `Score >= cfg.CompositeThreshold` and `Cycle >= cfg.MinCyclesBeforeCheck`.

### CycleState Changes

Add one field to `CycleState` in `internal/loop/state.go`:

```go
FilterHistory []string // accumulated FilterCheckName per cycle (index = cycle-1)
```

Append `FilterCheckName` to `FilterHistory` at the end of each cycle in `runLoop`, alongside the existing `CycleCommits` accumulation.

### Default Config Constructor

```go
// DefaultStruggleConfig returns a StruggleConfig with sensible defaults.
func DefaultStruggleConfig() StruggleConfig
```

### Helper

```go
// jaccardSimilarity computes the Jaccard similarity of two strings
// tokenized on whitespace. Returns a value in [0.0, 1.0].
func jaccardSimilarity(a, b string) float64
```

## Files

- `internal/loop/struggle.go` — `StruggleSignal`, `StruggleConfig`, `EvaluateStruggle`, `DefaultStruggleConfig`, `jaccardSimilarity`
- `internal/loop/struggle_test.go` — table-driven tests covering: no struggle on first cycle, filter repeat detection, finding overlap detection, budget burn detection, composite threshold triggering, disabled config short-circuit
- `internal/loop/state.go` — add `FilterHistory []string` field to `CycleState`

## Acceptance Criteria

- [ ] `EvaluateStruggle` returns `Triggered: false` when `cfg.Enabled` is false
- [ ] `EvaluateStruggle` returns `Triggered: false` when `Cycle < cfg.MinCyclesBeforeCheck`
- [ ] Filter repeat detection correctly counts consecutive trailing same-name failures
- [ ] Finding overlap uses Jaccard similarity and correctly computes the overlap ratio
- [ ] Budget burn rate compares actual per-cycle cost against ideal even distribution
- [ ] Composite score is the weighted sum of the three normalized signals
- [ ] `StruggleSignal.Reason` contains a human-readable explanation when triggered
- [ ] `FilterHistory` is accumulated in `runLoop` without breaking existing cycle logic
- [ ] `go test ./internal/loop/...` passes with at least 10 test cases for struggle detection
- [ ] `go vet ./internal/loop/...` reports no issues
