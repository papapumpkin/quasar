package tui

import "time"

// PhaseStatus represents the display state of a nebula phase.
type PhaseStatus int

const (
	PhaseWaiting PhaseStatus = iota
	PhaseWorking
	PhaseDone
	PhaseFailed
	PhaseGate
	PhaseSkipped
)

// PhaseStatusFromString maps a nebula state status string to a TUI PhaseStatus.
// This is used when initializing the TUI from saved state.
func PhaseStatusFromString(s string) PhaseStatus {
	switch s {
	case "done":
		return PhaseDone
	case "failed":
		return PhaseFailed
	case "in_progress":
		return PhaseWorking
	case "skipped":
		return PhaseSkipped
	case "gate":
		return PhaseGate
	default:
		return PhaseWaiting
	}
}

// PhaseHealth represents the derived health signal for a phase.
type PhaseHealth int

const (
	// HealthGreen indicates the phase is progressing well (early cycles or reviewer satisfied).
	HealthGreen PhaseHealth = iota
	// HealthYellow indicates the reviewer is unsatisfied but cycles remain.
	HealthYellow
	// HealthRed indicates the phase is on its final cycle with reviewer unsatisfied, or has pending hails.
	HealthRed
)

// PhaseEntry represents one phase in the nebula view.
type PhaseEntry struct {
	ID           string
	Title        string
	Status       PhaseStatus
	Wave         int
	CostUSD      float64
	Cycles       int
	MaxCycles    int
	MaxBudgetUSD float64 // per-phase budget cap (0 = unlimited)
	BlockedBy    string
	DependsOn    []string // original dependency IDs from the phase spec
	StartedAt    time.Time
	CompletedAt  time.Time // set when phase reaches a terminal state
	PlanBody     string    // markdown content from the phase file
	Refactored   bool      // true when a mid-run refactor was applied this cycle

	// Board card enrichment fields.
	LastActivity      string // most recent activity string from the worker (e.g. "coding...")
	HasPendingHails   bool   // true if this phase has unresolved hails
	ReviewerSatisfied bool   // true if the reviewer approved in the latest cycle
	CompletionNote    string // brief note from last reviewer (shown on done/failed cards)
}

// Health derives a health signal from cycle progress and reviewer satisfaction.
func (p PhaseEntry) Health() PhaseHealth {
	// Pending hails always signal red.
	if p.HasPendingHails {
		return HealthRed
	}

	// Non-running phases: green by default (no signal needed).
	if p.Status != PhaseWorking {
		// Gate phases are yellow — they need attention.
		if p.Status == PhaseGate {
			return HealthYellow
		}
		return HealthGreen
	}

	// Running: derive from cycle progress and reviewer satisfaction.
	if p.MaxCycles > 0 && p.Cycles >= p.MaxCycles && !p.ReviewerSatisfied {
		return HealthRed
	}
	if p.MaxCycles > 0 {
		ratio := float64(p.Cycles) / float64(p.MaxCycles)
		if ratio > 0.6 && !p.ReviewerSatisfied {
			return HealthYellow
		}
	}

	return HealthGreen
}
