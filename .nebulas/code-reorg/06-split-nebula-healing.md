+++
id = "split-nebula-healing"
title = "Split internal/nebula/healing.go into diagnosis and remediation"
type = "task"
priority = 2
depends_on = ["consolidate-nebula"]
scope = ["internal/nebula/"]
+++

## Problem

`internal/nebula/healing.go` is ~300 lines (~10 KB) mixing two distinct concerns:

1. **Failure diagnosis** — analyzing what went wrong (classify failure kind, extract context)
2. **Remediation** — generating a healing phase to fix the failure (build partial work, insert remediation phase)

These are separate steps with different consumers: diagnosis is informational (used for logging, metrics), while remediation is operational (modifies the DAG and creates new phases).

## Solution

Split into two focused files, both remaining in `package nebula`:

### File 1: `healing.go` (stays, ~150 lines)
Failure analysis and diagnosis:
- `FailureDiagnosis` struct
- `FailureKind` constants (`FailureMaxCycles`, `FailureBudgetExceeded`, `FailureFilterFailure`, `FailureUnhealable`)
- `FailureContext` struct
- `HealingPolicy` struct and `CanHeal()` method
- `AnalyzeFailure()` function
- `HealingSummary()` function
- `HealingContext()` function
- Classification helpers: `isMaxCyclesErr()`, `isBudgetErr()`, `hasErrMessage()`, `isFilterFailure()`
- `extractFindingDescriptions()`, `extractDecomposeFindingDescriptions()`
- Sentinel errors: `ErrHealMaxCycles`, `ErrHealBudgetExceeded`

### File 2: `healing_remediate.go` (new, ~150 lines)
Remediation phase generation and insertion:
- `PartialWork` struct
- `GitDiffLister` interface
- `BuildPartialWork()` function
- `InsertRemediationPhase()` function
- `BuildRemediationRequest()` function
- `FinalizeRemediationSpec()` function
- `truncate()` helper (if used only by remediation; if shared, keep in healing.go)

### Approach

Read `healing.go` fully, identify the boundary between diagnosis functions and remediation functions. Move remediation-related types and functions to the new file. Ensure imports are correct for each file.

## Files

- `internal/nebula/healing.go` — trim to diagnosis only (~150 lines)
- `internal/nebula/healing_remediate.go` — create (remediation logic)

## Acceptance Criteria

- [ ] `healing.go` contains only diagnosis/analysis types and functions
- [ ] `healing_remediate.go` contains only remediation/insertion types and functions
- [ ] No functions duplicated between files
- [ ] `go build ./internal/nebula/...` succeeds
- [ ] `go test ./internal/nebula/...` passes
