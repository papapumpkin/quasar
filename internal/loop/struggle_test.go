package loop

import (
	"math"
	"strings"
	"testing"
)

func TestEvaluateStruggle(t *testing.T) {
	t.Parallel()

	enabled := StruggleConfig{
		Enabled:                 true,
		MinCyclesBeforeCheck:    2,
		FilterRepeatThreshold:   2,
		FindingOverlapThreshold: 0.6,
		BudgetBurnThreshold:     0.3,
		CompositeThreshold:      0.6,
	}

	t.Run("DisabledConfig", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{Cycle: 5}
		cfg := DefaultStruggleConfig() // Enabled: false
		sig := EvaluateStruggle(state, cfg)
		if sig.Triggered {
			t.Error("expected Triggered=false when config is disabled")
		}
	})

	t.Run("BelowMinCycles", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{Cycle: 1}
		sig := EvaluateStruggle(state, enabled)
		if sig.Triggered {
			t.Error("expected Triggered=false when cycle < MinCyclesBeforeCheck")
		}
		if sig.Score != 0 {
			t.Errorf("expected Score=0, got %f", sig.Score)
		}
	})

	t.Run("NoStruggleCleanState", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        3,
			MaxCycles:    10,
			TotalCostUSD: 1.0,
			MaxBudgetUSD: 10.0,
			FilterHistory: []string{"", "", ""},
			Findings:     nil,
			AllFindings:  nil,
		}
		sig := EvaluateStruggle(state, enabled)
		if sig.Triggered {
			t.Error("expected Triggered=false for clean state")
		}
	})

	t.Run("FilterRepeatDetection", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        3,
			MaxCycles:    10,
			TotalCostUSD: 0.5,
			MaxBudgetUSD: 10.0,
			FilterHistory: []string{"check-imports", "check-imports", "check-imports"},
			Findings:     nil,
			AllFindings:  nil,
		}
		sig := EvaluateStruggle(state, enabled)
		if sig.FilterRepeat != 3 {
			t.Errorf("expected FilterRepeat=3, got %d", sig.FilterRepeat)
		}
	})

	t.Run("FilterRepeatIgnoresEmpty", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:         3,
			MaxCycles:     10,
			TotalCostUSD:  0.5,
			MaxBudgetUSD:  10.0,
			FilterHistory: []string{"check-imports", "", ""},
		}
		sig := EvaluateStruggle(state, enabled)
		if sig.FilterRepeat != 0 {
			t.Errorf("expected FilterRepeat=0 for trailing empty, got %d", sig.FilterRepeat)
		}
	})

	t.Run("FindingOverlapDetection", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        3,
			MaxCycles:    10,
			TotalCostUSD: 0.5,
			MaxBudgetUSD: 10.0,
			FilterHistory: []string{"", "", ""},
			Findings: []ReviewFinding{
				{Description: "missing error handling in function foo"},
				{Description: "unused variable bar"},
			},
			AllFindings: []ReviewFinding{
				{Description: "missing error handling in function foo", Cycle: 1},
				{Description: "unused variable bar", Cycle: 1},
			},
		}
		sig := EvaluateStruggle(state, enabled)
		if sig.FindingOverlap < 0.99 {
			t.Errorf("expected FindingOverlap ~1.0, got %f", sig.FindingOverlap)
		}
	})

	t.Run("FindingOverlapPartial", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        3,
			MaxCycles:    10,
			TotalCostUSD: 0.5,
			MaxBudgetUSD: 10.0,
			FilterHistory: []string{"", "", ""},
			Findings: []ReviewFinding{
				{Description: "missing error handling in function foo"},
				{Description: "completely new and different issue found"},
			},
			AllFindings: []ReviewFinding{
				{Description: "missing error handling in function foo", Cycle: 1},
			},
		}
		sig := EvaluateStruggle(state, enabled)
		if sig.FindingOverlap < 0.49 || sig.FindingOverlap > 0.51 {
			t.Errorf("expected FindingOverlap ~0.5, got %f", sig.FindingOverlap)
		}
	})

	t.Run("BudgetBurnDetection", func(t *testing.T) {
		t.Parallel()
		// 10 max cycles, $10 budget => ideal per-cycle = $1.
		// 2 cycles spent, $5 total => actual per-cycle = $2.50.
		// burn rate = 2.50 / 1.0 = 2.50.
		state := &CycleState{
			Cycle:        2,
			MaxCycles:    10,
			TotalCostUSD: 5.0,
			MaxBudgetUSD: 10.0,
			FilterHistory: []string{"", ""},
		}
		sig := EvaluateStruggle(state, enabled)
		if sig.BudgetBurnRate < 2.49 || sig.BudgetBurnRate > 2.51 {
			t.Errorf("expected BudgetBurnRate ~2.5, got %f", sig.BudgetBurnRate)
		}
	})

	t.Run("CompositeTriggersAboveThreshold", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        4,
			MaxCycles:    10,
			TotalCostUSD: 5.0,
			MaxBudgetUSD: 10.0,
			FilterHistory: []string{"check-lint", "check-lint", "check-lint", "check-lint"},
			Findings: []ReviewFinding{
				{Description: "the same bug keeps reappearing over and over"},
			},
			AllFindings: []ReviewFinding{
				{Description: "the same bug keeps reappearing over and over", Cycle: 1},
				{Description: "the same bug keeps reappearing over and over", Cycle: 2},
			},
		}
		sig := EvaluateStruggle(state, enabled)
		if !sig.Triggered {
			t.Errorf("expected Triggered=true, got false (score=%f)", sig.Score)
		}
		if sig.Score < enabled.CompositeThreshold {
			t.Errorf("expected Score >= %f, got %f", enabled.CompositeThreshold, sig.Score)
		}
		if sig.Reason == "" {
			t.Error("expected non-empty Reason when triggered")
		}
	})

	t.Run("ReasonContainsDetails", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        4,
			MaxCycles:    10,
			TotalCostUSD: 5.0,
			MaxBudgetUSD: 10.0,
			FilterHistory: []string{"check-lint", "check-lint", "check-lint", "check-lint"},
			Findings: []ReviewFinding{
				{Description: "recurring issue that never gets fixed"},
			},
			AllFindings: []ReviewFinding{
				{Description: "recurring issue that never gets fixed", Cycle: 1},
			},
		}
		sig := EvaluateStruggle(state, enabled)
		if !sig.Triggered {
			t.Fatalf("expected Triggered=true for reason test, got false (score=%f)", sig.Score)
		}
		if !strings.Contains(sig.Reason, "struggle detected") {
			t.Errorf("Reason should contain 'struggle detected', got: %s", sig.Reason)
		}
		if !strings.Contains(sig.Reason, "filter-repeat") {
			t.Errorf("Reason should mention filter-repeat, got: %s", sig.Reason)
		}
	})

	t.Run("NoFindingsNoOverlap", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        3,
			MaxCycles:    10,
			TotalCostUSD: 1.0,
			MaxBudgetUSD: 10.0,
			FilterHistory: []string{"", "", ""},
			Findings:     []ReviewFinding{},
			AllFindings:  []ReviewFinding{{Description: "old finding"}},
		}
		sig := EvaluateStruggle(state, enabled)
		if sig.FindingOverlap != 0 {
			t.Errorf("expected FindingOverlap=0 with no current findings, got %f", sig.FindingOverlap)
		}
	})

	t.Run("ZeroBudgetSafe", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        3,
			MaxCycles:    10,
			TotalCostUSD: 1.0,
			MaxBudgetUSD: 0,
			FilterHistory: []string{"", "", ""},
		}
		sig := EvaluateStruggle(state, enabled)
		if sig.BudgetBurnRate != 0 {
			t.Errorf("expected BudgetBurnRate=0 with zero budget, got %f", sig.BudgetBurnRate)
		}
	})

	t.Run("ZeroCyclesSafe", func(t *testing.T) {
		t.Parallel()
		state := &CycleState{
			Cycle:        0,
			MaxCycles:    10,
			TotalCostUSD: 1.0,
			MaxBudgetUSD: 10.0,
		}
		// Cycle 0 is below MinCyclesBeforeCheck, so should short-circuit.
		sig := EvaluateStruggle(state, enabled)
		if sig.Triggered {
			t.Error("expected Triggered=false at cycle 0")
		}
	})
}

