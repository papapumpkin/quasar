package constellations

import (
	"context"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
)

// reviewer-decision-v1 payloads the fake reviewer returns to drive the embedded
// coder-reviewer loop deterministically.
const (
	reviewerApproveJSON        = `{"verdict":"approve","comments":[]}`
	reviewerRequestChangesJSON = `{"verdict":"request_changes","comments":[{"severity":"major","detail":"handle the nil case"}]}`
)

// rolePlayInvoker returns coder vs reviewer output based on the star's system
// prompt, so a single invoker can drive both stars of the embedded
// coder-reviewer constellation. The reviewer's verdicts are scripted: the i-th
// reviewer invocation returns reviewerScript[i], with the last entry repeating so
// an "always requests changes" run loops until the cap. It counts each role's
// invocations so a test can assert how many coder revisions happened.
type rolePlayInvoker struct {
	reviewerScript []string
	coderCalls     int
	reviewerCalls  int
}

func (i *rolePlayInvoker) Invoke(_ context.Context, a agent.Agent, _ string, _ string) (agent.InvocationResult, error) {
	if strings.Contains(a.SystemPrompt, "You are the reviewer") {
		idx := i.reviewerCalls
		if idx >= len(i.reviewerScript) {
			idx = len(i.reviewerScript) - 1
		}
		i.reviewerCalls++
		return agent.InvocationResult{ResultText: i.reviewerScript[idx]}, nil
	}
	i.coderCalls++
	return agent.InvocationResult{ResultText: "implemented the change"}, nil
}

func (i *rolePlayInvoker) Validate() error { return nil }

// newCoderReviewerRuntime builds a runtime over the real embedded artifacts (so
// the shipped coder-reviewer.toml and its coder/reviewer stars are exercised),
// wired to the scripted invoker and a committer whose commits always succeed.
func newCoderReviewerRuntime(t *testing.T, inv agent.Invoker) (*Runtime, string) {
	t.Helper()
	rt, nebID := newTestRuntime(t, artifacts.New(embeddedResolver{}), inv)
	rt.committer = &fakeCommitter{sha: "abc123"}
	return rt, nebID
}

// TestCoderReviewerLoopRequestChangesThenApprove drives the embedded
// coder-reviewer constellation end-to-end: the reviewer requests changes once,
// then approves. The run must reach _done after exactly two coder invocations
// (the initial implementation plus one revision), proving the decide -> implement
// back-edge feeds the reviewer's verdict back into a real revision cycle.
func TestCoderReviewerLoopRequestChangesThenApprove(t *testing.T) {
	ctx := context.Background()
	inv := &rolePlayInvoker{reviewerScript: []string{reviewerRequestChangesJSON, reviewerApproveJSON}}
	rt, nebID := newCoderReviewerRuntime(t, inv)

	runID, err := rt.Fire(ctx, "coder-reviewer", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	driveToTerminal(ctx, t, rt, runID)

	run := mustRun(t, rt, runID)
	if run.State != StateDone {
		t.Fatalf("final state = %q, want done", run.State)
	}
	if inv.coderCalls != 2 {
		t.Errorf("coder invocations = %d, want 2 (implement + one revision)", inv.coderCalls)
	}
	if inv.reviewerCalls != 2 {
		t.Errorf("reviewer invocations = %d, want 2 (request_changes then approve)", inv.reviewerCalls)
	}
}

// TestCoderReviewerLoopExhaustsCyclesAndFails drives the embedded coder-reviewer
// constellation with a reviewer that always requests changes. The run must loop
// until the embedded [meta].max_cycles cap (3) is exhausted, then route to
// give-up and terminate failed with the structured reason — never _done. This is
// the inner-loop analogue of master-review's cap enforcement, using the same
// back-edge cycle counter.
func TestCoderReviewerLoopExhaustsCyclesAndFails(t *testing.T) {
	ctx := context.Background()
	inv := &rolePlayInvoker{reviewerScript: []string{reviewerRequestChangesJSON}}
	rt, nebID := newCoderReviewerRuntime(t, inv)

	runID, err := rt.Fire(ctx, "coder-reviewer", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	driveToTerminal(ctx, t, rt, runID)

	run := mustRun(t, rt, runID)
	if run.State != StateFailed {
		t.Fatalf("final state = %q, want failed", run.State)
	}
	if run.Cycle != 3 {
		t.Errorf("run cycle = %d, want 3 (embedded cap, one per back-edge)", run.Cycle)
	}
	// max_cycles = 3 permits 3 revise back-edges, i.e. 4 coder invocations before
	// the cap blocks the 4th revision and routes to give-up.
	if inv.coderCalls != 4 {
		t.Errorf("coder invocations = %d, want 4 (initial + 3 capped revisions)", inv.coderCalls)
	}
	st, err := UnmarshalState(run.DAGStateTOML)
	if err != nil {
		t.Fatalf("UnmarshalState: %v", err)
	}
	if got := st.Nodes["give-up"]["reason"]; got != "max coder-reviewer cycles exhausted" {
		t.Errorf("give-up reason = %v, want structured cycle-exhaustion reason", got)
	}
}
