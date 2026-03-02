package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/filter"
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
func (n *noopUI) FindingLifecycle(int, ui.FindingLifecycleData)     {}
func (n *noopUI) HailReceived(ui.HailInfo)                          {}
func (n *noopUI) HailResolved(string, string)                       {}

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

func TestCoderAgentWithProjectContext(t *testing.T) {
	t.Parallel()

	l := &Loop{
		CoderPrompt:    "You are a coder.",
		ProjectContext: "# Project: quasar",
	}
	a := l.coderAgent(1.0)
	if !strings.HasPrefix(a.SystemPrompt, "# Project: quasar") {
		t.Errorf("expected system prompt to start with project context, got:\n%s", a.SystemPrompt)
	}
	if !strings.Contains(a.SystemPrompt, "You are a coder.") {
		t.Error("expected base prompt to be present after project context")
	}
}

func TestReviewerAgentWithProjectContext(t *testing.T) {
	t.Parallel()

	l := &Loop{
		ReviewPrompt:   "You are a reviewer.",
		ProjectContext: "# Project: quasar",
	}
	a := l.reviewerAgent(1.0)
	if !strings.HasPrefix(a.SystemPrompt, "# Project: quasar") {
		t.Errorf("expected system prompt to start with project context, got:\n%s", a.SystemPrompt)
	}
	if !strings.Contains(a.SystemPrompt, "You are a reviewer.") {
		t.Error("expected base prompt to be present after project context")
	}
}

func TestReviewerAgentWithFabric(t *testing.T) {
	t.Parallel()

	l := &Loop{
		ReviewPrompt:  "You are a reviewer.",
		FabricEnabled: true,
	}
	a := l.reviewerAgent(1.0)
	if !strings.Contains(a.SystemPrompt, "## Fabric Protocol") {
		t.Error("expected fabric protocol in reviewer system prompt when FabricEnabled")
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
		if result == nil {
			t.Fatal("expected non-nil result on max cycles")
		}
		if result.CyclesUsed != 2 {
			t.Errorf("CyclesUsed = %d, want 2", result.CyclesUsed)
		}
		if rUI.maxCyclesCalls != 1 {
			t.Errorf("MaxCyclesReached calls = %d, want 1", rUI.maxCyclesCalls)
		}
	})

	t.Run("ApprovedPopulatesSHAs", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "implemented", CostUSD: 0.50},
				{ResultText: "APPROVED: LGTM.", CostUSD: 0.25},
			},
		}
		git := &fakeGit{
			headSHA:    "base-sha-abc",
			commitSHAs: []string{"cycle1-sha-def"},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &recordingUI{},
			Git:          git,
			MaxCycles:    3,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "task")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.BaseCommitSHA != "base-sha-abc" {
			t.Errorf("BaseCommitSHA = %q, want %q", result.BaseCommitSHA, "base-sha-abc")
		}
		if result.FinalCommitSHA != "cycle1-sha-def" {
			t.Errorf("FinalCommitSHA = %q, want %q", result.FinalCommitSHA, "cycle1-sha-def")
		}
	})

	t.Run("MaxCyclesPopulatesSHAs", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "attempt 1", CostUSD: 0.20},
				{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: broken.", CostUSD: 0.15},
			},
		}
		git := &fakeGit{
			headSHA:    "base-sha-111",
			commitSHAs: []string{"cycle1-sha-222"},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &recordingUI{},
			Git:          git,
			MaxCycles:    1,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "task")
		if !errors.Is(err, ErrMaxCycles) {
			t.Fatalf("expected ErrMaxCycles, got %v", err)
		}
		if result.BaseCommitSHA != "base-sha-111" {
			t.Errorf("BaseCommitSHA = %q, want %q", result.BaseCommitSHA, "base-sha-111")
		}
		if result.FinalCommitSHA != "cycle1-sha-222" {
			t.Errorf("FinalCommitSHA = %q, want %q", result.FinalCommitSHA, "cycle1-sha-222")
		}
	})

	t.Run("SHAsEmptyWhenNoGit", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "implemented", CostUSD: 0.50},
				{ResultText: "APPROVED: LGTM.", CostUSD: 0.25},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &recordingUI{},
			MaxCycles:    3,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "task")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.BaseCommitSHA != "" {
			t.Errorf("BaseCommitSHA = %q, want empty", result.BaseCommitSHA)
		}
		if result.FinalCommitSHA != "" {
			t.Errorf("FinalCommitSHA = %q, want empty", result.FinalCommitSHA)
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

// ---------------------------------------------------------------------------
// fakeFilter implements filter.Filter for testing.
// ---------------------------------------------------------------------------

type fakeFilter struct {
	result *filter.Result
	err    error
	calls  int
}

func (f *fakeFilter) Run(_ context.Context, _ string) (*filter.Result, error) {
	f.calls++
	return f.result, f.err
}

// ---------------------------------------------------------------------------
// fakeFilterWithRunCheck implements both filter.Filter and SingleCheckRunner
// for testing the inner filter fix loop.
// ---------------------------------------------------------------------------

type fakeFilterWithRunCheck struct {
	// results is a queue for Run calls.
	results []*filter.Result
	runErr  error
	runIdx  int

	// checkResults is a queue for RunCheck calls.
	checkResults []*filter.CheckResult
	checkErr     error
	checkIdx     int
}

func (f *fakeFilterWithRunCheck) Run(_ context.Context, _ string) (*filter.Result, error) {
	if f.runErr != nil {
		return nil, f.runErr
	}
	idx := f.runIdx
	f.runIdx++
	if idx < len(f.results) {
		return f.results[idx], nil
	}
	return &filter.Result{Passed: true}, nil
}

func (f *fakeFilterWithRunCheck) RunCheck(_ context.Context, _ string, _ string) (*filter.CheckResult, error) {
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	idx := f.checkIdx
	f.checkIdx++
	if idx < len(f.checkResults) {
		return f.checkResults[idx], nil
	}
	return &filter.CheckResult{Passed: true}, nil
}

func TestRunFilterChecks(t *testing.T) {
	t.Parallel()

	t.Run("FilterPasses", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		ff := &fakeFilter{
			result: &filter.Result{
				Passed: true,
				Checks: []filter.CheckResult{
					{Name: "build", Passed: true, Elapsed: 100 * time.Millisecond},
				},
			},
		}
		l := &Loop{UI: rUI, Filter: ff, WorkDir: "/tmp"}
		state := &CycleState{Cycle: 1}

		failed, err := l.runFilterChecks(context.Background(), state)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if failed {
			t.Error("expected filter to pass")
		}
		if state.FilterOutput != "" {
			t.Errorf("FilterOutput = %q, want empty", state.FilterOutput)
		}
	})

	t.Run("FilterFails", func(t *testing.T) {
		t.Parallel()
		rUI := &recordingUI{}
		ff := &fakeFilter{
			result: &filter.Result{
				Passed: false,
				Checks: []filter.CheckResult{
					{Name: "build", Passed: true},
					{Name: "vet", Passed: false, Output: "vet found issues", Elapsed: 50 * time.Millisecond},
				},
			},
		}
		l := &Loop{UI: rUI, Filter: ff, WorkDir: "/tmp"}
		state := &CycleState{Cycle: 1}

		failed, err := l.runFilterChecks(context.Background(), state)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !failed {
			t.Error("expected filter to fail")
		}
		if state.FilterCheckName != "vet" {
			t.Errorf("FilterCheckName = %q, want 'vet'", state.FilterCheckName)
		}
		if !strings.Contains(state.FilterOutput, "vet found issues") {
			t.Errorf("FilterOutput = %q, want to contain 'vet found issues'", state.FilterOutput)
		}
		if len(state.Findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(state.Findings))
		}
		if state.Findings[0].Severity != "critical" {
			t.Errorf("severity = %q, want 'critical'", state.Findings[0].Severity)
		}
		if !strings.Contains(state.Findings[0].Description, "[filter:vet]") {
			t.Errorf("description = %q, want to contain '[filter:vet]'", state.Findings[0].Description)
		}
	})

	t.Run("FilterInfraError", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilter{err: errors.New("context canceled")}
		l := &Loop{UI: &noopUI{}, Filter: ff, WorkDir: "/tmp"}
		state := &CycleState{Cycle: 1}

		_, err := l.runFilterChecks(context.Background(), state)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "filter execution failed") {
			t.Errorf("error = %q, want to contain 'filter execution failed'", err.Error())
		}
	})
}

