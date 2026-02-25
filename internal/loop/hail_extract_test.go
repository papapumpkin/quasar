package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/fabric"
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

func TestBridgeDiscoveryHails(t *testing.T) {
	t.Parallel()

	phaseID := "phase-xyz"
	cycle := 3

	t.Run("empty discoveries returns nil", func(t *testing.T) {
		t.Parallel()
		got := bridgeDiscoveryHails(nil, phaseID, cycle)
		if got != nil {
			t.Errorf("bridgeDiscoveryHails(nil) = %v, want nil", got)
		}
	})

	t.Run("skips resolved discoveries", func(t *testing.T) {
		t.Parallel()
		discoveries := []fabric.Discovery{
			{Kind: fabric.DiscoveryRequirementsAmbiguity, Detail: "unclear spec", Resolved: true},
		}
		got := bridgeDiscoveryHails(discoveries, phaseID, cycle)
		if len(got) != 0 {
			t.Errorf("bridgeDiscoveryHails(resolved) = %d hails, want 0", len(got))
		}
	})

	t.Run("skips unrelated discovery kinds", func(t *testing.T) {
		t.Parallel()
		discoveries := []fabric.Discovery{
			{Kind: fabric.DiscoveryFileConflict, Detail: "conflict on foo.go"},
			{Kind: fabric.DiscoveryBudgetAlert, Detail: "budget high"},
		}
		got := bridgeDiscoveryHails(discoveries, phaseID, cycle)
		if len(got) != 0 {
			t.Errorf("bridgeDiscoveryHails(unrelated) = %d hails, want 0", len(got))
		}
	})

	t.Run("bridges requirements_ambiguity", func(t *testing.T) {
		t.Parallel()
		discoveries := []fabric.Discovery{
			{
				Kind:       fabric.DiscoveryRequirementsAmbiguity,
				Detail:     "Should we use OAuth or JWT for auth?",
				SourceTask: "task-1",
				Affects:    "auth module",
			},
		}
		got := bridgeDiscoveryHails(discoveries, phaseID, cycle)
		if len(got) != 1 {
			t.Fatalf("got %d hails, want 1", len(got))
		}
		h := got[0]
		if h.Kind != HailAmbiguity {
			t.Errorf("Kind = %q, want %q", h.Kind, HailAmbiguity)
		}
		if h.PhaseID != phaseID {
			t.Errorf("PhaseID = %q, want %q", h.PhaseID, phaseID)
		}
		if h.Cycle != cycle {
			t.Errorf("Cycle = %d, want %d", h.Cycle, cycle)
		}
		if h.SourceRole != "agent" {
			t.Errorf("SourceRole = %q, want %q", h.SourceRole, "agent")
		}
		if !strings.Contains(h.Detail, "OAuth or JWT") {
			t.Errorf("Detail missing discovery detail: %q", h.Detail)
		}
		if !strings.Contains(h.Detail, "Affects: auth module") {
			t.Errorf("Detail missing affects: %q", h.Detail)
		}
	})

	t.Run("bridges missing_dependency", func(t *testing.T) {
		t.Parallel()
		discoveries := []fabric.Discovery{
			{
				Kind:       fabric.DiscoveryMissingDependency,
				Detail:     "Need redis client library",
				SourceTask: "task-2",
			},
		}
		got := bridgeDiscoveryHails(discoveries, phaseID, cycle)
		if len(got) != 1 {
			t.Fatalf("got %d hails, want 1", len(got))
		}
		h := got[0]
		if h.Kind != HailBlocker {
			t.Errorf("Kind = %q, want %q", h.Kind, HailBlocker)
		}
		if !strings.Contains(h.Summary, "redis client library") {
			t.Errorf("Summary missing detail: %q", h.Summary)
		}
	})

	t.Run("truncates long summaries", func(t *testing.T) {
		t.Parallel()
		longDetail := strings.Repeat("x", 200)
		discoveries := []fabric.Discovery{
			{Kind: fabric.DiscoveryRequirementsAmbiguity, Detail: longDetail, SourceTask: "t"},
		}
		got := bridgeDiscoveryHails(discoveries, phaseID, cycle)
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

	t.Run("mixed discoveries", func(t *testing.T) {
		t.Parallel()
		discoveries := []fabric.Discovery{
			{Kind: fabric.DiscoveryRequirementsAmbiguity, Detail: "ambiguous", SourceTask: "t1"},
			{Kind: fabric.DiscoveryMissingDependency, Detail: "missing lib", SourceTask: "t2"},
			{Kind: fabric.DiscoveryFileConflict, Detail: "conflict"},                          // skipped
			{Kind: fabric.DiscoveryRequirementsAmbiguity, Detail: "resolved", Resolved: true}, // skipped
		}
		got := bridgeDiscoveryHails(discoveries, phaseID, cycle)
		if len(got) != 2 {
			t.Fatalf("got %d hails, want 2", len(got))
		}
		if got[0].Kind != HailAmbiguity {
			t.Errorf("got[0].Kind = %q, want %q", got[0].Kind, HailAmbiguity)
		}
		if got[1].Kind != HailBlocker {
			t.Errorf("got[1].Kind = %q, want %q", got[1].Kind, HailBlocker)
		}
	})
}
