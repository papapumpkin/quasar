package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/ui"
)

// ---------------------------------------------------------------------------
// noopUI satisfies ui.UI for tests without producing any output.
// ---------------------------------------------------------------------------

type noopUI struct{}

var _ ui.UI = (*noopUI)(nil)

func (n *noopUI) TaskStarted(string, string)                        {}
func (n *noopUI) TaskComplete(string, float64)                      {}
func (n *noopUI) CycleStart(int, int)                               {}
func (n *noopUI) AgentStart(string)                                 {}
func (n *noopUI) AgentDone(string, float64, int64)                  {}
func (n *noopUI) CycleSummary(ui.CycleSummaryData)                  {}
func (n *noopUI) IssuesFound(int)                                   {}
func (n *noopUI) Approved()                                         {}
func (n *noopUI) MaxCyclesReached(int)                              {}
func (n *noopUI) BudgetExceeded(float64, float64)                   {}
func (n *noopUI) Error(string)                                      {}
func (n *noopUI) Info(string)                                       {}
func (n *noopUI) AgentOutput(string, int, string)                   {}
func (n *noopUI) BeadUpdate(string, string, string, []ui.BeadChild) {}
func (n *noopUI) RefactorApplied(string)                            {}

// ---------------------------------------------------------------------------
// noopBeads satisfies beads.Client for tests without side effects.
// ---------------------------------------------------------------------------

type noopBeads struct{}

var _ beads.Client = (*noopBeads)(nil)

func (n *noopBeads) Create(context.Context, string, beads.CreateOpts) (string, error) {
	return "test-bead", nil
}
func (n *noopBeads) Show(context.Context, string) (*beads.Bead, error)      { return nil, nil }
func (n *noopBeads) Update(context.Context, string, beads.UpdateOpts) error { return nil }
func (n *noopBeads) Close(context.Context, string, string) error            { return nil }
func (n *noopBeads) AddComment(context.Context, string, string) error       { return nil }
func (n *noopBeads) Validate() error                                        { return nil }

// ---------------------------------------------------------------------------
// recordingUI captures method calls for assertions.
// ---------------------------------------------------------------------------

type recordingUI struct {
	noopUI
	mu              sync.Mutex
	taskStartedIDs  []string
	taskCompleteIDs []string
	cycleStarts     []int
	agentStarts     []string
	agentDones      []string
	approvedCalls   int
	maxCyclesCalls  int
	budgetCalls     int
	issuesCounts    []int
	errors          []string
	beadUpdates     []beadUpdateCall
	cycleSummaries  []ui.CycleSummaryData
	refactorIDs     []string
}

type beadUpdateCall struct {
	beadID   string
	title    string
	status   string
	children []ui.BeadChild
}

func (r *recordingUI) TaskStarted(id, title string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taskStartedIDs = append(r.taskStartedIDs, id)
}
func (r *recordingUI) TaskComplete(id string, _ float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taskCompleteIDs = append(r.taskCompleteIDs, id)
}
func (r *recordingUI) CycleStart(cycle, _ int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cycleStarts = append(r.cycleStarts, cycle)
}
func (r *recordingUI) AgentStart(role string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentStarts = append(r.agentStarts, role)
}
func (r *recordingUI) AgentDone(role string, _ float64, _ int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentDones = append(r.agentDones, role)
}
func (r *recordingUI) Approved() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.approvedCalls++
}
func (r *recordingUI) MaxCyclesReached(_ int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxCyclesCalls++
}
func (r *recordingUI) BudgetExceeded(_, _ float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.budgetCalls++
}
func (r *recordingUI) IssuesFound(count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.issuesCounts = append(r.issuesCounts, count)
}
func (r *recordingUI) Error(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, msg)
}
func (r *recordingUI) BeadUpdate(beadID, title, status string, children []ui.BeadChild) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beadUpdates = append(r.beadUpdates, beadUpdateCall{beadID, title, status, children})
}
func (r *recordingUI) CycleSummary(d ui.CycleSummaryData) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cycleSummaries = append(r.cycleSummaries, d)
}
func (r *recordingUI) RefactorApplied(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refactorIDs = append(r.refactorIDs, id)
}

// ---------------------------------------------------------------------------
// recordingBeads captures bead operations for assertions.
// ---------------------------------------------------------------------------

type recordingBeads struct {
	noopBeads
	mu       sync.Mutex
	creates  []string // titles
	updates  []beads.UpdateOpts
	comments []string
	closes   []string // reasons
	createID string   // returned from Create
}

func newRecordingBeads() *recordingBeads {
	return &recordingBeads{createID: "test-bead"}
}

// newBeadHook creates a BeadHook wired to the given beads.Client and UI for testing.
func newBeadHook(b beads.Client, u ui.UI) *BeadHook {
	return &BeadHook{Beads: b, UI: u}
}

func (r *recordingBeads) Create(_ context.Context, title string, _ beads.CreateOpts) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates = append(r.creates, title)
	return r.createID, nil
}
func (r *recordingBeads) Update(_ context.Context, _ string, opts beads.UpdateOpts) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, opts)
	return nil
}
func (r *recordingBeads) AddComment(_ context.Context, _ string, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.comments = append(r.comments, body)
	return nil
}
func (r *recordingBeads) Close(_ context.Context, _ string, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes = append(r.closes, reason)
	return nil
}

// ---------------------------------------------------------------------------
// fakeInvoker returns controlled responses for testing the loop.
// ---------------------------------------------------------------------------

type fakeInvoker struct {
	// responses is a queue of results returned by successive Invoke calls.
	// Each call pops the first element. If the queue is empty, returns an error.
	responses []agent.InvocationResult
	errors    []error // parallel to responses; nil means no error for that call
	mu        sync.Mutex
	prompts   []string // captured prompts
	agents    []agent.Agent
	calls     int
}

