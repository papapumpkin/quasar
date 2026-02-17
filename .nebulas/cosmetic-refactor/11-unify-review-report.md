+++
id = "unify-review-report"
title = "Unify duplicated ReviewReport type across loop and nebula packages"
type = "task"
priority = 2
depends_on = ["loop-extract-bead-error-helper", "worker-gate-strategy"]
scope = ["internal/nebula/types.go", "internal/nebula/worker.go", "internal/loop/report.go", "cmd/nebula.go"]
+++

## Problem

`ReviewReport` is defined identically in two packages:

**`internal/loop/report.go`**:
```go
type ReviewReport struct {
    Satisfaction     string `toml:"satisfaction"`
    Risk             string `toml:"risk"`
    NeedsHumanReview bool   `toml:"needs_human_review"`
    Summary          string `toml:"summary"`
}
```

**`internal/nebula/types.go`**:
```go
type ReviewReport struct {
    Satisfaction     string `toml:"satisfaction"`
    Risk             string `toml:"risk"`
    NeedsHumanReview bool   `toml:"needs_human_review"`
    Summary          string `toml:"summary"`
}
```

And `cmd/nebula.go` manually copies fields between them in `toPhaseRunnerResult`:

```go
tr.Report = &nebula.ReviewReport{
    Satisfaction:     result.Report.Satisfaction,
    Risk:             result.Report.Risk,
    NeedsHumanReview: result.Report.NeedsHumanReview,
    Summary:          result.Report.Summary,
}
```

Two identical types and a manual field-by-field copy adapter is a classic DRY violation and a maintenance trap — adding a field to one but not the other is a silent bug.

## Solution

Keep a single `ReviewReport` type. The cleanest home is `internal/loop/report.go` since it's the package that parses review output. Then:

1. Remove `ReviewReport` from `internal/nebula/types.go`.
2. Update `nebula.PhaseRunnerResult` to use `*loop.ReviewReport` instead of `*nebula.ReviewReport`.
3. In `cmd/nebula.go`, the `toPhaseRunnerResult` adapter can now directly assign `result.Report` without field-by-field copy.
4. Update any other references in the `nebula` package that use `nebula.ReviewReport` to use `loop.ReviewReport`.

**Import direction**: `nebula` already depends on types from `loop` (or if not, `nebula` depending on `loop` for a shared type is a clean dependency direction since `nebula` orchestrates `loop`). If there's a concern about import cycles, the type could alternatively live in a shared package like `internal/agent/`, but `loop` is the natural owner.

## Files

- `internal/nebula/types.go` — remove `ReviewReport`
- `internal/nebula/worker.go` — update references from `nebula.ReviewReport` to `loop.ReviewReport` (or just `ReviewReport` if aliased)
- `internal/loop/report.go` — no change (already has the type)
- `cmd/nebula.go` — simplify `toPhaseRunnerResult` to direct assignment

## Acceptance Criteria

- [ ] `ReviewReport` exists in exactly one package
- [ ] No field-by-field copy adapter for `ReviewReport`
- [ ] No import cycles
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
