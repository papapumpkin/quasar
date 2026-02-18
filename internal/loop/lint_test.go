package loop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

// ---------------------------------------------------------------------------
// fakeLinter returns controlled output for testing the lint integration.
// ---------------------------------------------------------------------------

type fakeLinter struct {
	// outputs is a queue of results returned by successive Run calls.
	outputs []string
	errs    []error
	calls   int
}

func (f *fakeLinter) Run(_ context.Context) (string, error) {
	idx := f.calls
	f.calls++
	var output string
	if idx < len(f.outputs) {
		output = f.outputs[idx]
	}
	var err error
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	return output, err
}

// ---------------------------------------------------------------------------
// TestNewLinter
// ---------------------------------------------------------------------------

func TestNewLinter(t *testing.T) {
	t.Parallel()

	t.Run("NilForEmptyCommands", func(t *testing.T) {
		t.Parallel()
		l := NewLinter(nil, "/tmp")
		if l != nil {
			t.Error("expected nil Linter for empty commands")
		}
	})

	t.Run("NonNilForCommands", func(t *testing.T) {
		t.Parallel()
		l := NewLinter([]string{"go vet ./..."}, "/tmp")
		if l == nil {
			t.Error("expected non-nil Linter for non-empty commands")
		}
	})
}

// ---------------------------------------------------------------------------
// TestMaxLintRetries
// ---------------------------------------------------------------------------

func TestMaxLintRetries(t *testing.T) {
	t.Parallel()

	t.Run("DefaultWhenZero", func(t *testing.T) {
		t.Parallel()
		l := &Loop{MaxLintRetries: 0}
		if got := l.maxLintRetries(); got != DefaultMaxLintRetries {
			t.Errorf("maxLintRetries() = %d, want %d", got, DefaultMaxLintRetries)
		}
	})

	t.Run("CustomValue", func(t *testing.T) {
		t.Parallel()
		l := &Loop{MaxLintRetries: 5}
		if got := l.maxLintRetries(); got != 5 {
			t.Errorf("maxLintRetries() = %d, want 5", got)
		}
	})
}

// ---------------------------------------------------------------------------
// TestRunLintFixLoop
// ---------------------------------------------------------------------------

