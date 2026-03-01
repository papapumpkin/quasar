package nebula

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

func TestCorrectValidationErrors_MissingID(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{Title: "Setup Models", SourceFile: "01-setup.md"},
	}
	manifest := Manifest{
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	errs := []ValidationError{
		{
			Category:   ValCatMissingField,
			SourceFile: "01-setup.md",
			Field:      "id",
			Err:        fmt.Errorf("%w: id", ErrMissingField),
		},
	}

	corrected, fixes, remaining := correctValidationErrors(phases, manifest, errs)

	if len(remaining) != 0 {
		t.Errorf("expected no remaining errors, got %d: %v", len(remaining), remaining)
	}
	if len(fixes) != 1 {
		t.Fatalf("expected 1 fix, got %d", len(fixes))
	}
	if !strings.Contains(fixes[0], "derived id") {
		t.Errorf("fix message should mention derived id, got %q", fixes[0])
	}
	if corrected[0].ID != "setup-models" {
		t.Errorf("expected ID %q, got %q", "setup-models", corrected[0].ID)
	}
}

func TestCorrectValidationErrors_MissingTitle(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "setup-models", SourceFile: "01-setup.md"},
	}
	manifest := Manifest{
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	errs := []ValidationError{
		{
			Category:   ValCatMissingField,
			PhaseID:    "setup-models",
			SourceFile: "01-setup.md",
			Field:      "title",
			Err:        fmt.Errorf("%w: title", ErrMissingField),
		},
	}

	corrected, fixes, remaining := correctValidationErrors(phases, manifest, errs)

	if len(remaining) != 0 {
		t.Errorf("expected no remaining errors, got %d", len(remaining))
	}
	if len(fixes) != 1 {
		t.Fatalf("expected 1 fix, got %d", len(fixes))
	}
	if corrected[0].Title != "Setup Models" {
		t.Errorf("expected title %q, got %q", "Setup Models", corrected[0].Title)
	}
}

func TestCorrectValidationErrors_DuplicateID(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "setup", Title: "Setup A", SourceFile: "01-setup.md"},
		{ID: "setup", Title: "Setup B", SourceFile: "02-setup.md"},
		{ID: "deploy", Title: "Deploy", SourceFile: "03-deploy.md", DependsOn: []string{"setup"}},
	}
	manifest := Manifest{
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	errs := []ValidationError{
		{
			Category:   ValCatDuplicateID,
			PhaseID:    "setup",
			SourceFile: "02-setup.md",
			Err:        fmt.Errorf("%w: %q already defined in %s", ErrDuplicateID, "setup", "01-setup.md"),
		},
	}

	corrected, fixes, remaining := correctValidationErrors(phases, manifest, errs)

	if len(remaining) != 0 {
		t.Errorf("expected no remaining errors, got %d: %v", len(remaining), remaining)
	}
	if len(fixes) != 1 {
		t.Fatalf("expected 1 fix, got %d", len(fixes))
	}

	// The second "setup" should be renamed.
	if corrected[1].ID != "setup-2" {
		t.Errorf("expected renamed ID %q, got %q", "setup-2", corrected[1].ID)
	}
	// The first "setup" should remain unchanged.
	if corrected[0].ID != "setup" {
		t.Errorf("first phase ID should be unchanged, got %q", corrected[0].ID)
	}
}

func TestCorrectValidationErrors_DanglingDep(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "setup", Title: "Setup", SourceFile: "01-setup.md"},
		{ID: "build", Title: "Build", SourceFile: "02-build.md", DependsOn: []string{"setup", "nonexistent"}},
	}
	manifest := Manifest{
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	errs := []ValidationError{
		{
			Category:   ValCatUnknownDep,
			PhaseID:    "build",
			SourceFile: "02-build.md",
			Field:      "depends_on",
			Err:        fmt.Errorf("%w: %q depends on unknown phase %q", ErrUnknownDep, "build", "nonexistent"),
		},
	}

	corrected, fixes, remaining := correctValidationErrors(phases, manifest, errs)

	if len(remaining) != 0 {
		t.Errorf("expected no remaining errors, got %d", len(remaining))
	}
	if len(fixes) != 1 {
		t.Fatalf("expected 1 fix, got %d", len(fixes))
	}
	if !strings.Contains(fixes[0], "removed dangling dependency") {
		t.Errorf("fix message should mention removed dangling dependency, got %q", fixes[0])
	}
	if !strings.Contains(fixes[0], "nonexistent") {
		t.Errorf("fix message should mention the removed dep name, got %q", fixes[0])
	}
	// The valid dep should remain.
	if len(corrected[1].DependsOn) != 1 || corrected[1].DependsOn[0] != "setup" {
		t.Errorf("expected [setup], got %v", corrected[1].DependsOn)
	}
}

