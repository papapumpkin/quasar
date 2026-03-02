package nebula

import (
	"math"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
)

func TestScoreComplexity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		signals   ComplexitySignals
		wantMin   float64 // inclusive lower bound
		wantMax   float64 // inclusive upper bound
		wantExact float64 // if >= 0, exact score expected (with tolerance)
	}{
		{
			name:      "zero inputs task type",
			signals:   ComplexitySignals{ScopeCount: 0, BodyLength: 0, DepthCount: 0, TaskType: "task"},
			wantExact: 0.15 * 0.3, // only type contributes
		},
		{
			name:      "zero inputs bug type",
			signals:   ComplexitySignals{ScopeCount: 0, BodyLength: 0, DepthCount: 0, TaskType: "bug"},
			wantExact: 0.15 * 0.4,
		},
		{
			name:      "zero inputs feature type",
			signals:   ComplexitySignals{ScopeCount: 0, BodyLength: 0, DepthCount: 0, TaskType: "feature"},
			wantExact: 0.15 * 0.8,
		},
		{
			name:      "unknown type falls back to 0.5",
			signals:   ComplexitySignals{ScopeCount: 0, BodyLength: 0, DepthCount: 0, TaskType: "unknown"},
			wantExact: 0.15 * 0.5,
		},
		{
			name:      "empty type falls back to 0.5",
			signals:   ComplexitySignals{ScopeCount: 0, BodyLength: 0, DepthCount: 0, TaskType: ""},
			wantExact: 0.15 * 0.5,
		},
		{
			name: "all signals at saturation",
			signals: ComplexitySignals{
				ScopeCount: 10,
				BodyLength: 3000,
				DepthCount: 8,
				TaskType:   "feature",
			},
			wantExact: 0.25 + 0.35 + 0.25 + 0.15*0.8,
		},
		{
			name: "beyond saturation clamps to 1",
			signals: ComplexitySignals{
				ScopeCount: 100,
				BodyLength: 10000,
				DepthCount: 50,
				TaskType:   "feature",
			},
			wantExact: 0.25 + 0.35 + 0.25 + 0.15*0.8,
		},
		{
			name: "mid-range values",
			signals: ComplexitySignals{
				ScopeCount: 5,
				BodyLength: 1500,
				DepthCount: 4,
				TaskType:   "task",
			},
			wantExact: 0.25*(5.0/10.0) + 0.35*(1500.0/3000.0) + 0.25*(4.0/8.0) + 0.15*0.3,
		},
		{
			name: "single scope pattern only",
			signals: ComplexitySignals{
				ScopeCount: 1,
				BodyLength: 0,
				DepthCount: 0,
				TaskType:   "task",
			},
			wantExact: 0.25*(1.0/10.0) + 0.15*0.3,
		},
		{
			name: "body only contribution",
			signals: ComplexitySignals{
				ScopeCount: 0,
				BodyLength: 600,
				DepthCount: 0,
				TaskType:   "task",
			},
			wantExact: 0.35*(600.0/3000.0) + 0.15*0.3,
		},
		{
			name: "depth only contribution",
			signals: ComplexitySignals{
				ScopeCount: 0,
				BodyLength: 0,
				DepthCount: 3,
				TaskType:   "task",
			},
			wantExact: 0.25*(3.0/8.0) + 0.15*0.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ScoreComplexity(tt.signals)

			// Score must be in [0.0, 1.0].
			if result.Score < 0 || result.Score > 1 {
				t.Errorf("score %f out of [0,1] range", result.Score)
			}

			if tt.wantExact >= 0 {
				if math.Abs(result.Score-tt.wantExact) > 1e-9 {
					t.Errorf("score = %f, want %f", result.Score, tt.wantExact)
				}
			}
			if tt.wantMin != 0 && result.Score < tt.wantMin {
				t.Errorf("score %f < min %f", result.Score, tt.wantMin)
			}
			if tt.wantMax != 0 && result.Score > tt.wantMax {
				t.Errorf("score %f > max %f", result.Score, tt.wantMax)
			}

			// Contributions must contain all four signal keys.
			for _, key := range []string{"scope", "body", "depth", "type"} {
				if _, ok := result.Contributions[key]; !ok {
					t.Errorf("missing contribution key %q", key)
				}
			}
			if len(result.Contributions) != 4 {
				t.Errorf("expected 4 contribution keys, got %d", len(result.Contributions))
			}

			// Signals must be preserved.
			if result.Signals != tt.signals {
				t.Errorf("signals not preserved: got %+v, want %+v", result.Signals, tt.signals)
			}
		})
	}
}