func (f *fakeInvoker) Invoke(_ context.Context, a agent.Agent, prompt string, _ string) (agent.InvocationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	f.agents = append(f.agents, a)
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		return agent.InvocationResult{}, fmt.Errorf("unexpected invocation #%d", idx)
	}
	var err error
	if idx < len(f.errors) {
		err = f.errors[idx]
	}
	return f.responses[idx], err
}
func (f *fakeInvoker) Validate() error { return nil }

// ---------------------------------------------------------------------------
// fakeGit implements CycleCommitter for testing.
// ---------------------------------------------------------------------------

type fakeGit struct {
	headSHA    string
	commitSHAs []string // returned by successive CommitCycle calls
	mu         sync.Mutex
	commits    int
	headErr    error
	commitErr  error
}

func (g *fakeGit) HeadSHA(_ context.Context) (string, error) {
	return g.headSHA, g.headErr
}

func (g *fakeGit) CommitCycle(_ context.Context, _ string, _ int, _ string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.commitErr != nil {
		return "", g.commitErr
	}
	idx := g.commits
	g.commits++
	if idx < len(g.commitSHAs) {
		return g.commitSHAs[idx], nil
	}
	return fmt.Sprintf("sha-%d", idx), nil
}

func (g *fakeGit) DiffRange(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (g *fakeGit) ResetTo(_ context.Context, _ string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Existing tests (kept as-is)
// ---------------------------------------------------------------------------

func TestNilGitDoesNotPanic(t *testing.T) {
	t.Parallel()

	l := &Loop{
		UI:        &noopUI{},
		Git:       nil,
		MaxCycles: 1,
	}

	ctx := context.Background()
	state := l.initCycleState(ctx, "test-bead", "test task")

	if state.BaseCommitSHA != "" {
		t.Errorf("expected empty BaseCommitSHA with nil Git, got %q", state.BaseCommitSHA)
	}
	if len(state.CycleCommits) != 0 {
		t.Errorf("expected empty CycleCommits with nil Git, got %v", state.CycleCommits)
	}
}

// ---------------------------------------------------------------------------
// TestPerAgentBudget
// ---------------------------------------------------------------------------

func TestPerAgentBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		budget   float64
		cycles   int
		expected float64
	}{
		{"ZeroBudget", 0, 3, 0},
		{"NegativeBudget", -1.0, 3, 0},
		{"NormalBudget", 6.0, 3, 1.0},       // 6 / (2*3) = 1.0
		{"SingleCycle", 2.0, 1, 1.0},        // 2 / (2*1) = 1.0
		{"FractionalBudget", 1.0, 4, 0.125}, // 1 / (2*4) = 0.125
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := &Loop{MaxBudgetUSD: tt.budget, MaxCycles: tt.cycles}
			got := l.perAgentBudget()
			if got != tt.expected {
				t.Errorf("perAgentBudget() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCheckBudget
// ---------------------------------------------------------------------------

func TestCheckBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		maxBudget  float64
		totalCost  float64
		wantErr    error
		wantBudget int // expected budgetCalls on UI
	}{
		{"NoBudgetLimit", 0, 100.0, nil, 0},
		{"NegativeLimit", -5.0, 100.0, nil, 0},
		{"UnderBudget", 10.0, 5.0, nil, 0},
		{"AtBudget", 10.0, 10.0, ErrBudgetExceeded, 1},
		{"OverBudget", 10.0, 15.0, ErrBudgetExceeded, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rUI := &recordingUI{}
			l := &Loop{
				MaxBudgetUSD: tt.maxBudget,
				UI:           rUI,
			}
			state := &CycleState{
				TaskBeadID:   "bead-1",
				TotalCostUSD: tt.totalCost,
			}
			err := l.checkBudget(context.Background(), state)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("checkBudget() error = %v, want %v", err, tt.wantErr)
			}
			if rUI.budgetCalls != tt.wantBudget {
				t.Errorf("budgetCalls = %d, want %d", rUI.budgetCalls, tt.wantBudget)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestInitCycleState
// ---------------------------------------------------------------------------

func TestInitCycleState(t *testing.T) {
	t.Parallel()

	t.Run("WithGit", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		git := &fakeGit{headSHA: "abc123"}
		l := &Loop{
			UI:        rUI,
			Git:       git,
			MaxCycles: 3,
		}
		state := l.initCycleState(context.Background(), "bead-42", "implement feature")
		if state.TaskBeadID != "bead-42" {
			t.Errorf("TaskBeadID = %q, want %q", state.TaskBeadID, "bead-42")
		}
		if state.TaskTitle != "implement feature" {
			t.Errorf("TaskTitle = %q, want %q", state.TaskTitle, "implement feature")
		}
		if state.Phase != PhaseBeadCreated {
			t.Errorf("Phase = %v, want PhaseBeadCreated", state.Phase)
		}
		if state.MaxCycles != 3 {
			t.Errorf("MaxCycles = %d, want 3", state.MaxCycles)
		}
		if state.BaseCommitSHA != "abc123" {
			t.Errorf("BaseCommitSHA = %q, want %q", state.BaseCommitSHA, "abc123")
		}
		if len(rUI.taskStartedIDs) != 1 || rUI.taskStartedIDs[0] != "bead-42" {
			t.Errorf("TaskStarted not called correctly: %v", rUI.taskStartedIDs)
		}
	})

	t.Run("GitHeadError", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		git := &fakeGit{headErr: errors.New("git error")}
		l := &Loop{
			UI:        rUI,
			Git:       git,
			MaxCycles: 1,
		}
		state := l.initCycleState(context.Background(), "bead-1", "task")
		if state.BaseCommitSHA != "" {
			t.Errorf("expected empty BaseCommitSHA on git error, got %q", state.BaseCommitSHA)
		}
		if len(rUI.errors) == 0 {
			t.Error("expected error to be logged for git failure")
		}
	})
}

// ---------------------------------------------------------------------------
// TestCoderAgent / TestReviewerAgent
// ---------------------------------------------------------------------------

func TestCoderAgent(t *testing.T) {
	t.Parallel()

	l := &Loop{
		Model:       "claude-sonnet",
		CoderPrompt: "You are a coder.",
	}
	a := l.coderAgent(2.5)
	if a.Role != agent.RoleCoder {
		t.Errorf("Role = %q, want %q", a.Role, agent.RoleCoder)
	}
	if a.Model != "claude-sonnet" {
		t.Errorf("Model = %q, want %q", a.Model, "claude-sonnet")
	}
	if a.MaxBudgetUSD != 2.5 {
		t.Errorf("MaxBudgetUSD = %v, want 2.5", a.MaxBudgetUSD)
	}
	if a.SystemPrompt != "You are a coder." {
		t.Errorf("SystemPrompt = %q, want %q", a.SystemPrompt, "You are a coder.")
	}
	if len(a.AllowedTools) == 0 {
		t.Error("expected non-empty AllowedTools for coder")
	}
}

func TestReviewerAgent(t *testing.T) {
	t.Parallel()

	l := &Loop{
		Model:        "claude-opus",
		ReviewPrompt: "You are a reviewer.",
	}
	a := l.reviewerAgent(1.5)
	if a.Role != agent.RoleReviewer {
		t.Errorf("Role = %q, want %q", a.Role, agent.RoleReviewer)
	}
	if a.Model != "claude-opus" {
		t.Errorf("Model = %q, want %q", a.Model, "claude-opus")
	}
	if a.MaxBudgetUSD != 1.5 {
		t.Errorf("MaxBudgetUSD = %v, want 1.5", a.MaxBudgetUSD)
	}
	if a.SystemPrompt != "You are a reviewer." {
		t.Errorf("SystemPrompt = %q, want %q", a.SystemPrompt, "You are a reviewer.")
	}
}

func TestCoderAgentWithMCP(t *testing.T) {
	t.Parallel()

	mcp := &agent.MCPConfig{ConfigPath: "/tmp/mcp.json"}
	l := &Loop{MCP: mcp}
	a := l.coderAgent(1.0)
	if a.MCP != mcp {
		t.Error("expected MCP config to be passed to coder agent")
	}
}

// ---------------------------------------------------------------------------
// TestRunCoderPhase
// ---------------------------------------------------------------------------

func TestRunCoderPhase(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "implemented feature", CostUSD: 0.50, DurationMs: 1000},
			},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        rUI,
			MaxCycles: 3,
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			TaskTitle:  "implement feature",
			Cycle:      1,
			MaxCycles:  3,
		}
		err := l.runCoderPhase(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state.CoderOutput != "implemented feature" {
			t.Errorf("CoderOutput = %q, want %q", state.CoderOutput, "implemented feature")
		}
		if state.TotalCostUSD != 0.50 {
			t.Errorf("TotalCostUSD = %v, want 0.50", state.TotalCostUSD)
		}
		if state.Phase != PhaseCodeComplete {
			t.Errorf("Phase = %v, want PhaseCodeComplete", state.Phase)
		}
		if len(rUI.agentStarts) == 0 || rUI.agentStarts[0] != "coder" {
			t.Errorf("expected AgentStart('coder'), got %v", rUI.agentStarts)
		}
	})

	t.Run("InvokerError", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{{}},
			errors:    []error{errors.New("invoke failed")},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        &noopUI{},
			MaxCycles: 1,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", Cycle: 1}
		err := l.runCoderPhase(context.Background(), state, 1.0)
		if err == nil {
			t.Fatal("expected error from coder invocation")
		}
		if !strings.Contains(err.Error(), "coder invocation failed") {
			t.Errorf("error = %q, want to contain 'coder invocation failed'", err.Error())
		}
		if state.Phase != PhaseError {
			t.Errorf("Phase = %v, want PhaseError", state.Phase)
		}
	})

	t.Run("WithGitCommit", func(t *testing.T) {
		t.Parallel()
		git := &fakeGit{commitSHAs: []string{"commit-abc"}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "done", CostUSD: 0.10},
			},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        &noopUI{},
			Git:       git,
			MaxCycles: 1,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", Cycle: 1}
		err := l.runCoderPhase(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// runCoderPhase stores SHA in lastCycleSHA (sealed at cycle end).
		if state.lastCycleSHA != "commit-abc" {
			t.Errorf("lastCycleSHA = %q, want %q", state.lastCycleSHA, "commit-abc")
		}
		if len(state.CycleCommits) != 0 {
			t.Errorf("CycleCommits should be empty before sealing, got %v", state.CycleCommits)
		}
	})

	t.Run("WithGitCommitError", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		git := &fakeGit{commitErr: errors.New("commit failed")}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "done", CostUSD: 0.10},
			},
		}
		l := &Loop{
			Invoker: inv,

			UI:        rUI,
			Git:       git,
			MaxCycles: 1,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", Cycle: 1}
		err := l.runCoderPhase(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state.lastCycleSHA != "" {
			t.Errorf("expected empty lastCycleSHA on commit error, got %q", state.lastCycleSHA)
		}
		if len(state.CycleCommits) != 0 {
			t.Errorf("expected no commits on error, got %v", state.CycleCommits)
		}
		if len(rUI.errors) == 0 {
			t.Error("expected error logged for commit failure")
		}
	})

	t.Run("WithRefactor", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		rb := newRecordingBeads()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "refactored", CostUSD: 0.30},
			},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        rUI,
			Hooks:     []Hook{newBeadHook(rb, rUI)},
			MaxCycles: 3,
		}
		state := &CycleState{
			TaskBeadID:          "bead-1",
			TaskTitle:           "updated task",
			Cycle:               2,
			Refactored:          true,
			OriginalDescription: "original task",
			RefactorDescription: "updated task",
		}
		err := l.runCoderPhase(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have posted a refactor comment
		foundRefactorComment := false
		for _, c := range rb.comments {
			if strings.Contains(c, "[refactor cycle") {
				foundRefactorComment = true
				break
			}
		}
		if !foundRefactorComment {
			t.Error("expected a refactor bead comment, none found")
		}
		if len(rUI.refactorIDs) == 0 {
			t.Error("expected RefactorApplied to be called")
		}
	})
}