func TestRunLoopWithFilter(t *testing.T) {
	t.Parallel()

	t.Run("NilFilterSkipsFiltering", func(t *testing.T) {
		t.Parallel()
		// Verify backward compatibility: nil Filter goes straight to reviewer.
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},
				{ResultText: "APPROVED: Good.", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			Filter:       nil, // explicitly nil
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

	t.Run("FilterPassesGoesToReviewer", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilter{
			result: &filter.Result{
				Passed: true,
				Checks: []filter.CheckResult{{Name: "build", Passed: true}},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},
				{ResultText: "APPROVED: Good.", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			Filter:       ff,
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
		if ff.calls != 1 {
			t.Errorf("filter calls = %d, want 1", ff.calls)
		}
	})

	t.Run("FilterFailsBouncesToCoder", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilter{
			result: &filter.Result{
				Passed: false,
				Checks: []filter.CheckResult{
					{Name: "build", Passed: false, Output: "does not compile", Elapsed: 200 * time.Millisecond},
				},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				// Cycle 1: coder
				{ResultText: "first attempt", CostUSD: 0.30},
				// Cycle 2: coder (bounced by filter, no reviewer in cycle 1)
				{ResultText: "fixed build", CostUSD: 0.30},
				// Cycle 2: reviewer (filter passes on 2nd call — but we reuse same fakeFilter)
				// Actually, fakeFilter always returns the same result. Let me handle this differently.
				// For this test, the filter always fails so we'll hit max cycles.
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &recordingUI{},
			Filter:       ff,
			MaxCycles:    2,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "fix build")
		if !errors.Is(err, ErrMaxCycles) {
			t.Errorf("expected ErrMaxCycles, got %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		// Filter always fails, so reviewer is never called.
		// 2 cycles × coder only = 2 invocations.
		if inv.calls != 2 {
			t.Errorf("invoker calls = %d, want 2 (coder only, no reviewer)", inv.calls)
		}
		if ff.calls != 2 {
			t.Errorf("filter calls = %d, want 2", ff.calls)
		}
	})

	t.Run("FilterFailsThenPassesSecondCycle", func(t *testing.T) {
		t.Parallel()
		// Filter that fails first time, passes second time.
		// Use a custom filter that fails first time, passes second time.
		var customFilter countingFilter
		customFilter.results = []*filter.Result{
			{
				Passed: false,
				Checks: []filter.CheckResult{
					{Name: "test", Passed: false, Output: "tests failed", Elapsed: 100 * time.Millisecond},
				},
			},
			{
				Passed: true,
				Checks: []filter.CheckResult{
					{Name: "build", Passed: true},
					{Name: "test", Passed: true},
				},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				// Cycle 1: coder
				{ResultText: "first attempt", CostUSD: 0.30},
				// Cycle 1: filter fails → no reviewer, bounce to cycle 2
				// Cycle 2: coder
				{ResultText: "fixed tests", CostUSD: 0.30},
				// Cycle 2: filter passes → reviewer
				{ResultText: "APPROVED: All good.", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &recordingUI{},
			Filter:       &customFilter,
			MaxCycles:    3,
			MaxBudgetUSD: 10.0,
		}
		result, err := l.runLoop(context.Background(), "bead-1", "fix tests")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.CyclesUsed != 2 {
			t.Errorf("CyclesUsed = %d, want 2", result.CyclesUsed)
		}
		// 3 invocations: coder, coder, reviewer (filter bounced first cycle)
		if inv.calls != 3 {
			t.Errorf("invoker calls = %d, want 3", inv.calls)
		}
	})
}

// countingFilter returns different results on successive calls.
type countingFilter struct {
	results []*filter.Result
	calls   int
}

func (f *countingFilter) Run(_ context.Context, _ string) (*filter.Result, error) {
	idx := f.calls
	f.calls++
	if idx < len(f.results) {
		return f.results[idx], nil
	}
	// Default: pass
	return &filter.Result{Passed: true}, nil
}

// ---------------------------------------------------------------------------
// Struggle detection integration tests
// ---------------------------------------------------------------------------

func TestRunLoopWithStruggleDetection(t *testing.T) {
	t.Parallel()

	t.Run("StruggleTriggersDecompose", func(t *testing.T) {
		t.Parallel()
		// Set up a loop that has high finding overlap to trigger struggle.
		// Cycle 1: coder + reviewer (rejected with findings)
		// Cycle 2: coder + reviewer (rejected with overlapping findings -> struggle triggers)
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				// Cycle 1: coder
				{ResultText: "attempt 1", CostUSD: 0.50},
				// Cycle 1: reviewer — rejected
				{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: missing error handling", CostUSD: 0.30},
				// Cycle 2: coder
				{ResultText: "attempt 2", CostUSD: 0.50},
				// Cycle 2: reviewer — rejected with same finding
				{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: missing error handling", CostUSD: 0.30},
			},
		}

		var events []EventKind
		hook := HookFunc(func(_ context.Context, e Event) {
			events = append(events, e.Kind)
		})

		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			Hooks:        []Hook{hook},
			MaxCycles:    5,
			MaxBudgetUSD: 10.0,
			StruggleConfig: StruggleConfig{
				Enabled:                 true,
				MinCyclesBeforeCheck:    2,
				FilterRepeatThreshold:   2,
				FindingOverlapThreshold: 0.3, // low threshold to trigger easily
				BudgetBurnThreshold:     0.3,
				CompositeThreshold:      0.05, // very low to ensure trigger
			},
		}

		result, err := l.runLoop(context.Background(), "bead-1", "implement feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Decompose {
			t.Fatal("expected Decompose=true when struggle is detected")
		}
		if result.StruggleReason == "" {
			t.Error("expected non-empty StruggleReason")
		}
		if len(result.AllFindings) == 0 {
			t.Error("expected AllFindings to be populated")
		}
		if result.CyclesUsed != 2 {
			t.Errorf("CyclesUsed = %d, want 2", result.CyclesUsed)
		}

		// Verify EventStruggleDetected was emitted.
		foundStruggle := false
		for _, e := range events {
			if e == EventStruggleDetected {
				foundStruggle = true
				break
			}
		}
		if !foundStruggle {
			t.Error("expected EventStruggleDetected event to be emitted")
		}
	})

	t.Run("DisabledStruggleNoDecompose", func(t *testing.T) {
		t.Parallel()
		// Even with overlapping findings, disabled config should not trigger.
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "attempt 1", CostUSD: 0.50},
				{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: missing error handling", CostUSD: 0.30},
				{ResultText: "attempt 2", CostUSD: 0.50},
				{ResultText: "APPROVED: Looks good.", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			MaxCycles:      5,
			MaxBudgetUSD:   10.0,
			StruggleConfig: DefaultStruggleConfig(), // Enabled: false
		}

		result, err := l.runLoop(context.Background(), "bead-1", "implement feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Decompose {
			t.Error("expected Decompose=false when struggle detection is disabled")
		}
	})

	t.Run("StruggleNotTriggeredBelowMinCycles", func(t *testing.T) {
		t.Parallel()
		// With MinCyclesBeforeCheck=3, struggle should not trigger on cycle 2.
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "attempt 1", CostUSD: 0.50},
				{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: missing error handling", CostUSD: 0.30},
				{ResultText: "attempt 2", CostUSD: 0.50},
				{ResultText: "APPROVED: OK", CostUSD: 0.20},
			},
		}
		l := &Loop{
			Invoker:      inv,
			UI:           &noopUI{},
			MaxCycles:    5,
			MaxBudgetUSD: 10.0,
			StruggleConfig: StruggleConfig{
				Enabled:                 true,
				MinCyclesBeforeCheck:    3, // Won't trigger until cycle 3
				FilterRepeatThreshold:   2,
				FindingOverlapThreshold: 0.3,
				BudgetBurnThreshold:     0.3,
				CompositeThreshold:      0.05,
			},
		}

		result, err := l.runLoop(context.Background(), "bead-1", "implement feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Decompose {
			t.Error("expected Decompose=false when below MinCyclesBeforeCheck")
		}
	})
}

// ---------------------------------------------------------------------------
// TestMaxFilterFixes
// ---------------------------------------------------------------------------

func TestMaxFilterFixes(t *testing.T) {
	t.Parallel()

	t.Run("DefaultWhenZero", func(t *testing.T) {
		t.Parallel()
		l := &Loop{MaxFilterFixes: 0}
		if got := l.maxFilterFixes(); got != DefaultMaxFilterFixes {
			t.Errorf("maxFilterFixes() = %d, want %d", got, DefaultMaxFilterFixes)
		}
	})

	t.Run("CustomValue", func(t *testing.T) {
		t.Parallel()
		l := &Loop{MaxFilterFixes: 5}
		if got := l.maxFilterFixes(); got != 5 {
			t.Errorf("maxFilterFixes() = %d, want 5", got)
		}
	})
}

// ---------------------------------------------------------------------------
// TestRunFilterFixLoop
// ---------------------------------------------------------------------------

func TestRunFilterFixLoop(t *testing.T) {
	t.Parallel()

	t.Run("ClaimsGuard", func(t *testing.T) {
		t.Parallel()
		// Claims failures should never be short-circuited.
		l := &Loop{
			UI:     &noopUI{},
			Filter: &fakeFilterWithRunCheck{},
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task"}
		fixed, err := l.runFilterFixLoop(context.Background(), state, "claims", "unclaimed files")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fixed {
			t.Error("claims failures must not be fixed by inner loop")
		}
	})

	t.Run("NilFilterFallsThrough", func(t *testing.T) {
		t.Parallel()
		// When Filter is nil, runFilterFixLoop can't run.
		l := &Loop{
			UI:     &noopUI{},
			Filter: nil,
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task"}
		fixed, err := l.runFilterFixLoop(context.Background(), state, "build", "error")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fixed {
			t.Error("expected false when Filter is nil")
		}
	})

	t.Run("FilterWithoutRunCheckFallsThrough", func(t *testing.T) {
		t.Parallel()
		// fakeFilter doesn't implement SingleCheckRunner.
		l := &Loop{
			UI:     &noopUI{},
			Filter: &fakeFilter{result: &filter.Result{Passed: true}},
		}
		state := &CycleState{TaskBeadID: "bead-1", TaskTitle: "task"}
		fixed, err := l.runFilterFixLoop(context.Background(), state, "build", "error")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fixed {
			t.Error("expected false when Filter doesn't implement SingleCheckRunner")
		}
	})

	t.Run("SuccessfulFix", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{
			checkResults: []*filter.CheckResult{
				{Name: "build", Passed: true}, // passes on first re-check
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "fixed the build error", CostUSD: 0.05},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			MaxFilterFixes: 3,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			TaskTitle:  "implement feature",
			Cycle:      1,
		}

		fixed, err := l.runFilterFixLoop(context.Background(), state, "build", "main.go:10:5: undefined: foo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !fixed {
			t.Error("expected filter fix to succeed")
		}
		if inv.calls != 1 {
			t.Errorf("expected 1 coder invocation, got %d", inv.calls)
		}
		if state.FilterFixAttempts != 1 {
			t.Errorf("FilterFixAttempts = %d, want 1", state.FilterFixAttempts)
		}
		if state.TotalCostUSD != 0.05 {
			t.Errorf("TotalCostUSD = %v, want 0.05", state.TotalCostUSD)
		}
		if state.FilterFixCostUSD != 0.05 {
			t.Errorf("FilterFixCostUSD = %v, want 0.05", state.FilterFixCostUSD)
		}
		// Verify the prompt was a filter fix prompt.
		if len(inv.prompts) < 1 || !strings.Contains(inv.prompts[0], "build") {
			t.Error("expected filter fix prompt to mention build check")
		}
		// Verify restricted tool set — no Bash tools.
		if len(inv.agents) < 1 {
			t.Fatal("expected at least 1 agent captured")
		}
		for _, tool := range inv.agents[0].AllowedTools {
			if strings.HasPrefix(tool, "Bash") {
				t.Errorf("filter fix agent should not have Bash tools, got %q", tool)
			}
		}
	})

	t.Run("ExhaustedRetries", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{
			checkResults: []*filter.CheckResult{
				{Name: "build", Passed: false, Output: "still broken 1"},
				{Name: "build", Passed: false, Output: "still broken 2"},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "attempt 1", CostUSD: 0.05},
				{ResultText: "attempt 2", CostUSD: 0.05},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			MaxFilterFixes: 2,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			TaskTitle:  "implement feature",
			Cycle:      1,
		}

		fixed, err := l.runFilterFixLoop(context.Background(), state, "build", "main.go:10:5: undefined: foo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fixed {
			t.Error("expected fix to fail after exhausting retries")
		}
		if inv.calls != 2 {
			t.Errorf("expected 2 coder invocations, got %d", inv.calls)
		}
		if state.FilterFixAttempts != 2 {
			t.Errorf("FilterFixAttempts = %d, want 2", state.FilterFixAttempts)
		}
		if state.TotalCostUSD != 0.10 {
			t.Errorf("TotalCostUSD = %v, want 0.10", state.TotalCostUSD)
		}
	})

	t.Run("BudgetExceeded", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{
			checkResults: []*filter.CheckResult{
				{Name: "build", Passed: false, Output: "still broken"},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "expensive fix", CostUSD: 10.0},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &recordingUI{},
			Filter:         ff,
			MaxFilterFixes: 3,
			MaxBudgetUSD:   5.0,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			TaskTitle:  "implement feature",
			Cycle:      1,
		}

		_, err := l.runFilterFixLoop(context.Background(), state, "build", "error output")
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Errorf("expected ErrBudgetExceeded, got %v", err)
		}
	})

	t.Run("CoderInvokeError", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{{}},
			errors:    []error{errors.New("coder crashed")},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			MaxFilterFixes: 3,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			TaskTitle:  "task",
			Cycle:      1,
		}

		_, err := l.runFilterFixLoop(context.Background(), state, "build", "error output")
		if err == nil {
			t.Fatal("expected error from coder invocation")
		}
		if !strings.Contains(err.Error(), "coder filter-fix invocation failed") {
			t.Errorf("error = %q, want to contain 'coder filter-fix invocation failed'", err.Error())
		}
	})

	t.Run("WithGitCommit", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{
			checkResults: []*filter.CheckResult{
				{Name: "vet", Passed: true},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "fixed vet", CostUSD: 0.03},
			},
		}
		git := &fakeGit{commitSHAs: []string{"filter-fix-sha"}}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			Git:            git,
			MaxFilterFixes: 3,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			TaskTitle:  "task",
			Cycle:      1,
		}

		fixed, err := l.runFilterFixLoop(context.Background(), state, "vet", "vet error")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !fixed {
			t.Error("expected fix to succeed")
		}
		if state.lastCycleSHA != "filter-fix-sha" {
			t.Errorf("lastCycleSHA = %q, want 'filter-fix-sha'", state.lastCycleSHA)
		}
	})

	t.Run("FixOnSecondAttempt", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{
			checkResults: []*filter.CheckResult{
				{Name: "test", Passed: false, Output: "still failing"},
				{Name: "test", Passed: true}, // passes on second re-check
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "attempt 1", CostUSD: 0.03},
				{ResultText: "attempt 2", CostUSD: 0.04},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			MaxFilterFixes: 3,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			TaskTitle:  "task",
			Cycle:      1,
		}

		fixed, err := l.runFilterFixLoop(context.Background(), state, "test", "test failure output")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !fixed {
			t.Error("expected fix to succeed on second attempt")
		}
		if inv.calls != 2 {
			t.Errorf("expected 2 coder invocations, got %d", inv.calls)
		}
		if state.FilterFixAttempts != 2 {
			t.Errorf("FilterFixAttempts = %d, want 2", state.FilterFixAttempts)
		}
		if state.TotalCostUSD != 0.07 {
			t.Errorf("TotalCostUSD = %v, want 0.07", state.TotalCostUSD)
		}
	})

	t.Run("RunCheckError", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{
			checkErr: errors.New("check infrastructure error"),
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "tried to fix", CostUSD: 0.02},
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			MaxFilterFixes: 3,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-1",
			TaskTitle:  "task",
			Cycle:      1,
		}

		_, err := l.runFilterFixLoop(context.Background(), state, "build", "error output")
		if err == nil {
			t.Fatal("expected error from RunCheck")
		}
		if !strings.Contains(err.Error(), "re-running check") {
			t.Errorf("error = %q, want to contain 're-running check'", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// TestRunLoopWithFilterFixLoop
// ---------------------------------------------------------------------------

func TestRunLoopWithFilterFixLoop(t *testing.T) {
	t.Parallel()

	t.Run("FilterFixSucceedsProceedsToReviewer", func(t *testing.T) {
		t.Parallel()
		// Filter fails initially, inner fix loop fixes it, then proceeds to reviewer.
		ff := &fakeFilterWithRunCheck{
			results: []*filter.Result{
				{
					Passed: false,
					Checks: []filter.CheckResult{
						{Name: "build", Passed: false, Output: "main.go:5:1: undefined: x"},
					},
				},
			},
			checkResults: []*filter.CheckResult{
				{Name: "build", Passed: true}, // passes after fix
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},                 // coder
				{ResultText: "fixed build error", CostUSD: 0.05},     // filter fix coder
				{ResultText: "APPROVED: Looks good.", CostUSD: 0.20}, // reviewer
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			MaxFilterFixes: 3,
			MaxCycles:      3,
			MaxBudgetUSD:   10.0,
			WorkDir:        "/tmp",
		}
		result, err := l.runLoop(context.Background(), "bead-1", "implement feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.CyclesUsed != 1 {
			t.Errorf("CyclesUsed = %d, want 1 (fixed in inner loop, no outer bounce)", result.CyclesUsed)
		}
		// 3 invocations: coder + filter-fix coder + reviewer.
		if inv.calls != 3 {
			t.Errorf("invoker calls = %d, want 3", inv.calls)
		}
	})

	t.Run("FilterFixExhaustedBouncesToOuterCycle", func(t *testing.T) {
		t.Parallel()
		// Filter always fails, inner fix loop exhausted, bounces to outer cycle.
		ff := &fakeFilterWithRunCheck{
			results: []*filter.Result{
				// Cycle 1: filter fails
				{Passed: false, Checks: []filter.CheckResult{
					{Name: "build", Passed: false, Output: "error"},
				}},
				// Cycle 2: filter fails again
				{Passed: false, Checks: []filter.CheckResult{
					{Name: "build", Passed: false, Output: "error"},
				}},
			},
			checkResults: []*filter.CheckResult{
				// All re-checks fail (MaxFilterFixes=1 per cycle, 2 cycles)
				{Name: "build", Passed: false, Output: "still broken"},
				{Name: "build", Passed: false, Output: "still broken"},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded cycle 1", CostUSD: 0.30},      // coder cycle 1
				{ResultText: "filter fix attempt", CostUSD: 0.05}, // filter fix cycle 1
				{ResultText: "coded cycle 2", CostUSD: 0.30},      // coder cycle 2
				{ResultText: "filter fix attempt", CostUSD: 0.05}, // filter fix cycle 2
			},
		}
		l := &Loop{
			Invoker:        inv,
			UI:             &recordingUI{},
			Filter:         ff,
			MaxFilterFixes: 1,
			MaxCycles:      2,
			MaxBudgetUSD:   10.0,
			WorkDir:        "/tmp",
		}
		result, err := l.runLoop(context.Background(), "bead-1", "implement feature")
		if !errors.Is(err, ErrMaxCycles) {
			t.Errorf("expected ErrMaxCycles, got %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		// 4 invocations: (coder + filter-fix) × 2 cycles.
		if inv.calls != 4 {
			t.Errorf("invoker calls = %d, want 4", inv.calls)
		}
	})
}

// ---------------------------------------------------------------------------
// recordingHook captures lifecycle events for test assertions.
// ---------------------------------------------------------------------------

type recordingHook struct {
	mu     sync.Mutex
	events []Event
}

func (h *recordingHook) OnEvent(_ context.Context, event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func (h *recordingHook) getEvents() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	dst := make([]Event, len(h.events))
	copy(dst, h.events)
	return dst
}

// ---------------------------------------------------------------------------
// TestFilterFixLoopEmitsEvents
// ---------------------------------------------------------------------------

func TestFilterFixLoopEmitsEvents(t *testing.T) {
	t.Parallel()

	t.Run("SuccessEmitsAttemptAndResult", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{
			checkResults: []*filter.CheckResult{
				{Name: "build", Passed: true},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "fixed it", CostUSD: 0.05, DurationMs: 1200},
			},
		}
		hook := &recordingHook{}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			Hooks:          []Hook{hook},
			MaxFilterFixes: 3,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-ev",
			TaskTitle:  "task",
			Cycle:      2,
		}

		fixed, err := l.runFilterFixLoop(context.Background(), state, "build", "main.go:1: error")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !fixed {
			t.Fatal("expected fix to succeed")
		}

		events := hook.getEvents()
		// Expect exactly 1 attempt event + 1 result event.
		var attempts, results []Event
		for _, e := range events {
			switch e.Kind {
			case EventFilterFixAttempt:
				attempts = append(attempts, e)
			case EventFilterFixResult:
				results = append(results, e)
			}
		}

		if len(attempts) != 1 {
			t.Fatalf("expected 1 EventFilterFixAttempt, got %d", len(attempts))
		}
		a := attempts[0]
		if a.BeadID != "bead-ev" {
			t.Errorf("attempt BeadID = %q, want %q", a.BeadID, "bead-ev")
		}
		if a.Cycle != 2 {
			t.Errorf("attempt Cycle = %d, want 2", a.Cycle)
		}
		if a.FilterFix == nil {
			t.Fatal("attempt FilterFix is nil")
		}
		if a.FilterFix.CheckName != "build" {
			t.Errorf("attempt CheckName = %q, want %q", a.FilterFix.CheckName, "build")
		}
		if a.FilterFix.Attempt != 1 {
			t.Errorf("attempt Attempt = %d, want 1", a.FilterFix.Attempt)
		}
		if a.FilterFix.MaxAttempts != 3 {
			t.Errorf("attempt MaxAttempts = %d, want 3", a.FilterFix.MaxAttempts)
		}
		if !a.FilterFix.Fixed {
			t.Error("attempt Fixed = false, want true (check passed)")
		}
		if a.FilterFix.CostUSD != 0.05 {
			t.Errorf("attempt CostUSD = %v, want 0.05", a.FilterFix.CostUSD)
		}
		if a.FilterFix.DurationMs != 1200 {
			t.Errorf("attempt DurationMs = %d, want 1200", a.FilterFix.DurationMs)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 EventFilterFixResult, got %d", len(results))
		}
		r := results[0]
		if r.FilterFix == nil {
			t.Fatal("result FilterFix is nil")
		}
		if !r.FilterFix.Fixed {
			t.Error("result Fixed = false, want true")
		}
		if r.FilterFix.Attempt != 1 {
			t.Errorf("result Attempt = %d, want 1 (total attempts)", r.FilterFix.Attempt)
		}
		if r.FilterFix.CostUSD != 0.05 {
			t.Errorf("result CostUSD = %v, want 0.05 (cumulative)", r.FilterFix.CostUSD)
		}
	})

	t.Run("ExhaustedEmitsMultipleAttemptsAndFailedResult", func(t *testing.T) {
		t.Parallel()
		ff := &fakeFilterWithRunCheck{
			checkResults: []*filter.CheckResult{
				{Name: "vet", Passed: false, Output: "still broken 1"},
				{Name: "vet", Passed: false, Output: "still broken 2"},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "attempt 1", CostUSD: 0.03, DurationMs: 500},
				{ResultText: "attempt 2", CostUSD: 0.04, DurationMs: 600},
			},
		}
		hook := &recordingHook{}
		l := &Loop{
			Invoker:        inv,
			UI:             &noopUI{},
			Filter:         ff,
			Hooks:          []Hook{hook},
			MaxFilterFixes: 2,
			MaxCycles:      3,
			WorkDir:        "/tmp",
		}
		state := &CycleState{
			TaskBeadID: "bead-ex",
			TaskTitle:  "task",
			Cycle:      1,
		}

		fixed, err := l.runFilterFixLoop(context.Background(), state, "vet", "vet error output")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fixed {
			t.Fatal("expected fix to fail")
		}

		events := hook.getEvents()
		var attempts, results []Event
		for _, e := range events {
			switch e.Kind {
			case EventFilterFixAttempt:
				attempts = append(attempts, e)
			case EventFilterFixResult:
				results = append(results, e)
			}
		}

		if len(attempts) != 2 {
			t.Fatalf("expected 2 EventFilterFixAttempt, got %d", len(attempts))
		}
		// Verify attempt ordering.
		if attempts[0].FilterFix.Attempt != 1 {
			t.Errorf("first attempt Attempt = %d, want 1", attempts[0].FilterFix.Attempt)
		}
		if attempts[1].FilterFix.Attempt != 2 {
			t.Errorf("second attempt Attempt = %d, want 2", attempts[1].FilterFix.Attempt)
		}
		// Both should be not fixed.
		if attempts[0].FilterFix.Fixed || attempts[1].FilterFix.Fixed {
			t.Error("individual attempt Fixed should be false when check still fails")
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 EventFilterFixResult, got %d", len(results))
		}
		r := results[0]
		if r.FilterFix.Fixed {
			t.Error("result Fixed = true, want false (exhausted)")
		}
		if r.FilterFix.Attempt != 2 {
			t.Errorf("result Attempt = %d, want 2", r.FilterFix.Attempt)
		}
		if r.FilterFix.CostUSD != 0.07 {
			t.Errorf("result CostUSD = %v, want 0.07", r.FilterFix.CostUSD)
		}
	})
}

