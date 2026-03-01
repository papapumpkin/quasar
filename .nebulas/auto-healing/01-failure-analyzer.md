+++
id = "failure-analyzer"
title = "Failure analysis and classification for healable phase errors"
type = "feature"
priority = 1
depends_on = []
labels = ["quasar", "auto-healing", "reliability"]
scope = ["internal/nebula/healing.go", "internal/nebula/healing_test.go"]
+++

## Problem

When a phase fails today, `WorkerGroup` marks it as failed in the `PhaseTracker` and moves on. The failure context — which sentinel error triggered termination, how many cycles were used, what the last coder/reviewer output was, which filter check failed — is captured in `loop.CycleState` and `loop.TaskResult` but is discarded after the `WorkerResult` is recorded. There is no structured analysis of *why* a phase failed or whether the failure is amenable to automated remediation.

We need a failure analyzer that inspects the error and surrounding context, classifies the failure into a healable category, and produces a structured diagnosis that the architect can consume to generate a remediation phase.

## Solution

Create a new file `internal/nebula/healing.go` with the failure analysis types and logic.

### Failure classification

Define a `FailureKind` enum and a `FailureDiagnosis` struct:

```go
// FailureKind classifies why a phase failed.
type FailureKind string

const (
    FailureKindMaxCycles   FailureKind = "max_cycles"
    FailureKindBudget      FailureKind = "budget_exceeded"
    FailureKindFilter      FailureKind = "filter_failure"
    FailureKindUnhealable  FailureKind = "unhealable"
)

// FailureDiagnosis is the structured output of failure analysis.
type FailureDiagnosis struct {
    PhaseID       string
    Kind          FailureKind
    Healable      bool
    Summary       string   // one-line human-readable explanation
    CyclesUsed    int
    BudgetSpent   float64
    LastCoderOut  string   // truncated last coder output for architect context
    LastReviewOut string   // truncated last reviewer output
    FilterName    string   // non-empty only for FailureKindFilter
    FilterOutput  string   // the failing filter's output
    Findings      []string // reviewer findings from the final cycle
}
```

### Analyzer function

```go
// AnalyzeFailure inspects a WorkerResult and its associated loop state to produce
// a FailureDiagnosis. Returns a diagnosis with Healable=false for errors that
// cannot be remediated (e.g., context cancellation, unknown errors).
func AnalyzeFailure(phaseID string, err error, result *loop.TaskResult, state *loop.CycleState) *FailureDiagnosis
```

The function uses `errors.Is` to match against the sentinel errors:

- `loop.ErrMaxCycles` — classify as `FailureKindMaxCycles`, healable. Extract `state.AllFindings` to identify the recurring reviewer objections.
- `loop.ErrBudgetExceeded` — classify as `FailureKindBudget`, healable. Record `state.TotalCostUSD` and `state.MaxBudgetUSD`.
- When `state.FilterCheckName != ""` and the error wraps a filter-related message — classify as `FailureKindFilter`, healable. Capture `state.FilterCheckName` and `state.FilterOutput`.
- All other errors — classify as `FailureKindUnhealable`, `Healable=false`.

Truncate `LastCoderOut` and `LastReviewOut` to 2000 characters to keep the architect prompt within token budget.

### Healing eligibility check

```go
// HealingPolicy controls whether and how healing is attempted.
type HealingPolicy struct {
    Enabled       bool    // master switch; false = never heal
    MaxAttempts   int     // per-phase healing attempts (default 1)
    BudgetReserve float64 // USD reserved from nebula budget for healing phases
}

// CanHeal returns true if the diagnosis is healable and policy permits an attempt.
// attempts is the number of prior healing attempts for this phase.
func (p HealingPolicy) CanHeal(diag *FailureDiagnosis, attempts int) bool
```

`CanHeal` returns `true` only when:
1. `p.Enabled` is true
2. `diag.Healable` is true
3. `attempts < p.MaxAttempts`
4. `p.BudgetReserve > 0` (enough budget headroom)

### Integration point

This phase does NOT wire the analyzer into `WorkerGroup` yet — that happens in phase `03-dag-insertion`. This phase only creates and tests the analysis logic in isolation.

## Files

- `internal/nebula/healing.go` — `FailureKind`, `FailureDiagnosis`, `AnalyzeFailure`, `HealingPolicy`, `CanHeal`
- `internal/nebula/healing_test.go` — table-driven tests covering each `FailureKind`, truncation behavior, `CanHeal` policy logic

## Acceptance Criteria

- [ ] `AnalyzeFailure` correctly classifies `ErrMaxCycles`, `ErrBudgetExceeded`, and filter failures
- [ ] `AnalyzeFailure` returns `Healable=false` for unknown/context-cancelled errors
- [ ] `LastCoderOut` and `LastReviewOut` are truncated to at most 2000 characters
- [ ] `CanHeal` respects all four policy conditions (enabled, healable, attempts, budget)
- [ ] `go test ./internal/nebula/...` passes with new tests
- [ ] `go vet ./internal/nebula/...` clean
