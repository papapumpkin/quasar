package loop

import (
	"context"
	"fmt"
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

func TestBuildMaxCyclesHail(t *testing.T) {
	t.Parallel()

	t.Run("basic max cycles hail", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			MaxCycles:    5,
			Cycle:        5,
			ReviewOutput: "",
		}
		h := buildMaxCyclesHail(state, "phase-max")
		if h.Kind != HailBlocker {
			t.Errorf("Kind = %q, want %q", h.Kind, HailBlocker)
		}
		if h.PhaseID != "phase-max" {
			t.Errorf("PhaseID = %q, want %q", h.PhaseID, "phase-max")
		}
		if h.Cycle != 5 {
			t.Errorf("Cycle = %d, want 5", h.Cycle)
		}
		if !strings.Contains(h.Summary, "Max cycles") {
			t.Errorf("Summary missing max cycles: %q", h.Summary)
		}
		if !strings.Contains(h.Detail, "maximum of 5 cycles") {
			t.Errorf("Detail missing cycle count: %q", h.Detail)
		}
		if len(h.Options) != 3 {
			t.Errorf("Options = %v, want 3 options", h.Options)
		}
	})

	t.Run("includes reviewer summary when available", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			MaxCycles:    3,
			Cycle:        3,
			ReviewOutput: "REPORT:\nSATISFACTION: low\nRISK: high\nSUMMARY: fundamental design flaw",
		}
		h := buildMaxCyclesHail(state, "phase-sum")
		if !strings.Contains(h.Detail, "fundamental design flaw") {
			t.Errorf("Detail missing reviewer summary: %q", h.Detail)
		}
		if !strings.Contains(h.Detail, "Risk: high") {
			t.Errorf("Detail missing risk: %q", h.Detail)
		}
		if !strings.Contains(h.Detail, "Satisfaction: low") {
			t.Errorf("Detail missing satisfaction: %q", h.Detail)
		}
	})

	t.Run("includes unresolved findings", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			MaxCycles: 3,
			Cycle:     3,
			AllFindings: []ReviewFinding{
				{Severity: "critical", Description: "security vulnerability"},
				{Severity: "major", Description: "missing error handling"},
			},
		}
		h := buildMaxCyclesHail(state, "phase-find")
		if !strings.Contains(h.Detail, "[critical] security vulnerability") {
			t.Errorf("Detail missing critical finding: %q", h.Detail)
		}
		if !strings.Contains(h.Detail, "[major] missing error handling") {
			t.Errorf("Detail missing major finding: %q", h.Detail)
		}
	})

	t.Run("truncates many findings", func(t *testing.T) {
		t.Parallel()
		var findings []ReviewFinding
		for i := 0; i < 15; i++ {
			findings = append(findings, ReviewFinding{
				Severity:    "minor",
				Description: fmt.Sprintf("finding %d", i),
			})
		}
		state := &CycleState{
			MaxCycles:   3,
			Cycle:       3,
			AllFindings: findings,
		}
		h := buildMaxCyclesHail(state, "phase-trunc")
		if !strings.Contains(h.Detail, "... and 5 more") {
			t.Errorf("Detail should truncate after 10 findings: %q", h.Detail)
		}
	})
}

func TestExtractAndPostHailsWithEscalation(t *testing.T) {
	t.Parallel()

	t.Run("critical finding escalates to blocker hail", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		l := &Loop{
			UI:        &noopUI{},
			HailQueue: q,
			TaskID:    "phase-esc",
		}
		state := &CycleState{
			ReviewOutput: "ISSUE:\nSEVERITY: critical\nDESCRIPTION: buffer overflow in parser",
			Cycle:        2,
			Findings:     ParseReviewFindings("ISSUE:\nSEVERITY: critical\nDESCRIPTION: buffer overflow in parser"),
		}
		l.extractAndPostHails(context.Background(), state)

		all := q.All()
		if len(all) != 1 {
			t.Fatalf("HailQueue has %d hails, want 1", len(all))
		}
		if all[0].Kind != HailBlocker {
			t.Errorf("Kind = %q, want %q", all[0].Kind, HailBlocker)
		}
		if !strings.Contains(all[0].Detail, "buffer overflow") {
			t.Errorf("Detail missing finding info: %q", all[0].Detail)
		}
	})

	t.Run("high risk low satisfaction without NEEDS_HUMAN_REVIEW escalates", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		l := &Loop{
			UI:        &noopUI{},
			HailQueue: q,
			TaskID:    "phase-risk",
		}
		state := &CycleState{
			ReviewOutput: "REPORT:\nSATISFACTION: low\nRISK: high\nNEEDS_HUMAN_REVIEW: no\nSUMMARY: risky approach",
			Cycle:        1,
		}
		l.extractAndPostHails(context.Background(), state)

		all := q.All()
		if len(all) != 1 {
			t.Fatalf("HailQueue has %d hails, want 1 (decision_needed)", len(all))
		}
		if all[0].Kind != HailDecisionNeeded {
			t.Errorf("Kind = %q, want %q", all[0].Kind, HailDecisionNeeded)
		}
	})

	t.Run("high risk low satisfaction with NEEDS_HUMAN_REVIEW does not duplicate", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		l := &Loop{
			UI:        &noopUI{},
			HailQueue: q,
			TaskID:    "phase-nodup",
		}
		state := &CycleState{
			ReviewOutput: "REPORT:\nSATISFACTION: low\nRISK: high\nNEEDS_HUMAN_REVIEW: yes\nSUMMARY: needs review",
			Cycle:        2,
		}
		l.extractAndPostHails(context.Background(), state)

		all := q.All()
		if len(all) != 1 {
			t.Fatalf("HailQueue has %d hails, want 1 (only human_review, no duplicate)", len(all))
		}
		if all[0].Kind != HailHumanReviewFlag {
			t.Errorf("Kind = %q, want %q", all[0].Kind, HailHumanReviewFlag)
		}
	})
}

func TestPostMaxCyclesHail(t *testing.T) {
	t.Parallel()

	t.Run("posts blocker hail", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		l := &Loop{
			UI:        &noopUI{},
			HailQueue: q,
			TaskID:    "phase-mc",
		}
		state := &CycleState{
			MaxCycles:    3,
			Cycle:        3,
			ReviewOutput: "REPORT:\nSUMMARY: still broken",
		}
		l.postMaxCyclesHail(state)

		all := q.All()
		if len(all) != 1 {
			t.Fatalf("HailQueue has %d hails, want 1", len(all))
		}
		if all[0].Kind != HailBlocker {
			t.Errorf("Kind = %q, want %q", all[0].Kind, HailBlocker)
		}
		if !strings.Contains(all[0].Summary, "Max cycles") {
			t.Errorf("Summary missing max cycles: %q", all[0].Summary)
		}
	})

	t.Run("nil HailQueue is no-op", func(t *testing.T) {
		t.Parallel()
		l := &Loop{
			UI:        &noopUI{},
			HailQueue: nil,
		}
		state := &CycleState{MaxCycles: 3, Cycle: 3}
		// Should not panic.
		l.postMaxCyclesHail(state)
	})
}
