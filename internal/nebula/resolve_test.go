package nebula

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
)

func TestResolveExecution_BuiltInDefaults(t *testing.T) {
	r := ResolveExecution(0, 0, "", nil, nil, nil)
	if r.MaxReviewCycles != DefaultMaxReviewCycles {
		t.Errorf("expected %d cycles, got %d", DefaultMaxReviewCycles, r.MaxReviewCycles)
	}
	if r.MaxBudgetUSD != DefaultMaxBudgetUSD {
		t.Errorf("expected $%.2f budget, got $%.2f", DefaultMaxBudgetUSD, r.MaxBudgetUSD)
	}
	if r.Model != "" {
		t.Errorf("expected empty model, got %q", r.Model)
	}
}

func TestResolveExecution_GlobalOverridesDefaults(t *testing.T) {
	r := ResolveExecution(10, 20.0, "claude-sonnet", nil, nil, nil)
	if r.MaxReviewCycles != 10 {
		t.Errorf("expected 10 cycles, got %d", r.MaxReviewCycles)
	}
	if r.MaxBudgetUSD != 20.0 {
		t.Errorf("expected $20.00, got $%.2f", r.MaxBudgetUSD)
	}
	if r.Model != "claude-sonnet" {
		t.Errorf("expected claude-sonnet, got %q", r.Model)
	}
}

func TestResolveExecution_NebulaOverridesGlobal(t *testing.T) {
	neb := &Execution{MaxReviewCycles: 5, MaxBudgetUSD: 8.0, Model: "claude-opus"}
	r := ResolveExecution(10, 20.0, "claude-sonnet", neb, nil, nil)
	if r.MaxReviewCycles != 5 {
		t.Errorf("expected 5 cycles, got %d", r.MaxReviewCycles)
	}
	if r.MaxBudgetUSD != 8.0 {
		t.Errorf("expected $8.00, got $%.2f", r.MaxBudgetUSD)
	}
	if r.Model != "claude-opus" {
		t.Errorf("expected claude-opus, got %q", r.Model)
	}
}

func TestResolveExecution_PhaseOverridesNebula(t *testing.T) {
	neb := &Execution{MaxReviewCycles: 5, MaxBudgetUSD: 8.0, Model: "claude-opus"}
	phase := &PhaseSpec{MaxReviewCycles: 7, MaxBudgetUSD: 15.0, Model: "claude-haiku"}
	r := ResolveExecution(10, 20.0, "claude-sonnet", neb, phase, nil)
	if r.MaxReviewCycles != 7 {
		t.Errorf("expected 7 cycles, got %d", r.MaxReviewCycles)
	}
	if r.MaxBudgetUSD != 15.0 {
		t.Errorf("expected $15.00, got $%.2f", r.MaxBudgetUSD)
	}
	if r.Model != "claude-haiku" {
		t.Errorf("expected claude-haiku, got %q", r.Model)
	}
}

func TestResolveExecution_PartialOverrides(t *testing.T) {
	// Nebula sets cycles, phase sets budget, global sets model.
	neb := &Execution{MaxReviewCycles: 5}
	phase := &PhaseSpec{MaxBudgetUSD: 12.0}
	r := ResolveExecution(0, 0, "claude-sonnet", neb, phase, nil)
	if r.MaxReviewCycles != 5 {
		t.Errorf("expected 5 cycles from nebula, got %d", r.MaxReviewCycles)
	}
	if r.MaxBudgetUSD != 12.0 {
		t.Errorf("expected $12.00 from phase, got $%.2f", r.MaxBudgetUSD)
	}
	if r.Model != "claude-sonnet" {
		t.Errorf("expected claude-sonnet from global, got %q", r.Model)
	}
}

func TestResolveExecution_ZeroPhaseDoesNotOverride(t *testing.T) {
	neb := &Execution{MaxReviewCycles: 5, MaxBudgetUSD: 8.0}
	phase := &PhaseSpec{MaxReviewCycles: 0, MaxBudgetUSD: 0}
	r := ResolveExecution(0, 0, "", neb, phase, nil)
	if r.MaxReviewCycles != 5 {
		t.Errorf("zero phase cycles should not override nebula, got %d", r.MaxReviewCycles)
	}
	if r.MaxBudgetUSD != 8.0 {
		t.Errorf("zero phase budget should not override nebula, got $%.2f", r.MaxBudgetUSD)
	}
}