func TestCorrectValidationErrors_InvalidGate(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "setup", Title: "Setup", SourceFile: "01-setup.md", Gate: "invalid"},
	}
	manifest := Manifest{
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	errs := []ValidationError{
		{
			Category:   ValCatInvalidGate,
			PhaseID:    "setup",
			SourceFile: "01-setup.md",
			Field:      "gate",
			Err:        fmt.Errorf("%w: %q", ErrInvalidGate, "invalid"),
		},
	}

	corrected, fixes, remaining := correctValidationErrors(phases, manifest, errs)

	if len(remaining) != 0 {
		t.Errorf("expected no remaining errors, got %d", len(remaining))
	}
	if len(fixes) != 1 {
		t.Fatalf("expected 1 fix, got %d", len(fixes))
	}
	if corrected[0].Gate != "" {
		t.Errorf("expected empty gate, got %q", corrected[0].Gate)
	}
}

func TestCorrectValidationErrors_BoundsViolation(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "setup", Title: "Setup", SourceFile: "01-setup.md", MaxReviewCycles: -3, MaxBudgetUSD: -1.5},
	}
	manifest := Manifest{
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	errs := []ValidationError{
		{
			Category:   ValCatBoundsViolation,
			PhaseID:    "setup",
			SourceFile: "01-setup.md",
			Field:      "max_review_cycles",
			Err:        fmt.Errorf("max_review_cycles must be >= 0, got %d", -3),
		},
		{
			Category:   ValCatBoundsViolation,
			PhaseID:    "setup",
			SourceFile: "01-setup.md",
			Field:      "max_budget_usd",
			Err:        fmt.Errorf("max_budget_usd must be >= 0, got %f", -1.5),
		},
	}

	corrected, fixes, remaining := correctValidationErrors(phases, manifest, errs)

	if len(remaining) != 0 {
		t.Errorf("expected no remaining errors, got %d", len(remaining))
	}
	if len(fixes) != 2 {
		t.Fatalf("expected 2 fixes, got %d", len(fixes))
	}
	if corrected[0].MaxReviewCycles != 0 {
		t.Errorf("expected MaxReviewCycles=0, got %d", corrected[0].MaxReviewCycles)
	}
	if corrected[0].MaxBudgetUSD != 0 {
		t.Errorf("expected MaxBudgetUSD=0, got %f", corrected[0].MaxBudgetUSD)
	}
}

func TestCorrectValidationErrors_CycleNotCorrected(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "A", SourceFile: "01-a.md", DependsOn: []string{"b"}},
		{ID: "b", Title: "B", SourceFile: "02-b.md", DependsOn: []string{"a"}},
	}
	manifest := Manifest{
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	errs := []ValidationError{
		{
			Category:   ValCatCycle,
			SourceFile: "nebula.toml",
			Err:        ErrDependencyCycle,
		},
	}

	_, _, remaining := correctValidationErrors(phases, manifest, errs)

	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining error, got %d", len(remaining))
	}
	if remaining[0].Category != ValCatCycle {
		t.Errorf("expected ValCatCycle, got %q", remaining[0].Category)
	}
}

func TestCorrectValidationErrors_ScopeOverlap(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "A", SourceFile: "01-a.md", Scope: []string{"internal/**"}},
		{ID: "b", Title: "B", SourceFile: "02-b.md", Scope: []string{"internal/**"}},
	}
	manifest := Manifest{
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	errs := []ValidationError{
		{
			Category:   ValCatScopeOverlap,
			PhaseID:    "a",
			SourceFile: "01-a.md",
			Field:      "scope",
			Err:        fmt.Errorf("%w: phases %q and %q both match %q", ErrScopeOverlap, "a", "b", "internal/**"),
		},
	}

	corrected, fixes, remaining := correctValidationErrors(phases, manifest, errs)

	if len(remaining) != 0 {
		t.Errorf("expected no remaining errors, got %d", len(remaining))
	}
	if len(fixes) != 1 {
		t.Fatalf("expected 1 fix, got %d", len(fixes))
	}
	if !corrected[0].AllowScopeOverlap {
		t.Error("expected AllowScopeOverlap=true on phase a")
	}
}

