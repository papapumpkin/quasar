package nebula

import "testing"

func TestResolveExecution_BuiltInDefaults(t *testing.T) {
	r := ResolveExecution(0, 0, "", nil, nil)
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
	r := ResolveExecution(10, 20.0, "claude-sonnet", nil, nil)
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
	r := ResolveExecution(10, 20.0, "claude-sonnet", neb, nil)
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

func TestResolveExecution_TaskOverridesNebula(t *testing.T) {
	neb := &Execution{MaxReviewCycles: 5, MaxBudgetUSD: 8.0, Model: "claude-opus"}
	task := &TaskSpec{MaxReviewCycles: 7, MaxBudgetUSD: 15.0, Model: "claude-haiku"}
	r := ResolveExecution(10, 20.0, "claude-sonnet", neb, task)
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
	// Nebula sets cycles, task sets budget, global sets model.
	neb := &Execution{MaxReviewCycles: 5}
	task := &TaskSpec{MaxBudgetUSD: 12.0}
	r := ResolveExecution(0, 0, "claude-sonnet", neb, task)
	if r.MaxReviewCycles != 5 {
		t.Errorf("expected 5 cycles from nebula, got %d", r.MaxReviewCycles)
	}
	if r.MaxBudgetUSD != 12.0 {
		t.Errorf("expected $12.00 from task, got $%.2f", r.MaxBudgetUSD)
	}
	if r.Model != "claude-sonnet" {
		t.Errorf("expected claude-sonnet from global, got %q", r.Model)
	}
}

func TestResolveExecution_ZeroTaskDoesNotOverride(t *testing.T) {
	neb := &Execution{MaxReviewCycles: 5, MaxBudgetUSD: 8.0}
	task := &TaskSpec{MaxReviewCycles: 0, MaxBudgetUSD: 0}
	r := ResolveExecution(0, 0, "", neb, task)
	if r.MaxReviewCycles != 5 {
		t.Errorf("zero task cycles should not override nebula, got %d", r.MaxReviewCycles)
	}
	if r.MaxBudgetUSD != 8.0 {
		t.Errorf("zero task budget should not override nebula, got $%.2f", r.MaxBudgetUSD)
	}
}
