package web

import "time"

// PhaseDetail holds accumulated cycle data for a single phase.
// Built incrementally from events by PhaseAccumulator.
type PhaseDetail struct {
	ID          string
	Title       string
	Status      string
	TotalCost   float64
	Cycles      []CycleDetail
	BlockedBy   []string
	StartedAt   time.Time
	CompletedAt time.Time
}

// CycleDetail holds data for one coder-reviewer cycle.
type CycleDetail struct {
	Number  int
	Agents  []AgentDetail
	Summary *CycleSummary
}

// AgentDetail mirrors tui.AgentEntry for web rendering.
type AgentDetail struct {
	Role       string
	CostUSD    float64
	DurationMs int64
	IssueCount int
	Output     string   // truncated agent output
	Diff       string   // raw unified diff output
	DiffFiles  []string // list of changed file paths
	Done       bool
}

// CycleSummary holds reviewer assessment data.
type CycleSummary struct {
	Satisfaction string // "satisfied", "unsatisfied", etc.
	Risk         string // "low", "medium", "high"
	Summary      string
	IssueCount   int
}