// retryMockInvoker is a mock that returns different results on sequential calls.
type retryMockInvoker struct {
	results    []agent.InvocationResult
	errors     []error
	callCount  int
	lastPrompt string
}

func (m *retryMockInvoker) Invoke(_ context.Context, _ agent.Agent, prompt string, _ string) (agent.InvocationResult, error) {
	m.lastPrompt = prompt
	idx := m.callCount
	m.callCount++
	if idx >= len(m.results) {
		return agent.InvocationResult{}, fmt.Errorf("unexpected invocation %d", idx)
	}
	return m.results[idx], m.errors[idx]
}

func (m *retryMockInvoker) Validate() error { return nil }

func TestRetryWithFeedback(t *testing.T) {
	t.Parallel()

	correctedOutput := `PHASE_FILE: 01-setup.md
+++
id = "setup"
title = "Setup Models"
+++

## Problem

Setup the models.

## Solution

Create model files.
END_PHASE_FILE

PHASE_FILE: 02-build.md
+++
id = "build"
title = "Build System"
depends_on = ["setup"]
+++

## Problem

Build the system.

## Solution

Compile everything.
END_PHASE_FILE
`

	mock := &retryMockInvoker{
		results: []agent.InvocationResult{
			{ResultText: correctedOutput, CostUSD: 0.03},
		},
		errors: []error{nil},
	}

	prevResult := &GenerateResult{
		Manifest: Manifest{
			Nebula:   Info{Name: "test-nebula"},
			Defaults: Defaults{Type: "task", Priority: 2},
		},
		Phases: []PhaseSpec{
			{ID: "setup", Title: "Setup Models", SourceFile: "01-setup.md"},
		},
		CostUSD: 0.05,
	}

	valErrs := []ValidationError{
		{
			Category: ValCatCycle,
			Err:      ErrDependencyCycle,
		},
	}

	req := GenerateRequest{
		UserPrompt:   "Build a REST API",
		NebulaName:   "test-nebula",
		OutputDir:    "/tmp/test",
		WorkDir:      "/tmp/repo",
		MaxBudgetUSD: 50.0,
	}

	result, err := retryWithFeedback(context.Background(), mock, req, prevResult, valErrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the retry was invoked.
	if mock.callCount != 1 {
		t.Errorf("expected 1 invocation, got %d", mock.callCount)
	}

	// Verify the prompt includes error descriptions.
	if !strings.Contains(mock.lastPrompt, "Validation Error") {
		t.Error("retry prompt should include validation error context")
	}
	if !strings.Contains(mock.lastPrompt, string(ValCatCycle)) {
		t.Error("retry prompt should include error category")
	}

	// Verify we got corrected phases.
	if len(result.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(result.Phases))
	}

	// Verify cost accumulation.
	if result.CostUSD != 0.08 {
		t.Errorf("expected total cost 0.08, got %f", result.CostUSD)
	}
}