// ---------------------------------------------------------------------------
// TestBeadHookFilterFixResult
// ---------------------------------------------------------------------------

func TestBeadHookFilterFixResult(t *testing.T) {
	t.Parallel()

	t.Run("FixedComment", func(t *testing.T) {
		t.Parallel()
		rb := newRecordingBeads()
		hook := newBeadHook(rb, &noopUI{})

		hook.OnEvent(context.Background(), Event{
			Kind:   EventFilterFixResult,
			BeadID: "bead-ffr",
			Cycle:  1,
			FilterFix: &FilterFixData{
				CheckName:   "build",
				Attempt:     2,
				MaxAttempts: 3,
				Fixed:       true,
				CostUSD:     0.08,
			},
		})

		if len(rb.comments) != 1 {
			t.Fatalf("expected 1 comment, got %d", len(rb.comments))
		}
		c := rb.comments[0]
		if !strings.Contains(c, "build") {
			t.Errorf("comment should mention check name, got %q", c)
		}
		if !strings.Contains(c, "fixed") {
			t.Errorf("comment should mention fixed status, got %q", c)
		}
		if !strings.Contains(c, "2 attempt") {
			t.Errorf("comment should mention attempt count, got %q", c)
		}
		if !strings.Contains(c, "0.0800") {
			t.Errorf("comment should mention cost, got %q", c)
		}
	})

	t.Run("NotFixedComment", func(t *testing.T) {
		t.Parallel()
		rb := newRecordingBeads()
		hook := newBeadHook(rb, &noopUI{})

		hook.OnEvent(context.Background(), Event{
			Kind:   EventFilterFixResult,
			BeadID: "bead-ffr2",
			Cycle:  3,
			FilterFix: &FilterFixData{
				CheckName:   "test",
				Attempt:     3,
				MaxAttempts: 3,
				Fixed:       false,
				CostUSD:     0.12,
			},
		})

		if len(rb.comments) != 1 {
			t.Fatalf("expected 1 comment, got %d", len(rb.comments))
		}
		c := rb.comments[0]
		if !strings.Contains(c, "not fixed") {
			t.Errorf("comment should mention not fixed status, got %q", c)
		}
	})

	t.Run("NilFilterFixNoComment", func(t *testing.T) {
		t.Parallel()
		rb := newRecordingBeads()
		hook := newBeadHook(rb, &noopUI{})

		hook.OnEvent(context.Background(), Event{
			Kind:   EventFilterFixResult,
			BeadID: "bead-nil",
			Cycle:  1,
			// FilterFix is nil.
		})

		if len(rb.comments) != 0 {
			t.Errorf("expected 0 comments when FilterFix is nil, got %d", len(rb.comments))
		}
	})
}