// ---------------------------------------------------------------------------
// TestRunReviewerPhase
// ---------------------------------------------------------------------------

func TestRunReviewerPhase(t *testing.T) {
	t.Parallel()

	t.Run("Approved", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "APPROVED: Looks good.", CostUSD: 0.25, DurationMs: 500},
			},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        rUI,
			MaxCycles: 3,
		}
		state := &CycleState{
			TaskBeadID:  "bead-1",
			TaskTitle:   "task",
			CoderOutput: "did the work",
			Cycle:       1,
		}
		err := l.runReviewerPhase(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state.ReviewOutput != "APPROVED: Looks good." {
			t.Errorf("ReviewOutput = %q, want APPROVED message", state.ReviewOutput)
		}
		if state.TotalCostUSD != 0.25 {
			t.Errorf("TotalCostUSD = %v, want 0.25", state.TotalCostUSD)
		}
		if state.Phase != PhaseReviewComplete {
			t.Errorf("Phase = %v, want PhaseReviewComplete", state.Phase)
		}
		if len(state.Findings) != 0 {
			t.Errorf("expected no findings for approved review, got %d", len(state.Findings))
		}
	})

	t.Run("WithIssues", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "ISSUE:\nSEVERITY: critical\nDESCRIPTION: Missing nil check.", CostUSD: 0.30},
			},
		}
		l := &Loop{
			Invoker: inv,

			UI:        &noopUI{},
			MaxCycles: 3,
		}
		state := &CycleState{
			TaskBeadID:  "bead-1",
			TaskTitle:   "task",
			CoderOutput: "code output",
			Cycle:       1,
		}
		err := l.runReviewerPhase(context.Background(), state, 1.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(state.Findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(state.Findings))
		}
		if state.Findings[0].Severity != "critical" {
			t.Errorf("Severity = %q, want %q", state.Findings[0].Severity, "critical")
		}
	})

	t.Run("InvokerError", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{{}},
			errors:    []error{errors.New("reviewer failed")},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        &noopUI{},
			MaxCycles: 1,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task", CoderOutput: "code", Cycle: 1}
		err := l.runReviewerPhase(context.Background(), state, 1.0)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "reviewer invocation failed") {
			t.Errorf("error = %q, want to contain 'reviewer invocation failed'", err.Error())
		}
		if state.Phase != PhaseError {
			t.Errorf("Phase = %v, want PhaseError", state.Phase)
		}
	})
}

