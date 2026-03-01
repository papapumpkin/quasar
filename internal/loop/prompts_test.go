package loop

import (
	"strings"
	"testing"
)

func TestBuildReviewerPrompt_NoPriorFindings(t *testing.T) {
	t.Parallel()

	l := &Loop{}
	state := &CycleState{
		TaskBeadID:  "test-123",
		TaskTitle:   "Fix the widget",
		Cycle:       1,
		CoderOutput: "I fixed the widget by updating foo.go.",
		AllFindings: nil,
	}

	prompt := l.buildReviewerPrompt(state)

	if strings.Contains(prompt, "[PRIOR FINDINGS]") {
		t.Error("cycle 1 prompt should not contain [PRIOR FINDINGS] block")
	}
	if strings.Contains(prompt, "VERIFICATION:") {
		t.Error("cycle 1 prompt should not contain VERIFICATION instructions")
	}
	// Verify standard review instructions are present.
	if !strings.Contains(prompt, "REVIEW INSTRUCTIONS:") {
		t.Error("expected REVIEW INSTRUCTIONS in prompt")
	}
	if !strings.Contains(prompt, "Task (bead test-123)") {
		t.Error("expected task bead ID in prompt")
	}
}

func TestBuildReviewerPrompt_WithPriorFindings(t *testing.T) {
	t.Parallel()

	l := &Loop{}
	state := &CycleState{
		TaskBeadID:  "test-456",
		TaskTitle:   "Refactor auth module",
		Cycle:       2,
		CoderOutput: "Applied fixes from reviewer feedback.",
		AllFindings: []ReviewFinding{
			{
				ID:          "f-abc123",
				Severity:    "critical",
				Description: "SQL injection in login handler",
				Cycle:       1,
				Status:      FindingStatusFound,
			},
			{
				ID:          "f-def456",
				Severity:    "minor",
				Description: "Unused variable in auth.go",
				Cycle:       1,
				Status:      FindingStatusFound,
			},
		},
	}

	prompt := l.buildReviewerPrompt(state)

	// Verify prior findings block is present.
	if !strings.Contains(prompt, "[PRIOR FINDINGS]") {
		t.Error("cycle 2 prompt should contain [PRIOR FINDINGS] block")
	}

	// Verify each finding's metadata appears.
	checks := []string{
		"id=f-abc123",
		"id=f-def456",
		"critical",
		"minor",
		"cycle=1",
		"status=found",
		"SQL injection in login handler",
		"Unused variable in auth.go",
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected prompt to contain %q", want)
		}
	}

	// Verify verification instructions are present.
	if !strings.Contains(prompt, "VERIFICATION:") {
		t.Error("expected VERIFICATION instructions in prompt")
	}
	if !strings.Contains(prompt, "FINDING_ID:") {
		t.Error("expected FINDING_ID field in verification instructions")
	}
	if !strings.Contains(prompt, "STATUS: fixed|still_present|regressed") {
		t.Error("expected STATUS options in verification instructions")
	}

	// Standard review instructions should still be present.
	if !strings.Contains(prompt, "REVIEW INSTRUCTIONS:") {
		t.Error("expected REVIEW INSTRUCTIONS in prompt")
	}
}

func TestBuildReviewerPrompt_EmptyAllFindings(t *testing.T) {
	t.Parallel()

	l := &Loop{}
	state := &CycleState{
		TaskBeadID:  "test-789",
		TaskTitle:   "Add tests",
		Cycle:       1,
		CoderOutput: "Added unit tests.",
		AllFindings: []ReviewFinding{},
	}

	prompt := l.buildReviewerPrompt(state)

	if strings.Contains(prompt, "[PRIOR FINDINGS]") {
		t.Error("empty AllFindings should not produce [PRIOR FINDINGS] block")
	}
}

func TestBuildPriorFindingsBlock(t *testing.T) {
	t.Parallel()

	t.Run("ContainsSerializedFindings", func(t *testing.T) {
		t.Parallel()

		findings := []ReviewFinding{
			{
				ID:          "f-111",
				Severity:    "critical",
				Description: "Null pointer dereference in handler",
				Cycle:       1,
				Status:      FindingStatusFound,
			},
			{
				ID:          "f-222",
				Severity:    "major",
				Description: "Missing error context in wrap",
				Cycle:       1,
				Status:      FindingStatusStillPresent,
			},
		}

		block := buildPriorFindingsBlock(findings)

		checks := []string{
			"[PRIOR FINDINGS]",
			"You MUST verify each one",
			"id=f-111",
			"id=f-222",
			"VERIFICATION:",
			"FINDING_ID:",
			"STATUS: fixed|still_present|regressed",
			"COMMENT:",
			"Report any NEW issues as ISSUE: blocks",
		}
		for _, want := range checks {
			if !strings.Contains(block, want) {
				t.Errorf("expected block to contain %q, got:\n%s", want, block)
			}
		}
	})

	t.Run("TruncatesDescriptions", func(t *testing.T) {
		t.Parallel()

		longDesc := strings.Repeat("a", 300)
		findings := []ReviewFinding{{
			ID:          "f-333",
			Severity:    "major",
			Description: longDesc,
			Cycle:       1,
			Status:      FindingStatusFound,
		}}

		block := buildPriorFindingsBlock(findings)

		if strings.Contains(block, longDesc) {
			t.Error("expected description to be truncated to 200 chars")
		}
		if !strings.Contains(block, "... [truncated]") {
			t.Error("expected truncation marker in output")
		}
	})
}

func TestBuildReviewerPrompt_WithLintOutput(t *testing.T) {
	t.Parallel()

	l := &Loop{}
	state := &CycleState{
		TaskBeadID:  "test-lint",
		TaskTitle:   "Fix linting",
		Cycle:       2,
		CoderOutput: "Applied fixes.",
		LintOutput:  "main.go:10: unused variable x",
		AllFindings: []ReviewFinding{{
			ID:          "f-lint1",
			Severity:    "minor",
			Description: "Unused variable",
			Cycle:       1,
			Status:      FindingStatusFound,
		}},
	}

	prompt := l.buildReviewerPrompt(state)

	// Both lint output and prior findings should be present.
	if !strings.Contains(prompt, "lint issues were not fully resolved") {
		t.Error("expected lint output note in prompt")
	}
	if !strings.Contains(prompt, "[PRIOR FINDINGS]") {
		t.Error("expected [PRIOR FINDINGS] block after lint output")
	}
}
