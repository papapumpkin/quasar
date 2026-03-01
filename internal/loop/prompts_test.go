package loop

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/filter"
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

// ---------------------------------------------------------------------------
// TestBuildFilterFixPrompt
// ---------------------------------------------------------------------------

func TestBuildFilterFixPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		loop   *Loop
		state  *CycleState
		parsed filter.ParseResult
		checks []string // substrings that must be present
		absent []string // substrings that must NOT be present
	}{
		{
			name:  "StructuredErrors_SingleFile",
			loop:  &Loop{MaxBudgetUSD: 10.0, MaxCycles: 5},
			state: &CycleState{TaskBeadID: "bead-100"},
			parsed: filter.ParseResult{
				CheckName: "build",
				RawOutput: "raw build output",
				Errors: []filter.Error{
					{File: "internal/loop/loop.go", Line: 42, Column: 15, Message: "undefined: foo", Tool: "build"},
					{File: "internal/loop/loop.go", Line: 58, Column: 3, Message: "cannot use x as string", Tool: "build"},
				},
			},
			checks: []string{
				"bead-100",
				"Fix failing build check",
				"failed the build filter check",
				"ERRORS:",
				"internal/loop/loop.go:42:15 — undefined: foo",
				"internal/loop/loop.go:58:3 — cannot use x as string",
				"AFFECTED FILES:",
				"internal/loop/loop.go (2 errors)",
				"Read each affected file",
				"Do NOT make any other changes",
				"targeted fix pass",
			},
			absent: []string{
				"RAW OUTPUT:",
				"raw build output",
			},
		},
		{
			name:  "StructuredErrors_MultiFile_SortedByCount",
			loop:  &Loop{MaxBudgetUSD: 8.0, MaxCycles: 4},
			state: &CycleState{TaskBeadID: "bead-200"},
			parsed: filter.ParseResult{
				CheckName: "vet",
				Errors: []filter.Error{
					{File: "cmd/run.go", Line: 10, Column: 5, Message: "unreachable code", Tool: "vet"},
					{File: "internal/loop/loop.go", Line: 42, Column: 15, Message: "error one", Tool: "vet"},
					{File: "internal/loop/loop.go", Line: 58, Column: 3, Message: "error two", Tool: "vet"},
					{File: "internal/loop/loop.go", Line: 20, Column: 1, Message: "error zero", Tool: "vet"},
				},
			},
			checks: []string{
				"Fix failing vet check",
				"ERRORS:",
				"AFFECTED FILES:",
				"internal/loop/loop.go (3 errors)",
				"cmd/run.go (1 error)",
			},
		},
		{
			name:  "RawFallback_NoStructuredErrors",
			loop:  &Loop{MaxBudgetUSD: 10.0, MaxCycles: 5},
			state: &CycleState{TaskBeadID: "bead-300"},
			parsed: filter.ParseResult{
				CheckName: "test",
				RawOutput: "FAIL: some test failed with unexpected output",
				Errors:    nil,
			},
			checks: []string{
				"bead-300",
				"Fix failing test check",
				"RAW OUTPUT:",
				"FAIL: some test failed with unexpected output",
			},
			absent: []string{
				"ERRORS:",
				"AFFECTED FILES:",
			},
		},
		{
			name:  "EmptyErrors_EmptyRaw",
			loop:  &Loop{MaxBudgetUSD: 10.0, MaxCycles: 5},
			state: &CycleState{TaskBeadID: "bead-400"},
			parsed: filter.ParseResult{
				CheckName: "build",
				RawOutput: "",
				Errors:    []filter.Error{},
			},
			checks: []string{
				"Fix failing build check",
				"RAW OUTPUT:",
			},
			absent: []string{
				"ERRORS:",
				"AFFECTED FILES:",
			},
		},
		{
			name:  "NoBudget_NoBudgetHint",
			loop:  &Loop{MaxBudgetUSD: 0, MaxCycles: 5},
			state: &CycleState{TaskBeadID: "bead-500"},
			parsed: filter.ParseResult{
				CheckName: "lint",
				Errors: []filter.Error{
					{File: "main.go", Line: 1, Column: 1, Message: "unused", Tool: "lint"},
				},
			},
			checks: []string{
				"Fix failing lint check",
				"ERRORS:",
			},
			absent: []string{
				"targeted fix pass",
			},
		},
		{
			name:  "ErrorWithoutColumn",
			loop:  &Loop{MaxBudgetUSD: 10.0, MaxCycles: 5},
			state: &CycleState{TaskBeadID: "bead-600"},
			parsed: filter.ParseResult{
				CheckName: "test",
				Errors: []filter.Error{
					{File: "foo_test.go", Line: 15, Column: 0, Message: "[TestFoo] expected true", Tool: "test"},
				},
			},
			checks: []string{
				"foo_test.go:15 — [TestFoo] expected true",
			},
			absent: []string{
				"foo_test.go:15:0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prompt := tt.loop.buildFilterFixPrompt(tt.state, tt.parsed)

			for _, want := range tt.checks {
				if !strings.Contains(prompt, want) {
					t.Errorf("expected prompt to contain %q\ngot:\n%s", want, prompt)
				}
			}
			for _, notWant := range tt.absent {
				if strings.Contains(prompt, notWant) {
					t.Errorf("expected prompt NOT to contain %q\ngot:\n%s", notWant, prompt)
				}
			}
		})
	}
}

