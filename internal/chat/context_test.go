package chat

import (
	"strings"
	"testing"
)

func TestContextBuilder_Build(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		ctx          PhaseContext
		wantContains []string
		wantMissing  []string
	}{
		{
			name: "full context includes all sections",
			ctx: PhaseContext{
				PhaseID:          "fix-bug-42",
				PhaseSpec:        "## Problem\n\nThe widget crashes on nil input.",
				Cycle:            3,
				MaxCycles:        5,
				LastSummary:      "Added nil check to widget.Process()",
				DiffStat:         "+15 -3 across 2 files",
				ReviewerFindings: "Missing test coverage for edge case",
				FileClaims:       []string{"internal/widget/process.go", "internal/widget/process_test.go"},
			},
			wantContains: []string{
				"# Phase Execution Context",
				"**Phase ID:** fix-bug-42",
				"**Cycle:** 3 / 5",
				"## Phase Specification",
				"The widget crashes on nil input",
				"## Last Agent Summary",
				"Added nil check",
				"## Current Diff",
				"+15 -3 across 2 files",
				"## Reviewer Findings",
				"Missing test coverage",
				"## File Claims",
				"- internal/widget/process.go",
				"- internal/widget/process_test.go",
			},
		},
		{
			name: "minimal context omits empty sections",
			ctx: PhaseContext{
				PhaseID: "simple-task",
			},
			wantContains: []string{
				"**Phase ID:** simple-task",
			},
			wantMissing: []string{
				"## Phase Specification",
				"## Last Agent Summary",
				"## Current Diff",
				"## Reviewer Findings",
				"## File Claims",
			},
		},
		{
			name: "cycle without max",
			ctx: PhaseContext{
				PhaseID: "task-1",
				Cycle:   2,
			},
			wantContains: []string{
				"**Cycle:** 2\n",
			},
			wantMissing: []string{
				"/ ",
			},
		},
		{
			name: "includes assistant instructions",
			ctx: PhaseContext{
				PhaseID: "any",
			},
			wantContains: []string{
				"assisting a developer",
				"informed feedback",
			},
		},
	}

	cb := NewContextBuilder()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cb.Build(tt.ctx)

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("Build() missing %q\ngot:\n%s", want, got)
				}
			}
			for _, notWant := range tt.wantMissing {
				if strings.Contains(got, notWant) {
					t.Errorf("Build() should not contain %q\ngot:\n%s", notWant, got)
				}
			}
		})
	}
}

func TestNewContextBuilder(t *testing.T) {
	t.Parallel()
	cb := NewContextBuilder()
	if cb == nil {
		t.Fatal("NewContextBuilder() returned nil")
	}
}