func TestResolveExecution_Routing(t *testing.T) {
	t.Parallel()

	enabledRouting := TierConfig{
		Enabled: true,
		Tiers:   DefaultTiers,
	}

	// simplePhase produces a low complexity score (small scope, short body, no deps).
	simplePhase := &PhaseSpec{
		ID:   "simple",
		Type: "task",
		Body: "Fix the typo.",
	}

	// complexPhase produces a high complexity score (large scope, long body).
	complexPhase := &PhaseSpec{
		ID:    "complex",
		Type:  "feature",
		Body:  strings.Repeat("x", 5000),
		Scope: []string{"a/", "b/", "c/", "d/", "e/", "f/", "g/", "h/", "i/", "j/"},
	}

	t.Run("NilRoutingContext", func(t *testing.T) {
		t.Parallel()
		r := ResolveExecution(0, 0, "", nil, simplePhase, nil)
		if r.Model != "" {
			t.Errorf("expected empty model with nil routing context, got %q", r.Model)
		}
		if r.RoutedTier != "" {
			t.Errorf("expected empty routed tier, got %q", r.RoutedTier)
		}
		if r.ComplexityScore != 0 {
			t.Errorf("expected zero complexity score, got %.2f", r.ComplexityScore)
		}
	})

	t.Run("RoutingDisabled", func(t *testing.T) {
		t.Parallel()
		rc := &RoutingContext{
			Routing: TierConfig{Enabled: false, Tiers: DefaultTiers},
		}
		r := ResolveExecution(0, 0, "", nil, simplePhase, rc)
		if r.Model != "" {
			t.Errorf("expected empty model with routing disabled, got %q", r.Model)
		}
		if r.RoutedTier != "" {
			t.Errorf("expected empty routed tier, got %q", r.RoutedTier)
		}
	})

	t.Run("RoutingSelectsFastTier", func(t *testing.T) {
		t.Parallel()
		rc := &RoutingContext{Routing: enabledRouting}
		r := ResolveExecution(0, 0, "", nil, simplePhase, rc)
		if r.Model != "claude-haiku" {
			t.Errorf("expected claude-haiku for simple phase, got %q", r.Model)
		}
		if r.RoutedTier != "fast" {
			t.Errorf("expected fast tier, got %q", r.RoutedTier)
		}
		if r.ComplexityScore == 0 {
			t.Error("expected non-zero complexity score")
		}
	})

	t.Run("RoutingSelectsHeavyTier", func(t *testing.T) {
		t.Parallel()
		rc := &RoutingContext{Routing: enabledRouting}
		r := ResolveExecution(0, 0, "", nil, complexPhase, rc)
		if r.RoutedTier != "heavy" {
			t.Errorf("expected heavy tier for complex phase, got %q", r.RoutedTier)
		}
		if r.Model != "claude-opus" {
			t.Errorf("expected claude-opus, got %q", r.Model)
		}
	})

	t.Run("PhaseModelOverridesRouting", func(t *testing.T) {
		t.Parallel()
		phase := &PhaseSpec{
			ID:    "pinned",
			Type:  "task",
			Body:  "Fix the typo.",
			Model: "claude-custom",
		}
		rc := &RoutingContext{Routing: enabledRouting}
		r := ResolveExecution(0, 0, "", nil, phase, rc)
		if r.Model != "claude-custom" {
			t.Errorf("phase model should win, got %q", r.Model)
		}
		if r.RoutedTier != "" {
			t.Errorf("routed tier should be empty when phase has explicit model, got %q", r.RoutedTier)
		}
	})

	t.Run("NebulaModelOverridesRouting", func(t *testing.T) {
		t.Parallel()
		neb := &Execution{Model: "claude-blanket"}
		rc := &RoutingContext{Routing: enabledRouting}
		r := ResolveExecution(0, 0, "", neb, simplePhase, rc)
		if r.Model != "claude-blanket" {
			t.Errorf("nebula model should win, got %q", r.Model)
		}
		if r.RoutedTier != "" {
			t.Errorf("routed tier should be empty when nebula has explicit model, got %q", r.RoutedTier)
		}
	})

	t.Run("GlobalModelOverridesRouting", func(t *testing.T) {
		t.Parallel()
		rc := &RoutingContext{Routing: enabledRouting}
		r := ResolveExecution(0, 0, "claude-global", nil, simplePhase, rc)
		if r.Model != "claude-global" {
			t.Errorf("global model should win, got %q", r.Model)
		}
		if r.RoutedTier != "" {
			t.Errorf("routed tier should be empty when global model is set, got %q", r.RoutedTier)
		}
	})

	t.Run("NilDAGStillRoutes", func(t *testing.T) {
		t.Parallel()
		rc := &RoutingContext{
			Routing: enabledRouting,
			DAG:     nil, // depth signal becomes 0
		}
		r := ResolveExecution(0, 0, "", nil, simplePhase, rc)
		if r.Model == "" {
			t.Error("expected a routed model even with nil DAG")
		}
		if r.RoutedTier == "" {
			t.Error("expected a routed tier even with nil DAG")
		}
	})

	t.Run("RoutingWithDAGDepth", func(t *testing.T) {
		t.Parallel()
		d := dag.New()
		d.AddNodeIdempotent("deep-phase", 0)
		d.AddNodeIdempotent("dep1", 0)
		d.AddNodeIdempotent("dep2", 0)
		d.AddNodeIdempotent("dep3", 0)
		_ = d.AddEdge("dep1", "deep-phase")
		_ = d.AddEdge("dep2", "dep1")
		_ = d.AddEdge("dep3", "dep2")

		phase := &PhaseSpec{
			ID:   "deep-phase",
			Type: "task",
			Body: "A phase with deep dependencies.",
		}
		rc := &RoutingContext{
			Routing: enabledRouting,
			DAG:     d,
		}
		r := ResolveExecution(0, 0, "", nil, phase, rc)
		if r.RoutedTier == "" {
			t.Error("expected routing to select a tier")
		}
		if r.ComplexityScore == 0 {
			t.Error("expected non-zero complexity score with depth")
		}
	})

	t.Run("NilPhaseSkipsRouting", func(t *testing.T) {
		t.Parallel()
		rc := &RoutingContext{Routing: enabledRouting}
		r := ResolveExecution(0, 0, "", nil, nil, rc)
		if r.RoutedTier != "" {
			t.Errorf("expected empty routed tier with nil phase, got %q", r.RoutedTier)
		}
		if r.Model != "" {
			t.Errorf("expected empty model with nil phase, got %q", r.Model)
		}
	})
}