// ---------------------------------------------------------------------------
// TestCycleSummaryFilterFixTracking
// ---------------------------------------------------------------------------

func TestCycleSummaryFilterFixTracking(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulFilterFixVisibleInSummary", func(t *testing.T) {
		t.Parallel()
		// Verify that after a successful filter fix, the cycle summary
		// shows the correct filter fix info (Issue #1: previously invisible).
		ff := &fakeFilterWithRunCheck{
			results: []*filter.Result{
				{
					Passed: false,
					Checks: []filter.CheckResult{
						{Name: "build", Passed: false, Output: "main.go:5:1: undefined: x"},
					},
				},
			},
			checkResults: []*filter.CheckResult{
				{Name: "build", Passed: true},
			},
		}
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coded", CostUSD: 0.30},
				{ResultText: "fixed build error", CostUSD: 0.05},
				{ResultText: "APPROVED: Looks good.", CostUSD: 0.20},
			},
		}
		rUI := &recordingUI{}
		l := &Loop{
			Invoker:        inv,
			UI:             rUI,
			Filter:         ff,
			MaxFilterFixes: 3,
			MaxCycles:      3,
			MaxBudgetUSD:   10.0,
			WorkDir:        "/tmp",
		}
		_, err := l.runLoop(context.Background(), "bead-cs", "implement feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Find the reviewer's cycle summary (Phase == "review_complete").
		var reviewSummary *ui.CycleSummaryData
		for i := range rUI.cycleSummaries {
			if rUI.cycleSummaries[i].Phase == "review_complete" {
				reviewSummary = &rUI.cycleSummaries[i]
				break
			}
		}
		if reviewSummary == nil {
			t.Fatal("expected a review_complete cycle summary")
		}
		if reviewSummary.FilterFixAttempts != 1 {
			t.Errorf("FilterFixAttempts = %d, want 1", reviewSummary.FilterFixAttempts)
		}
		if reviewSummary.FilterFixCostUSD != 0.05 {
			t.Errorf("FilterFixCostUSD = %v, want 0.05", reviewSummary.FilterFixCostUSD)
		}
		if !reviewSummary.FilterFixSuccess {
			t.Error("FilterFixSuccess = false, want true")
		}
	})
}

