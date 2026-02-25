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