func TestScoreComplexityContributions(t *testing.T) {
	t.Parallel()

	// Verify that contributions sum to the total score.
	signals := ComplexitySignals{ScopeCount: 3, BodyLength: 800, DepthCount: 2, TaskType: "bug"}
	result := ScoreComplexity(signals)

	sum := 0.0
	for _, v := range result.Contributions {
		sum += v
	}
	if math.Abs(sum-result.Score) > 1e-9 {
		t.Errorf("contributions sum %f != score %f", sum, result.Score)
	}
}

func TestBuildComplexitySignals(t *testing.T) {
	t.Parallel()

	t.Run("nil DAG", func(t *testing.T) {
		t.Parallel()
		phase := &PhaseSpec{
			ID:    "p1",
			Type:  "task",
			Scope: []string{"*.go"},
			Body:  "Fix a typo.",
		}
		signals := BuildComplexitySignals(phase, nil)
		if signals.DepthCount != 0 {
			t.Errorf("expected depth 0 with nil DAG, got %d", signals.DepthCount)
		}
		if signals.ScopeCount != 1 {
			t.Errorf("expected scope 1, got %d", signals.ScopeCount)
		}
		if signals.BodyLength != len([]rune(phase.Body)) {
			t.Errorf("expected body length %d, got %d", len([]rune(phase.Body)), signals.BodyLength)
		}
		if signals.TaskType != "task" {
			t.Errorf("expected type %q, got %q", "task", signals.TaskType)
		}
	})

	t.Run("with DAG and ancestors", func(t *testing.T) {
		t.Parallel()
		// Build a chain: c depends on b, b depends on a.
		phases := []PhaseSpec{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B", DependsOn: []string{"a"}},
			{ID: "c", Title: "C", DependsOn: []string{"b"}},
		}
		d, err := NewDAGFromPhases(phases)
		if err != nil {
			t.Fatalf("unexpected error building DAG: %v", err)
		}

		phase := &PhaseSpec{
			ID:    "c",
			Type:  "feature",
			Scope: []string{"internal/**/*.go", "cmd/**/*.go", "*.md"},
			Body:  "Implement feature C.",
		}
		signals := BuildComplexitySignals(phase, d)
		if signals.DepthCount != 2 {
			t.Errorf("expected depth 2 (a, b), got %d", signals.DepthCount)
		}
		if signals.ScopeCount != 3 {
			t.Errorf("expected scope 3, got %d", signals.ScopeCount)
		}
		if signals.TaskType != "feature" {
			t.Errorf("expected type %q, got %q", "feature", signals.TaskType)
		}
	})

	t.Run("unknown phase ID in DAG", func(t *testing.T) {
		t.Parallel()
		d := dag.New()
		d.AddNodeIdempotent("x", 0)

		phase := &PhaseSpec{
			ID:   "missing",
			Type: "bug",
		}
		signals := BuildComplexitySignals(phase, d)
		// Ancestors returns nil for unknown ID; len(nil) == 0.
		if signals.DepthCount != 0 {
			t.Errorf("expected depth 0 for unknown ID, got %d", signals.DepthCount)
		}
	})

	t.Run("unicode body length", func(t *testing.T) {
		t.Parallel()
		// Body with multi-byte runes: 5 runes, more bytes.
		body := "日本語αβ"
		phase := &PhaseSpec{
			ID:   "u1",
			Type: "task",
			Body: body,
		}
		signals := BuildComplexitySignals(phase, nil)
		if signals.BodyLength != 5 {
			t.Errorf("expected body length 5 runes, got %d", signals.BodyLength)
		}
	})

	t.Run("empty phase", func(t *testing.T) {
		t.Parallel()
		phase := &PhaseSpec{}
		signals := BuildComplexitySignals(phase, nil)
		if signals.ScopeCount != 0 || signals.BodyLength != 0 || signals.DepthCount != 0 {
			t.Errorf("expected all zeros for empty phase, got %+v", signals)
		}
		if signals.TaskType != "" {
			t.Errorf("expected empty task type, got %q", signals.TaskType)
		}
	})
}

func TestScoreComplexityDeterministic(t *testing.T) {
	t.Parallel()

	signals := ComplexitySignals{ScopeCount: 7, BodyLength: 2000, DepthCount: 5, TaskType: "feature"}
	first := ScoreComplexity(signals)
	for i := 0; i < 100; i++ {
		result := ScoreComplexity(signals)
		if result.Score != first.Score {
			t.Fatalf("non-deterministic: run %d score %f != %f", i, result.Score, first.Score)
		}
	}
}

func TestClamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{"negative", -1.0, 0.0},
		{"zero", 0.0, 0.0},
		{"mid", 0.5, 0.5},
		{"one", 1.0, 1.0},
		{"over", 2.5, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clamp(tt.in)
			if got != tt.want {
				t.Errorf("clamp(%f) = %f, want %f", tt.in, got, tt.want)
			}
		})
	}
}

func TestScoreComplexityBoundedForAllTypes(t *testing.T) {
	t.Parallel()

	// Verify score is always in [0,1] for known and unknown types with extreme inputs.
	types := []string{"task", "bug", "feature", "", "exotic", "refactor"}
	for _, tp := range types {
		t.Run("type_"+tp, func(t *testing.T) {
			t.Parallel()
			for _, extreme := range []ComplexitySignals{
				{ScopeCount: 0, BodyLength: 0, DepthCount: 0, TaskType: tp},
				{ScopeCount: 1000, BodyLength: 100000, DepthCount: 1000, TaskType: tp},
			} {
				result := ScoreComplexity(extreme)
				if result.Score < 0 || result.Score > 1 {
					t.Errorf("type %q: score %f out of [0,1]", tp, result.Score)
				}
			}
		})
	}
}

func TestBuildComplexitySignalsIntegration(t *testing.T) {
	t.Parallel()

	// End-to-end: build signals then score, verifying the full pipeline.
	phases := []PhaseSpec{
		{ID: "base", Title: "Base"},
		{ID: "mid", Title: "Mid", DependsOn: []string{"base"}},
		{ID: "leaf", Title: "Leaf", DependsOn: []string{"mid"}},
	}
	d, err := NewDAGFromPhases(phases)
	if err != nil {
		t.Fatalf("DAG construction failed: %v", err)
	}

	phase := &PhaseSpec{
		ID:    "leaf",
		Type:  "feature",
		Scope: []string{"internal/**/*.go", "cmd/**/*.go"},
		Body:  strings.Repeat("x", 1500),
	}
	signals := BuildComplexitySignals(phase, d)
	result := ScoreComplexity(signals)

	if result.Score < 0 || result.Score > 1 {
		t.Errorf("integrated score %f out of [0,1]", result.Score)
	}
	// With 2 scope, 1500 body, 2 depth, feature type, expect a moderate score.
	if result.Score < 0.3 {
		t.Errorf("expected moderate+ score for non-trivial phase, got %f", result.Score)
	}
}