// ---------------------------------------------------------------------------
// Cross-invocation system prompt caching tests.
// ---------------------------------------------------------------------------

func TestCacheSystemPrompts(t *testing.T) {
	t.Parallel()

	t.Run("coder and reviewer prompts are populated", func(t *testing.T) {
		t.Parallel()
		l := &Loop{
			CoderPrompt:  "You are a coder.",
			ReviewPrompt: "You are a reviewer.",
		}
		l.cacheSystemPrompts()
		if l.cachedCoderSystemPrompt == "" {
			t.Error("cachedCoderSystemPrompt is empty after cacheSystemPrompts")
		}
		if l.cachedReviewerSystemPrompt == "" {
			t.Error("cachedReviewerSystemPrompt is empty after cacheSystemPrompts")
		}
	})

	t.Run("matches direct BuildSystemPrompt output", func(t *testing.T) {
		t.Parallel()
		l := &Loop{
			CoderPrompt:    "You are a coder.",
			ReviewPrompt:   "You are a reviewer.",
			FabricEnabled:  true,
			TaskID:         "phase-42",
			ProjectContext: "# Project: quasar\nLanguage: Go",
		}
		l.cacheSystemPrompts()

		opts := agent.PromptOpts{
			FabricEnabled:  l.FabricEnabled,
			TaskID:         l.TaskID,
			ProjectContext: l.ProjectContext,
		}
		wantCoder := agent.BuildSystemPrompt(l.CoderPrompt, opts)
		wantReviewer := agent.BuildSystemPrompt(l.ReviewPrompt, opts)

		if l.cachedCoderSystemPrompt != wantCoder {
			t.Errorf("cached coder prompt differs from direct BuildSystemPrompt\ncached len=%d, direct len=%d",
				len(l.cachedCoderSystemPrompt), len(wantCoder))
		}
		if l.cachedReviewerSystemPrompt != wantReviewer {
			t.Errorf("cached reviewer prompt differs from direct BuildSystemPrompt\ncached len=%d, direct len=%d",
				len(l.cachedReviewerSystemPrompt), len(wantReviewer))
		}
	})

	t.Run("idempotent across multiple calls", func(t *testing.T) {
		t.Parallel()
		l := &Loop{
			CoderPrompt:    "You are a coder.",
			ReviewPrompt:   "You are a reviewer.",
			FabricEnabled:  true,
			ProjectContext: "# Project context",
		}
		l.cacheSystemPrompts()
		first := l.cachedCoderSystemPrompt

		l.cacheSystemPrompts()
		second := l.cachedCoderSystemPrompt

		if first != second {
			t.Errorf("cacheSystemPrompts is not idempotent: first len=%d, second len=%d",
				len(first), len(second))
		}
	})
}

func TestCoderAgentUsesCachedPrompt(t *testing.T) {
	t.Parallel()

	l := &Loop{
		Model:          "claude-sonnet",
		CoderPrompt:    "You are a coder.",
		ProjectContext: "# Project: quasar",
		FabricEnabled:  true,
	}
	l.cacheSystemPrompts()

	a1 := l.coderAgent(2.0)
	a2 := l.coderAgent(3.0)

	if a1.SystemPrompt != l.cachedCoderSystemPrompt {
		t.Error("coderAgent did not use cached system prompt")
	}
	if a1.SystemPrompt != a2.SystemPrompt {
		t.Error("coderAgent returned different system prompts on successive calls")
	}
}

func TestReviewerAgentUsesCachedPrompt(t *testing.T) {
	t.Parallel()

	l := &Loop{
		Model:          "claude-opus",
		ReviewPrompt:   "You are a reviewer.",
		ProjectContext: "# Project: quasar",
		FabricEnabled:  true,
	}
	l.cacheSystemPrompts()

	a1 := l.reviewerAgent(1.0)
	a2 := l.reviewerAgent(2.0)

	if a1.SystemPrompt != l.cachedReviewerSystemPrompt {
		t.Error("reviewerAgent did not use cached system prompt")
	}
	if a1.SystemPrompt != a2.SystemPrompt {
		t.Error("reviewerAgent returned different system prompts on successive calls")
	}
}

