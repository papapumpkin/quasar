package nebula

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAnalyzeFailure_MaxCycles(t *testing.T) {
	t.Parallel()

	fctx := &FailureContext{
		Cycle:        5,
		TotalCostUSD: 3.5,
		CoderOutput:  "coder output",
		ReviewOutput: "review output",
		AllFindings: []DecomposeFinding{
			{Description: "missing error handling"},
			{Description: "test coverage too low"},
		},
	}
	result := &PhaseRunnerResult{
		CyclesUsed:   5,
		TotalCostUSD: 3.5,
	}

	diag := AnalyzeFailure("phase-1", ErrHealMaxCycles, result, fctx)

	if diag.Kind != FailureKindMaxCycles {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindMaxCycles)
	}
	if !diag.Healable {
		t.Error("expected healable = true")
	}
	if diag.PhaseID != "phase-1" {
		t.Errorf("phaseID = %q, want %q", diag.PhaseID, "phase-1")
	}
	if diag.CyclesUsed != 5 {
		t.Errorf("cyclesUsed = %d, want 5", diag.CyclesUsed)
	}
	if diag.BudgetSpent != 3.5 {
		t.Errorf("budgetSpent = %f, want 3.5", diag.BudgetSpent)
	}
	if len(diag.Findings) != 2 {
		t.Fatalf("findings count = %d, want 2", len(diag.Findings))
	}
	if diag.Findings[0] != "missing error handling" {
		t.Errorf("findings[0] = %q, want %q", diag.Findings[0], "missing error handling")
	}
	if diag.LastCoderOut != "coder output" {
		t.Errorf("lastCoderOut = %q, want %q", diag.LastCoderOut, "coder output")
	}
	if diag.LastReviewOut != "review output" {
		t.Errorf("lastReviewOut = %q, want %q", diag.LastReviewOut, "review output")
	}
}

func TestAnalyzeFailure_WrappedMaxCycles(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("phase execution: %w", ErrHealMaxCycles)
	diag := AnalyzeFailure("p-2", wrapped, nil, nil)

	if diag.Kind != FailureKindMaxCycles {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindMaxCycles)
	}
	if !diag.Healable {
		t.Error("expected healable = true for wrapped ErrHealMaxCycles")
	}
}

func TestAnalyzeFailure_MatchesLoopSentinelByMessage(t *testing.T) {
	t.Parallel()

	// Simulate an error from the loop package with the same message text but
	// a different error value (as would happen when errors cross the package boundary
	// without being wrapped with nebula sentinels).
	loopErr := errors.New("maximum review cycles reached")
	diag := AnalyzeFailure("p-cross", loopErr, nil, nil)

	if diag.Kind != FailureKindMaxCycles {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindMaxCycles)
	}
	if !diag.Healable {
		t.Error("expected healable = true for loop-originating max cycles error")
	}
}

func TestAnalyzeFailure_BudgetExceeded(t *testing.T) {
	t.Parallel()

	fctx := &FailureContext{
		Cycle:        3,
		TotalCostUSD: 10.0,
	}
	result := &PhaseRunnerResult{
		CyclesUsed:   3,
		TotalCostUSD: 10.0,
	}

	diag := AnalyzeFailure("phase-budget", ErrHealBudgetExceeded, result, fctx)

	if diag.Kind != FailureKindBudget {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindBudget)
	}
	if !diag.Healable {
		t.Error("expected healable = true")
	}
	if diag.BudgetSpent != 10.0 {
		t.Errorf("budgetSpent = %f, want 10.0", diag.BudgetSpent)
	}
}

func TestAnalyzeFailure_BudgetExceeded_ByMessage(t *testing.T) {
	t.Parallel()

	loopErr := errors.New("budget exceeded")
	diag := AnalyzeFailure("p-budget-loop", loopErr, nil, nil)

	if diag.Kind != FailureKindBudget {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindBudget)
	}
}

func TestAnalyzeFailure_FilterFailure(t *testing.T) {
	t.Parallel()

	fctx := &FailureContext{
		Cycle:           2,
		TotalCostUSD:    1.5,
		FilterCheckName: "go-vet",
		FilterOutput:    "vet: ./pkg/foo.go:12: unreachable code",
		CoderOutput:     "fixed the function",
		ReviewOutput:    "still has issues",
	}

	// A non-sentinel error with filter context triggers filter classification.
	diag := AnalyzeFailure("phase-filter", errors.New("filter check failed"), nil, fctx)

	if diag.Kind != FailureKindFilter {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindFilter)
	}
	if !diag.Healable {
		t.Error("expected healable = true")
	}
	if diag.FilterName != "go-vet" {
		t.Errorf("filterName = %q, want %q", diag.FilterName, "go-vet")
	}
	if diag.FilterOutput != "vet: ./pkg/foo.go:12: unreachable code" {
		t.Errorf("filterOutput = %q, want vet output", diag.FilterOutput)
	}
}

