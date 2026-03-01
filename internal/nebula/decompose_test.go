package nebula

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

func TestParseDecomposeOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCount int
		wantIDs   []string
		wantErr   string
	}{
		{
			name: "valid 2 sub-phases",
			input: `Some preamble text.

PHASE_FILE: setup-auth-part-1.md
+++
id = "setup-auth-part-1"
title = "Set up auth models"
depends_on = []
+++

## Problem

Set up the data models.

## Acceptance Criteria

- [ ] Models created
END_PHASE_FILE

PHASE_FILE: setup-auth-part-2.md
+++
id = "setup-auth-part-2"
title = "Set up auth routes"
depends_on = ["setup-auth-part-1"]
+++

## Problem

Set up the routes.

## Acceptance Criteria

- [ ] Routes created
END_PHASE_FILE
`,
			wantCount: 2,
			wantIDs:   []string{"setup-auth-part-1", "setup-auth-part-2"},
		},
		{
			name: "valid 3 sub-phases",
			input: `PHASE_FILE: impl-api-part-1.md
+++
id = "impl-api-part-1"
title = "Define API types"
+++

## Problem
Define request/response types.

## Acceptance Criteria
- [ ] Types defined
END_PHASE_FILE

PHASE_FILE: impl-api-part-2.md
+++
id = "impl-api-part-2"
title = "Implement handlers"
depends_on = ["impl-api-part-1"]
+++

## Problem
Write the HTTP handlers.

## Acceptance Criteria
- [ ] Handlers working
END_PHASE_FILE

PHASE_FILE: impl-api-part-3.md
+++
id = "impl-api-part-3"
title = "Add API tests"
depends_on = ["impl-api-part-2"]
+++

## Problem
Write integration tests.

## Acceptance Criteria
- [ ] Tests pass
END_PHASE_FILE
`,
			wantCount: 3,
			wantIDs:   []string{"impl-api-part-1", "impl-api-part-2", "impl-api-part-3"},
		},
		{
			name:    "no markers",
			input:   "Just some text without any phase files.",
			wantErr: "contains no",
		},
		{
			name: "missing end marker",
			input: `PHASE_FILE: broken.md
+++
id = "broken"
title = "Broken phase"
+++

No end marker here.
`,
			wantErr: "missing",
		},
		{
			name:    "missing newline after marker",
			input:   `PHASE_FILE: no-newline.md`,
			wantErr: "missing newline",
		},
		{
			name: "invalid filename with path traversal",
			input: `PHASE_FILE: ../evil.md
+++
id = "evil"
title = "Evil"
+++

## Problem
Evil.
END_PHASE_FILE
`,
			wantErr: "path separators",
		},
		{
			name: "invalid frontmatter",
			input: `PHASE_FILE: bad-toml.md
+++
id = [invalid toml
+++

Body.
END_PHASE_FILE
`,
			wantErr: "parsing TOML",
		},
		{
			name: "missing frontmatter delimiters",
			input: `PHASE_FILE: no-frontmatter.md
Just content without +++ delimiters.
END_PHASE_FILE
`,
			wantErr: "parsing frontmatter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results, err := parseDecomposeOutput(tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(results) != tt.wantCount {
				t.Fatalf("expected %d results, got %d", tt.wantCount, len(results))
			}
			for i, wantID := range tt.wantIDs {
				if results[i].PhaseSpec.ID != wantID {
					t.Errorf("result[%d].ID = %q, want %q", i, results[i].PhaseSpec.ID, wantID)
				}
			}
		})
	}
}

