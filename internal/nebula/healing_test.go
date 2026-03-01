package nebula

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
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

func TestAnalyzeFailure_ContextCanceledWithStaleFilterFields(t *testing.T) {
	t.Parallel()

	// When context.Canceled is the error but the FailureContext retains
	// populated filter fields from a prior cycle, the diagnosis must be
	// unhealable — not misclassified as a filter failure.
	fctx := &FailureContext{
		Cycle:           3,
		FilterCheckName: "go-vet",
		FilterOutput:    "vet: stale output from prior cycle",
	}

	diag := AnalyzeFailure("phase-stale-filter", context.Canceled, nil, fctx)

	if diag.Kind != FailureKindUnhealable {
		t.Errorf("kind = %q, want %q (context.Canceled must take precedence over stale filter fields)", diag.Kind, FailureKindUnhealable)
	}
	if diag.Healable {
		t.Error("expected healable = false for context.Canceled even with filter fields populated")
	}
	if !strings.Contains(diag.Summary, "cancelled") {
		t.Errorf("summary = %q, expected to contain 'cancelled'", diag.Summary)
	}
}

func TestAnalyzeFailure_DeadlineExceededWithStaleFilterFields(t *testing.T) {
	t.Parallel()

	// Same as above but for DeadlineExceeded.
	fctx := &FailureContext{
		Cycle:           2,
		FilterCheckName: "go-build",
		FilterOutput:    "build: stale output",
	}

	diag := AnalyzeFailure("phase-deadline-filter", context.DeadlineExceeded, nil, fctx)

	if diag.Kind != FailureKindUnhealable {
		t.Errorf("kind = %q, want %q (DeadlineExceeded must take precedence over stale filter fields)", diag.Kind, FailureKindUnhealable)
	}
	if diag.Healable {
		t.Error("expected healable = false for DeadlineExceeded even with filter fields populated")
	}
}

func TestAnalyzeFailure_ResultFindingsOverrideContextFindings(t *testing.T) {
	t.Parallel()

	// When both FailureContext and PhaseRunnerResult have findings,
	// the result's findings should take precedence (more authoritative).
	fctx := &FailureContext{
		Cycle: 5,
		AllFindings: []DecomposeFinding{
			{Description: "context finding A"},
		},
	}
	result := &PhaseRunnerResult{
		CyclesUsed:   5,
		TotalCostUSD: 4.0,
		AllFindings: []DecomposeFinding{
			{Description: "result finding X"},
			{Description: "result finding Y"},
		},
	}

	diag := AnalyzeFailure("phase-findings-override", ErrHealMaxCycles, result, fctx)

	if len(diag.Findings) != 2 {
		t.Fatalf("findings count = %d, want 2 (from result)", len(diag.Findings))
	}
	if diag.Findings[0] != "result finding X" {
		t.Errorf("findings[0] = %q, want %q", diag.Findings[0], "result finding X")
	}
	if diag.Findings[1] != "result finding Y" {
		t.Errorf("findings[1] = %q, want %q", diag.Findings[1], "result finding Y")
	}
}