// ---------------------------------------------------------------------------
// TestHandleApproval
// ---------------------------------------------------------------------------

func TestHandleApproval(t *testing.T) {
	t.Parallel()

	t.Run("Basic", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		rb := newRecordingBeads()
		l := &Loop{
			UI:        rUI,
			Hooks:     []Hook{newBeadHook(rb, rUI)},
			MaxCycles: 3,
		}
		state := &CycleState{
			TaskBeadID:   "bead-1",
			TaskTitle:    "task",
			Cycle:        2,
			TotalCostUSD: 1.50,
			ReviewOutput: "APPROVED: Good work.",
		}
		result, err := l.handleApproval(context.Background(), state)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.TotalCostUSD != 1.50 {
			t.Errorf("TotalCostUSD = %v, want 1.50", result.TotalCostUSD)
		}
		if result.CyclesUsed != 2 {
			t.Errorf("CyclesUsed = %d, want 2", result.CyclesUsed)
		}
		if state.Phase != PhaseApproved {
			t.Errorf("Phase = %v, want PhaseApproved", state.Phase)
		}
		if rUI.approvedCalls != 1 {
			t.Errorf("Approved() calls = %d, want 1", rUI.approvedCalls)
		}
		if len(rb.closes) != 1 {
			t.Errorf("expected 1 bead close, got %d", len(rb.closes))
		}
		if len(rUI.taskCompleteIDs) != 1 || rUI.taskCompleteIDs[0] != "bead-1" {
			t.Errorf("TaskComplete not called with bead-1: %v", rUI.taskCompleteIDs)
		}
	})

	t.Run("WithReport", func(t *testing.T) {
		t.Parallel()
		rb := newRecordingBeads()
		rUI := &recordingUI{}
		l := &Loop{
			UI:        rUI,
			Hooks:     []Hook{newBeadHook(rb, rUI)},
			MaxCycles: 3,
		}
		state := &CycleState{
			TaskBeadID:   "bead-1",
			TaskTitle:    "task",
			Cycle:        1,
			TotalCostUSD: 0.50,
			ReviewOutput: "APPROVED: Good.\n\nREPORT:\nSATISFACTION: high\nRISK: low\nSUMMARY: All good",
		}
		result, err := l.handleApproval(context.Background(), state)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Report == nil {
			t.Fatal("expected non-nil Report")
		}
		if result.Report.Satisfaction != "high" {
			t.Errorf("Satisfaction = %q, want %q", result.Report.Satisfaction, "high")
		}
		// Should have added a report comment
		foundReportComment := false
		for _, c := range rb.comments {
			if strings.Contains(c, "[reviewer report]") {
				foundReportComment = true
				break
			}
		}
		if !foundReportComment {
			t.Error("expected reviewer report comment to be added")
		}
	})
}

// ---------------------------------------------------------------------------
// TestEmitCycleSummary
// ---------------------------------------------------------------------------