func TestCorrectAndRetry_NoErrors(t *testing.T) {
	t.Parallel()

	result := &GenerateResult{
		Phases: []PhaseSpec{
			{ID: "setup", Title: "Setup"},
		},
	}

	// No errors — should return immediately without invoking anything.
	got, err := CorrectAndRetry(context.Background(), nil, GenerateRequest{}, result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != result {
		t.Error("expected same result back when no errors")
	}
}

func TestCorrectAndRetry_AutoCorrectResolvesAll(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "setup", Title: "Setup", SourceFile: "01-setup.md", Gate: "invalid"},
	}
	manifest := Manifest{
		Nebula:   Info{Name: "test"},
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	neb := &Nebula{Dir: "/tmp", Manifest: manifest, Phases: phases}
	result := &GenerateResult{
		Nebula:   neb,
		Manifest: manifest,
		Phases:   phases,
	}

	valErrs := []ValidationError{
		{
			Category:   ValCatInvalidGate,
			PhaseID:    "setup",
			SourceFile: "01-setup.md",
			Field:      "gate",
			Err:        fmt.Errorf("%w: %q", ErrInvalidGate, "invalid"),
		},
	}

	// No invoker needed — auto-correction should handle it.
	mock := &retryMockInvoker{}
	got, err := CorrectAndRetry(context.Background(), mock, GenerateRequest{}, result, valErrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no retry was needed.
	if mock.callCount != 0 {
		t.Errorf("expected 0 invocations (auto-correction only), got %d", mock.callCount)
	}

	// Verify the fix was applied.
	if got.Phases[0].Gate != "" {
		t.Errorf("expected empty gate after fix, got %q", got.Phases[0].Gate)
	}
	if len(got.Errors) != 1 {
		t.Fatalf("expected 1 fix message, got %d: %v", len(got.Errors), got.Errors)
	}
}

func TestValidateCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nebula   *Nebula
		wantCats []ValidationCategory
	}{
		{
			name: "missing nebula name",
			nebula: &Nebula{
				Manifest: Manifest{Nebula: Info{Name: ""}},
			},
			wantCats: []ValidationCategory{ValCatMissingField},
		},
		{
			name: "missing phase id",
			nebula: &Nebula{
				Manifest: Manifest{Nebula: Info{Name: "test"}},
				Phases:   []PhaseSpec{{Title: "No ID", SourceFile: "01.md"}},
			},
			wantCats: []ValidationCategory{ValCatMissingField},
		},
		{
			name: "missing phase title",
			nebula: &Nebula{
				Manifest: Manifest{Nebula: Info{Name: "test"}},
				Phases:   []PhaseSpec{{ID: "a", SourceFile: "01.md"}},
			},
			wantCats: []ValidationCategory{ValCatMissingField},
		},
		{
			name: "duplicate id",
			nebula: &Nebula{
				Manifest: Manifest{Nebula: Info{Name: "test"}},
				Phases: []PhaseSpec{
					{ID: "a", Title: "A", SourceFile: "01.md"},
					{ID: "a", Title: "B", SourceFile: "02.md"},
				},
			},
			wantCats: []ValidationCategory{ValCatDuplicateID},
		},
		{
			name: "unknown dep",
			nebula: &Nebula{
				Manifest: Manifest{Nebula: Info{Name: "test"}},
				Phases: []PhaseSpec{
					{ID: "a", Title: "A", SourceFile: "01.md", DependsOn: []string{"missing"}},
				},
			},
			wantCats: []ValidationCategory{ValCatUnknownDep},
		},
		{
			name: "invalid gate on manifest",
			nebula: &Nebula{
				Manifest: Manifest{
					Nebula:    Info{Name: "test"},
					Execution: Execution{Gate: "bogus"},
				},
				Phases: []PhaseSpec{{ID: "a", Title: "A", SourceFile: "01.md"}},
			},
			wantCats: []ValidationCategory{ValCatInvalidGate},
		},
		{
			name: "invalid gate on phase",
			nebula: &Nebula{
				Manifest: Manifest{Nebula: Info{Name: "test"}},
				Phases:   []PhaseSpec{{ID: "a", Title: "A", SourceFile: "01.md", Gate: "bogus"}},
			},
			wantCats: []ValidationCategory{ValCatInvalidGate},
		},
		{
			name: "bounds violation review cycles",
			nebula: &Nebula{
				Manifest: Manifest{
					Nebula:    Info{Name: "test"},
					Execution: Execution{MaxReviewCycles: -1},
				},
				Phases: []PhaseSpec{{ID: "a", Title: "A", SourceFile: "01.md"}},
			},
			wantCats: []ValidationCategory{ValCatBoundsViolation},
		},
		{
			name: "bounds violation budget",
			nebula: &Nebula{
				Manifest: Manifest{
					Nebula:    Info{Name: "test"},
					Execution: Execution{MaxBudgetUSD: -5.0},
				},
				Phases: []PhaseSpec{{ID: "a", Title: "A", SourceFile: "01.md"}},
			},
			wantCats: []ValidationCategory{ValCatBoundsViolation},
		},
		{
			name: "phase bounds violation",
			nebula: &Nebula{
				Manifest: Manifest{Nebula: Info{Name: "test"}},
				Phases:   []PhaseSpec{{ID: "a", Title: "A", SourceFile: "01.md", MaxReviewCycles: -2}},
			},
			wantCats: []ValidationCategory{ValCatBoundsViolation},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errs := Validate(tc.nebula)
			if len(errs) == 0 {
				t.Fatal("expected validation errors, got none")
			}
			for i, wantCat := range tc.wantCats {
				if i >= len(errs) {
					t.Errorf("expected error at index %d with category %q, but only got %d errors", i, wantCat, len(errs))
					continue
				}
				if errs[i].Category != wantCat {
					t.Errorf("error %d: expected category %q, got %q (err: %s)", i, wantCat, errs[i].Category, errs[i].Error())
				}
			}
		})
	}
}