func TestAnalyzeFailure_MaxCyclesTakesPrecedenceOverFilter(t *testing.T) {
	t.Parallel()

	// When ErrHealMaxCycles is the error AND filter fields are populated,
	// max cycles classification takes precedence.
	fctx := &FailureContext{
		Cycle:           5,
		FilterCheckName: "go-vet",
		FilterOutput:    "some output",
	}

	diag := AnalyzeFailure("p-precedence", ErrHealMaxCycles, nil, fctx)

	if diag.Kind != FailureKindMaxCycles {
		t.Errorf("kind = %q, want %q (ErrHealMaxCycles takes precedence)", diag.Kind, FailureKindMaxCycles)
	}
}

func TestAnalyzeFailure_Unhealable_ContextCanceled(t *testing.T) {
	t.Parallel()

	diag := AnalyzeFailure("phase-cancel", context.Canceled, nil, nil)

	if diag.Kind != FailureKindUnhealable {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindUnhealable)
	}
	if diag.Healable {
		t.Error("expected healable = false for context.Canceled")
	}
	if !strings.Contains(diag.Summary, "cancelled") {
		t.Errorf("summary = %q, expected to contain 'cancelled'", diag.Summary)
	}
}

func TestAnalyzeFailure_Unhealable_DeadlineExceeded(t *testing.T) {
	t.Parallel()

	diag := AnalyzeFailure("phase-timeout", context.DeadlineExceeded, nil, nil)

	if diag.Kind != FailureKindUnhealable {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindUnhealable)
	}
	if diag.Healable {
		t.Error("expected healable = false for DeadlineExceeded")
	}
}

func TestAnalyzeFailure_Unhealable_UnknownError(t *testing.T) {
	t.Parallel()

	diag := AnalyzeFailure("phase-unknown", errors.New("something unexpected"), nil, nil)

	if diag.Kind != FailureKindUnhealable {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindUnhealable)
	}
	if diag.Healable {
		t.Error("expected healable = false for unknown error")
	}
}

func TestAnalyzeFailure_Truncation(t *testing.T) {
	t.Parallel()

	longOutput := strings.Repeat("x", 5000)
	fctx := &FailureContext{
		Cycle:        1,
		CoderOutput:  longOutput,
		ReviewOutput: longOutput,
	}

	diag := AnalyzeFailure("phase-trunc", ErrHealMaxCycles, nil, fctx)

	if len(diag.LastCoderOut) != maxOutputLen {
		t.Errorf("lastCoderOut length = %d, want %d", len(diag.LastCoderOut), maxOutputLen)
	}
	if len(diag.LastReviewOut) != maxOutputLen {
		t.Errorf("lastReviewOut length = %d, want %d", len(diag.LastReviewOut), maxOutputLen)
	}
}

func TestAnalyzeFailure_NoTruncationForShortOutput(t *testing.T) {
	t.Parallel()

	fctx := &FailureContext{
		Cycle:        1,
		CoderOutput:  "short",
		ReviewOutput: "also short",
	}

	diag := AnalyzeFailure("phase-short", ErrHealMaxCycles, nil, fctx)

	if diag.LastCoderOut != "short" {
		t.Errorf("lastCoderOut = %q, want %q", diag.LastCoderOut, "short")
	}
	if diag.LastReviewOut != "also short" {
		t.Errorf("lastReviewOut = %q, want %q", diag.LastReviewOut, "also short")
	}
}

func TestAnalyzeFailure_NilContextAndResult(t *testing.T) {
	t.Parallel()

	diag := AnalyzeFailure("phase-nil", ErrHealMaxCycles, nil, nil)

	if diag.Kind != FailureKindMaxCycles {
		t.Errorf("kind = %q, want %q", diag.Kind, FailureKindMaxCycles)
	}
	if diag.CyclesUsed != 0 {
		t.Errorf("cyclesUsed = %d, want 0", diag.CyclesUsed)
	}
	if diag.LastCoderOut != "" {
		t.Errorf("lastCoderOut = %q, want empty", diag.LastCoderOut)
	}
	if diag.Findings != nil {
		t.Errorf("findings = %v, want nil", diag.Findings)
	}
}