func TestCachedSystemPromptStableAcrossCycles(t *testing.T) {
	t.Parallel()

	// Simulate a multi-cycle run: the system prompt should be identical
	// on cycle 1 and cycle N because it was cached once at phase start.
	inv := &fakeInvoker{
		responses: []agent.InvocationResult{
			// Cycle 1: coder
			{ResultText: "first attempt", CostUSD: 0.50},
			// Cycle 1: reviewer — rejected
			{ResultText: "ISSUE:\nSEVERITY: major\nDESCRIPTION: Missing error handling.", CostUSD: 0.30},
			// Cycle 2: coder
			{ResultText: "fixed error handling", CostUSD: 0.40},
			// Cycle 2: reviewer — approved
			{ResultText: "APPROVED: Looks great now.", CostUSD: 0.20},
		},
	}

	l := &Loop{
		Invoker:           inv,
		UI:                &noopUI{},
		MaxCycles:         3,
		MaxBudgetUSD:      10.0,
		CoderPrompt:       "You are a coder.",
		ReviewPrompt:      "You are a reviewer.",
		ProjectContext:    "# Project: quasar\nLanguage: Go",
		FabricEnabled:     true,
		CacheOptimization: true,
	}

	result, err := l.runLoop(context.Background(), "bead-1", "implement feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CyclesUsed != 2 {
		t.Fatalf("CyclesUsed = %d, want 2", result.CyclesUsed)
	}

	// All invocations should have received the same cached system prompt.
	// The fakeInvoker records agents, so verify they all have the expected
	// system prompt from the cache.
	if l.cachedCoderSystemPrompt == "" {
		t.Fatal("cachedCoderSystemPrompt is empty after runLoop")
	}
	if l.cachedReviewerSystemPrompt == "" {
		t.Fatal("cachedReviewerSystemPrompt is empty after runLoop")
	}

	// Verify the prompts start with project context.
	if !strings.HasPrefix(l.cachedCoderSystemPrompt, "# Project: quasar") {
		t.Error("coder system prompt does not start with project context")
	}
	if !strings.HasPrefix(l.cachedReviewerSystemPrompt, "# Project: quasar") {
		t.Error("reviewer system prompt does not start with project context")
	}
}

func TestCrossPhaseSharedPrefix(t *testing.T) {
	t.Parallel()

	projectCtx := "# Project: quasar\nLanguage: Go\nVersion: 1.25"

	loopA := &Loop{
		CoderPrompt:    "You are coder A.",
		ReviewPrompt:   "You are reviewer A.",
		ProjectContext: projectCtx,
		FabricEnabled:  true,
	}
	loopA.cacheSystemPrompts()

	loopB := &Loop{
		CoderPrompt:    "You are coder B.",
		ReviewPrompt:   "You are reviewer B.",
		ProjectContext: projectCtx,
		FabricEnabled:  true,
	}
	loopB.cacheSystemPrompts()

	// Both coder prompts should share the ProjectContext prefix.
	// The shared prefix length should be at least len(projectCtx) + separator.
	separator := "\n\n---\n\n"
	minShared := len(projectCtx) + len(separator)

	if len(loopA.cachedCoderSystemPrompt) < minShared {
		t.Fatalf("coder A prompt too short: len=%d, need at least %d",
			len(loopA.cachedCoderSystemPrompt), minShared)
	}
	if len(loopB.cachedCoderSystemPrompt) < minShared {
		t.Fatalf("coder B prompt too short: len=%d, need at least %d",
			len(loopB.cachedCoderSystemPrompt), minShared)
	}

	prefixA := loopA.cachedCoderSystemPrompt[:minShared]
	prefixB := loopB.cachedCoderSystemPrompt[:minShared]

	if prefixA != prefixB {
		t.Errorf("cross-phase coder prompts do not share ProjectContext prefix:\nA: %q\nB: %q",
			prefixA, prefixB)
	}

	// Cross-role: coder and reviewer within the same phase also share the
	// ProjectContext prefix.
	prefixCoder := loopA.cachedCoderSystemPrompt[:minShared]
	prefixReviewer := loopA.cachedReviewerSystemPrompt[:minShared]

	if prefixCoder != prefixReviewer {
		t.Errorf("coder and reviewer do not share ProjectContext prefix:\ncoder: %q\nreviewer: %q",
			prefixCoder, prefixReviewer)
	}

	// The prompts should diverge after the shared prefix (different base prompts).
	if loopA.cachedCoderSystemPrompt == loopB.cachedCoderSystemPrompt {
		t.Error("different CoderPrompt values produced identical system prompts")
	}
}

func TestCoderAgentFallbackWithoutCache(t *testing.T) {
	t.Parallel()

	// When cacheSystemPrompts has NOT been called, coderAgent should
	// fall back to building the prompt on the fly.
	l := &Loop{
		Model:       "claude-sonnet",
		CoderPrompt: "You are a coder.",
	}
	a := l.coderAgent(1.0)
	if a.SystemPrompt != "You are a coder." {
		t.Errorf("SystemPrompt = %q, want %q", a.SystemPrompt, "You are a coder.")
	}
}

func TestReviewerAgentFallbackWithoutCache(t *testing.T) {
	t.Parallel()

	// When cacheSystemPrompts has NOT been called, reviewerAgent should
	// fall back to building the prompt on the fly.
	l := &Loop{
		Model:        "claude-opus",
		ReviewPrompt: "You are a reviewer.",
	}
	a := l.reviewerAgent(1.0)
	if a.SystemPrompt != "You are a reviewer." {
		t.Errorf("SystemPrompt = %q, want %q", a.SystemPrompt, "You are a reviewer.")
	}
}

func TestFilterFixLoopUsesCachedPrompt(t *testing.T) {
	t.Parallel()

	// When cacheSystemPrompts has been called, runFilterFixLoop should
	// use the cached coder system prompt rather than rebuilding it.
	ff := &fakeFilterWithRunCheck{
		checkResults: []*filter.CheckResult{
			{Name: "build", Passed: true},
		},
	}
	inv := &fakeInvoker{
		responses: []agent.InvocationResult{
			{ResultText: "fixed the error", CostUSD: 0.03},
		},
	}
	l := &Loop{
		Invoker:        inv,
		UI:             &noopUI{},
		Filter:         ff,
		MaxFilterFixes: 3,
		MaxCycles:      3,
		WorkDir:        "/tmp",
		CoderPrompt:    "You are a coder.",
		ProjectContext: "# Project: quasar",
		FabricEnabled:  true,
	}
	l.cacheSystemPrompts()

	state := &CycleState{
		TaskBeadID: "bead-1",
		TaskTitle:  "implement feature",
		Cycle:      1,
	}

	fixed, err := l.runFilterFixLoop(context.Background(), state, "build", "main.go:10:5: undefined: foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fixed {
		t.Error("expected filter fix to succeed")
	}

	// The agent passed to Invoke must carry the cached system prompt.
	if len(inv.agents) < 1 {
		t.Fatal("expected at least one agent invocation")
	}
	got := inv.agents[0].SystemPrompt
	if got != l.cachedCoderSystemPrompt {
		t.Errorf("runFilterFixLoop did not use cached system prompt\ngot len=%d, cached len=%d",
			len(got), len(l.cachedCoderSystemPrompt))
	}
}

func TestFilterFixLoopFallbackWithoutCache(t *testing.T) {
	t.Parallel()

	// When cacheSystemPrompts has NOT been called, runFilterFixLoop should
	// fall back to building the prompt on the fly (backward compatibility).
	ff := &fakeFilterWithRunCheck{
		checkResults: []*filter.CheckResult{
			{Name: "build", Passed: true},
		},
	}
	inv := &fakeInvoker{
		responses: []agent.InvocationResult{
			{ResultText: "fixed the error", CostUSD: 0.03},
		},
	}
	l := &Loop{
		Invoker:        inv,
		UI:             &noopUI{},
		Filter:         ff,
		MaxFilterFixes: 3,
		MaxCycles:      3,
		WorkDir:        "/tmp",
		CoderPrompt:    "You are a coder.",
	}
	// Deliberately do NOT call l.cacheSystemPrompts().

	state := &CycleState{
		TaskBeadID: "bead-1",
		TaskTitle:  "implement feature",
		Cycle:      1,
	}

	fixed, err := l.runFilterFixLoop(context.Background(), state, "build", "main.go:5: error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fixed {
		t.Error("expected filter fix to succeed")
	}

	// Without caching, the prompt should still be built correctly.
	if len(inv.agents) < 1 {
		t.Fatal("expected at least one agent invocation")
	}
	got := inv.agents[0].SystemPrompt
	if got != "You are a coder." {
		t.Errorf("SystemPrompt = %q, want %q", got, "You are a coder.")
	}
}

// ---------------------------------------------------------------------------
// Cache telemetry tests
// ---------------------------------------------------------------------------

func TestTrackCacheMetrics_EmitsEvent(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}
	l := &Loop{
		Invoker:   &fakeInvoker{},
		UI:        &noopUI{},
		MaxCycles: 3,
		Hooks:     []Hook{hook},
	}
	state := &CycleState{TaskBeadID: "bead-1", Cycle: 1}
	result := &agent.InvocationResult{
		SystemPromptLen:  100,
		UserPromptLen:    200,
		SystemPromptHash: "abc123",
		CostUSD:          0.50,
	}

	l.trackCacheMetrics(context.Background(), state, "coder", result)

	events := hook.getEvents()
	found := false
	for _, e := range events {
		if e.Kind == EventCacheMetrics {
			found = true
			if e.Agent != "coder" {
				t.Errorf("Agent = %q, want %q", e.Agent, "coder")
			}
			if e.BeadID != "bead-1" {
				t.Errorf("BeadID = %q, want %q", e.BeadID, "bead-1")
			}
			if e.Cycle != 1 {
				t.Errorf("Cycle = %d, want 1", e.Cycle)
			}
			if e.Result == nil {
				t.Error("Result should not be nil")
			}
			if !strings.Contains(e.Message, "sys_prompt_hash=abc123") {
				t.Errorf("Message = %q, want to contain 'sys_prompt_hash=abc123'", e.Message)
			}
		}
	}
	if !found {
		t.Error("expected EventCacheMetrics event to be emitted")
	}
}