// TestBuildFilterFixPrompt_ErrorOrdering verifies that errors within a file
// are sorted by line number and files are sorted by error count.
func TestBuildFilterFixPrompt_ErrorOrdering(t *testing.T) {
	t.Parallel()

	l := &Loop{MaxBudgetUSD: 10.0, MaxCycles: 5}
	state := &CycleState{TaskBeadID: "bead-ord"}
	parsed := filter.ParseResult{
		CheckName: "build",
		Errors: []filter.Error{
			{File: "b.go", Line: 99, Column: 1, Message: "err b:99", Tool: "build"},
			{File: "a.go", Line: 50, Column: 1, Message: "err a:50", Tool: "build"},
			{File: "a.go", Line: 10, Column: 1, Message: "err a:10", Tool: "build"},
			{File: "a.go", Line: 30, Column: 1, Message: "err a:30", Tool: "build"},
		},
	}

	prompt := l.buildFilterFixPrompt(state, parsed)

	// a.go has 3 errors, b.go has 1, so a.go should appear first in AFFECTED FILES.
	aIdx := strings.Index(prompt, "a.go (3 errors)")
	bIdx := strings.Index(prompt, "b.go (1 error)")
	if aIdx < 0 || bIdx < 0 {
		t.Fatalf("expected both files in AFFECTED FILES section\ngot:\n%s", prompt)
	}
	if aIdx > bIdx {
		t.Error("expected a.go (3 errors) to appear before b.go (1 error)")
	}

	// Within a.go, errors should be sorted: line 10, 30, 50.
	idx10 := strings.Index(prompt, "a.go:10:1")
	idx30 := strings.Index(prompt, "a.go:30:1")
	idx50 := strings.Index(prompt, "a.go:50:1")
	if idx10 < 0 || idx30 < 0 || idx50 < 0 {
		t.Fatalf("expected all a.go errors in prompt\ngot:\n%s", prompt)
	}
	if idx10 >= idx30 || idx30 >= idx50 {
		t.Errorf("expected a.go errors sorted by line: 10 < 30 < 50, got indices %d, %d, %d", idx10, idx30, idx50)
	}
}

// TestFilterFixBudget verifies the 1/4 budget calculation.
func TestFilterFixBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		maxBudget  float64
		maxCycles  int
		wantBudget float64
	}{
		{
			name:       "NormalBudget",
			maxBudget:  10.0,
			maxCycles:  5,
			wantBudget: 0.25, // 10 / (2*5) = 1.0; 1.0 / 4 = 0.25
		},
		{
			name:       "ZeroBudget",
			maxBudget:  0,
			maxCycles:  5,
			wantBudget: 0,
		},
		{
			name:       "NegativeBudget",
			maxBudget:  -5.0,
			maxCycles:  5,
			wantBudget: 0,
		},
		{
			name:       "SingleCycle",
			maxBudget:  4.0,
			maxCycles:  1,
			wantBudget: 0.5, // 4 / (2*1) = 2.0; 2.0 / 4 = 0.5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := &Loop{MaxBudgetUSD: tt.maxBudget, MaxCycles: tt.maxCycles}
			got := l.filterFixBudget()
			if got != tt.wantBudget {
				t.Errorf("filterFixBudget() = %v, want %v", got, tt.wantBudget)
			}
		})
	}
}

// TestBuildLintFixPrompt_DelegatesToFilterFix verifies that buildLintFixPrompt
// now delegates to buildFilterFixPrompt, producing the new prompt format.
func TestBuildLintFixPrompt_DelegatesToFilterFix(t *testing.T) {
	t.Parallel()

	l := &Loop{MaxBudgetUSD: 10.0, MaxCycles: 5}
	state := &CycleState{
		TaskBeadID: "bead-lint-delegate",
		TaskTitle:  "fix lint",
		LintOutput: "internal/loop/loop.go:42:15: unused import (unused)",
	}

	prompt := l.buildLintFixPrompt(state)

	// Should use the filter fix prompt format, not the old format.
	checks := []string{
		"bead-lint-delegate",
		"Fix failing lint check",
		"ERRORS:",
		"AFFECTED FILES:",
		"internal/loop/loop.go",
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected prompt to contain %q\ngot:\n%s", want, prompt)
		}
	}
}

// TestBuildLintFixPrompt_EmptyLintOutput verifies the edge case where
// buildLintFixPrompt is called with no lint output.
func TestBuildLintFixPrompt_EmptyLintOutput(t *testing.T) {
	t.Parallel()

	l := &Loop{}
	state := &CycleState{
		TaskBeadID: "bead-empty-lint",
		LintOutput: "",
	}

	prompt := l.buildLintFixPrompt(state)

	if !strings.Contains(prompt, "bead-empty-lint") {
		t.Error("prompt should contain bead ID")
	}
	if !strings.Contains(prompt, "lint") {
		t.Error("prompt should reference lint")
	}
}