func TestResolveExecution_AutoDecompose(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		name       string
		neb        *Execution
		phase      *PhaseSpec
		wantDecomp bool
	}{
		{
			name:       "DefaultDisabled",
			neb:        nil,
			phase:      &PhaseSpec{ID: "a"},
			wantDecomp: false,
		},
		{
			name:       "ManifestEnabled",
			neb:        &Execution{AutoDecompose: true},
			phase:      &PhaseSpec{ID: "a"},
			wantDecomp: true,
		},
		{
			name:       "ManifestDisabled",
			neb:        &Execution{AutoDecompose: false},
			phase:      &PhaseSpec{ID: "a"},
			wantDecomp: false,
		},
		{
			name:       "PhaseOverrideTrue",
			neb:        &Execution{AutoDecompose: false},
			phase:      &PhaseSpec{ID: "a", AutoDecompose: &trueVal},
			wantDecomp: true,
		},
		{
			name:       "PhaseOverrideFalse",
			neb:        &Execution{AutoDecompose: true},
			phase:      &PhaseSpec{ID: "a", AutoDecompose: &falseVal},
			wantDecomp: false,
		},
		{
			name:       "DecomposedPhaseBlocksEvenIfEnabled",
			neb:        &Execution{AutoDecompose: true},
			phase:      &PhaseSpec{ID: "a", Decomposed: true},
			wantDecomp: false,
		},
		{
			name:       "DecomposedOverridesPhaseOverride",
			neb:        &Execution{AutoDecompose: true},
			phase:      &PhaseSpec{ID: "a", AutoDecompose: &trueVal, Decomposed: true},
			wantDecomp: false,
		},
		{
			name:       "NilPhase",
			neb:        &Execution{AutoDecompose: true},
			phase:      nil,
			wantDecomp: true,
		},
		{
			name:       "NilEverything",
			neb:        nil,
			phase:      nil,
			wantDecomp: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := ResolveExecution(0, 0, "", tt.neb, tt.phase, nil)
			if r.AutoDecompose != tt.wantDecomp {
				t.Errorf("AutoDecompose = %v, want %v", r.AutoDecompose, tt.wantDecomp)
			}
		})
	}
}
