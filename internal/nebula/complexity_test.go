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