func TestAnalyzeFailure_ResultOverridesContext(t *testing.T) {
	t.Parallel()

	fctx := &FailureContext{
		Cycle:        3,
		TotalCostUSD: 2.0,
	}
	result := &PhaseRunnerResult{
		CyclesUsed:   5,
		TotalCostUSD: 4.5,
	}

	diag := AnalyzeFailure("phase-override", ErrHealMaxCycles, result, fctx)

	if diag.CyclesUsed != 5 {
		t.Errorf("cyclesUsed = %d, want 5 (from result)", diag.CyclesUsed)
	}
	if diag.BudgetSpent != 4.5 {
		t.Errorf("budgetSpent = %f, want 4.5 (from result)", diag.BudgetSpent)
	}
}

func TestAnalyzeFailure_FilterWithoutOutput(t *testing.T) {
	t.Parallel()

	// FilterCheckName set but FilterOutput empty — should NOT classify as filter failure.
	fctx := &FailureContext{
		FilterCheckName: "go-vet",
		FilterOutput:    "",
	}

	diag := AnalyzeFailure("phase-no-filter-out", errors.New("something"), nil, fctx)

	if diag.Kind != FailureKindUnhealable {
		t.Errorf("kind = %q, want %q (filter without output is not a filter failure)", diag.Kind, FailureKindUnhealable)
	}
}

func TestCanHeal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		policy   HealingPolicy
		diag     *FailureDiagnosis
		attempts int
		want     bool
	}{
		{
			name:     "AllConditionsMet",
			policy:   HealingPolicy{Enabled: true, MaxAttempts: 1, BudgetReserve: 5.0},
			diag:     &FailureDiagnosis{Healable: true},
			attempts: 0,
			want:     true,
		},
		{
			name:     "Disabled",
			policy:   HealingPolicy{Enabled: false, MaxAttempts: 1, BudgetReserve: 5.0},
			diag:     &FailureDiagnosis{Healable: true},
			attempts: 0,
			want:     false,
		},
		{
			name:     "NotHealable",
			policy:   HealingPolicy{Enabled: true, MaxAttempts: 1, BudgetReserve: 5.0},
			diag:     &FailureDiagnosis{Healable: false},
			attempts: 0,
			want:     false,
		},
		{
			name:     "AttemptsExhausted",
			policy:   HealingPolicy{Enabled: true, MaxAttempts: 1, BudgetReserve: 5.0},
			diag:     &FailureDiagnosis{Healable: true},
			attempts: 1,
			want:     false,
		},
		{
			name:     "AttemptsExceeded",
			policy:   HealingPolicy{Enabled: true, MaxAttempts: 2, BudgetReserve: 5.0},
			diag:     &FailureDiagnosis{Healable: true},
			attempts: 3,
			want:     false,
		},
		{
			name:     "ZeroBudgetReserve",
			policy:   HealingPolicy{Enabled: true, MaxAttempts: 1, BudgetReserve: 0},
			diag:     &FailureDiagnosis{Healable: true},
			attempts: 0,
			want:     false,
		},
		{
			name:     "NegativeBudgetReserve",
			policy:   HealingPolicy{Enabled: true, MaxAttempts: 1, BudgetReserve: -1.0},
			diag:     &FailureDiagnosis{Healable: true},
			attempts: 0,
			want:     false,
		},
		{
			name:     "NilDiagnosis",
			policy:   HealingPolicy{Enabled: true, MaxAttempts: 1, BudgetReserve: 5.0},
			diag:     nil,
			attempts: 0,
			want:     false,
		},
		{
			name:     "MultipleAttemptsAllowed",
			policy:   HealingPolicy{Enabled: true, MaxAttempts: 3, BudgetReserve: 5.0},
			diag:     &FailureDiagnosis{Healable: true},
			attempts: 2,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.policy.CanHeal(tt.diag, tt.attempts)
			if got != tt.want {
				t.Errorf("CanHeal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"Empty", "", 100, ""},
		{"UnderLimit", "hello", 10, "hello"},
		{"AtLimit", "hello", 5, "hello"},
		{"OverLimit", "hello world", 5, "hello"},
		{"ZeroLimit", "hello", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