func TestEmitCycleSummary(t *testing.T) {
	t.Parallel()

	rUI := &recordingUI{}
	l := &Loop{
		UI:           rUI,
		MaxCycles:    5,
		MaxBudgetUSD: 10.0,
	}
	state := &CycleState{
		Cycle:        2,
		TotalCostUSD: 3.0,
		ReviewOutput: "APPROVED: ok",
		Findings:     []ReviewFinding{{Severity: "major", Description: "bug"}},
	}
	result := agent.InvocationResult{CostUSD: 1.5, DurationMs: 2000}
	l.emitCycleSummary(state, PhaseCodeComplete, result)

	if len(rUI.cycleSummaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(rUI.cycleSummaries))
	}
	s := rUI.cycleSummaries[0]
	if s.Cycle != 2 {
		t.Errorf("Cycle = %d, want 2", s.Cycle)
	}
	if s.MaxCycles != 5 {
		t.Errorf("MaxCycles = %d, want 5", s.MaxCycles)
	}
	if s.Phase != "code_complete" {
		t.Errorf("Phase = %q, want %q", s.Phase, "code_complete")
	}
	if s.CostUSD != 1.5 {
		t.Errorf("CostUSD = %v, want 1.5", s.CostUSD)
	}
	if s.TotalCostUSD != 3.0 {
		t.Errorf("TotalCostUSD = %v, want 3.0", s.TotalCostUSD)
	}
	if s.MaxBudgetUSD != 10.0 {
		t.Errorf("MaxBudgetUSD = %v, want 10.0", s.MaxBudgetUSD)
	}
	if !s.Approved {
		t.Error("expected Approved=true")
	}
	if s.IssueCount != 1 {
		t.Errorf("IssueCount = %d, want 1", s.IssueCount)
	}
}

// ---------------------------------------------------------------------------
// TestEmitBeadUpdate
// ---------------------------------------------------------------------------

func TestEmitBeadUpdate(t *testing.T) {
	t.Parallel()

	t.Run("InProgress", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		l := &Loop{UI: rUI}
		state := &CycleState{
			TaskBeadID:   "bead-1",
			TaskTitle:    "my task",
			ChildBeadIDs: []string{"child-1", "child-2"},
			AllFindings: []ReviewFinding{
				{Description: "first issue", Severity: "critical", Cycle: 1},
				{Description: "second issue", Severity: "minor", Cycle: 2},
			},
		}
		l.emitBeadUpdate(state, "in_progress")

		if len(rUI.beadUpdates) != 1 {
			t.Fatalf("expected 1 bead update, got %d", len(rUI.beadUpdates))
		}
		u := rUI.beadUpdates[0]
		if u.beadID != "bead-1" {
			t.Errorf("beadID = %q, want %q", u.beadID, "bead-1")
		}
		if u.status != "in_progress" {
			t.Errorf("status = %q, want %q", u.status, "in_progress")
		}
		if len(u.children) != 2 {
			t.Fatalf("expected 2 children, got %d", len(u.children))
		}
		if u.children[0].Status != "open" {
			t.Errorf("child[0].Status = %q, want %q", u.children[0].Status, "open")
		}
		if u.children[0].Severity != "critical" {
			t.Errorf("child[0].Severity = %q, want %q", u.children[0].Severity, "critical")
		}
		if u.children[0].Cycle != 1 {
			t.Errorf("child[0].Cycle = %d, want 1", u.children[0].Cycle)
		}
	})

	t.Run("Closed", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		l := &Loop{UI: rUI}
		state := &CycleState{
			TaskBeadID:   "bead-1",
			TaskTitle:    "task",
			ChildBeadIDs: []string{"child-1"},
			AllFindings:  []ReviewFinding{{Description: "issue", Severity: "major", Cycle: 1}},
		}
		l.emitBeadUpdate(state, "closed")

		if len(rUI.beadUpdates) != 1 {
			t.Fatalf("expected 1 bead update, got %d", len(rUI.beadUpdates))
		}
		if rUI.beadUpdates[0].children[0].Status != "closed" {
			t.Errorf("child status = %q, want %q", rUI.beadUpdates[0].children[0].Status, "closed")
		}
	})

	t.Run("NoChildren", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		l := &Loop{UI: rUI}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task"}
		l.emitBeadUpdate(state, "in_progress")

		if len(rUI.beadUpdates) != 1 {
			t.Fatalf("expected 1 bead update, got %d", len(rUI.beadUpdates))
		}
		if len(rUI.beadUpdates[0].children) != 0 {
			t.Errorf("expected no children, got %d", len(rUI.beadUpdates[0].children))
		}
	})

	t.Run("MoreChildrenThanFindings", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		l := &Loop{UI: rUI}
		state := &CycleState{
			TaskBeadID:   "bead-1",
			TaskTitle:    "task",
			ChildBeadIDs: []string{"child-1", "child-2", "child-3"},
			AllFindings:  []ReviewFinding{{Description: "only finding", Severity: "minor", Cycle: 1}},
		}
		l.emitBeadUpdate(state, "in_progress")

		children := rUI.beadUpdates[0].children
		if len(children) != 3 {
			t.Fatalf("expected 3 children, got %d", len(children))
		}
		// First child uses the finding data.
		if children[0].Severity != "minor" {
			t.Errorf("child[0].Severity = %q, want %q", children[0].Severity, "minor")
		}
		// Remaining children use defaults.
		if children[1].Title != "review finding" {
			t.Errorf("child[1].Title = %q, want %q", children[1].Title, "review finding")
		}
		if children[2].Severity != "major" {
			t.Errorf("child[2].Severity = %q, want default %q", children[2].Severity, "major")
		}
	})
}

// ---------------------------------------------------------------------------
// TestCreateFindingBeads
// ---------------------------------------------------------------------------