func TestJaccardSimilarity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a, b     string
		wantMin  float64
		wantMax  float64
	}{
		{
			name:    "IdenticalStrings",
			a:       "missing error handling in foo",
			b:       "missing error handling in foo",
			wantMin: 1.0,
			wantMax: 1.0,
		},
		{
			name:    "CompletelyDifferent",
			a:       "alpha beta gamma",
			b:       "delta epsilon zeta",
			wantMin: 0.0,
			wantMax: 0.0,
		},
		{
			name:    "PartialOverlap",
			a:       "missing error handling in foo",
			b:       "missing error handling in bar",
			wantMin: 0.66, // 4 shared / 6 union = 0.667
			wantMax: 0.67,
		},
		{
			name:    "BothEmpty",
			a:       "",
			b:       "",
			wantMin: 1.0,
			wantMax: 1.0,
		},
		{
			name:    "OneEmpty",
			a:       "hello world",
			b:       "",
			wantMin: 0.0,
			wantMax: 0.0,
		},
		{
			name:    "CaseInsensitive",
			a:       "Missing Error",
			b:       "missing error",
			wantMin: 1.0,
			wantMax: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := jaccardSimilarity(tt.a, tt.b)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("jaccardSimilarity(%q, %q) = %f, want [%f, %f]",
					tt.a, tt.b, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCountTrailingFilterRepeats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		history []string
		want    int
	}{
		{name: "Empty", history: nil, want: 0},
		{name: "AllEmpty", history: []string{"", "", ""}, want: 0},
		{name: "SingleFailure", history: []string{"check-lint"}, want: 1},
		{name: "ThreeConsecutive", history: []string{"check-lint", "check-lint", "check-lint"}, want: 3},
		{name: "DifferentChecks", history: []string{"check-lint", "check-format", "check-format"}, want: 2},
		{name: "BreakInMiddle", history: []string{"check-lint", "", "check-lint"}, want: 1},
		{name: "TrailingEmpty", history: []string{"check-lint", "check-lint", ""}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := countTrailingFilterRepeats(tt.history)
			if got != tt.want {
				t.Errorf("countTrailingFilterRepeats(%v) = %d, want %d", tt.history, got, tt.want)
			}
		})
	}
}

func TestDefaultStruggleConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultStruggleConfig()
	if cfg.Enabled {
		t.Error("expected Enabled=false by default")
	}
	if cfg.MinCyclesBeforeCheck != 2 {
		t.Errorf("expected MinCyclesBeforeCheck=2, got %d", cfg.MinCyclesBeforeCheck)
	}
	if cfg.FilterRepeatThreshold != 2 {
		t.Errorf("expected FilterRepeatThreshold=2, got %d", cfg.FilterRepeatThreshold)
	}
	if cfg.CompositeThreshold != 0.6 {
		t.Errorf("expected CompositeThreshold=0.6, got %f", cfg.CompositeThreshold)
	}
}

func TestClamp01(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    float64
		want float64
	}{
		{name: "Negative", v: -0.5, want: 0.0},
		{name: "Zero", v: 0.0, want: 0.0},
		{name: "Middle", v: 0.5, want: 0.5},
		{name: "One", v: 1.0, want: 1.0},
		{name: "OverOne", v: 1.5, want: 1.0},
		{name: "NaN", v: math.NaN(), want: 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clamp01(tt.v)
			if got != tt.want {
				t.Errorf("clamp01(%f) = %f, want %f", tt.v, got, tt.want)
			}
		})
	}
}

func TestComputeBudgetBurnRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state *CycleState
		want  float64
	}{
		{
			name:  "EvenDistribution",
			state: &CycleState{Cycle: 5, MaxCycles: 10, TotalCostUSD: 5.0, MaxBudgetUSD: 10.0},
			want:  1.0, // spending exactly as expected
		},
		{
			name:  "DoubleBurn",
			state: &CycleState{Cycle: 2, MaxCycles: 10, TotalCostUSD: 4.0, MaxBudgetUSD: 10.0},
			want:  2.0,
		},
		{
			name:  "ZeroBudget",
			state: &CycleState{Cycle: 2, MaxCycles: 10, TotalCostUSD: 1.0, MaxBudgetUSD: 0},
			want:  0.0,
		},
		{
			name:  "ZeroCycles",
			state: &CycleState{Cycle: 0, MaxCycles: 10, TotalCostUSD: 1.0, MaxBudgetUSD: 10.0},
			want:  0.0,
		},
		{
			name:  "ZeroMaxCycles",
			state: &CycleState{Cycle: 1, MaxCycles: 0, TotalCostUSD: 1.0, MaxBudgetUSD: 10.0},
			want:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := computeBudgetBurnRate(tt.state)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("computeBudgetBurnRate() = %f, want %f", got, tt.want)
			}
		})
	}
}