func TestSlugifyID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"Setup Models", "setup-models"},
		{"Add HTTP Handlers", "add-http-handlers"},
		{"  spaces  ", "spaces"},
		{"under_score", "under-score"},
		{"Special!@#Chars", "specialchars"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := slugifyID(tc.input)
			if got != tc.want {
				t.Errorf("slugifyID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestUnslugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"setup-models", "Setup Models"},
		{"add-http-handlers", "Add Http Handlers"},
		{"single", "Single"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := unslugify(tc.input)
			if got != tc.want {
				t.Errorf("unslugify(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestExtractUnknownDep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "standard format",
			err:  fmt.Errorf("%w: %q depends on unknown phase %q", ErrUnknownDep, "build", "missing-dep"),
			want: "missing-dep",
		},
		{
			name: "no unknown phase marker",
			err:  fmt.Errorf("some other error"),
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ve := ValidationError{Err: tc.err}
			got := extractUnknownDep(ve)
			if got != tc.want {
				t.Errorf("extractUnknownDep() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildRetryPrompt(t *testing.T) {
	t.Parallel()

	prevResult := &GenerateResult{
		Phases: []PhaseSpec{
			{ID: "a", Title: "Phase A", Type: "task", DependsOn: []string{"b"}},
			{ID: "b", Title: "Phase B", Type: "task"},
		},
	}
	valErrs := []ValidationError{
		{Category: ValCatCycle, Err: errors.New("dependency cycle detected")},
	}

	prompt := buildRetryPrompt(prevResult, valErrs)

	if !strings.Contains(prompt, "Validation Error Correction") {
		t.Error("prompt should contain header")
	}
	if !strings.Contains(prompt, string(ValCatCycle)) {
		t.Error("prompt should contain error category")
	}
	if !strings.Contains(prompt, "Phase A") {
		t.Error("prompt should contain previous phase titles")
	}
	if !strings.Contains(prompt, "PHASE_FILE/END_PHASE_FILE") {
		t.Error("prompt should contain output format instructions")
	}
}

func TestCorrectAndRetry_RetryInvokerError(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "A", SourceFile: "01.md", DependsOn: []string{"b"}},
		{ID: "b", Title: "B", SourceFile: "02.md", DependsOn: []string{"a"}},
	}
	manifest := Manifest{
		Nebula:   Info{Name: "test"},
		Defaults: Defaults{Type: "task", Priority: 2},
	}
	neb := &Nebula{Dir: "/tmp", Manifest: manifest, Phases: phases}
	result := &GenerateResult{
		Nebula:   neb,
		Manifest: manifest,
		Phases:   phases,
	}

	valErrs := []ValidationError{
		{Category: ValCatCycle, Err: ErrDependencyCycle},
	}

	// Invoker returns an error on retry.
	mock := &retryMockInvoker{
		results: []agent.InvocationResult{{}},
		errors:  []error{fmt.Errorf("network error")},
	}

	req := GenerateRequest{
		UserPrompt:   "Build something",
		NebulaName:   "test",
		OutputDir:    "/tmp/test",
		WorkDir:      "/tmp/repo",
		MaxBudgetUSD: 50.0,
	}

	got, err := CorrectAndRetry(context.Background(), mock, req, result, valErrs)
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}

	// Should not fail hard — should return the original result with errors annotated.
	if len(got.Errors) == 0 {
		t.Error("expected error annotations when retry fails")
	}
}