func TestRunDecompose(t *testing.T) {
	t.Parallel()

	baseNebula := &Nebula{
		Dir: "/tmp/test-nebula",
		Manifest: Manifest{
			Nebula: Info{Name: "test-nebula", Description: "A test nebula"},
			Defaults: Defaults{
				Type:     "task",
				Priority: 2,
			},
			Execution: Execution{
				MaxBudgetUSD: 10.0,
			},
		},
		Phases: []PhaseSpec{
			{ID: "setup", Title: "Setup", Type: "task"},
			{ID: "struggling-phase", Title: "Struggling Phase", Type: "task", DependsOn: []string{"setup"}},
			{ID: "final", Title: "Final", Type: "task", DependsOn: []string{"struggling-phase"}},
		},
	}

	makeOutput := func(phases ...string) string {
		var b strings.Builder
		for _, p := range phases {
			b.WriteString(p)
		}
		return b.String()
	}

	phase := func(id, title string, deps []string) string {
		depStr := "[]"
		if len(deps) > 0 {
			quoted := make([]string, len(deps))
			for i, d := range deps {
				quoted[i] = fmt.Sprintf("%q", d)
			}
			depStr = "[" + strings.Join(quoted, ", ") + "]"
		}
		return fmt.Sprintf(`PHASE_FILE: %s.md
+++
id = "%s"
title = "%s"
depends_on = %s
+++

## Problem
Part of the decomposed phase.

## Acceptance Criteria
- [ ] Done
END_PHASE_FILE

`, id, id, title, depStr)
	}

	tests := []struct {
		name         string
		nebula       *Nebula
		phaseID      string
		output       string
		invokeErr    error
		wantErr      string
		wantCount    int
		wantWarnings int // number of non-fatal errors in DecomposeResult.Errors
	}{
		{
			name:    "nil nebula",
			nebula:  nil,
			phaseID: "test",
			wantErr: "non-nil nebula",
		},
		{
			name:    "empty phase ID",
			nebula:  baseNebula,
			phaseID: "",
			wantErr: "non-empty phase ID",
		},
		{
			name:      "invoker error",
			nebula:    baseNebula,
			phaseID:   "struggling-phase",
			invokeErr: fmt.Errorf("model unavailable"),
			wantErr:   "decompose invocation failed",
		},
		{
			name:    "valid 2 sub-phases",
			nebula:  baseNebula,
			phaseID: "struggling-phase",
			output: makeOutput(
				phase("struggling-phase-part-1", "Part 1", []string{"setup"}),
				phase("struggling-phase-part-2", "Part 2", []string{"struggling-phase-part-1"}),
			),
			wantCount: 2,
		},
		{
			name:    "valid 3 sub-phases",
			nebula:  baseNebula,
			phaseID: "struggling-phase",
			output: makeOutput(
				phase("struggling-phase-part-1", "Part 1", []string{"setup"}),
				phase("struggling-phase-part-2", "Part 2", []string{"struggling-phase-part-1"}),
				phase("struggling-phase-part-3", "Part 3", []string{"struggling-phase-part-2"}),
			),
			wantCount: 3,
		},
		{
			name:    "reject 1 sub-phase",
			nebula:  baseNebula,
			phaseID: "struggling-phase",
			output: makeOutput(
				phase("struggling-phase-part-1", "Part 1", nil),
			),
			wantErr: "need at least 2",
		},
		{
			name:    "reject 4 sub-phases",
			nebula:  baseNebula,
			phaseID: "struggling-phase",
			output: makeOutput(
				phase("struggling-phase-part-1", "Part 1", nil),
				phase("struggling-phase-part-2", "Part 2", nil),
				phase("struggling-phase-part-3", "Part 3", nil),
				phase("struggling-phase-part-4", "Part 4", nil),
			),
			wantErr: "maximum is 3",
		},
		{
			name:    "ID prefix validation warning",
			nebula:  baseNebula,
			phaseID: "struggling-phase",
			output: makeOutput(
				phase("wrong-prefix-1", "Part 1", nil),
				phase("wrong-prefix-2", "Part 2", nil),
			),
			wantCount:    2,
			wantWarnings: 2,
		},
		{
			name:    "unknown dependency warning",
			nebula:  baseNebula,
			phaseID: "struggling-phase",
			output: makeOutput(
				phase("struggling-phase-part-1", "Part 1", []string{"nonexistent-phase"}),
				phase("struggling-phase-part-2", "Part 2", nil),
			),
			wantCount:    2,
			wantWarnings: 1,
		},
		{
			name:    "defaults applied",
			nebula:  baseNebula,
			phaseID: "struggling-phase",
			output: makeOutput(
				phase("struggling-phase-part-1", "Part 1", nil),
				phase("struggling-phase-part-2", "Part 2", nil),
			),
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inv := &mockInvoker{
				result: agent.InvocationResult{ResultText: tt.output},
				err:    tt.invokeErr,
			}

			req := ArchitectRequest{
				Mode:           ArchitectModeDecompose,
				Nebula:         tt.nebula,
				PhaseID:        tt.phaseID,
				StruggleReason: "repeated filter failures",
				CyclesUsed:     3,
				CostSoFar:      1.50,
				AllFindings: []DecomposeFinding{
					{Severity: "error", Description: "lint failure", Cycle: 1},
					{Severity: "warning", Description: "unused import", Cycle: 2},
				},
			}

			result, err := RunDecompose(context.Background(), inv, req)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.SubPhases) != tt.wantCount {
				t.Fatalf("expected %d sub-phases, got %d", tt.wantCount, len(result.SubPhases))
			}
			if result.OriginalPhaseID != tt.phaseID {
				t.Errorf("OriginalPhaseID = %q, want %q", result.OriginalPhaseID, tt.phaseID)
			}
			if tt.wantWarnings > 0 && len(result.Errors) < tt.wantWarnings {
				t.Errorf("expected at least %d warnings, got %d: %v", tt.wantWarnings, len(result.Errors), result.Errors)
			}

			// Verify defaults are applied for successful cases.
			if tt.name == "defaults applied" {
				for i, sp := range result.SubPhases {
					if sp.PhaseSpec.Type != "task" {
						t.Errorf("sub-phase[%d].Type = %q, want %q", i, sp.PhaseSpec.Type, "task")
					}
					if sp.PhaseSpec.Priority != 2 {
						t.Errorf("sub-phase[%d].Priority = %d, want %d", i, sp.PhaseSpec.Priority, 2)
					}
				}
			}
		})
	}
}

func TestBuildDecomposePrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      ArchitectRequest
		wantSubs []string // substrings that must appear in the prompt
	}{
		{
			name: "includes all context",
			req: ArchitectRequest{
				Mode:    ArchitectModeDecompose,
				PhaseID: "struggling-phase",
				Nebula: &Nebula{
					Manifest: Manifest{
						Nebula: Info{Name: "my-nebula", Description: "Test"},
					},
					Phases: []PhaseSpec{
						{ID: "struggling-phase", Title: "Struggling Phase", Type: "task", Priority: 1, Body: "## Problem\n\nDo the thing."},
						{ID: "other-phase", Title: "Other Phase", Type: "task"},
					},
				},
				StruggleReason: "repeated filter failures and recurring findings",
				CyclesUsed:     4,
				CostSoFar:      2.75,
				AllFindings: []DecomposeFinding{
					{Severity: "error", Description: "compilation failed", Cycle: 1},
					{Severity: "error", Description: "test timeout", Cycle: 3},
				},
			},
			wantSubs: []string{
				"struggling-phase",
				"Struggling Phase",
				"Do the thing",
				"repeated filter failures",
				"Cycles used",
				"$2.75",
				"compilation failed",
				"test timeout",
				"my-nebula",
				"other-phase",
				"Decomposition Request",
			},
		},
		{
			name: "empty struggle context",
			req: ArchitectRequest{
				Mode:    ArchitectModeDecompose,
				PhaseID: "phase-x",
				Nebula: &Nebula{
					Manifest: Manifest{
						Nebula: Info{Name: "test"},
					},
					Phases: []PhaseSpec{
						{ID: "phase-x", Title: "Phase X"},
					},
				},
			},
			wantSubs: []string{
				"phase-x",
				"Phase X",
				"Cycles used",
				"$0.00",
			},
		},
		{
			name: "skips decomposed phase in existing phases list",
			req: ArchitectRequest{
				Mode:    ArchitectModeDecompose,
				PhaseID: "target",
				Nebula: &Nebula{
					Manifest: Manifest{
						Nebula: Info{Name: "test"},
					},
					Phases: []PhaseSpec{
						{ID: "target", Title: "Target Phase"},
						{ID: "other", Title: "Other Phase"},
					},
				},
			},
			wantSubs: []string{
				"other",
				"Other Phase",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prompt, err := buildDecomposePrompt(tt.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, sub := range tt.wantSubs {
				if !strings.Contains(prompt, sub) {
					t.Errorf("prompt missing expected substring %q", sub)
				}
			}
		})
	}
}

func TestRunDecomposeSystemPrompt(t *testing.T) {
	t.Parallel()

	inv := &mockInvoker{
		result: agent.InvocationResult{
			ResultText: `PHASE_FILE: test-part-1.md
+++
id = "test-part-1"
title = "Part 1"
+++

Body 1.
END_PHASE_FILE

PHASE_FILE: test-part-2.md
+++
id = "test-part-2"
title = "Part 2"
+++

Body 2.
END_PHASE_FILE
`,
		},
	}

	req := ArchitectRequest{
		Mode:    ArchitectModeDecompose,
		PhaseID: "test",
		Nebula: &Nebula{
			Dir: "/tmp",
			Manifest: Manifest{
				Nebula:    Info{Name: "test"},
				Defaults:  Defaults{Type: "task", Priority: 2},
				Execution: Execution{MaxBudgetUSD: 5.0},
			},
			Phases: []PhaseSpec{
				{ID: "test", Title: "Test Phase"},
			},
		},
	}

	_, err := RunDecompose(context.Background(), inv, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the agent was invoked with the decompose system prompt.
	if inv.lastAgent.SystemPrompt != decomposeSystemPrompt {
		t.Error("expected decompose system prompt to be used")
	}
	if inv.lastAgent.Role != agent.RoleArchitect {
		t.Errorf("expected architect role, got %q", inv.lastAgent.Role)
	}
}
