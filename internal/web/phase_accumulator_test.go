package web

import (
	"encoding/json"
	"testing"
)

func TestPhaseAccumulator_TaskStarted(t *testing.T) {
	t.Parallel()

	acc := NewPhaseAccumulator()
	acc.handle(makeEvent("phase.task.started", "p1", eventPayload{
		Phase: "p1",
		Title: "My Phase",
	}))

	pd := acc.Get("p1")
	if pd == nil {
		t.Fatal("expected PhaseDetail for p1")
	}
	if pd.Title != "My Phase" {
		t.Errorf("title = %q, want %q", pd.Title, "My Phase")
	}
	if pd.Status != "in_progress" {
		t.Errorf("status = %q, want %q", pd.Status, "in_progress")
	}
	if pd.StartedAt.IsZero() {
		t.Error("expected StartedAt to be set")
	}
}

func TestPhaseAccumulator_FullCycle(t *testing.T) {
	t.Parallel()

	acc := NewPhaseAccumulator()

	// Task started.
	acc.handle(makeEvent("phase.task.started", "p1", eventPayload{
		Phase: "p1",
		Title: "Test Phase",
	}))

	// Cycle 1 start.
	acc.handle(makeEvent("phase.cycle.start", "p1", eventPayload{
		Phase: "p1",
		Cycle: 1,
	}))

	// Coder start.
	acc.handle(makeEvent("phase.agent.start", "p1", eventPayload{
		Phase: "p1",
		Role:  "coder",
	}))

	// Coder done.
	acc.handle(makeEvent("phase.agent.done", "p1", eventPayload{
		Phase:      "p1",
		Role:       "coder",
		CostUSD:    0.05,
		DurationMs: 3000,
	}))

	// Reviewer start.
	acc.handle(makeEvent("phase.agent.start", "p1", eventPayload{
		Phase: "p1",
		Role:  "reviewer",
	}))

	// Reviewer done.
	acc.handle(makeEvent("phase.agent.done", "p1", eventPayload{
		Phase:      "p1",
		Role:       "reviewer",
		CostUSD:    0.02,
		DurationMs: 1500,
	}))

	// Issues found.
	acc.handle(makeEvent("phase.issues.found", "p1", eventPayload{
		Phase: "p1",
		Count: 3,
	}))

	// Cycle summary.
	acc.handle(makeEvent("phase.cycle.summary", "p1", eventPayload{
		Phase:   "p1",
		Message: "Reviewer found issues",
		Summary: &summaryPayload{
			Cycle:      1,
			Approved:   false,
			IssueCount: 3,
			CostUSD:    0.02,
		},
	}))

	// Task complete.
	acc.handle(makeEvent("phase.task.complete", "p1", eventPayload{
		Phase: "p1",
	}))

	pd := acc.Get("p1")
	if pd == nil {
		t.Fatal("expected PhaseDetail for p1")
	}

	if pd.Status != "done" {
		t.Errorf("status = %q, want %q", pd.Status, "done")
	}
	if pd.TotalCost != 0.07 {
		t.Errorf("totalCost = %f, want 0.07", pd.TotalCost)
	}
	if len(pd.Cycles) != 1 {
		t.Fatalf("cycles = %d, want 1", len(pd.Cycles))
	}

	cycle := pd.Cycles[0]
	if cycle.Number != 1 {
		t.Errorf("cycle number = %d, want 1", cycle.Number)
	}
	if len(cycle.Agents) != 2 {
		t.Fatalf("agents = %d, want 2", len(cycle.Agents))
	}

	coder := cycle.Agents[0]
	if coder.Role != "coder" {
		t.Errorf("agent[0] role = %q, want %q", coder.Role, "coder")
	}
	if !coder.Done {
		t.Error("coder should be done")
	}
	if coder.CostUSD != 0.05 {
		t.Errorf("coder cost = %f, want 0.05", coder.CostUSD)
	}
	if coder.DurationMs != 3000 {
		t.Errorf("coder duration = %d, want 3000", coder.DurationMs)
	}

	reviewer := cycle.Agents[1]
	if reviewer.Role != "reviewer" {
		t.Errorf("agent[1] role = %q, want %q", reviewer.Role, "reviewer")
	}
	if reviewer.IssueCount != 3 {
		t.Errorf("reviewer issue count = %d, want 3", reviewer.IssueCount)
	}

	if cycle.Summary == nil {
		t.Fatal("expected cycle summary")
	}
	if cycle.Summary.Satisfaction != "unsatisfied" {
		t.Errorf("satisfaction = %q, want %q", cycle.Summary.Satisfaction, "unsatisfied")
	}
	if cycle.Summary.IssueCount != 3 {
		t.Errorf("summary issue count = %d, want 3", cycle.Summary.IssueCount)
	}
}

