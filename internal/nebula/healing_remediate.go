package nebula

import (
	"context"
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/dag"
)

// PartialWork captures the state of a phase's progress at the point of failure.
// It is injected into the remediation phase's coder prompt so the agent can
// build on existing work rather than starting from scratch.
type PartialWork struct {
	PhaseID       string
	CommitSHAs    []string // one SHA per completed cycle
	BaseCommitSHA string   // HEAD before the phase started
	FilesTouched  []string // files modified across all commits (extracted via git diff)
	CyclesUsed    int
	LastFindings  []string // final cycle's reviewer findings (the unresolved objections)
}

// GitDiffLister lists files changed between two commits.
type GitDiffLister interface {
	DiffFileList(ctx context.Context, base, head string) ([]string, error)
}

// BuildPartialWork extracts a PartialWork snapshot from a PhaseRunnerResult.
// The filesTouched list is computed by diffing BaseCommitSHA against the last
// CycleCommit SHA via the provided GitDiffLister.
//
// When result is nil or BaseCommitSHA is empty (no commits made), returns a
// PartialWork with empty FilesTouched.
func BuildPartialWork(ctx context.Context, result *PhaseRunnerResult, findings []string, git GitDiffLister) (*PartialWork, error) {
	if result == nil {
		return &PartialWork{}, nil
	}

	pw := &PartialWork{
		CommitSHAs:    result.CycleCommits,
		BaseCommitSHA: result.BaseCommitSHA,
		CyclesUsed:    result.CyclesUsed,
		LastFindings:  findings,
	}

	// Compute files touched by diffing base against the final commit.
	if pw.BaseCommitSHA != "" && len(pw.CommitSHAs) > 0 && git != nil {
		head := pw.CommitSHAs[len(pw.CommitSHAs)-1]
		files, err := git.DiffFileList(ctx, pw.BaseCommitSHA, head)
		if err != nil {
			return pw, fmt.Errorf("listing files touched: %w", err)
		}
		pw.FilesTouched = files
	}

	return pw, nil
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

	if diag.PartialWork != nil && len(diag.PartialWork.CommitSHAs) > 0 {
		b.WriteString("### Partial Work from Failed Phase\n")
		fmt.Fprintf(&b, "- Base commit: %s\n", diag.PartialWork.BaseCommitSHA)
		fmt.Fprintf(&b, "- Cycle commits: %s\n", strings.Join(diag.PartialWork.CommitSHAs, ", "))
		if len(diag.PartialWork.FilesTouched) > 0 {
			b.WriteString("- Files touched:\n")
			for _, f := range diag.PartialWork.FilesTouched {
				fmt.Fprintf(&b, "  - %s\n", f)
			}
		}
		fmt.Fprintf(&b, "- Cycles completed: %d\n", diag.PartialWork.CyclesUsed)
		if len(diag.PartialWork.LastFindings) > 0 {
			b.WriteString("- Unresolved findings:\n")
			for _, f := range diag.PartialWork.LastFindings {
				fmt.Fprintf(&b, "  - %s\n", f)
			}
		}
		b.WriteString("\nThe remediation phase MUST NOT revert these commits. Build on the existing\n")
		b.WriteString("changes and address only the unresolved findings listed above.\n\n")
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