func TestCreateFindingBeads(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		rb := newRecordingBeads()
		nui := &noopUI{}
		l := &Loop{
			UI:    nui,
			Hooks: []Hook{newBeadHook(rb, nui)},
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			Findings: []ReviewFinding{
				{Severity: "critical", Description: "nil pointer"},
				{Severity: "minor", Description: "naming"},
			},
		}
		ids := l.createFindingBeads(context.Background(), state)
		if len(ids) != 2 {
			t.Fatalf("expected 2 IDs, got %d", len(ids))
		}
		if len(rb.creates) != 2 {
			t.Fatalf("expected 2 creates, got %d", len(rb.creates))
		}
		if !strings.Contains(rb.creates[0], "[critical]") {
			t.Errorf("first create title = %q, want to contain [critical]", rb.creates[0])
		}
	})

	t.Run("CreateError", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		// A beads that always errors on Create.
		errBeads := &errorBeads{createErr: errors.New("create failed")}
		l := &Loop{
			UI:    rUI,
			Hooks: []Hook{newBeadHook(errBeads, rUI)},
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			Findings: []ReviewFinding{
				{Severity: "major", Description: "bug"},
			},
		}
		ids := l.createFindingBeads(context.Background(), state)
		if len(ids) != 0 {
			t.Errorf("expected 0 IDs on error, got %d", len(ids))
		}
		if len(rUI.errors) == 0 {
			t.Error("expected error to be logged")
		}
	})

	t.Run("NoFindings", func(t *testing.T) {
		t.Parallel()
		nui := &noopUI{}
		l := &Loop{UI: nui, Hooks: []Hook{newBeadHook(&noopBeads{}, nui)}}
		state := &CycleState{TaskBeadID: "bead-1"}
		ids := l.createFindingBeads(context.Background(), state)
		if len(ids) != 0 {
			t.Errorf("expected 0 IDs for no findings, got %d", len(ids))
		}
	})
}

// errorBeads is a beads.Client that returns errors for configurable operations.
type errorBeads struct {
	noopBeads
	createErr  error
	updateErr  error
	closeErr   error
	commentErr error
}

func (e *errorBeads) Create(context.Context, string, beads.CreateOpts) (string, error) {
	return "", e.createErr
}
func (e *errorBeads) Update(_ context.Context, _ string, _ beads.UpdateOpts) error {
	return e.updateErr
}
func (e *errorBeads) Close(_ context.Context, _ string, _ string) error {
	return e.closeErr
}
func (e *errorBeads) AddComment(_ context.Context, _ string, _ string) error {
	return e.commentErr
}

// ---------------------------------------------------------------------------
// TestBuildReviewerPrompt
// ---------------------------------------------------------------------------

func TestBuildReviewerPrompt(t *testing.T) {
	t.Parallel()

	l := &Loop{MaxCycles: 3}
	state := &CycleState{
		TaskBeadID:  "bead-42",
		TaskTitle:   "fix the bug",
		CoderOutput: "I fixed the nil pointer in handler.go",
	}
	prompt := l.buildReviewerPrompt(state)

	if !strings.Contains(prompt, "bead-42") {
		t.Error("prompt should contain bead ID")
	}
	if !strings.Contains(prompt, "fix the bug") {
		t.Error("prompt should contain task title")
	}
	if !strings.Contains(prompt, "I fixed the nil pointer") {
		t.Error("prompt should contain coder output")
	}
	if !strings.Contains(prompt, "REVIEW INSTRUCTIONS") {
		t.Error("prompt should contain review instructions")
	}
	if !strings.Contains(prompt, "APPROVED:") {
		t.Error("prompt should mention APPROVED format")
	}
}

// ---------------------------------------------------------------------------
// TestRunLoop — end-to-end via runLoop
// ---------------------------------------------------------------------------

