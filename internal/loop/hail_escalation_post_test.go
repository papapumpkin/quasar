package loop

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestBuildMaxCyclesHail(t *testing.T) {
	t.Parallel()

	t.Run("basic max cycles hail", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{MaxCycles: 5, Cycle: 5}
		h := buildMaxCyclesHail(state, "phase-max")
		if h.Kind != HailBlocker || h.PhaseID != "phase-max" || h.Cycle != 5 {
			t.Errorf("got Kind=%q PhaseID=%q Cycle=%d, want blocker/phase-max/5", h.Kind, h.PhaseID, h.Cycle)
		}
		if !strings.Contains(h.Summary, "Max cycles") {
			t.Errorf("Summary missing max cycles: %q", h.Summary)
		}
		if !strings.Contains(h.Detail, "maximum of 5 cycles") || len(h.Options) != 3 {
			t.Errorf("Detail or Options unexpected: detail=%q options=%v", h.Detail, h.Options)
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