func TestPhaseAccumulator_MultipleCycles(t *testing.T) {
	t.Parallel()

	acc := NewPhaseAccumulator()

	acc.handle(makeEvent("phase.task.started", "p1", eventPayload{Phase: "p1", Title: "Multi-cycle"}))

	// Cycle 1.
	acc.handle(makeEvent("phase.cycle.start", "p1", eventPayload{Phase: "p1", Cycle: 1}))
	acc.handle(makeEvent("phase.agent.start", "p1", eventPayload{Phase: "p1", Role: "coder"}))
	acc.handle(makeEvent("phase.agent.done", "p1", eventPayload{Phase: "p1", Role: "coder", CostUSD: 0.01}))
	acc.handle(makeEvent("phase.cycle.summary", "p1", eventPayload{
		Phase: "p1",
		Summary: &summaryPayload{Cycle: 1, Approved: false, IssueCount: 2},
	}))

	// Cycle 2.
	acc.handle(makeEvent("phase.cycle.start", "p1", eventPayload{Phase: "p1", Cycle: 2}))
	acc.handle(makeEvent("phase.agent.start", "p1", eventPayload{Phase: "p1", Role: "coder"}))
	acc.handle(makeEvent("phase.agent.done", "p1", eventPayload{Phase: "p1", Role: "coder", CostUSD: 0.02}))
	acc.handle(makeEvent("phase.cycle.summary", "p1", eventPayload{
		Phase: "p1",
		Summary: &summaryPayload{Cycle: 2, Approved: true},
	}))

	pd := acc.Get("p1")
	if len(pd.Cycles) != 2 {
		t.Fatalf("cycles = %d, want 2", len(pd.Cycles))
	}
	if pd.Cycles[0].Summary.Satisfaction != "unsatisfied" {
		t.Errorf("cycle 1 satisfaction = %q, want unsatisfied", pd.Cycles[0].Summary.Satisfaction)
	}
	if pd.Cycles[1].Summary.Satisfaction != "satisfied" {
		t.Errorf("cycle 2 satisfaction = %q, want satisfied", pd.Cycles[1].Summary.Satisfaction)
	}
	if pd.TotalCost != 0.03 {
		t.Errorf("total cost = %f, want 0.03", pd.TotalCost)
	}
}

func TestPhaseAccumulator_AgentOutput(t *testing.T) {
	t.Parallel()

	acc := NewPhaseAccumulator()
	acc.handle(makeEvent("phase.task.started", "p1", eventPayload{Phase: "p1"}))
	acc.handle(makeEvent("phase.cycle.start", "p1", eventPayload{Phase: "p1", Cycle: 1}))
	acc.handle(makeEvent("phase.agent.start", "p1", eventPayload{Phase: "p1", Role: "coder"}))
	acc.handle(makeEvent("phase.agent.output", "p1", eventPayload{
		Phase:  "p1",
		Role:   "coder",
		Cycle:  1,
		Output: "wrote some code",
	}))

	pd := acc.Get("p1")
	if len(pd.Cycles) == 0 || len(pd.Cycles[0].Agents) == 0 {
		t.Fatal("expected cycle with agent")
	}
	if pd.Cycles[0].Agents[0].Output != "wrote some code" {
		t.Errorf("output = %q, want %q", pd.Cycles[0].Agents[0].Output, "wrote some code")
	}
}

func TestPhaseAccumulator_IgnoresNonPhaseEvents(t *testing.T) {
	t.Parallel()

	acc := NewPhaseAccumulator()
	// Event with no PhaseID should be ignored.
	acc.handle(Event{Type: "phase.task.started", Data: `{"phase":""}`, PhaseID: ""})

	if pd := acc.Get(""); pd != nil {
		t.Error("expected nil for empty phase ID")
	}
}

func TestPhaseAccumulator_TaskCompleteFailed(t *testing.T) {
	t.Parallel()

	acc := NewPhaseAccumulator()
	acc.handle(makeEvent("phase.task.started", "p1", eventPayload{Phase: "p1"}))
	acc.handle(makeEvent("phase.task.complete", "p1", eventPayload{Phase: "p1", Message: "failed"}))

	pd := acc.Get("p1")
	if pd.Status != "failed" {
		t.Errorf("status = %q, want %q", pd.Status, "failed")
	}
}

func TestPhaseAccumulator_OutputTruncation(t *testing.T) {
	t.Parallel()

	acc := NewPhaseAccumulator()
	acc.handle(makeEvent("phase.task.started", "p1", eventPayload{Phase: "p1"}))
	acc.handle(makeEvent("phase.cycle.start", "p1", eventPayload{Phase: "p1", Cycle: 1}))
	acc.handle(makeEvent("phase.agent.start", "p1", eventPayload{Phase: "p1", Role: "coder"}))

	// Create output longer than maxOutputLen.
	longOutput := make([]byte, maxOutputLen+500)
	for i := range longOutput {
		longOutput[i] = 'x'
	}
	acc.handle(makeEvent("phase.agent.output", "p1", eventPayload{
		Phase:  "p1",
		Role:   "coder",
		Cycle:  1,
		Output: string(longOutput),
	}))

	pd := acc.Get("p1")
	agent := pd.Cycles[0].Agents[0]
	if len(agent.Output) > maxOutputLen+50 {
		t.Errorf("output length = %d, should be truncated near %d", len(agent.Output), maxOutputLen)
	}
}

// makeEvent constructs a web.Event with the given type, phase ID, and JSON payload.
func makeEvent(typ, phaseID string, payload eventPayload) Event {
	data, _ := json.Marshal(payload)
	return Event{
		Type:    typ,
		Data:    string(data),
		PhaseID: phaseID,
	}
}
