package nebula

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/dag"
)

// Sentinel errors used by AnalyzeFailure for failure classification.
// These mirror the sentinel errors in the loop package. The adapter layer
// (cmd/nebula_adapters.go) passes loop errors through unchanged, so
// errors.Is works across the boundary without importing loop.
var (
	// ErrHealMaxCycles classifies a failure as max-cycles-exhausted.
	ErrHealMaxCycles = errors.New("maximum review cycles reached")
	// ErrHealBudgetExceeded classifies a failure as budget-exceeded.
	ErrHealBudgetExceeded = errors.New("budget exceeded")
)

// FailureKind classifies why a phase failed.
type FailureKind string

const (
	// FailureKindMaxCycles indicates the phase exhausted all review cycles.
	FailureKindMaxCycles FailureKind = "max_cycles"
	// FailureKindBudget indicates the phase exceeded its budget.
	FailureKindBudget FailureKind = "budget_exceeded"
	// FailureKindFilter indicates a pre-reviewer filter check failed persistently.
	FailureKindFilter FailureKind = "filter_failure"
	// FailureKindUnhealable indicates a failure that cannot be automatically remediated.
	FailureKindUnhealable FailureKind = "unhealable"

	// maxOutputLen is the maximum length for coder/reviewer output kept in a diagnosis.
	maxOutputLen = 2000
)

// FailureDiagnosis is the structured output of failure analysis.
type FailureDiagnosis struct {
	PhaseID       string
	Kind          FailureKind
	Healable      bool
	Summary       string // one-line human-readable explanation
	CyclesUsed    int
	BudgetSpent   float64
	LastCoderOut  string   // truncated last coder output for architect context
	LastReviewOut string   // truncated last reviewer output
	FilterName    string   // non-empty only for FailureKindFilter
	FilterOutput  string   // the failing filter's output
	Findings      []string // reviewer findings from the final cycle
}

// FailureContext carries the cycle-state fields needed for failure analysis.
// It mirrors the relevant fields from loop.CycleState without creating an
// import dependency on the loop package.
type FailureContext struct {
	Cycle           int
	TotalCostUSD    float64
	CoderOutput     string
	ReviewOutput    string
	FilterCheckName string
	FilterOutput    string
	AllFindings     []DecomposeFinding // reuses the existing nebula type
}

// HealingPolicy controls whether and how healing is attempted.
type HealingPolicy struct {
	Enabled       bool    // master switch; false = never heal
	MaxAttempts   int     // per-phase healing attempts (default 1)
	BudgetReserve float64 // USD reserved from nebula budget for healing phases
}

// CanHeal returns true if the diagnosis is healable and policy permits an attempt.
// attempts is the number of prior healing attempts for this phase.
func (p HealingPolicy) CanHeal(diag *FailureDiagnosis, attempts int) bool {
	if !p.Enabled {
		return false
	}
	if diag == nil || !diag.Healable {
		return false
	}
	if attempts >= p.MaxAttempts {
		return false
	}
	return p.BudgetReserve > 0
}

