package agent

import (
	"strings"
	"testing"
)

func TestRoleNeedsFullNebula(t *testing.T) {
	t.Parallel()

	cases := map[Role]bool{
		RoleArchitect: true,
		RoleCoder:     false,
		RoleReviewer:  false,
		Role("other"): false,
	}
	for role, want := range cases {
		role, want := role, want
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			if got := RoleNeedsFullNebula(role); got != want {
				t.Errorf("RoleNeedsFullNebula(%q) = %v, want %v", role, got, want)
			}
		})
	}
}

func TestBudgetForRole(t *testing.T) {
	t.Parallel()

	t.Run("coder default does not include siblings", func(t *testing.T) {
		t.Parallel()
		b := BudgetForRole(RoleCoder)
		if b.IncludeSiblingPhases {
			t.Error("coder should not include sibling phases")
		}
		if b.ToolResultMaxBytes != 16*1024 {
			t.Errorf("coder ToolResultMaxBytes = %d, want 16384", b.ToolResultMaxBytes)
		}
	})

	t.Run("reviewer gets a larger tool result cap", func(t *testing.T) {
		t.Parallel()
		b := BudgetForRole(RoleReviewer)
		if b.ToolResultMaxBytes <= BudgetForRole(RoleCoder).ToolResultMaxBytes {
			t.Error("reviewer should allow a larger tool result cap than coder")
		}
	})

	t.Run("architect includes siblings", func(t *testing.T) {
		t.Parallel()
		if !BudgetForRole(RoleArchitect).IncludeSiblingPhases {
			t.Error("architect should include sibling phases")
		}
	})
}

func TestRenderPhaseContext(t *testing.T) {
	t.Parallel()

	goals := []string{"Goal A", "Goal B"}
	constraints := []string{"Constraint A"}
	current := "Implement the truncate fix."
	siblings := []string{"SIBLING ONE BODY", "SIBLING TWO BODY"}

	t.Run("coder includes only the current phase", func(t *testing.T) {
		t.Parallel()
		out := RenderPhaseContext(PhaseContextInput{
			Role:          RoleCoder,
			Goals:         goals,
			Constraints:   constraints,
			CurrentPhase:  current,
			SiblingPhases: siblings,
		})
		if !strings.Contains(out, current) {
			t.Error("current phase body must be present")
		}
		if !strings.Contains(out, "Goal A") || !strings.Contains(out, "Constraint A") {
			t.Error("goals and constraints must be present")
		}
		for _, s := range siblings {
			if strings.Contains(out, s) {
				t.Errorf("sibling phase %q must be elided for coder", s)
			}
		}
	})

	t.Run("architect includes sibling phases", func(t *testing.T) {
		t.Parallel()
		out := RenderPhaseContext(PhaseContextInput{
			Role:          RoleArchitect,
			Goals:         goals,
			Constraints:   constraints,
			CurrentPhase:  current,
			SiblingPhases: siblings,
		})
		for _, s := range siblings {
			if !strings.Contains(out, s) {
				t.Errorf("architect output must include sibling phase %q", s)
			}
		}
	})

	t.Run("no goals or constraints yields phase body alone for coder", func(t *testing.T) {
		t.Parallel()
		out := RenderPhaseContext(PhaseContextInput{
			Role:         RoleCoder,
			CurrentPhase: current,
		})
		if out != current {
			t.Errorf("expected bare phase body, got %q", out)
		}
	})

	t.Run("large sibling specs do not bloat coder context", func(t *testing.T) {
		t.Parallel()
		// A nebula whose sibling specs alone exceed 80 KB.
		big := []string{
			strings.Repeat("S", 80*1024),
			strings.Repeat("T", 40*1024),
		}
		out := RenderPhaseContext(PhaseContextInput{
			Role:          RoleCoder,
			Goals:         goals,
			Constraints:   constraints,
			CurrentPhase:  current,
			SiblingPhases: big,
		})
		if len(out) > 60*1024 {
			t.Errorf("coder context = %d bytes, expected <= 60KB after phase-only injection", len(out))
		}
	})
}
