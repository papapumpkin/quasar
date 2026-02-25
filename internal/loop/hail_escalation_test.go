package loop

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

func TestEscalateCriticalFindings(t *testing.T) {
	t.Parallel()

	state := &CycleState{Cycle: 3}
	phaseID := "phase-crit"

	t.Run("no findings returns nil", func(t *testing.T) {
		t.Parallel()
		got := escalateCriticalFindings(nil, state, phaseID)
		if got != nil {
			t.Errorf("escalateCriticalFindings(nil) = %v, want nil", got)
		}
	})

	t.Run("non-critical findings return nil", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{Severity: "major", Description: "style issue"},
			{Severity: "minor", Description: "nit"},
		}
		got := escalateCriticalFindings(findings, state, phaseID)
		if len(got) != 0 {
			t.Errorf("escalateCriticalFindings(non-critical) = %d hails, want 0", len(got))
		}
	})

	t.Run("critical finding creates blocker hail", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{Severity: "critical", Description: "SQL injection vulnerability in user input handler"},
		}
		got := escalateCriticalFindings(findings, state, phaseID)
		if len(got) != 1 {
			t.Fatalf("got %d hails, want 1", len(got))
		}
		h := got[0]
		if h.Kind != HailBlocker {
			t.Errorf("Kind = %q, want %q", h.Kind, HailBlocker)
		}
		if h.PhaseID != phaseID {
			t.Errorf("PhaseID = %q, want %q", h.PhaseID, phaseID)
		}
		if h.Cycle != 3 {
			t.Errorf("Cycle = %d, want 3", h.Cycle)
		}
		if h.SourceRole != "reviewer" {
			t.Errorf("SourceRole = %q, want %q", h.SourceRole, "reviewer")
		}
		if !strings.Contains(h.Detail, "SQL injection") {
			t.Errorf("Detail missing finding description: %q", h.Detail)
		}
		if len(h.Options) != 3 {
			t.Errorf("Options = %v, want 3 options", h.Options)
		}
	})

	t.Run("case-insensitive severity matching", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{Severity: "Critical", Description: "uppercase case"},
			{Severity: "CRITICAL", Description: "all caps case"},
		}
		got := escalateCriticalFindings(findings, state, phaseID)
		if len(got) != 2 {
			t.Fatalf("got %d hails, want 2", len(got))
		}
	})

	t.Run("multiple critical findings create multiple hails", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{Severity: "critical", Description: "security flaw"},
			{Severity: "minor", Description: "style nit"},
			{Severity: "critical", Description: "data loss risk"},
		}
		got := escalateCriticalFindings(findings, state, phaseID)
		if len(got) != 2 {
			t.Fatalf("got %d hails, want 2 (only critical)", len(got))
		}
	})

	t.Run("long descriptions are truncated in summary", func(t *testing.T) {
		t.Parallel()
		longDesc := strings.Repeat("x", 200)
		findings := []ReviewFinding{
			{Severity: "critical", Description: longDesc},
		}
		got := escalateCriticalFindings(findings, state, phaseID)
		if len(got) != 1 {
			t.Fatalf("got %d hails, want 1", len(got))
		}
		if len(got[0].Summary) > 120 {
			t.Errorf("Summary length = %d, want <= 120", len(got[0].Summary))
		}
		if !strings.HasSuffix(got[0].Summary, "...") {
			t.Errorf("Summary should end with ..., got %q", got[0].Summary)
		}
	})
}

func TestEscalateHighRiskLowSatisfaction(t *testing.T) {
	t.Parallel()

	state := &CycleState{Cycle: 4}
	phaseID := "phase-risk"

	t.Run("nil report returns nil", func(t *testing.T) {
		t.Parallel()
		got := escalateHighRiskLowSatisfaction(nil, state, phaseID)
		if got != nil {
			t.Errorf("escalateHighRiskLowSatisfaction(nil) = %v, want nil", got)
		}
	})

	t.Run("skips when NeedsHumanReview is true", func(t *testing.T) {
		t.Parallel()
		report := &agent.ReviewReport{
			Risk:             "high",
			Satisfaction:     "low",
			NeedsHumanReview: true,
			Summary:          "already flagged",
		}
		got := escalateHighRiskLowSatisfaction(report, state, phaseID)
		if got != nil {
			t.Error("expected nil when NeedsHumanReview is true")
		}
	})

	t.Run("low risk returns nil", func(t *testing.T) {
		t.Parallel()
		report := &agent.ReviewReport{
			Risk:         "low",
			Satisfaction: "low",
		}
		got := escalateHighRiskLowSatisfaction(report, state, phaseID)
		if got != nil {
			t.Error("expected nil for low risk")
		}
	})

	t.Run("high satisfaction returns nil", func(t *testing.T) {
		t.Parallel()
		report := &agent.ReviewReport{
			Risk:         "high",
			Satisfaction: "high",
		}
		got := escalateHighRiskLowSatisfaction(report, state, phaseID)
		if got != nil {
			t.Error("expected nil for high satisfaction")
		}
	})

	t.Run("high risk and low satisfaction creates decision hail", func(t *testing.T) {
		t.Parallel()
		report := &agent.ReviewReport{
			Risk:         "high",
			Satisfaction: "low",
			Summary:      "significant concerns about approach",
		}
		got := escalateHighRiskLowSatisfaction(report, state, phaseID)
		if got == nil {
			t.Fatal("expected non-nil hail")
		}
		if got.Kind != HailDecisionNeeded {
			t.Errorf("Kind = %q, want %q", got.Kind, HailDecisionNeeded)
		}
		if got.PhaseID != phaseID {
			t.Errorf("PhaseID = %q, want %q", got.PhaseID, phaseID)
		}
		if got.Cycle != 4 {
			t.Errorf("Cycle = %d, want 4", got.Cycle)
		}
		if got.SourceRole != "reviewer" {
			t.Errorf("SourceRole = %q, want %q", got.SourceRole, "reviewer")
		}
		if !strings.Contains(got.Detail, "high risk") {
			t.Errorf("Detail missing risk context: %q", got.Detail)
		}
		if len(got.Options) != 3 {
			t.Errorf("Options = %v, want 3 options", got.Options)
		}
	})

	t.Run("case-insensitive matching", func(t *testing.T) {
		t.Parallel()
		report := &agent.ReviewReport{
			Risk:         "HIGH",
			Satisfaction: "LOW",
		}
		got := escalateHighRiskLowSatisfaction(report, state, phaseID)
		if got == nil {
			t.Fatal("expected hail for case-insensitive match")
		}
		if got.Kind != HailDecisionNeeded {
			t.Errorf("Kind = %q, want %q", got.Kind, HailDecisionNeeded)
		}
	})
}

