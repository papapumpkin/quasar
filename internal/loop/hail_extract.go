package loop

import (
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// extractReviewerHails inspects a parsed ReviewReport and produces hails when
// the reviewer has flagged the work for human attention. Returns nil when the
// report is nil or does not require human review.
func extractReviewerHails(report *agent.ReviewReport, state *CycleState, phaseID string) []Hail {
	if report == nil || !report.NeedsHumanReview {
		return nil
	}

	summary := "Reviewer flagged work for human review"
	if report.Summary != "" {
		summary = report.Summary
	}

	var detail strings.Builder
	detail.WriteString("The reviewer flagged this work as requiring human review.\n")
	if report.Risk != "" {
		fmt.Fprintf(&detail, "Risk: %s\n", report.Risk)
	}
	if report.Satisfaction != "" {
		fmt.Fprintf(&detail, "Satisfaction: %s\n", report.Satisfaction)
	}
	if report.Summary != "" {
		fmt.Fprintf(&detail, "Summary: %s\n", report.Summary)
	}

	return []Hail{{
		PhaseID:    phaseID,
		Cycle:      state.Cycle,
		SourceRole: "reviewer",
		Kind:       HailHumanReviewFlag,
		Summary:    summary,
		Detail:     detail.String(),
		Options:    []string{"approve", "reject", "revise"},
	}}
}

// escalateCriticalFindings inspects parsed review findings and creates a
// HailBlocker for any finding with "critical" severity. Each critical finding
// produces a separate hail so the human can address them individually.
func escalateCriticalFindings(findings []ReviewFinding, state *CycleState, phaseID string) []Hail {
	var hails []Hail
	for _, f := range findings {
		if strings.EqualFold(f.Severity, "critical") {
			summary := f.Description
			if len(summary) > 120 {
				summary = summary[:117] + "..."
			}

			var detail strings.Builder
			fmt.Fprintf(&detail, "Critical issue found by reviewer in cycle %d.\n", state.Cycle)
			detail.WriteString(f.Description)

			hails = append(hails, Hail{
				PhaseID:    phaseID,
				Cycle:      state.Cycle,
				SourceRole: "reviewer",
				Kind:       HailBlocker,
				Summary:    summary,
				Detail:     detail.String(),
				Options:    []string{"acknowledge", "override", "abort"},
			})
		}
	}
	return hails
}

// escalateHighRiskLowSatisfaction creates a HailDecisionNeeded when the
// reviewer report indicates high risk combined with low satisfaction. This
// signals the human should consider intervening before the loop continues.
// Returns nil when the report is nil or the escalation criteria are not met.
func escalateHighRiskLowSatisfaction(report *agent.ReviewReport, state *CycleState, phaseID string) *Hail {
	if report == nil {
		return nil
	}
	if !strings.EqualFold(report.Risk, "high") || !strings.EqualFold(report.Satisfaction, "low") {
		return nil
	}

	summary := "High risk with low satisfaction — human review recommended"
	if report.Summary != "" {
		summary = report.Summary
	}

	var detail strings.Builder
	detail.WriteString("The reviewer assessed this work as high risk with low satisfaction.\n")
	detail.WriteString("Human intervention is recommended before continuing.\n")
	if report.Summary != "" {
		fmt.Fprintf(&detail, "Summary: %s\n", report.Summary)
	}

	return &Hail{
		PhaseID:    phaseID,
		Cycle:      state.Cycle,
		SourceRole: "reviewer",
		Kind:       HailDecisionNeeded,
		Summary:    summary,
		Detail:     detail.String(),
		Options:    []string{"continue", "intervene", "abort"},
	}
}

// buildMaxCyclesHail creates a HailBlocker when the coder-reviewer loop
// exhausts its maximum cycle count without approval. The hail includes
// the final reviewer summary and any unresolved findings for context.
func buildMaxCyclesHail(state *CycleState, phaseID string) Hail {
	var detail strings.Builder
	fmt.Fprintf(&detail, "The coder-reviewer loop reached the maximum of %d cycles without approval.\n", state.MaxCycles)

	report := ParseReviewReport(state.ReviewOutput)
	if report != nil && report.Summary != "" {
		fmt.Fprintf(&detail, "Final reviewer summary: %s\n", report.Summary)
		if report.Risk != "" {
			fmt.Fprintf(&detail, "Risk: %s\n", report.Risk)
		}
		if report.Satisfaction != "" {
			fmt.Fprintf(&detail, "Satisfaction: %s\n", report.Satisfaction)
		}
	}

	if len(state.AllFindings) > 0 {
		detail.WriteString("\nUnresolved findings:\n")
		for i, f := range state.AllFindings {
			if i >= 10 {
				fmt.Fprintf(&detail, "... and %d more\n", len(state.AllFindings)-10)
				break
			}
			fmt.Fprintf(&detail, "- [%s] %s\n", f.Severity, firstLine(f.Description, 100))
		}
	}

	summary := fmt.Sprintf("Max cycles reached (%d) — manual review required", state.MaxCycles)

	return Hail{
		PhaseID:    phaseID,
		Cycle:      state.Cycle,
		SourceRole: "reviewer",
		Kind:       HailBlocker,
		Summary:    summary,
		Detail:     detail.String(),
		Options:    []string{"retry", "accept as-is", "abort"},
	}
}

// bridgeDiscoveryHails converts Fabric discoveries of kind requirements_ambiguity
// and missing_dependency into Hail objects so they surface in the UI. Discoveries
// that are already resolved are skipped.
func bridgeDiscoveryHails(discoveries []fabric.Discovery, phaseID string, cycle int) []Hail {
	var hails []Hail
	for _, d := range discoveries {
		if d.Resolved {
			continue
		}

		var kind HailKind
		switch d.Kind {
		case fabric.DiscoveryRequirementsAmbiguity:
			kind = HailAmbiguity
		case fabric.DiscoveryMissingDependency:
			kind = HailBlocker
		default:
			continue
		}

		summary := d.Detail
		if len(summary) > 120 {
			summary = summary[:117] + "..."
		}

		var detail strings.Builder
		fmt.Fprintf(&detail, "Discovery (kind: %s) from task %s\n", d.Kind, d.SourceTask)
		detail.WriteString(d.Detail)
		if d.Affects != "" {
			fmt.Fprintf(&detail, "\nAffects: %s", d.Affects)
		}

		hails = append(hails, Hail{
			PhaseID:    phaseID,
			Cycle:      cycle,
			SourceRole: "agent",
			Kind:       kind,
			Summary:    summary,
			Detail:     detail.String(),
		})
	}
	return hails
}
