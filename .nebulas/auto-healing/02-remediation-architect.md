+++
id = "remediation-architect"
title = "Invoke the architect to generate a remediation phase from failure diagnosis"
type = "feature"
priority = 2
depends_on = ["failure-analyzer"]
labels = ["quasar", "auto-healing", "reliability"]
scope = ["internal/nebula/healing.go", "internal/nebula/architect.go"]
allow_scope_overlap = true
+++

## Problem

Once a failure has been classified as healable by `AnalyzeFailure`, we need to invoke the architect agent to generate a remediation phase — a new `PhaseSpec` with a markdown body that addresses the specific failure. Today `RunArchitect` accepts an `ArchitectRequest` with `Mode` set to `ArchitectModeCreate` or `ArchitectModeRefactor` and a free-form `UserPrompt`. There is no dedicated mode or prompt builder for remediation, and no way to pass structured failure context into the architect.

## Solution

### New architect mode

Add a third `ArchitectMode` constant:

```go
// ArchitectModeRemediate instructs the architect to generate a remediation
// phase that addresses a specific failure diagnosis.
ArchitectModeRemediate ArchitectMode = "remediate"
```

### Remediation prompt builder

Add a function in `internal/nebula/healing.go` that constructs the `ArchitectRequest` from a `FailureDiagnosis`:

```go
// BuildRemediationRequest constructs an ArchitectRequest from a failure diagnosis.
// The generated prompt includes the failure kind, summary, last agent outputs,
// and reviewer findings so the architect can produce a targeted fix phase.
func BuildRemediationRequest(diag *FailureDiagnosis, neb *Nebula, failedSpec *PhaseSpec) ArchitectRequest
```

The function builds a `UserPrompt` using `strings.Builder` structured as:

```
## Remediation Request

Phase "${failedSpec.Title}" (id: ${diag.PhaseID}) failed with: ${diag.Kind}

### Failure Summary
${diag.Summary}

### Context
- Cycles used: ${diag.CyclesUsed}
- Budget spent: $${diag.BudgetSpent}
[if filter failure]
- Failing filter: ${diag.FilterName}
- Filter output:
${diag.FilterOutput}
[end if]

### Last Coder Output (truncated)
${diag.LastCoderOut}

### Last Reviewer Findings
${bulletedFindings}

### Instructions
Generate a remediation phase that:
1. Addresses the root cause identified above
2. Builds on the partial work already committed by the failed phase
3. Has a narrower scope than the original phase
4. Can complete within fewer cycles and lower budget
```

The `ArchitectRequest` is:

```go
ArchitectRequest{
    Mode:       ArchitectModeRemediate,
    UserPrompt: prompt,
    Nebula:     neb,
    PhaseID:    diag.PhaseID, // context: which phase failed
}
```

### Architect output processing

`RunArchitect` already returns `*ArchitectResult` with a `PhaseSpec` and `Body`. For remediation mode, the caller must post-process the result to:

1. Set `result.PhaseSpec.ID` to `"heal-" + diag.PhaseID` (prefix to avoid ID collision)
2. Copy `failedSpec.Scope` into `result.PhaseSpec.Scope` (remediation inherits the failed phase's file ownership)
3. Set `result.PhaseSpec.Gate` to `failedSpec.Gate` (inherit gate mode)
4. Set `result.PhaseSpec.Labels` to append `"auto-healing"` to the failed phase's labels

Add this post-processing as:

```go
// FinalizeRemediationSpec post-processes an ArchitectResult for remediation,
// inheriting scope and gate from the failed phase and setting a heal- prefixed ID.
func FinalizeRemediationSpec(result *ArchitectResult, diag *FailureDiagnosis, failedSpec *PhaseSpec) *ArchitectResult
```

### Handling `RunArchitect` in remediate mode

The existing `RunArchitect` function dispatches on `req.Mode` when building the system prompt. Add a branch for `ArchitectModeRemediate` that uses a system prompt emphasizing:
- The phase is a *remediation* of a prior failure, not a greenfield task
- Partial work exists on disk (committed by the failed phase's coder runs)
- The generated phase should be narrowly scoped and surgical

No changes to the architect's output parsing — the same TOML+markdown format is used.

## Files

- `internal/nebula/architect.go` — add `ArchitectModeRemediate` constant; add remediate-mode system prompt branch in `RunArchitect`
- `internal/nebula/healing.go` — add `BuildRemediationRequest`, `FinalizeRemediationSpec`
- `internal/nebula/healing_test.go` — tests for `BuildRemediationRequest` (prompt structure, field inclusion) and `FinalizeRemediationSpec` (ID prefix, scope inheritance, label merging)

## Acceptance Criteria

- [ ] `ArchitectModeRemediate` constant exists and is handled in `RunArchitect`
- [ ] `BuildRemediationRequest` produces a well-structured prompt containing all diagnosis fields
- [ ] `FinalizeRemediationSpec` sets `heal-` prefixed ID, inherits scope, gate, and labels
- [ ] Filter failure diagnoses include `FilterName` and `FilterOutput` in the prompt
- [ ] Max-cycles diagnoses include `AllFindings` as bulleted list in the prompt
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./internal/nebula/...` clean
