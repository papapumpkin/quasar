package constellations

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// conflictResolverInvoker role-plays the conflict-resolver star: the i-th
// invocation returns script[i] (the last entry repeats), so a test can drive the
// merge-conflict-resolve loop deterministically and assert how many resolver
// passes ran. Non-resolver stars are not expected in this constellation.
type conflictResolverInvoker struct {
	script []string
	calls  int
}

func (i *conflictResolverInvoker) Invoke(_ context.Context, a agent.Agent, _ string, _ string) (agent.InvocationResult, error) {
	if !strings.Contains(a.SystemPrompt, "You are reconciling work from two parallel workstreams") {
		return agent.InvocationResult{}, nil
	}
	idx := i.calls
	if idx >= len(i.script) {
		idx = len(i.script) - 1
	}
	i.calls++
	return agent.InvocationResult{ResultText: i.script[idx]}, nil
}

func (i *conflictResolverInvoker) Validate() error { return nil }

// fireConflictResolve fires the embedded merge-conflict-resolve constellation
// over the real shipped artifacts, seeds the inputs the merge gate would supply,
// and drives it to terminal. It returns the runtime and the terminal run id.
func fireConflictResolve(t *testing.T, inv agent.Invoker, inputs map[string]any) (*Runtime, string) {
	t.Helper()
	rt, nebID := newTestRuntime(t, artifacts.New(embeddedResolver{}), inv)
	rt.committer = &fakeCommitter{sha: "merged-sha"}

	ctx := context.Background()
	runID, err := rt.Fire(ctx, "merge-conflict-resolve", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if err := rt.seedChildInputs(ctx, runID, inputs); err != nil {
		t.Fatalf("seed inputs: %v", err)
	}
	driveToTerminal(ctx, t, rt, runID)
	return rt, runID
}

// TestMergeConflictResolveMarkersResolved drives the markers happy path: the
// resolver returns resolved+green on its first pass, so the run commits and ends
// _done with the merge SHA exposed.
func TestMergeConflictResolveMarkersResolved(t *testing.T) {
	inv := &conflictResolverInvoker{script: []string{
		`{"status":"resolved","files_changed":["internal/sensors/sensors.go"],"build_passed":true,"escalation_reason":null}`,
	}}
	rt, runID := fireConflictResolve(t, inv, map[string]any{
		"mode":     conflictModeMarkers,
		"files":    []string{"internal/sensors/sensors.go"},
		"worktree": "/tmp/wt",
	})

	run := mustRun(t, rt, runID)
	if run.State != StateDone {
		t.Fatalf("final state = %q, want done", run.State)
	}
	if inv.calls != 1 {
		t.Errorf("resolver invocations = %d, want 1", inv.calls)
	}
	st, err := UnmarshalState(run.DAGStateTOML)
	if err != nil {
		t.Fatalf("UnmarshalState: %v", err)
	}
	if got := st.Nodes["commit"]["sha"]; got != "merged-sha" {
		t.Errorf("commit sha = %v, want merged-sha", got)
	}
}

// TestMergeConflictResolveEmitsTelemetry drives the markers happy path with a
// conflict log wired and asserts the run records exactly one resolution-outcome
// row (status=resolved) on the way to _done — proving the emit_conflict_telemetry
// node sits on the terminal path, not just in unit tests.
func TestMergeConflictResolveEmitsTelemetry(t *testing.T) {
	inv := &conflictResolverInvoker{script: []string{
		`{"status":"resolved","files_changed":["internal/sensors/sensors.go"],"build_passed":true,"escalation_reason":null}`,
	}}
	rt, nebID := newTestRuntime(t, artifacts.New(embeddedResolver{}), inv)
	rt.committer = &fakeCommitter{sha: "merged-sha"}
	logPath := filepath.Join(t.TempDir(), "conflict_resolutions.jsonl")
	rt.conflictLog = telemetry.NewConflictResolutionLog(logPath)

	ctx := context.Background()
	runID, err := rt.Fire(ctx, "merge-conflict-resolve", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if err := rt.seedChildInputs(ctx, runID, map[string]any{
		"mode":     conflictModeMarkers,
		"files":    []string{"internal/sensors/sensors.go"},
		"worktree": "/tmp/wt",
	}); err != nil {
		t.Fatalf("seed inputs: %v", err)
	}
	driveToTerminal(ctx, t, rt, runID)

	if run := mustRun(t, rt, runID); run.State != StateDone {
		t.Fatalf("final state = %q, want done", run.State)
	}
	events, err := rt.conflictLog.ReadSince(ctx, time.Time{})
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("recorded %d telemetry rows, want exactly 1", len(events))
	}
	if events[0].Status != conflictResolverStatusResolved || events[0].Mode != conflictModeMarkers {
		t.Errorf("row status/mode = %q/%q, want resolved/markers", events[0].Status, events[0].Mode)
	}
}

// TestMergeConflictResolveExhaustsCycles drives the no_markers case where the
// resolver always reports resolved but the build never greens. The run must loop
// to the embedded [meta].max_cycles cap (2), then escalate to _awaiting_human —
// never commit.
func TestMergeConflictResolveExhaustsCycles(t *testing.T) {
	inv := &conflictResolverInvoker{script: []string{
		`{"status":"resolved","files_changed":["internal/loop/loop.go"],"build_passed":false,"escalation_reason":null}`,
	}}
	rt, runID := fireConflictResolve(t, inv, map[string]any{
		"mode":         conflictModeNoMarkers,
		"build_output": "undefined: Foo",
		"worktree":     "/tmp/wt",
	})

	run := mustRun(t, rt, runID)
	if run.State != StateAwaitingHuman {
		t.Fatalf("final state = %q, want awaiting_human", run.State)
	}
	if run.Cycle != 2 {
		t.Errorf("run cycle = %d, want 2 (embedded cap, one per back-edge)", run.Cycle)
	}
	// max_cycles = 2 permits 2 retry back-edges, i.e. 3 resolver passes before the
	// cap routes to give_up.
	if inv.calls != 3 {
		t.Errorf("resolver invocations = %d, want 3 (initial + 2 capped retries)", inv.calls)
	}
}

// TestMergeConflictResolveConfigFileEscalates drives a config-file conflict: the
// decision operator must short-circuit to needs_human and escalate to
// _awaiting_human without consuming a cycle, even though the resolver claimed a
// clean green resolution.
func TestMergeConflictResolveConfigFileEscalates(t *testing.T) {
	inv := &conflictResolverInvoker{script: []string{
		`{"status":"resolved","files_changed":["go.mod"],"build_passed":true,"escalation_reason":null}`,
	}}
	rt, runID := fireConflictResolve(t, inv, map[string]any{
		"mode":     conflictModeMarkers,
		"files":    []string{"go.mod"},
		"worktree": "/tmp/wt",
	})

	run := mustRun(t, rt, runID)
	if run.State != StateAwaitingHuman {
		t.Fatalf("final state = %q, want awaiting_human", run.State)
	}
	if run.Cycle != 0 {
		t.Errorf("run cycle = %d, want 0 (config-file escalation consumes no cycle)", run.Cycle)
	}
	st, err := UnmarshalState(run.DAGStateTOML)
	if err != nil {
		t.Fatalf("UnmarshalState: %v", err)
	}
	reason, _ := st.Nodes["decide"]["escalation_reason"].(string)
	if !strings.Contains(reason, "config-file conflict") {
		t.Errorf("decide escalation_reason = %q, want config-file conflict", reason)
	}
}
