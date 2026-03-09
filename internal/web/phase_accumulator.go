package web

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// maxOutputLen caps the stored agent output to avoid unbounded memory growth.
const maxOutputLen = 10000

// PhaseAccumulator subscribes to the event bus (via EventSource) and maintains
// per-phase detail state for rendering. Thread-safe for concurrent reads and
// a single writer goroutine.
type PhaseAccumulator struct {
	mu     sync.RWMutex
	phases map[string]*PhaseDetail
}

// NewPhaseAccumulator creates an empty accumulator.
func NewPhaseAccumulator() *PhaseAccumulator {
	return &PhaseAccumulator{
		phases: make(map[string]*PhaseDetail),
	}
}

// Get returns the PhaseDetail for the given ID, or nil if not found.
func (a *PhaseAccumulator) Get(id string) *PhaseDetail {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.phases[id]
}

// Start subscribes to the EventSource and processes events until ctx is
// cancelled. Should be called in a goroutine.
func (a *PhaseAccumulator) Start(ctx context.Context, src EventSource) {
	events, cancel := src.Subscribe(ctx)
	defer cancel()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			a.handle(ev)
		case <-ctx.Done():
			return
		}
	}
}

// handle dispatches a single web event to the appropriate accumulator method.
func (a *PhaseAccumulator) handle(ev Event) {
	if ev.PhaseID == "" {
		return
	}

	switch ev.Type {
	case "phase.task.started":
		a.handleTaskStarted(ev)
	case "phase.cycle.start":
		a.handleCycleStart(ev)
	case "phase.agent.start":
		a.handleAgentStart(ev)
	case "phase.agent.done":
		a.handleAgentDone(ev)
	case "phase.agent.output":
		a.handleAgentOutput(ev)
	case "phase.cycle.summary":
		a.handleCycleSummary(ev)
	case "phase.task.complete":
		a.handleTaskComplete(ev)
	case "phase.issues.found":
		a.handleIssuesFound(ev)
	}
}

// parseData unmarshals the JSON data from a web event into the shared
// eventPayload structure defined in bus_adapter.go.
func parseData(ev Event) eventPayload {
	var d eventPayload
	_ = json.Unmarshal([]byte(ev.Data), &d)
	return d
}

// ensurePhase returns or creates a PhaseDetail for the given ID.
func (a *PhaseAccumulator) ensurePhase(id string) *PhaseDetail {
	pd, ok := a.phases[id]
	if !ok {
		pd = &PhaseDetail{
			ID:     id,
			Status: "pending",
		}
		a.phases[id] = pd
	}
	return pd
}

func (a *PhaseAccumulator) handleTaskStarted(ev Event) {
	d := parseData(ev)
	a.mu.Lock()
	defer a.mu.Unlock()
	pd := a.ensurePhase(ev.PhaseID)
	if d.Title != "" {
		pd.Title = d.Title
	}
	pd.Status = "in_progress"
	pd.StartedAt = time.Now()
}

func (a *PhaseAccumulator) handleCycleStart(ev Event) {
	d := parseData(ev)
	a.mu.Lock()
	defer a.mu.Unlock()
	pd := a.ensurePhase(ev.PhaseID)
	cycle := CycleDetail{Number: d.Cycle}
	pd.Cycles = append(pd.Cycles, cycle)
}

func (a *PhaseAccumulator) handleAgentStart(ev Event) {
	d := parseData(ev)
	a.mu.Lock()
	defer a.mu.Unlock()
	pd := a.ensurePhase(ev.PhaseID)
	if len(pd.Cycles) == 0 {
		pd.Cycles = append(pd.Cycles, CycleDetail{Number: 1})
	}
	last := &pd.Cycles[len(pd.Cycles)-1]
	last.Agents = append(last.Agents, AgentDetail{
		Role: d.Role,
	})
}

func (a *PhaseAccumulator) handleAgentDone(ev Event) {
	d := parseData(ev)
	a.mu.Lock()
	defer a.mu.Unlock()
	pd := a.ensurePhase(ev.PhaseID)
	if len(pd.Cycles) == 0 {
		return
	}
	last := &pd.Cycles[len(pd.Cycles)-1]
	for i := len(last.Agents) - 1; i >= 0; i-- {
		if last.Agents[i].Role == d.Role && !last.Agents[i].Done {
			last.Agents[i].Done = true
			last.Agents[i].CostUSD = d.CostUSD
			last.Agents[i].DurationMs = d.DurationMs
			pd.TotalCost += d.CostUSD
			break
		}
	}
}

func (a *PhaseAccumulator) handleAgentOutput(ev Event) {
	d := parseData(ev)
	a.mu.Lock()
	defer a.mu.Unlock()
	pd := a.ensurePhase(ev.PhaseID)

	output := d.Output
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen] + "\n... (truncated)"
	}

	// Find the matching agent in the specified cycle, or the last matching agent.
	if d.Cycle > 0 && d.Cycle <= len(pd.Cycles) {
		cycle := &pd.Cycles[d.Cycle-1]
		for i := len(cycle.Agents) - 1; i >= 0; i-- {
			if cycle.Agents[i].Role == d.Role {
				cycle.Agents[i].Output = output
				return
			}
		}
	}

	// Fallback: last cycle, last agent with matching role.
	if len(pd.Cycles) > 0 {
		last := &pd.Cycles[len(pd.Cycles)-1]
		for i := len(last.Agents) - 1; i >= 0; i-- {
			if last.Agents[i].Role == d.Role {
				last.Agents[i].Output = output
				return
			}
		}
	}
}

func (a *PhaseAccumulator) handleCycleSummary(ev Event) {
	d := parseData(ev)
	a.mu.Lock()
	defer a.mu.Unlock()
	pd := a.ensurePhase(ev.PhaseID)
	if d.Summary == nil {
		return
	}

	satisfaction := "unsatisfied"
	if d.Summary.Approved {
		satisfaction = "satisfied"
	}

	summary := &CycleSummary{
		Satisfaction: satisfaction,
		Risk:         "low", // default; bus events don't carry explicit risk level
		Summary:      d.Message,
		IssueCount:   d.Summary.IssueCount,
	}

	// Attach to matching cycle by number, or last cycle.
	if d.Summary.Cycle > 0 && d.Summary.Cycle <= len(pd.Cycles) {
		pd.Cycles[d.Summary.Cycle-1].Summary = summary
	} else if len(pd.Cycles) > 0 {
		pd.Cycles[len(pd.Cycles)-1].Summary = summary
	}
}

func (a *PhaseAccumulator) handleTaskComplete(ev Event) {
	d := parseData(ev)
	a.mu.Lock()
	defer a.mu.Unlock()
	pd := a.ensurePhase(ev.PhaseID)
	pd.CompletedAt = time.Now()
	// Determine final status from message content.
	switch {
	case d.Message == "failed" || d.Message == "error":
		pd.Status = "failed"
	default:
		pd.Status = "done"
	}
}

func (a *PhaseAccumulator) handleIssuesFound(ev Event) {
	d := parseData(ev)
	a.mu.Lock()
	defer a.mu.Unlock()
	pd := a.ensurePhase(ev.PhaseID)
	if len(pd.Cycles) == 0 {
		return
	}
	last := &pd.Cycles[len(pd.Cycles)-1]
	// Update issue count on the last reviewer agent.
	for i := len(last.Agents) - 1; i >= 0; i-- {
		if last.Agents[i].Role == "reviewer" {
			last.Agents[i].IssueCount = d.Count
			break
		}
	}
}
