package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
)

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

// PhaseEntry represents one phase in the nebula view.
type PhaseEntry struct {
	ID        string
	Title     string
	Status    PhaseStatus
	Wave      int
	CostUSD   float64
	Cycles    int
	BlockedBy string
}

// NebulaView renders the phase table for multi-task orchestration.
type NebulaView struct {
	Phases  []PhaseEntry
	Cursor  int
	Spinner spinner.Model
	Width   int
}

// NewNebulaView creates an empty nebula view.
func NewNebulaView() NebulaView {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return NebulaView{}
}

// SelectedPhase returns the phase entry at the cursor.
func (nv NebulaView) SelectedPhase() *PhaseEntry {
	if nv.Cursor < 0 || nv.Cursor >= len(nv.Phases) {
		return nil
	}
	return &nv.Phases[nv.Cursor]
}

// MoveUp moves the cursor up.
func (nv *NebulaView) MoveUp() {
	if nv.Cursor > 0 {
		nv.Cursor--
	}
}

// MoveDown moves the cursor down.
func (nv *NebulaView) MoveDown() {
	max := len(nv.Phases) - 1
	if max < 0 {
		max = 0
	}
	if nv.Cursor < max {
		nv.Cursor++
	}
}

// InitPhases populates the phase table from a MsgNebulaInit.
func (nv *NebulaView) InitPhases(phases []PhaseInfo) {
	nv.Phases = make([]PhaseEntry, len(phases))
	for i, p := range phases {
		blocked := ""
		if len(p.DependsOn) > 0 {
			blocked = p.DependsOn[0]
			if len(p.DependsOn) > 1 {
				blocked += fmt.Sprintf(" +%d", len(p.DependsOn)-1)
			}
		}
		nv.Phases[i] = PhaseEntry{
			ID:        p.ID,
			Title:     p.Title,
			Status:    PhaseWaiting,
			BlockedBy: blocked,
		}
	}
}

// SetPhaseStatus updates the status of a phase by ID.
func (nv *NebulaView) SetPhaseStatus(phaseID string, status PhaseStatus) {
	for i := range nv.Phases {
		if nv.Phases[i].ID == phaseID {
			nv.Phases[i].Status = status
			return
		}
	}
}

// SetPhaseCost updates the cost of a phase by ID.
func (nv *NebulaView) SetPhaseCost(phaseID string, cost float64) {
	for i := range nv.Phases {
		if nv.Phases[i].ID == phaseID {
			nv.Phases[i].CostUSD = cost
			return
		}
	}
}

// SetPhaseCycles updates the cycle count of a phase by ID.
func (nv *NebulaView) SetPhaseCycles(phaseID string, cycles int) {
	for i := range nv.Phases {
		if nv.Phases[i].ID == phaseID {
			nv.Phases[i].Cycles = cycles
			return
		}
	}
}

// View renders the phase table.
func (nv NebulaView) View() string {
	var b strings.Builder
	for i, p := range nv.Phases {
		prefix := "  "
		if i == nv.Cursor {
			prefix = "> "
		}

		var statusIcon string
		var style = styleRowNormal
		switch p.Status {
		case PhaseDone:
			statusIcon = "[done]"
			style = styleRowDone
		case PhaseWorking:
			statusIcon = "[" + nv.Spinner.View() + "  ]"
			style = styleRowWorking
		case PhaseFailed:
			statusIcon = "[fail]"
			style = styleRowFailed
		case PhaseGate:
			statusIcon = "[gate]"
			style = styleRowGate
		case PhaseSkipped:
			statusIcon = "[skip]"
			style = styleRowWaiting
		default:
			statusIcon = "[wait]"
			style = styleRowWaiting
		}

		if i == nv.Cursor {
			style = styleRowSelected
		}

		detail := ""
		if p.Status == PhaseDone {
			detail = fmt.Sprintf("W%d  $%.2f  %d cycle(s)", p.Wave, p.CostUSD, p.Cycles)
		} else if p.Status == PhaseWorking {
			detail = fmt.Sprintf("W%d  working…", p.Wave)
		} else if p.BlockedBy != "" {
			detail = fmt.Sprintf("blocked: %s", p.BlockedBy)
		}

		line := fmt.Sprintf("%s%s %-24s %s", prefix, statusIcon, p.ID, detail)
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}
	return b.String()
}