func TestAnalyzeFailure_ContextFindingsUsedWhenResultHasNone(t *testing.T) {
	t.Parallel()

	// When result has no findings, context findings should be used.
	fctx := &FailureContext{
		Cycle: 5,
		AllFindings: []DecomposeFinding{
			{Description: "context finding A"},
			{Description: "context finding B"},
		},
	}
	result := &PhaseRunnerResult{
		CyclesUsed:   5,
		TotalCostUSD: 3.0,
	}

	diag := AnalyzeFailure("phase-ctx-findings", ErrHealMaxCycles, result, fctx)

	if len(diag.Findings) != 2 {
		t.Fatalf("findings count = %d, want 2 (from context)", len(diag.Findings))
	}
	if diag.Findings[0] != "context finding A" {
		t.Errorf("findings[0] = %q, want %q", diag.Findings[0], "context finding A")
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

func TestBuildRemediationRequest_MaxCycles(t *testing.T) {
	t.Parallel()

	diag := &FailureDiagnosis{
		PhaseID:      "auth-login",
		Kind:         FailureKindMaxCycles,
		Healable:     true,
		Summary:      "phase exhausted all review cycles without approval",
		CyclesUsed:   5,
		BudgetSpent:  2.50,
		LastCoderOut: "implemented auth flow",
		Findings:     []string{"missing error handling", "test coverage too low"},
	}
	neb := &Nebula{
		Manifest: Manifest{Nebula: Info{Name: "test-nebula"}},
	}
	failedSpec := &PhaseSpec{
		ID:    "auth-login",
		Title: "Implement Auth Login",
	}

	req := BuildRemediationRequest(diag, neb, failedSpec)

	if req.Mode != ArchitectModeRemediate {
		t.Errorf("mode = %q, want %q", req.Mode, ArchitectModeRemediate)
	}
	if req.PhaseID != "auth-login" {
		t.Errorf("phaseID = %q, want %q", req.PhaseID, "auth-login")
	}
	if req.Nebula != neb {
		t.Error("nebula not set on request")
	}

	// Verify prompt structure.
	prompt := req.UserPrompt
	for _, want := range []string{
		"## Remediation Request",
		`"Implement Auth Login"`,
		"auth-login",
		"max_cycles",
		"### Failure Summary",
		"phase exhausted all review cycles without approval",
		"### Context",
		"Cycles used: 5",
		"Budget spent: $2.50",
		"### Last Coder Output (truncated)",
		"implemented auth flow",
		"### Last Reviewer Findings",
		"- missing error handling",
		"- test coverage too low",
		"### Instructions",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildRemediationRequest_FilterFailure(t *testing.T) {
	t.Parallel()

	diag := &FailureDiagnosis{
		PhaseID:      "build-step",
		Kind:         FailureKindFilter,
		Healable:     true,
		Summary:      "phase failed due to persistent filter check failure",
		CyclesUsed:   3,
		BudgetSpent:  1.20,
		FilterName:   "go-vet",
		FilterOutput: "found suspicious construct",
	}
	neb := &Nebula{
		Manifest: Manifest{Nebula: Info{Name: "test-nebula"}},
	}
	failedSpec := &PhaseSpec{
		ID:    "build-step",
		Title: "Build Step",
	}

	req := BuildRemediationRequest(diag, neb, failedSpec)
	prompt := req.UserPrompt

	for _, want := range []string{
		"filter_failure",
		"Failing filter: go-vet",
		"Filter output:",
		"found suspicious construct",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildRemediationRequest_NoCoderOutput(t *testing.T) {
	t.Parallel()

	diag := &FailureDiagnosis{
		PhaseID:     "phase-x",
		Kind:        FailureKindBudget,
		Summary:     "phase exceeded its budget",
		BudgetSpent: 10.0,
	}
	neb := &Nebula{
		Manifest: Manifest{Nebula: Info{Name: "test-nebula"}},
	}
	failedSpec := &PhaseSpec{ID: "phase-x", Title: "Phase X"}

	req := BuildRemediationRequest(diag, neb, failedSpec)
	prompt := req.UserPrompt

	if strings.Contains(prompt, "### Last Coder Output") {
		t.Error("prompt should not contain coder output section when output is empty")
	}
	if strings.Contains(prompt, "### Last Reviewer Findings") {
		t.Error("prompt should not contain findings section when findings are empty")
	}
}

func TestBuildRemediationRequest_NoFindings(t *testing.T) {
	t.Parallel()

	diag := &FailureDiagnosis{
		PhaseID:      "phase-y",
		Kind:         FailureKindMaxCycles,
		Summary:      "exhausted cycles",
		LastCoderOut: "some output",
	}
	neb := &Nebula{
		Manifest: Manifest{Nebula: Info{Name: "test-nebula"}},
	}
	failedSpec := &PhaseSpec{ID: "phase-y", Title: "Phase Y"}

	req := BuildRemediationRequest(diag, neb, failedSpec)
	prompt := req.UserPrompt

	if strings.Contains(prompt, "### Last Reviewer Findings") {
		t.Error("prompt should not contain findings section when there are no findings")
	}
	if !strings.Contains(prompt, "### Last Coder Output") {
		t.Error("prompt should contain coder output section")
	}
}

func TestFinalizeRemediationSpec_IDPrefix(t *testing.T) {
	t.Parallel()

	result := &ArchitectResult{
		PhaseSpec: PhaseSpec{
			ID:    "original-id",
			Title: "Generated Fix",
		},
	}
	diag := &FailureDiagnosis{PhaseID: "auth-login"}
	failedSpec := &PhaseSpec{
		ID:     "auth-login",
		Scope:  []string{"internal/auth/**"},
		Gate:   "review",
		Labels: []string{"auth"},
	}

	got := FinalizeRemediationSpec(result, diag, failedSpec)

	if got.PhaseSpec.ID != "heal-auth-login" {
		t.Errorf("ID = %q, want %q", got.PhaseSpec.ID, "heal-auth-login")
	}
}

func TestFinalizeRemediationSpec_ScopeInheritance(t *testing.T) {
	t.Parallel()

	result := &ArchitectResult{
		PhaseSpec: PhaseSpec{ID: "temp"},
	}
	diag := &FailureDiagnosis{PhaseID: "phase-1"}
	failedSpec := &PhaseSpec{
		ID:    "phase-1",
		Scope: []string{"src/**/*.go", "internal/**"},
	}

	got := FinalizeRemediationSpec(result, diag, failedSpec)

	if len(got.PhaseSpec.Scope) != 2 {
		t.Fatalf("scope length = %d, want 2", len(got.PhaseSpec.Scope))
	}
	if got.PhaseSpec.Scope[0] != "src/**/*.go" {
		t.Errorf("scope[0] = %q, want %q", got.PhaseSpec.Scope[0], "src/**/*.go")
	}
	if got.PhaseSpec.Scope[1] != "internal/**" {
		t.Errorf("scope[1] = %q, want %q", got.PhaseSpec.Scope[1], "internal/**")
	}

	// Verify it's a copy, not a shared reference.
	failedSpec.Scope[0] = "modified"
	if got.PhaseSpec.Scope[0] == "modified" {
		t.Error("scope should be a copy, not a shared reference")
	}
}

func TestFinalizeRemediationSpec_EmptyScope(t *testing.T) {
	t.Parallel()

	result := &ArchitectResult{
		PhaseSpec: PhaseSpec{ID: "temp", Scope: []string{"existing"}},
	}
	diag := &FailureDiagnosis{PhaseID: "phase-1"}
	failedSpec := &PhaseSpec{ID: "phase-1"}

	got := FinalizeRemediationSpec(result, diag, failedSpec)

	// When failed phase has no scope, the result's existing scope is preserved.
	if len(got.PhaseSpec.Scope) != 1 || got.PhaseSpec.Scope[0] != "existing" {
		t.Errorf("scope = %v, want [existing]", got.PhaseSpec.Scope)
	}
}

func TestFinalizeRemediationSpec_GateInheritance(t *testing.T) {
	t.Parallel()

	result := &ArchitectResult{
		PhaseSpec: PhaseSpec{ID: "temp", Gate: "trust"},
	}
	diag := &FailureDiagnosis{PhaseID: "phase-1"}
	failedSpec := &PhaseSpec{ID: "phase-1", Gate: "approve"}

	got := FinalizeRemediationSpec(result, diag, failedSpec)

	if got.PhaseSpec.Gate != "approve" {
		t.Errorf("gate = %q, want %q", got.PhaseSpec.Gate, "approve")
	}
}

func TestFinalizeRemediationSpec_LabelMerging(t *testing.T) {
	t.Parallel()

	result := &ArchitectResult{
		PhaseSpec: PhaseSpec{ID: "temp", Labels: []string{"generated"}},
	}
	diag := &FailureDiagnosis{PhaseID: "phase-1"}
	failedSpec := &PhaseSpec{
		ID:     "phase-1",
		Labels: []string{"auth", "critical"},
	}

	got := FinalizeRemediationSpec(result, diag, failedSpec)

	if len(got.PhaseSpec.Labels) != 3 {
		t.Fatalf("labels length = %d, want 3", len(got.PhaseSpec.Labels))
	}
	wantLabels := []string{"auth", "critical", "auto-healing"}
	for i, want := range wantLabels {
		if got.PhaseSpec.Labels[i] != want {
			t.Errorf("labels[%d] = %q, want %q", i, got.PhaseSpec.Labels[i], want)
		}
	}

	// Verify labels are a copy.
	failedSpec.Labels[0] = "modified"
	if got.PhaseSpec.Labels[0] == "modified" {
		t.Error("labels should be a copy, not a shared reference")
	}
}

func TestFinalizeRemediationSpec_LabelDedup(t *testing.T) {
	t.Parallel()

	result := &ArchitectResult{
		PhaseSpec: PhaseSpec{ID: "temp"},
	}
	diag := &FailureDiagnosis{PhaseID: "phase-1"}
	failedSpec := &PhaseSpec{
		ID:     "phase-1",
		Labels: []string{"auto-healing", "existing"},
	}

	got := FinalizeRemediationSpec(result, diag, failedSpec)

	// "auto-healing" already present, should not be duplicated.
	count := 0
	for _, l := range got.PhaseSpec.Labels {
		if l == "auto-healing" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("auto-healing label appears %d times, want 1", count)
	}
}

func TestFinalizeRemediationSpec_EmptyLabels(t *testing.T) {
	t.Parallel()

	result := &ArchitectResult{
		PhaseSpec: PhaseSpec{ID: "temp"},
	}
	diag := &FailureDiagnosis{PhaseID: "phase-1"}
	failedSpec := &PhaseSpec{ID: "phase-1"}

	got := FinalizeRemediationSpec(result, diag, failedSpec)

	if len(got.PhaseSpec.Labels) != 1 {
		t.Fatalf("labels length = %d, want 1", len(got.PhaseSpec.Labels))
	}
	if got.PhaseSpec.Labels[0] != "auto-healing" {
		t.Errorf("labels[0] = %q, want %q", got.PhaseSpec.Labels[0], "auto-healing")
	}
}

func TestInsertRemediationPhase_Diamond(t *testing.T) {
	t.Parallel()

	//     a
	//    / \
	//   b   c
	//    \ /
	//     d
	// Fail "d". Remediation "heal-d" should take d's place for b and c.
	d := dag.New()
	_ = d.AddNode("d", 1)
	_ = d.AddNode("b", 1)
	_ = d.AddNode("c", 1)
	_ = d.AddNode("a", 1)
	_ = d.AddEdge("b", "d")
	_ = d.AddEdge("c", "d")
	_ = d.AddEdge("a", "b")
	_ = d.AddEdge("a", "c")

	remediation := &PhaseSpec{ID: "heal-d", Priority: 2}
	rewired, err := InsertRemediationPhase(d, "d", remediation)
	if err != nil {
		t.Fatalf("InsertRemediationPhase: %v", err)
	}

	// b and c should have been rewired.
	if len(rewired) != 2 {
		t.Fatalf("rewired = %v, want 2 items", rewired)
	}
	if rewired[0] != "b" || rewired[1] != "c" {
		t.Errorf("rewired = %v, want [b, c]", rewired)
	}

	// b and c should now depend on heal-d, not d.
	bDeps := d.DepsFor("b")
	if len(bDeps) != 1 || bDeps[0] != "heal-d" {
		t.Errorf("DepsFor(b) = %v, want [heal-d]", bDeps)
	}
	cDeps := d.DepsFor("c")
	if len(cDeps) != 1 || cDeps[0] != "heal-d" {
		t.Errorf("DepsFor(c) = %v, want [heal-d]", cDeps)
	}

	// heal-d should have no dependencies.
	healDeps := d.DepsFor("heal-d")
	if healDeps != nil {
		t.Errorf("DepsFor(heal-d) = %v, want nil", healDeps)
	}

	// No cycles introduced.
	_, err = d.TopologicalSort()
	if err != nil {
		t.Errorf("TopologicalSort after insertion: %v", err)
	}
}

func TestInsertRemediationPhase_LinearChain(t *testing.T) {
	t.Parallel()

	// a → b → c (a depends on b, b depends on c)
	// Fail "c". Only b should be rewired.
	d := dag.New()
	_ = d.AddNode("c", 1)
	_ = d.AddNode("b", 1)
	_ = d.AddNode("a", 1)
	_ = d.AddEdge("b", "c")
	_ = d.AddEdge("a", "b")

	remediation := &PhaseSpec{ID: "heal-c", Priority: 1}
	rewired, err := InsertRemediationPhase(d, "c", remediation)
	if err != nil {
		t.Fatalf("InsertRemediationPhase: %v", err)
	}

	if len(rewired) != 1 || rewired[0] != "b" {
		t.Errorf("rewired = %v, want [b]", rewired)
	}

	bDeps := d.DepsFor("b")
	if len(bDeps) != 1 || bDeps[0] != "heal-c" {
		t.Errorf("DepsFor(b) = %v, want [heal-c]", bDeps)
	}

	_, err = d.TopologicalSort()
	if err != nil {
		t.Errorf("TopologicalSort after insertion: %v", err)
	}
}

func TestInsertRemediationPhase_NoDependents(t *testing.T) {
	t.Parallel()

	// a → b (a depends on b). Fail "a" which has no dependents.
	d := dag.New()
	_ = d.AddNode("b", 1)
	_ = d.AddNode("a", 1)
	_ = d.AddEdge("a", "b")

	remediation := &PhaseSpec{ID: "heal-a", Priority: 1}
	rewired, err := InsertRemediationPhase(d, "a", remediation)
	if err != nil {
		t.Fatalf("InsertRemediationPhase: %v", err)
	}

	if len(rewired) != 0 {
		t.Errorf("rewired = %v, want empty", rewired)
	}

	// heal-a should exist with no deps.
	if d.Node("heal-a") == nil {
		t.Error("heal-a node not found in DAG")
	}
	healDeps := d.DepsFor("heal-a")
	if healDeps != nil {
		t.Errorf("DepsFor(heal-a) = %v, want nil", healDeps)
	}

	_, err = d.TopologicalSort()
	if err != nil {
		t.Errorf("TopologicalSort after insertion: %v", err)
	}
}

func TestInsertRemediationPhase_FailedNodeNotFound(t *testing.T) {
	t.Parallel()

	d := dag.New()
	_ = d.AddNode("a", 1)

	remediation := &PhaseSpec{ID: "heal-x", Priority: 1}
	_, err := InsertRemediationPhase(d, "nonexistent", remediation)
	if err == nil {
		t.Fatal("expected error for nonexistent failed phase, got nil")
	}
	if !strings.Contains(err.Error(), "node not found") {
		t.Errorf("error = %v, want to contain 'node not found'", err)
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