func TestRunLintFixLoop(t *testing.T) {
	t.Parallel()

	t.Run("NilLinter", func(t *testing.T) {
		t.Parallel()
		l := &Loop{
			UI:     &noopUI{},
			Linter: nil,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task"}
		err := l.runLintFixLoop(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state.LintOutput != "" {
			t.Errorf("expected empty LintOutput with nil Linter, got %q", state.LintOutput)
		}
	})

	t.Run("CleanLintPass", func(t *testing.T) {
		t.Parallel()
		linter := &fakeLinter{outputs: []string{""}}
		l := &Loop{
			UI:     &noopUI{},
			Linter: linter,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task"}
		err := l.runLintFixLoop(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state.LintOutput != "" {
			t.Errorf("expected empty LintOutput for clean lint, got %q", state.LintOutput)
		}
		if linter.calls != 1 {
			t.Errorf("expected 1 lint call, got %d", linter.calls)
		}
	})

	t.Run("LintIssuesFixedByCoderOnRetry", func(t *testing.T) {
		t.Parallel()
		// First lint: issues found; second lint (after coder fix): clean.
		linter := &fakeLinter{outputs: []string{"vet error: unused var", ""}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "fixed lint issues", CostUSD: 0.10},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Linter:         linter,
			MaxLintRetries: 2,
			MaxCycles:      3,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", Cycle: 1}
		err := l.runLintFixLoop(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state.LintOutput != "" {
			t.Errorf("expected empty LintOutput after successful fix, got %q", state.LintOutput)
		}
		if linter.calls != 2 {
			t.Errorf("expected 2 lint calls, got %d", linter.calls)
		}
		if inv.calls != 1 {
			t.Errorf("expected 1 coder invocation for lint fix, got %d", inv.calls)
		}
		// The coder should have received a lint-fix prompt.
		if len(inv.prompts) < 1 || !strings.Contains(inv.prompts[0], "lint issues") {
			t.Error("expected lint fix prompt to mention lint issues")
		}
		// Cost should be accumulated.
		if state.TotalCostUSD != 0.10 {
			t.Errorf("TotalCostUSD = %v, want 0.10", state.TotalCostUSD)
		}
	})

	t.Run("LintIssuesPersistAfterMaxRetries", func(t *testing.T) {
		t.Parallel()
		// All lint runs return issues.
		linter := &fakeLinter{outputs: []string{"error 1", "error 2", "error 3"}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "attempt 1", CostUSD: 0.10},
				{ResultText: "attempt 2", CostUSD: 0.10},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Linter:         linter,
			MaxLintRetries: 2,
			MaxCycles:      3,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", Cycle: 1}
		err := l.runLintFixLoop(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Lint output should be preserved for the reviewer.
		if state.LintOutput == "" {
			t.Error("expected non-empty LintOutput after max retries")
		}
		// Should have called lint 3 times (initial + 2 retries).
		if linter.calls != 3 {
			t.Errorf("expected 3 lint calls, got %d", linter.calls)
		}
		// Coder should have been invoked 2 times (once per retry).
		if inv.calls != 2 {
			t.Errorf("expected 2 coder invocations, got %d", inv.calls)
		}
	})

	t.Run("LintExecutionError", func(t *testing.T) {
		t.Parallel()
		linter := &fakeLinter{
			outputs: []string{""},
			errs:    []error{errors.New("command not found")},
		}
		rUI := &recordingUI{}
		l := &Loop{
			UI:     rUI,
			Linter: linter,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task"}
		err := l.runLintFixLoop(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("lint execution error should not be fatal: %v", err)
		}
		if len(rUI.errors) == 0 {
			t.Error("expected error to be logged for lint execution failure")
		}
	})

	t.Run("CoderLintFixInvokeError", func(t *testing.T) {
		t.Parallel()
		linter := &fakeLinter{outputs: []string{"vet error"}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{{}},
			errors:    []error{errors.New("coder crashed")},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Linter:         linter,
			MaxLintRetries: 2,
			MaxCycles:      3,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", Cycle: 1}
		err := l.runLintFixLoop(context.Background(), state, 1.0)
		if err == nil {
			t.Fatal("expected error from coder lint-fix invocation")
		}
		if !strings.Contains(err.Error(), "coder lint-fix invocation failed") {
			t.Errorf("error = %q, want to contain 'coder lint-fix invocation failed'", err.Error())
		}
	})

	t.Run("WithGitCommitAfterLintFix", func(t *testing.T) {
		t.Parallel()
		linter := &fakeLinter{outputs: []string{"vet error", ""}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "fixed", CostUSD: 0.05},
			},
		}
		git := &fakeGit{commitSHAs: []string{"lint-fix-sha"}}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Linter:         linter,
			Git:            git,
			MaxLintRetries: 2,
			MaxCycles:      3,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", Cycle: 1}
		err := l.runLintFixLoop(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(state.CycleCommits) != 1 || state.CycleCommits[0] != "lint-fix-sha" {
			t.Errorf("CycleCommits = %v, want [lint-fix-sha]", state.CycleCommits)
		}
	})

	t.Run("BudgetExceededDuringLintFix", func(t *testing.T) {
		t.Parallel()
		linter := &fakeLinter{outputs: []string{"vet error"}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "expensive fix", CostUSD: 10.0},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &recordingUI{},
			Linter:         linter,
			MaxLintRetries: 2,
			MaxBudgetUSD:   5.0,
			MaxCycles:      3,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", Cycle: 1}
		err := l.runLintFixLoop(context.Background(), state, 1.0)
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Errorf("expected ErrBudgetExceeded, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestBuildLintFixPrompt
// ---------------------------------------------------------------------------

func TestBuildLintFixPrompt(t *testing.T) {
	t.Parallel()

	l := &Loop{}
	state := &CycleState{
		TaskBeadID: "bead-42",
		TaskTitle:  "fix the bug",
		LintOutput: "main.go:10: unused variable x",
	}
	prompt := l.buildLintFixPrompt(state)

	if !strings.Contains(prompt, "bead-42") {
		t.Error("prompt should contain bead ID")
	}
	if !strings.Contains(prompt, "fix the bug") {
		t.Error("prompt should contain task title")
	}
	if !strings.Contains(prompt, "lint issues") {
		t.Error("prompt should mention lint issues")
	}
	if !strings.Contains(prompt, "unused variable x") {
		t.Error("prompt should contain lint output")
	}
}

// ---------------------------------------------------------------------------
// TestBuildReviewerPromptWithLintOutput
// ---------------------------------------------------------------------------

func TestBuildReviewerPromptWithLintOutput(t *testing.T) {
	t.Parallel()

	t.Run("NoLintIssues", func(t *testing.T) {
		t.Parallel()
		l := &Loop{}
		state := &CycleState{
			TaskBeadID:  "bead-1",
			TaskTitle:   "task",
			CoderOutput: "done",
			LintOutput:  "",
		}
		prompt := l.buildReviewerPrompt(state)
		if strings.Contains(prompt, "lint issues were not fully resolved") {
			t.Error("prompt should not mention unresolved lint issues when lint is clean")
		}
		if !strings.Contains(prompt, "linting issues") {
			t.Error("prompt should still include the linting check instruction")
		}
	})

	t.Run("WithUnresolvedLintIssues", func(t *testing.T) {
		t.Parallel()
		l := &Loop{}
		state := &CycleState{
			TaskBeadID:  "bead-1",
			TaskTitle:   "task",
			CoderOutput: "done",
			LintOutput:  "main.go:5: error return value not checked",
		}
		prompt := l.buildReviewerPrompt(state)
		if !strings.Contains(prompt, "lint issues were not fully resolved") {
			t.Error("prompt should mention unresolved lint issues")
		}
		if !strings.Contains(prompt, "error return value not checked") {
			t.Error("prompt should include the lint output")
		}
	})
}

// ---------------------------------------------------------------------------
// TestRunLoopWithLinter
// ---------------------------------------------------------------------------

func TestRunLoopWithLinter(t *testing.T) {
	t.Parallel()

	t.Run("CleanLintDoesNotAddExtraInvocation", func(t *testing.T) {
		t.Parallel()
		linter := &fakeLinter{outputs: []string{""}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},
				{ResultText: "APPROVED: Good.", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			Linter:       linter,
			MaxCycles:    3,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "task")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.CyclesUsed != 1 {
			t.Errorf("CyclesUsed = %d, want 1", result.CyclesUsed)
		}
		// Only 2 invocations: coder + reviewer (no lint-fix coder call).
		if inv.calls != 2 {
			t.Errorf("expected 2 invocations, got %d", inv.calls)
		}
	})

	t.Run("LintIssueTriggersCoderReInvocation", func(t *testing.T) {
		t.Parallel()
		// First lint: issue; second lint (after fix): clean.
		linter := &fakeLinter{outputs: []string{"vet error", ""}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},           // coder
				{ResultText: "fixed lint", CostUSD: 0.10},      // coder lint fix
				{ResultText: "APPROVED: Good.", CostUSD: 0.20}, // reviewer
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Linter:         linter,
			MaxLintRetries: 2,
			MaxCycles:      3,
			MaxBudgetUSD:   10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "task")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.CyclesUsed != 1 {
			t.Errorf("CyclesUsed = %d, want 1", result.CyclesUsed)
		}
		// 3 invocations: coder + lint-fix coder + reviewer.
		if inv.calls != 3 {
			t.Errorf("expected 3 invocations, got %d", inv.calls)
		}
	})
}

// ---------------------------------------------------------------------------
// TestPhaseLintingString
// ---------------------------------------------------------------------------

func TestPhaseLintingString(t *testing.T) {
	t.Parallel()
	if PhaseLinting.String() != "linting" {
		t.Errorf("PhaseLinting.String() = %q, want %q", PhaseLinting.String(), "linting")
	}
}
