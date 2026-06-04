package loop

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/ui"
)

// TestLoopPackageBuilds is a placeholder ensuring the loop package's test
// target compiles. The full loop_test.go (parked at loop_test.go.disabled)
// was heavily coupled to the removed beads package and needs to be rewritten
// against the post-beads loop API. See docs/superpowers/specs/
// 2026-06-03-quasar-autonomous-issue-to-pr-design.md for the broader rework
// context.
func TestLoopPackageBuilds(t *testing.T) {
	// Nothing to assert here; the build-and-link step is the test.
}

// noopUI satisfies ui.UI for tests without producing any output. Defined here
// because other test files in this package (lint_, prompts_, refactor_)
// reference noopUI and fakeInvoker for their own assertions.
type noopUI struct{}

var _ ui.UI = (*noopUI)(nil)

func (n *noopUI) TaskStarted(string, string)                        {}
func (n *noopUI) TaskComplete(string, float64)                      {}
func (n *noopUI) CycleStart(int, int)                               {}
func (n *noopUI) AgentStart(string)                                 {}
func (n *noopUI) AgentOutput(string, int, string)                   {}
func (n *noopUI) AgentDone(string, float64, int64)                  {}
func (n *noopUI) IssuesFound(int)                                   {}
func (n *noopUI) Approved()                                         {}
func (n *noopUI) MaxCyclesReached(int)                              {}
func (n *noopUI) BudgetExceeded(float64, float64)                   {}
func (n *noopUI) Info(string)                                       {}
func (n *noopUI) Error(string)                                      {}
func (n *noopUI) BeadUpdate(string, string, string, []ui.BeadChild) {}
func (n *noopUI) CycleSummary(ui.CycleSummaryData)                  {}
func (n *noopUI) RefactorApplied(string)                            {}
func (n *noopUI) FindingLifecycle(int, ui.FindingLifecycleData)     {}

// fakeInvoker returns controlled responses for testing the loop. Other tests
// in this package use it to assert prompt content and per-cycle behavior.
type fakeInvoker struct {
	responses []agent.InvocationResult
	errors    []error
	mu        sync.Mutex
	prompts   []string
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

// recordingUI captures method calls for assertions.
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
	taskID   string
	title    string
	status   string
	children []ui.BeadChild
}

func (r *recordingUI) TaskStarted(id, _ string) {
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
func (r *recordingUI) BeadUpdate(taskID, title, status string, children []ui.BeadChild) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beadUpdates = append(r.beadUpdates, beadUpdateCall{taskID, title, status, children})
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

// fakeGit implements CycleCommitter for testing.
type fakeGit struct {
	headSHA    string
	commitSHAs []string
	mu         sync.Mutex
	commits    int
	headErr    error
	commitErr  error
}

func (g *fakeGit) HeadSHA(_ context.Context) (string, error) {
	return g.headSHA, g.headErr
}

func (g *fakeGit) CommitCycle(_ context.Context, _ string, _ int, _, _ string) (string, error) {
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

func (g *fakeGit) DiffRange(_ context.Context, _, _ string) (string, error) { return "", nil }
func (g *fakeGit) ResetTo(_ context.Context, _ string) error                { return nil }