// AnalyzeFailure inspects a phase error and its associated context to produce
// a FailureDiagnosis. The err is matched against ErrHealMaxCycles and
// ErrHealBudgetExceeded (which share error text with the loop package sentinels).
// Returns a diagnosis with Healable=false for errors that cannot be remediated
// (e.g., context cancellation, unknown errors).
func AnalyzeFailure(phaseID string, err error, result *PhaseRunnerResult, fctx *FailureContext) *FailureDiagnosis {
	diag := &FailureDiagnosis{
		PhaseID: phaseID,
	}

	// Populate cycle / budget info from failure context when available.
	if fctx != nil {
		diag.CyclesUsed = fctx.Cycle
		diag.BudgetSpent = fctx.TotalCostUSD
		diag.LastCoderOut = truncate(fctx.CoderOutput, maxOutputLen)
		diag.LastReviewOut = truncate(fctx.ReviewOutput, maxOutputLen)
	}

	// Override cycle count from result if available (more authoritative).
	if result != nil {
		diag.CyclesUsed = result.CyclesUsed
		diag.BudgetSpent = result.TotalCostUSD
	}

	// Classify the failure.
	// Order matters: sentinel errors take precedence over filter-context inspection.
	// Context cancellation is checked before filter to avoid misclassifying a
	// cancelled phase that has stale filter fields from a prior cycle.
	switch {
	case isMaxCyclesErr(err):
		diag.Kind = FailureKindMaxCycles
		diag.Healable = true
		diag.Summary = "phase exhausted all review cycles without approval"
		diag.Findings = extractFindingDescriptions(fctx)
		// Override findings from result if available (more authoritative).
		if result != nil && len(result.AllFindings) > 0 {
			diag.Findings = extractDecomposeFindingDescriptions(result.AllFindings)
		}

	case isBudgetErr(err):
		diag.Kind = FailureKindBudget
		diag.Healable = true
		diag.Summary = "phase exceeded its budget before completing"

	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		diag.Kind = FailureKindUnhealable
		diag.Healable = false
		diag.Summary = "phase cancelled or timed out"

	case isFilterFailure(fctx):
		diag.Kind = FailureKindFilter
		diag.Healable = true
		diag.Summary = "phase failed due to persistent filter check failure"
		diag.FilterName = fctx.FilterCheckName
		diag.FilterOutput = fctx.FilterOutput

	default:
		diag.Kind = FailureKindUnhealable
		diag.Healable = false
		diag.Summary = "phase failed with an unrecoverable error"
	}

	return diag
}

// isMaxCyclesErr returns true if err matches the max-cycles sentinel.
// It checks both the nebula-local sentinel and uses error message matching
// to support errors originating from the loop package.
func isMaxCyclesErr(err error) bool {
	return errors.Is(err, ErrHealMaxCycles) || hasErrMessage(err, ErrHealMaxCycles.Error())
}

// isBudgetErr returns true if err matches the budget-exceeded sentinel.
func isBudgetErr(err error) bool {
	return errors.Is(err, ErrHealBudgetExceeded) || hasErrMessage(err, ErrHealBudgetExceeded.Error())
}

// hasErrMessage checks whether the error's message matches the target exactly.
// This supports matching errors from the loop package which share the same
// error text as the nebula sentinels but are distinct error values.
func hasErrMessage(err error, msg string) bool {
	if err == nil {
		return false
	}
	return err.Error() == msg
}

// isFilterFailure returns true when the failure context indicates a pre-reviewer
// filter check failed.
func isFilterFailure(fctx *FailureContext) bool {
	if fctx == nil {
		return false
	}
	return fctx.FilterCheckName != "" && fctx.FilterOutput != ""
}

// extractFindingDescriptions collects finding descriptions from the failure context.
func extractFindingDescriptions(fctx *FailureContext) []string {
	if fctx == nil {
		return nil
	}
	return extractDecomposeFindingDescriptions(fctx.AllFindings)
}

// extractDecomposeFindingDescriptions collects description strings from a slice of DecomposeFinding.
func extractDecomposeFindingDescriptions(findings []DecomposeFinding) []string {
	if len(findings) == 0 {
		return nil
	}
	descs := make([]string, 0, len(findings))
	for _, f := range findings {
		descs = append(descs, f.Description)
	}
	return descs
}

// truncate returns s truncated to at most maxLen bytes.
// Note: this operates on byte length and may split a multi-byte UTF-8 character
// at the boundary. This is acceptable because the inputs are LLM-generated
// English text that is overwhelmingly ASCII.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// InsertRemediationPhase adds a remediation phase into the live DAG, rewiring
// edges so that dependents of the failed phase now depend on the remediation
// phase instead. The remediation phase has no dependencies of its own (the
// failed phase's deps are already satisfied).
//
// Returns the list of phase IDs whose dependency edges were rewired.
// Returns an error if the failed phase ID does not exist in the DAG or if
// the remediation node cannot be inserted.
func InsertRemediationPhase(d *dag.DAG, failedID string, remediation *PhaseSpec) ([]string, error) {
	if d.Node(failedID) == nil {
		return nil, fmt.Errorf("inserting remediation phase: %w: %s", dag.ErrNodeNotFound, failedID)
	}

	if err := d.AddNode(remediation.ID, remediation.Priority); err != nil {
		return nil, fmt.Errorf("inserting remediation node %s: %w", remediation.ID, err)
	}

	dependents := d.DirectDependents(failedID)
	rewired := make([]string, 0, len(dependents))
	for _, dep := range dependents {
		d.RemoveEdge(dep, failedID)
		if err := d.AddEdge(dep, remediation.ID); err != nil {
			return rewired, fmt.Errorf("rewiring edge %s → %s: %w", dep, remediation.ID, err)
		}
		rewired = append(rewired, dep)
	}
	return rewired, nil
}

