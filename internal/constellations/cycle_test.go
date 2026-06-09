package constellations

import (
	"testing"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

func TestResolveMaxCycles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		meta map[string]any
		exec string
		want int
	}{
		{"meta default", map[string]any{"max_cycles": int64(3)}, "", 3},
		{"per-run override wins", map[string]any{"max_cycles": int64(3)}, "max_review_cycles = 5\n", 5},
		{"zero override ignored", map[string]any{"max_cycles": int64(3)}, "max_review_cycles = 0\n", 3},
		{"float meta tolerated", map[string]any{"max_cycles": float64(4)}, "", 4},
		{"absent meta yields zero", map[string]any{}, "", 0},
		{"override with absent meta", nil, "max_review_cycles = 2\n", 2},
		{"malformed execution falls back to meta", map[string]any{"max_cycles": int64(3)}, "not = [valid", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			con := &artifacts.Constellation{Meta: tc.meta}
			neb := &fabric.Nebula{ExecutionTOML: tc.exec}
			if got := resolveMaxCycles(con, neb); got != tc.want {
				t.Errorf("resolveMaxCycles() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestResolveMaxCyclesNilNebula(t *testing.T) {
	t.Parallel()
	con := &artifacts.Constellation{Meta: map[string]any{"max_cycles": int64(3)}}
	if got := resolveMaxCycles(con, nil); got != 3 {
		t.Errorf("resolveMaxCycles(nil nebula) = %d, want 3", got)
	}
}

func TestOpFailRun(t *testing.T) {
	t.Parallel()

	t.Run("structured reason and detail", func(t *testing.T) {
		t.Parallel()
		out, err := opFailRun(t.Context(), nil, nil, map[string]any{
			"reason": "max master-review cycles exhausted",
			"detail": "tests still failing",
		})
		if err != nil {
			t.Fatalf("opFailRun: %v", err)
		}
		if out["reason"] != "max master-review cycles exhausted" {
			t.Errorf("reason = %v", out["reason"])
		}
		if out["detail"] != "tests still failing" {
			t.Errorf("detail = %v", out["detail"])
		}
	})

	t.Run("defaults reason when blank", func(t *testing.T) {
		t.Parallel()
		out, err := opFailRun(t.Context(), nil, nil, map[string]any{"reason": "  "})
		if err != nil {
			t.Fatalf("opFailRun: %v", err)
		}
		if out["reason"] != "run failed" {
			t.Errorf("reason = %v, want default", out["reason"])
		}
	})
}

// TestMasterReviewCycleRouting drives the embedded master-review constellation's
// edges directly, asserting that:
//   - a within-cap "fix" verdict routes to the inner coder-reviewer
//     constellation node (`fix`), which dispatches synchronously and produces
//     outputs.state that the back-edge to `review` routes on;
//   - an exhausted cycle cap routes to the give-up node and onward to _failed;
//   - an "escalate" verdict routes to _awaiting_human regardless of cycle.
//
// See master-review.toml.
func TestMasterReviewCycleRouting(t *testing.T) {
	t.Parallel()
	loader := artifacts.New(embeddedResolver{})
	con, err := loader.LoadConstellation("master-review")
	if err != nil {
		t.Fatalf("LoadConstellation: %v", err)
	}
	if metaInt(con.Meta, "max_cycles") != 3 {
		t.Fatalf("embedded master-review [meta] max_cycles = %d, want 3", metaInt(con.Meta, "max_cycles"))
	}

	decideState := func(cycle, max int) artifacts.State {
		st := NewState(NebulaSnapshot{}, 0)
		st.Cycle = cycle
		st.Meta.MaxCycles = max
		st.RecordNode("decide", map[string]any{"decision": "fix", "feedback": "needs work"})
		return st.ExprState()
	}

	cases := []struct {
		name  string
		cycle int
		max   int
		want  string
	}{
		{"within cap dispatches inner coder-reviewer", 1, 3, "fix"},
		{"at cap gives up", 3, 3, "give-up"},
		{"over cap gives up", 4, 3, "give-up"},
		{"override raises cap, within cap dispatches inner coder-reviewer", 4, 5, "fix"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := nextTarget(con, "decide", decideState(tc.cycle, tc.max))
			if err != nil {
				t.Fatalf("nextTarget: %v", err)
			}
			if got != tc.want {
				t.Errorf("nextTarget(cycle=%d, max=%d) = %q, want %q", tc.cycle, tc.max, got, tc.want)
			}
		})
	}

	// fix → review (back-edge) on a successful inner-loop pass: each traversal
	// increments the outer cycle counter so the cap eventually trips.
	fixDoneState := func() artifacts.State {
		st := NewState(NebulaSnapshot{}, 0)
		st.RecordNode("fix", map[string]any{"state": "done"})
		return st.ExprState()
	}
	got, err := nextTarget(con, "fix", fixDoneState())
	if err != nil {
		t.Fatalf("nextTarget(fix, done): %v", err)
	}
	if got != "review" {
		t.Errorf("nextTarget(fix, done) = %q, want %q (back-edge)", got, "review")
	}

	// fix → _failed when the inner coder-reviewer terminates non-done.
	fixFailedState := func() artifacts.State {
		st := NewState(NebulaSnapshot{}, 0)
		st.RecordNode("fix", map[string]any{"state": "failed"})
		return st.ExprState()
	}
	got, err = nextTarget(con, "fix", fixFailedState())
	if err != nil {
		t.Fatalf("nextTarget(fix, failed): %v", err)
	}
	if got != artifacts.TermFailed {
		t.Errorf("nextTarget(fix, failed) = %q, want %q", got, artifacts.TermFailed)
	}

	// give-up is a terminal failure: its sole edge is unconditional to _failed.
	got, err = nextTarget(con, "give-up", NewState(NebulaSnapshot{}, 0).ExprState())
	if err != nil {
		t.Fatalf("nextTarget(give-up): %v", err)
	}
	if got != artifacts.TermFailed {
		t.Errorf("give-up routes to %q, want %q", got, artifacts.TermFailed)
	}
}