func TestSelectTier(t *testing.T) {
	t.Parallel()

	tiers := []ModelTier{
		{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
		{Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
		{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
	}

	tests := []struct {
		name     string
		score    float64
		wantTier string
	}{
		{name: "zero score", score: 0.0, wantTier: "fast"},
		{name: "below fast boundary", score: 0.20, wantTier: "fast"},
		{name: "exact fast boundary", score: 0.35, wantTier: "fast"},
		{name: "just above fast", score: 0.36, wantTier: "balanced"},
		{name: "mid balanced", score: 0.50, wantTier: "balanced"},
		{name: "exact balanced boundary", score: 0.70, wantTier: "balanced"},
		{name: "just above balanced", score: 0.71, wantTier: "heavy"},
		{name: "exact heavy boundary", score: 1.00, wantTier: "heavy"},
		{name: "above all thresholds fallback", score: 1.50, wantTier: "heavy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SelectTier(tt.score, tiers)
			if got.Name != tt.wantTier {
				t.Errorf("SelectTier(%f) = %q, want %q", tt.score, got.Name, tt.wantTier)
			}
		})
	}
}

func TestSelectTierWithDefaults(t *testing.T) {
	t.Parallel()

	// Verify DefaultTiers works with SelectTier.
	got := SelectTier(0.10, DefaultTiers)
	if got.Name != "fast" {
		t.Errorf("SelectTier(0.10, DefaultTiers) = %q, want %q", got.Name, "fast")
	}

	got = SelectTier(0.50, DefaultTiers)
	if got.Name != "balanced" {
		t.Errorf("SelectTier(0.50, DefaultTiers) = %q, want %q", got.Name, "balanced")
	}

	got = SelectTier(0.90, DefaultTiers)
	if got.Name != "heavy" {
		t.Errorf("SelectTier(0.90, DefaultTiers) = %q, want %q", got.Name, "heavy")
	}
}

func TestSelectTierNilTiersFallsBackToDefaults(t *testing.T) {
	t.Parallel()

	// nil tiers should fall back to DefaultTiers.
	got := SelectTier(0.10, nil)
	if got.Name != "fast" {
		t.Errorf("SelectTier(0.10, nil) = %q, want %q", got.Name, "fast")
	}

	got = SelectTier(0.50, nil)
	if got.Name != "balanced" {
		t.Errorf("SelectTier(0.50, nil) = %q, want %q", got.Name, "balanced")
	}

	got = SelectTier(0.90, nil)
	if got.Name != "heavy" {
		t.Errorf("SelectTier(0.90, nil) = %q, want %q", got.Name, "heavy")
	}

	// Empty (non-nil) slice should also fall back.
	got = SelectTier(0.50, []ModelTier{})
	if got.Name != "balanced" {
		t.Errorf("SelectTier(0.50, []) = %q, want %q", got.Name, "balanced")
	}
}

func TestValidateRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        TierConfig
		wantErrs   int
		wantSubstr string // substring expected in at least one error message
	}{
		{
			name: "disabled config returns no errors",
			cfg: TierConfig{
				Enabled: false,
				Tiers: []ModelTier{
					{Name: "bad", Model: "", MaxScore: 0.5},
				},
			},
			wantErrs: 0,
		},
		{
			name: "enabled with no tiers is valid",
			cfg: TierConfig{
				Enabled: true,
				Tiers:   nil,
			},
			wantErrs: 0,
		},
		{
			name: "valid three-tier config",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
					{Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
					{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
				},
			},
			wantErrs: 0,
		},
		{
			name: "empty model field",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "fast", Model: "", MaxScore: 0.35},
					{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
				},
			},
			wantErrs:   1,
			wantSubstr: "empty model",
		},
		{
			name: "duplicate tier names",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
					{Name: "fast", Model: "claude-sonnet", MaxScore: 1.00},
				},
			},
			wantErrs:   1,
			wantSubstr: "duplicate tier name",
		},
		{
			name: "unsorted tiers",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
					{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
				},
			},
			wantErrs:   2, // unsorted + last tier < 1.0
			wantSubstr: "not sorted",
		},
		{
			name: "last tier below 1.0",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
					{Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
				},
			},
			wantErrs:   1,
			wantSubstr: "must be >= 1.0",
		},
		{
			name: "empty tier name",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "", Model: "claude-haiku", MaxScore: 0.35},
					{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
				},
			},
			wantErrs:   1,
			wantSubstr: "empty name",
		},
		{
			name: "multiple errors",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "a", Model: "m1", MaxScore: 0.80},
					{Name: "a", Model: "", MaxScore: 0.50}, // unsorted, duplicate name, empty model, last < 1.0
				},
			},
			wantErrs: 4, // empty model + duplicate name + unsorted + last tier < 1.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := ValidateRouting(tt.cfg)
			if len(errs) != tt.wantErrs {
				t.Errorf("ValidateRouting() returned %d errors, want %d", len(errs), tt.wantErrs)
				for _, e := range errs {
					t.Logf("  error: %s", e.Err)
				}
				return
			}
			if tt.wantSubstr != "" && len(errs) > 0 {
				found := false
				for _, e := range errs {
					if strings.Contains(e.Err.Error(), tt.wantSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no error contains %q", tt.wantSubstr)
					for _, e := range errs {
						t.Logf("  error: %s", e.Err)
					}
				}
			}
			// All routing errors should have the correct category.
			for _, e := range errs {
				if e.Category != ValCatInvalidRouting {
					t.Errorf("error category = %q, want %q", e.Category, ValCatInvalidRouting)
				}
			}
		})
	}
}

func TestDefaultTiers(t *testing.T) {
	t.Parallel()

	if len(DefaultTiers) != 3 {
		t.Fatalf("DefaultTiers has %d entries, want 3", len(DefaultTiers))
	}

	// Verify they cover [0.0, 1.0].
	if DefaultTiers[0].MaxScore <= 0 {
		t.Errorf("first default tier MaxScore = %f, want > 0", DefaultTiers[0].MaxScore)
	}
	if DefaultTiers[len(DefaultTiers)-1].MaxScore < 1.0 {
		t.Errorf("last default tier MaxScore = %f, want >= 1.0", DefaultTiers[len(DefaultTiers)-1].MaxScore)
	}

	// Verify sort order.
	for i := 1; i < len(DefaultTiers); i++ {
		if DefaultTiers[i].MaxScore <= DefaultTiers[i-1].MaxScore {
			t.Errorf("DefaultTiers not sorted: [%d].MaxScore (%f) <= [%d].MaxScore (%f)",
				i, DefaultTiers[i].MaxScore, i-1, DefaultTiers[i-1].MaxScore)
		}
	}

	// Verify all models are non-empty.
	for i, tier := range DefaultTiers {
		if tier.Model == "" {
			t.Errorf("DefaultTiers[%d].Model is empty", i)
		}
		if tier.Name == "" {
			t.Errorf("DefaultTiers[%d].Name is empty", i)
		}
	}
}