// BuildRemediationRequest constructs an ArchitectRequest from a failure diagnosis.
// The generated prompt includes the failure kind, summary, last agent outputs,
// and reviewer findings so the architect can produce a targeted fix phase.
func BuildRemediationRequest(diag *FailureDiagnosis, neb *Nebula, failedSpec *PhaseSpec) ArchitectRequest {
	var b strings.Builder

	b.WriteString("## Remediation Request\n\n")
	fmt.Fprintf(&b, "Phase %q (id: %s) failed with: %s\n\n", failedSpec.Title, diag.PhaseID, diag.Kind)

	b.WriteString("### Failure Summary\n")
	fmt.Fprintf(&b, "%s\n\n", diag.Summary)

	b.WriteString("### Context\n")
	fmt.Fprintf(&b, "- Cycles used: %d\n", diag.CyclesUsed)
	fmt.Fprintf(&b, "- Budget spent: $%.2f\n", diag.BudgetSpent)

	if diag.Kind == FailureKindFilter {
		fmt.Fprintf(&b, "- Failing filter: %s\n", diag.FilterName)
		b.WriteString("- Filter output:\n")
		fmt.Fprintf(&b, "%s\n", diag.FilterOutput)
	}
	b.WriteString("\n")

	if diag.LastCoderOut != "" {
		b.WriteString("### Last Coder Output (truncated)\n")
		fmt.Fprintf(&b, "%s\n\n", diag.LastCoderOut)
	}

	if len(diag.Findings) > 0 {
		b.WriteString("### Last Reviewer Findings\n")
		for _, f := range diag.Findings {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	b.WriteString("### Instructions\n")
	b.WriteString("Generate a remediation phase that:\n")
	b.WriteString("1. Addresses the root cause identified above\n")
	b.WriteString("2. Builds on the partial work already committed by the failed phase\n")
	b.WriteString("3. Has a narrower scope than the original phase\n")
	b.WriteString("4. Can complete within fewer cycles and lower budget\n")

	return ArchitectRequest{
		Mode:       ArchitectModeRemediate,
		UserPrompt: b.String(),
		Nebula:     neb,
		PhaseID:    diag.PhaseID,
	}
}

// FinalizeRemediationSpec post-processes an ArchitectResult for remediation,
// inheriting scope and gate from the failed phase and setting a heal- prefixed ID.
func FinalizeRemediationSpec(result *ArchitectResult, diag *FailureDiagnosis, failedSpec *PhaseSpec) *ArchitectResult {
	result.PhaseSpec.ID = "heal-" + diag.PhaseID

	// Inherit file ownership from the failed phase.
	if len(failedSpec.Scope) > 0 {
		result.PhaseSpec.Scope = make([]string, len(failedSpec.Scope))
		copy(result.PhaseSpec.Scope, failedSpec.Scope)
	}

	// Inherit gate mode from the failed phase.
	result.PhaseSpec.Gate = failedSpec.Gate

	// Merge labels: start with failed phase's labels, append "auto-healing" if not present.
	labels := make([]string, len(failedSpec.Labels))
	copy(labels, failedSpec.Labels)
	hasHealingLabel := false
	for _, l := range labels {
		if l == "auto-healing" {
			hasHealingLabel = true
			break
		}
	}
	if !hasHealingLabel {
		labels = append(labels, "auto-healing")
	}
	result.PhaseSpec.Labels = labels

	return result
}
