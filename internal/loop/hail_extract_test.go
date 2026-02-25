package loop

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

func TestExtractReviewerHails(t *testing.T) {
	t.Parallel()

	state := &CycleState{Cycle: 2}
	phaseID := "phase-abc"

	t.Run("nil report returns nil", func(t *testing.T) {
		t.Parallel()
		got := extractReviewerHails(nil, state, phaseID)
		if got != nil {
			t.Errorf("extractReviewerHails(nil) = %v, want nil", got)
		}
	})

	t.Run("NeedsHumanReview false returns nil", func(t *testing.T) {
		t.Parallel()
		report := &agent.ReviewReport{
			NeedsHumanReview: false,
			Summary:          "looks good",
		}
		got := extractReviewerHails(report, state, phaseID)
		if got != nil {
			t.Errorf("extractReviewerHails(not-needed) = %v, want nil", got)
		}
	})

	t.Run("NeedsHumanReview true creates hail", func(t *testing.T) {
		t.Parallel()
		report := &agent.ReviewReport{
			NeedsHumanReview: true,
			Risk:             "high",
			Satisfaction:     "low",
			Summary:          "risky changes detected",
		}
		got := extractReviewerHails(report, state, phaseID)
		if len(got) != 1 {
			t.Fatalf("extractReviewerHails() returned %d hails, want 1", len(got))
		}
		h := got[0]
		if h.Kind != HailHumanReviewFlag {
			t.Errorf("Kind = %q, want %q", h.Kind, HailHumanReviewFlag)
		}
		if h.PhaseID != phaseID {
			t.Errorf("PhaseID = %q, want %q", h.PhaseID, phaseID)
		}
		if h.Cycle != 2 {
			t.Errorf("Cycle = %d, want 2", h.Cycle)
		}
		if h.SourceRole != "reviewer" {
			t.Errorf("SourceRole = %q, want %q", h.SourceRole, "reviewer")
		}
		if h.Summary != "risky changes detected" {
			t.Errorf("Summary = %q, want %q", h.Summary, "risky changes detected")
		}
		if !strings.Contains(h.Detail, "Risk: high") {
			t.Errorf("Detail missing Risk info: %q", h.Detail)
		}
		if !strings.Contains(h.Detail, "Satisfaction: low") {
			t.Errorf("Detail missing Satisfaction info: %q", h.Detail)
		}
		if len(h.Options) != 3 {
			t.Errorf("Options = %v, want 3 options", h.Options)
		}
	})

	t.Run("empty summary uses fallback", func(t *testing.T) {
		t.Parallel()
		report := &agent.ReviewReport{
			NeedsHumanReview: true,
			Risk:             "medium",
		}
		got := extractReviewerHails(report, state, phaseID)
		if len(got) != 1 {
			t.Fatalf("got %d hails, want 1", len(got))
		}
		if got[0].Summary != "Reviewer flagged work for human review" {
			t.Errorf("Summary = %q, want fallback message", got[0].Summary)
		}
	})
}