func TestRunLoop(t *testing.T) {
	t.Parallel()

	t.Run("ApprovedFirstCycle", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "implemented feature", CostUSD: 0.50, DurationMs: 1000},
				{ResultText: "APPROVED: Looks great.", CostUSD: 0.25, DurationMs: 500},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           rUI,
			MaxCycles:    3,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "implement feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.CyclesUsed != 1 {
			t.Errorf("CyclesUsed = %d, want 1", result.CyclesUsed)
		}
		if result.TotalCostUSD != 0.75 {
			t.Errorf("TotalCostUSD = %v, want 0.75", result.TotalCostUSD)
		}
		if rUI.approvedCalls != 1 {
			t.Errorf("Approved calls = %d, want 1", rUI.approvedCalls)
		}
		if len(rUI.cycleStarts) != 1 {
			t.Errorf("CycleStart calls = %d, want 1", len(rUI.cycleStarts))
		}
	})

	t.Run("RejectedThenApproved", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		rb := newRecordingBeads()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				// Cycle 1: coder
				{ResultText: "first attempt", CostUSD: 0.50},
				// Cycle 1: reviewer — rejected
				{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: Missing error handling.", CostUSD: 0.30},
				// Cycle 2: coder
				{ResultText: "fixed error handling", CostUSD: 0.40},
				// Cycle 2: reviewer — approved
				{ResultText: "APPROVED: Error handling is correct now.", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           rUI,
			Hooks:        []Hook{newBeadHook(rb, rUI)},
			MaxCycles:    3,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "add error handling")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.CyclesUsed != 2 {
			t.Errorf("CyclesUsed = %d, want 2", result.CyclesUsed)
		}
		if len(rUI.cycleStarts) != 2 {
			t.Errorf("CycleStart calls = %d, want 2", len(rUI.cycleStarts))
		}
		if len(rUI.issuesCounts) != 1 || rUI.issuesCounts[0] != 1 {
			t.Errorf("IssuesFound calls = %v, want [1]", rUI.issuesCounts)
		}
		// Should have created child bead for the finding.
		if len(rb.creates) == 0 {
			t.Error("expected child bead to be created for finding")
		}
	})

	t.Run("MaxCyclesReached", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				// Cycle 1: coder
				{ResultText: "attempt 1", CostUSD: 0.20},
				// Cycle 1: reviewer — rejected
				{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: Still broken.", CostUSD: 0.15},
				// Cycle 2: coder
				{ResultText: "attempt 2", CostUSD: 0.20},
				// Cycle 2: reviewer — rejected again
				{ResultText: "ISSUE:\nSEVERITY: critical\nDESCRIPTION: Also broken.", CostUSD: 0.15},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           rUI,
			MaxCycles:    2,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "fix bug")
		if !errors.Is(err, ErrMaxCycles) {
			t.Errorf("expected ErrMaxCycles, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result on max cycles, got %v", result)
		}
		if rUI.maxCyclesCalls != 1 {
			t.Errorf("MaxCyclesReached calls = %d, want 1", rUI.maxCyclesCalls)
		}
	})

	t.Run("BudgetExceededAfterCoder", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				// Coder consumes entire budget.
				{ResultText: "expensive work", CostUSD: 10.0},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &recordingUI{},
			MaxCycles:    3,
			MaxBudgetUSD: 5.0,
		}
		_, err := l.runLoop(context.Background(), "bead-1", "task")
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Errorf("expected ErrBudgetExceeded, got %v", err)
		}
	})

	t.Run("BudgetExceededAfterReviewer", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "code", CostUSD: 3.0},
				// Reviewer pushes over budget.
				{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: bug", CostUSD: 3.0},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &recordingUI{},
			MaxCycles:    3,
			MaxBudgetUSD: 5.0,
		}
		_, err := l.runLoop(context.Background(), "bead-1", "task")
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Errorf("expected ErrBudgetExceeded, got %v", err)
		}
	})

	t.Run("CoderInvokeError", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{{}},
			errors:    []error{errors.New("coder crashed")},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			MaxCycles:    1,
			MaxBudgetUSD: 10.0,
		}
		_, err := l.runLoop(context.Background(), "bead-1", "task")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "coder invocation failed") {
			t.Errorf("error = %q, want to contain 'coder invocation failed'", err.Error())
		}
	})

	t.Run("ReviewerInvokeError", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "code done", CostUSD: 0.5},
				{},
			},
			errors: []error{nil, errors.New("reviewer crashed")},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			MaxCycles:    1,
			MaxBudgetUSD: 10.0,
		}
		_, err := l.runLoop(context.Background(), "bead-1", "task")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "reviewer invocation failed") {
			t.Errorf("error = %q, want to contain 'reviewer invocation failed'", err.Error())
		}
	})

	t.Run("WithGitIntegration", func(t *testing.T) {
		t.Parallel()
		git := &fakeGit{headSHA: "base-sha", commitSHAs: []string{"cycle1-sha"}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},
				{ResultText: "APPROVED: Good.", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			Git:          git,
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
	})

	t.Run("OneSHAPerCycleApprovedFirstCycle", func(t *testing.T) {
		t.Parallel()
		git := &fakeGit{headSHA: "base-sha", commitSHAs: []string{"sha-c1"}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},
				{ResultText: "APPROVED: Good.", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			Git:          git,
			MaxCycles:    3,
			MaxBudgetUSD: 10.0,
		}
		_, err := l.runLoop(context.Background(), "bead-1", "task")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Exactly 1 CommitCycle call for one approved cycle.
		git.mu.Lock()
		commits := git.commits
		git.mu.Unlock()
		if commits != 1 {
			t.Errorf("CommitCycle calls = %d, want 1", commits)
		}
	})
}

// ---------------------------------------------------------------------------
// TestCycleCommitsSealing
// ---------------------------------------------------------------------------

func TestCycleCommitsSealing(t *testing.T) {
	t.Parallel()

	t.Run("CoderThenSeal", func(t *testing.T) {
		t.Parallel()
		git := &fakeGit{commitSHAs: []string{"sha-coder"}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "done", CostUSD: 0.10},
			},
		}
		l := &Loop{Invoker: inv, UI: &noopUI{}, Git: git, MaxCycles: 1}
		state := &CycleState{TaskBeadID: "b1", TaskTitle: "task", Cycle: 1}

		if err := l.runCoderPhase(context.Background(), state, 1.0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Before sealing: lastCycleSHA set, CycleCommits empty.
		if state.lastCycleSHA != "sha-coder" {
			t.Errorf("lastCycleSHA = %q, want %q", state.lastCycleSHA, "sha-coder")
		}
		if len(state.CycleCommits) != 0 {
			t.Errorf("CycleCommits should be empty before seal, got %v", state.CycleCommits)
		}

		// Seal and verify.
		l.sealCycleSHA(state)
		if len(state.CycleCommits) != 1 || state.CycleCommits[0] != "sha-coder" {
			t.Errorf("CycleCommits = %v, want [sha-coder]", state.CycleCommits)
		}
		if state.lastCycleSHA != "" {
			t.Errorf("lastCycleSHA should be cleared after seal, got %q", state.lastCycleSHA)
		}
	})

	t.Run("LintFixOverwritesSHA", func(t *testing.T) {
		t.Parallel()
		// Coder commit then one lint-fix commit — only the lint-fix SHA should survive.
		git := &fakeGit{commitSHAs: []string{"sha-coder", "sha-lint"}}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "done", CostUSD: 0.10},       // coder
				{ResultText: "lint fixed", CostUSD: 0.05}, // lint-fix coder pass
			},
		}
		linter := &fakeLinter{outputs: []string{"error: unused var", ""}} // first run issues, second clean
		l := &Loop{Invoker: inv, UI: &noopUI{}, Git: git, Linter: linter, MaxCycles: 1, MaxLintRetries: 3}
		state := &CycleState{TaskBeadID: "b1", TaskTitle: "task", Cycle: 1}

		if err := l.runCoderPhase(context.Background(), state, 1.0); err != nil {
			t.Fatalf("runCoderPhase error: %v", err)
		}
		if state.lastCycleSHA != "sha-coder" {
			t.Errorf("after coder: lastCycleSHA = %q, want %q", state.lastCycleSHA, "sha-coder")
		}

		if err := l.runLintFixLoop(context.Background(), state, 1.0); err != nil {
			t.Fatalf("runLintFixLoop error: %v", err)
		}
		// Lint fix overwrites the coder SHA.
		if state.lastCycleSHA != "sha-lint" {
			t.Errorf("after lint fix: lastCycleSHA = %q, want %q", state.lastCycleSHA, "sha-lint")
		}
		// CycleCommits still empty — not sealed yet.
		if len(state.CycleCommits) != 0 {
			t.Errorf("CycleCommits should be empty before seal, got %v", state.CycleCommits)
		}

		l.sealCycleSHA(state)
		if len(state.CycleCommits) != 1 || state.CycleCommits[0] != "sha-lint" {
			t.Errorf("CycleCommits = %v, want [sha-lint]", state.CycleCommits)
		}
	})

	t.Run("SealNoOpWhenNoCommit", func(t *testing.T) {
		t.Parallel()
		l := &Loop{UI: &noopUI{}, MaxCycles: 1}
		state := &CycleState{}
		l.sealCycleSHA(state)
		if len(state.CycleCommits) != 0 {
			t.Errorf("expected empty CycleCommits, got %v", state.CycleCommits)
		}
	})
}