func TestTrackCacheMetrics_FirstInvocationIsMiss(t *testing.T) {
	t.Parallel()
	l := &Loop{Invoker: &fakeInvoker{}, UI: &noopUI{}, MaxCycles: 3}
	state := &CycleState{TaskBeadID: "bead-1", Cycle: 1}
	result := &agent.InvocationResult{
		SystemPromptHash: "hash-a",
		SystemPromptLen:  50,
	}

	l.trackCacheMetrics(context.Background(), state, "coder", result)

	if state.cacheHitCount != 0 {
		t.Errorf("cacheHitCount = %d, want 0 (first invocation is a miss)", state.cacheHitCount)
	}
	if state.cacheMissCount != 1 {
		t.Errorf("cacheMissCount = %d, want 1", state.cacheMissCount)
	}
	if state.prevSystemPromptHash != "hash-a" {
		t.Errorf("prevSystemPromptHash = %q, want %q", state.prevSystemPromptHash, "hash-a")
	}
}

func TestTrackCacheMetrics_CacheHitOnStablePrompt(t *testing.T) {
	t.Parallel()
	l := &Loop{Invoker: &fakeInvoker{}, UI: &noopUI{}, MaxCycles: 3}
	state := &CycleState{TaskBeadID: "bead-1", Cycle: 1}
	result := &agent.InvocationResult{
		SystemPromptHash: "hash-stable",
		SystemPromptLen:  100,
	}

	// First invocation — miss.
	l.trackCacheMetrics(context.Background(), state, "coder", result)

	// Second invocation with same hash — hit.
	state.Cycle = 2
	l.trackCacheMetrics(context.Background(), state, "coder", result)

	if state.cacheHitCount != 1 {
		t.Errorf("cacheHitCount = %d, want 1", state.cacheHitCount)
	}
	if state.cacheMissCount != 1 {
		t.Errorf("cacheMissCount = %d, want 1", state.cacheMissCount)
	}
	if state.totalCachedBytes != 100 {
		t.Errorf("totalCachedBytes = %d, want 100", state.totalCachedBytes)
	}
}

func TestTrackCacheMetrics_CacheMissOnChangedPrompt(t *testing.T) {
	t.Parallel()
	l := &Loop{Invoker: &fakeInvoker{}, UI: &noopUI{}, MaxCycles: 3}
	state := &CycleState{TaskBeadID: "bead-1", Cycle: 1}

	// First invocation.
	l.trackCacheMetrics(context.Background(), state, "coder", &agent.InvocationResult{
		SystemPromptHash: "hash-1",
		SystemPromptLen:  50,
	})

	// Second invocation with different hash — miss.
	state.Cycle = 2
	l.trackCacheMetrics(context.Background(), state, "coder", &agent.InvocationResult{
		SystemPromptHash: "hash-2",
		SystemPromptLen:  60,
	})

	if state.cacheHitCount != 0 {
		t.Errorf("cacheHitCount = %d, want 0", state.cacheHitCount)
	}
	if state.cacheMissCount != 2 {
		t.Errorf("cacheMissCount = %d, want 2", state.cacheMissCount)
	}
	if state.totalCachedBytes != 0 {
		t.Errorf("totalCachedBytes = %d, want 0", state.totalCachedBytes)
	}
}