// ---------------------------------------------------------------------------
// TestRunTask / TestRunExistingTask
// ---------------------------------------------------------------------------

func TestRunTask(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},
				{ResultText: "APPROVED: All good.", CostUSD: 0.20},
			},
		}
		rb := newRecordingBeads()
		nui := &noopUI{}
		l := &Loop{
			Invoker:      inv,
			UI:           nui,
			Hooks:        []Hook{newBeadHook(rb, nui)},
			MaxCycles:    3,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.RunTask(context.Background(), "do the thing")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		// Should have created a task bead.
		if len(rb.creates) == 0 {
			t.Error("expected bead to be created")
		}
	})

	t.Run("CreateBeadError", func(t *testing.T) {
		t.Parallel()
		eb := &errorBeads{createErr: errors.New("bead creation failed")}
		nui := &noopUI{}
		l := &Loop{
			Invoker:      &fakeInvoker{},
			UI:           nui,
			Hooks:        []Hook{newBeadHook(eb, nui)},
			MaxCycles:    1,
			MaxBudgetUSD: 10.0,
		}
		_, err := l.RunTask(context.Background(), "task")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "failed to create task bead") {
			t.Errorf("error = %q, want to contain 'failed to create task bead'", err.Error())
		}
	})
}

func TestRunExistingTask(t *testing.T) {
	t.Parallel()

	inv := &fakeInvoker{
		responses: []agent.InvocationResult{
			{ResultText: "coded", CostUSD: 0.20},
			{ResultText: "APPROVED: Correct.", CostUSD: 0.10},
		},
	}
	l := &Loop{
		Invoker:      inv,
		UI:           &noopUI{},
		MaxCycles:    3,
		MaxBudgetUSD: 10.0,
	}
	result, err := l.RunExistingTask(context.Background(), "existing-bead", "existing task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CyclesUsed != 1 {
		t.Errorf("CyclesUsed = %d, want 1", result.CyclesUsed)
	}
}

// ---------------------------------------------------------------------------
// TestRunLoopWithRefactor
// ---------------------------------------------------------------------------

func TestRunLoopWithRefactor(t *testing.T) {
	t.Parallel()

	// Simulate a refactor signal arriving between cycle 1 rejection and cycle 2.
	ch := make(chan string, 1)
	rUI := &recordingUI{}
	inv := &fakeInvoker{
		responses: []agent.InvocationResult{
			// Cycle 1: coder
			{ResultText: "first attempt", CostUSD: 0.30},
			// Cycle 1: reviewer — rejected
			{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: Not quite right.", CostUSD: 0.20},
			// Cycle 2: coder (will get refactor prompt)
			{ResultText: "updated implementation", CostUSD: 0.30},
			// Cycle 2: reviewer — approved
			{ResultText: "APPROVED: Good.", CostUSD: 0.20},
		},
	}

	l := &Loop{
		Invoker:      inv,
		UI:           rUI,
		MaxCycles:    3,
		MaxBudgetUSD: 10.0,
		RefactorCh:   ch,
	}

	// Enqueue the refactor signal so it's available between cycles.
	ch <- "new updated task description"

	result, err := l.runLoop(context.Background(), "bead-1", "original task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CyclesUsed != 2 {
		t.Errorf("CyclesUsed = %d, want 2", result.CyclesUsed)
	}

	// The cycle 2 coder prompt should contain the refactor marker.
	if len(inv.prompts) < 3 {
		t.Fatalf("expected at least 3 prompts, got %d", len(inv.prompts))
	}
	coderPrompt2 := inv.prompts[2]
	if !strings.Contains(coderPrompt2, "REFACTOR") {
		t.Errorf("cycle 2 coder prompt should contain REFACTOR, got %q", coderPrompt2)
	}
}

// ---------------------------------------------------------------------------
// TestGenerateCheckpoint
// ---------------------------------------------------------------------------

func TestGenerateCheckpoint(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "Progress: completed 50%", CostUSD: 0.10},
			},
		}
		l := &Loop{
			Invoker: inv,
			Model:   "claude-sonnet",
		}
		checkpoint, err := l.GenerateCheckpoint(context.Background(), "bead-1", "my task")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if checkpoint != "Progress: completed 50%" {
			t.Errorf("checkpoint = %q, want %q", checkpoint, "Progress: completed 50%")
		}
		if len(inv.prompts) != 1 {
			t.Fatalf("expected 1 prompt, got %d", len(inv.prompts))
		}
		if !strings.Contains(inv.prompts[0], "bead-1") {
			t.Error("prompt should contain bead ID")
		}
	})

	t.Run("Error", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{{}},
			errors:    []error{errors.New("invoke failed")},
		}
		l := &Loop{Invoker: inv}
		_, err := l.GenerateCheckpoint(context.Background(), "bead-1", "task")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