func TestRunLoop_CacheMetricsInTaskResult(t *testing.T) {
	t.Parallel()

	// Set up an invoker that returns "APPROVED:" on the second call (reviewer).
	inv := &fakeInvoker{
		responses: []agent.InvocationResult{
			{ResultText: "done coding", CostUSD: 0.20, SystemPromptLen: 100, SystemPromptHash: "stable-hash"},
			{ResultText: "APPROVED: Looks great.", CostUSD: 0.10, SystemPromptLen: 100, SystemPromptHash: "stable-hash"},
		},
	}
	l := &Loop{
		Invoker:   inv,
		UI:        &noopUI{},
		MaxCycles: 3,
	}

	result, err := l.runLoop(context.Background(), "bead-test", "implement cache telemetry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Two invocations with same hash: first is miss, second is hit.
	if result.CacheHitCount != 1 {
		t.Errorf("CacheHitCount = %d, want 1", result.CacheHitCount)
	}
	if result.CacheMissCount != 1 {
		t.Errorf("CacheMissCount = %d, want 1", result.CacheMissCount)
	}
	if result.TotalCachedBytes != 100 {
		t.Errorf("TotalCachedBytes = %d, want 100", result.TotalCachedBytes)
	}
}

func TestRunLoop_CacheMetricsMaxCycles(t *testing.T) {
	t.Parallel()

	// 2 cycles, no approval: coder, reviewer, coder, reviewer = 4 invocations.
	inv := &fakeInvoker{
		responses: []agent.InvocationResult{
			{ResultText: "coding 1", CostUSD: 0.10, SystemPromptLen: 80, SystemPromptHash: "hash-x"},
			{ResultText: "NEEDS_WORK\n- Fix bug", CostUSD: 0.10, SystemPromptLen: 80, SystemPromptHash: "hash-x"},
			{ResultText: "coding 2", CostUSD: 0.10, SystemPromptLen: 80, SystemPromptHash: "hash-x"},
			{ResultText: "NEEDS_WORK\n- Still broken", CostUSD: 0.10, SystemPromptLen: 80, SystemPromptHash: "hash-x"},
		},
	}
	l := &Loop{
		Invoker:   inv,
		UI:        &noopUI{},
		MaxCycles: 2,
	}

	result, err := l.runLoop(context.Background(), "bead-max", "fix the bug")
	if !errors.Is(err, ErrMaxCycles) {
		t.Fatalf("expected ErrMaxCycles, got: %v", err)
	}

	// 4 invocations, all same hash: first is miss, remaining 3 are hits.
	if result.CacheHitCount != 3 {
		t.Errorf("CacheHitCount = %d, want 3", result.CacheHitCount)
	}
	if result.CacheMissCount != 1 {
		t.Errorf("CacheMissCount = %d, want 1", result.CacheMissCount)
	}
	if result.TotalCachedBytes != 240 {
		t.Errorf("TotalCachedBytes = %d, want 240 (3 * 80)", result.TotalCachedBytes)
	}
}

// ---------------------------------------------------------------------------
// TestRunFromCheckpoint
// ---------------------------------------------------------------------------

func TestRunFromCheckpoint(t *testing.T) {
	t.Parallel()

	t.Run("ResumeFromReviewComplete_StartsNextCycle", func(t *testing.T) {
		t.Parallel()

		// Checkpoint says cycle 2 completed review → resume at cycle 3.
		// Provide responses for cycle 3 coder + reviewer (approved).
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coding cycle 3", CostUSD: 0.10},
				{ResultText: "APPROVED: Looks good.", CostUSD: 0.05},
			},
		}
		rUI := &recordingUI{}
		hook := &recordingHook{}
		l := &Loop{
			Invoker:   inv,
			UI:        rUI,
			MaxCycles: 5,
			Hooks:     []Hook{hook},
		}

		cs := &CycleState{
			TaskBeadID:    "bead-resume",
			TaskTitle:     "resume task",
			Phase:         PhaseReviewComplete,
			Cycle:         2,
			MaxCycles:     5,
			TotalCostUSD:  1.0,
			BaseCommitSHA: "abc123",
			CycleCommits:  []string{"sha-1", "sha-2"},
			CoderOutput:   "old coder output",
			ReviewOutput:  "old review output",
		}

		var cleanupCalled bool
		cleanup := func() error {
			cleanupCalled = true
			return nil
		}

		result, err := l.RunFromCheckpoint(context.Background(), cs, PhaseReviewComplete, cleanup)
		if err != nil {
			t.Fatalf("RunFromCheckpoint returned error: %v", err)
		}

		// Should have started at cycle 3 (2+1).
		if rUI.cycleStarts[0] != 3 {
			t.Errorf("first cycle start = %d, want 3", rUI.cycleStarts[0])
		}

		// Result should reflect the resumed run.
		if result.CyclesUsed != 3 {
			t.Errorf("CyclesUsed = %d, want 3", result.CyclesUsed)
		}

		// Cleanup should have been called on success.
		if !cleanupCalled {
			t.Error("cleanup was not called after successful completion")
		}

		// Verify EventResumed was emitted.
		events := hook.getEvents()
		foundResumed := false
		for _, e := range events {
			if e.Kind == EventResumed {
				foundResumed = true
				if e.Cycle != 3 {
					t.Errorf("EventResumed.Cycle = %d, want 3", e.Cycle)
				}
				if e.BeadID != "bead-resume" {
					t.Errorf("EventResumed.BeadID = %q, want %q", e.BeadID, "bead-resume")
				}
				if e.Message != "resumed from checkpoint" {
					t.Errorf("EventResumed.Message = %q, want %q", e.Message, "resumed from checkpoint")
				}
			}
		}
		if !foundResumed {
			t.Error("EventResumed was not emitted")
		}
	})

	t.Run("ResumeFromApproved_StartsNextCycle", func(t *testing.T) {
		t.Parallel()

		// PhaseApproved also increments the cycle (same as PhaseReviewComplete).
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coding", CostUSD: 0.10},
				{ResultText: "APPROVED: LGTM.", CostUSD: 0.05},
			},
		}
		rUI := &recordingUI{}
		l := &Loop{
			Invoker:   inv,
			UI:        rUI,
			MaxCycles: 5,
			Hooks:     []Hook{},
		}

		cs := &CycleState{
			TaskBeadID: "bead-approved",
			TaskTitle:  "approved task",
			Phase:      PhaseApproved,
			Cycle:      1,
			MaxCycles:  5,
		}

		result, err := l.RunFromCheckpoint(context.Background(), cs, PhaseApproved, nil)
		if err != nil {
			t.Fatalf("RunFromCheckpoint returned error: %v", err)
		}

		// Should start at cycle 2 (1+1).
		if rUI.cycleStarts[0] != 2 {
			t.Errorf("first cycle start = %d, want 2", rUI.cycleStarts[0])
		}
		if result.CyclesUsed != 2 {
			t.Errorf("CyclesUsed = %d, want 2", result.CyclesUsed)
		}
	})

	t.Run("ResumeFromCoding_RestartsSameCycle", func(t *testing.T) {
		t.Parallel()

		// PhaseCoding restarts the same cycle; agent outputs should be cleared.
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "fresh coding", CostUSD: 0.10},
				{ResultText: "APPROVED: Looks good.", CostUSD: 0.05},
			},
		}
		rUI := &recordingUI{}
		l := &Loop{
			Invoker:   inv,
			UI:        rUI,
			MaxCycles: 5,
			Hooks:     []Hook{},
		}

		cs := &CycleState{
			TaskBeadID:   "bead-coding",
			TaskTitle:    "coding task",
			Phase:        PhaseCoding,
			Cycle:        3,
			MaxCycles:    5,
			CoderOutput:  "incomplete coder output",
			ReviewOutput: "stale review output",
			LintOutput:   "stale lint output",
		}

		result, err := l.RunFromCheckpoint(context.Background(), cs, PhaseCoding, nil)
		if err != nil {
			t.Fatalf("RunFromCheckpoint returned error: %v", err)
		}

		// Should restart at cycle 3.
		if rUI.cycleStarts[0] != 3 {
			t.Errorf("first cycle start = %d, want 3", rUI.cycleStarts[0])
		}
		if result.CyclesUsed != 3 {
			t.Errorf("CyclesUsed = %d, want 3", result.CyclesUsed)
		}
	})

	t.Run("ResumeFromReviewing_RestartsSameCycle", func(t *testing.T) {
		t.Parallel()

		// PhaseReviewing also restarts the same cycle (incomplete review).
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "recoding", CostUSD: 0.10},
				{ResultText: "APPROVED: Looks good.", CostUSD: 0.05},
			},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        &noopUI{},
			MaxCycles: 5,
			Hooks:     []Hook{},
		}

		cs := &CycleState{
			TaskBeadID:   "bead-reviewing",
			TaskTitle:    "reviewing task",
			Phase:        PhaseReviewing,
			Cycle:        2,
			MaxCycles:    5,
			CoderOutput:  "incomplete",
			ReviewOutput: "incomplete",
		}

		result, err := l.RunFromCheckpoint(context.Background(), cs, PhaseReviewing, nil)
		if err != nil {
			t.Fatalf("RunFromCheckpoint returned error: %v", err)
		}

		// Should restart at cycle 2.
		if result.CyclesUsed != 2 {
			t.Errorf("CyclesUsed = %d, want 2", result.CyclesUsed)
		}
	})

	t.Run("CleanupNotCalledOnFailure", func(t *testing.T) {
		t.Parallel()

		// Resume at cycle 5 with MaxCycles=5 and reviewer doesn't approve.
		// This should exhaust cycles and NOT call cleanup.
		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coding", CostUSD: 0.10},
				{ResultText: "NEEDS_WORK\n- Not good", CostUSD: 0.05},
			},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        &noopUI{},
			MaxCycles: 5,
			Hooks:     []Hook{},
		}

		cs := &CycleState{
			TaskBeadID: "bead-fail",
			TaskTitle:  "failing task",
			Phase:      PhaseReviewComplete,
			Cycle:      4,
			MaxCycles:  5,
		}

		var cleanupCalled bool
		cleanup := func() error {
			cleanupCalled = true
			return nil
		}

		_, err := l.RunFromCheckpoint(context.Background(), cs, PhaseReviewComplete, cleanup)
		if !errors.Is(err, ErrMaxCycles) {
			t.Errorf("expected ErrMaxCycles, got %v", err)
		}

		if cleanupCalled {
			t.Error("cleanup should not be called on failure")
		}
	})

	t.Run("NilCleanupDoesNotPanic", func(t *testing.T) {
		t.Parallel()

		inv := &fakeInvoker{
			responses: []agent.InvocationResult{
				{ResultText: "coding", CostUSD: 0.10},
				{ResultText: "APPROVED: OK.", CostUSD: 0.05},
			},
		}
		l := &Loop{
			Invoker:   inv,
			UI:        &noopUI{},
			MaxCycles: 5,
			Hooks:     []Hook{},
		}

		cs := &CycleState{
			TaskBeadID: "bead-nil-cleanup",
			TaskTitle:  "nil cleanup task",
			Phase:      PhaseCoding,
			Cycle:      1,
			MaxCycles:  5,
		}

		// nil cleanup should not panic.
		_, err := l.RunFromCheckpoint(context.Background(), cs, PhaseCoding, nil)
		if err != nil {
			t.Fatalf("RunFromCheckpoint returned error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestWireCheckpointHook
// ---------------------------------------------------------------------------

func TestWireCheckpointHook(t *testing.T) {
	t.Parallel()

	t.Run("WiresHookWhenCheckpointDirSet", func(t *testing.T) {
		t.Parallel()

		var factoryCalled bool
		existingHook := &recordingHook{}

		l := &Loop{
			Invoker:       &fakeInvoker{},
			UI:            &noopUI{},
			MaxCycles:     1,
			CheckpointDir: "/tmp/checkpoints",
			Hooks:         []Hook{existingHook},
			NewCheckpointHook: func(stateFunc func() *CycleState) Hook {
				factoryCalled = true
				return HookFunc(func(_ context.Context, _ Event) {})
			},
		}

		cs := &CycleState{TaskBeadID: "test"}
		l.wireCheckpointHook(func() *CycleState { return cs })

		if !factoryCalled {
			t.Error("NewCheckpointHook factory was not called")
		}
		// The hook should be prepended, so Hooks should have 2 elements.
		if len(l.Hooks) != 2 {
			t.Errorf("Hooks length = %d, want 2", len(l.Hooks))
		}
	})

	t.Run("NoOpWhenCheckpointDirEmpty", func(t *testing.T) {
		t.Parallel()

		l := &Loop{
			Invoker:       &fakeInvoker{},
			UI:            &noopUI{},
			MaxCycles:     1,
			CheckpointDir: "", // empty
			Hooks:         []Hook{},
			NewCheckpointHook: func(stateFunc func() *CycleState) Hook {
				t.Error("factory should not be called when CheckpointDir is empty")
				return nil
			},
		}

		cs := &CycleState{TaskBeadID: "test"}
		l.wireCheckpointHook(func() *CycleState { return cs })

		if len(l.Hooks) != 0 {
			t.Errorf("Hooks length = %d, want 0", len(l.Hooks))
		}
	})

	t.Run("NoOpWhenFactoryNil", func(t *testing.T) {
		t.Parallel()

		l := &Loop{
			Invoker:           &fakeInvoker{},
			UI:                &noopUI{},
			MaxCycles:         1,
			CheckpointDir:     "/tmp/checkpoints",
			Hooks:             []Hook{},
			NewCheckpointHook: nil,
		}

		cs := &CycleState{TaskBeadID: "test"}
		l.wireCheckpointHook(func() *CycleState { return cs })

		if len(l.Hooks) != 0 {
			t.Errorf("Hooks length = %d, want 0", len(l.Hooks))
		}
	})
}
