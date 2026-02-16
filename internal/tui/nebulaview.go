package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
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
	StartedAt time.Time
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
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(colorBlue)
	return NebulaView{Spinner: s}
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
			if status == PhaseWorking && nv.Phases[i].StartedAt.IsZero() {
				nv.Phases[i].StartedAt = time.Now()
			}
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
		selected := i == nv.Cursor
		indicator := "  "
		if selected {
			indicator = styleSelectionIndicator.Render(selectionIndicator) + " "
		}

		var statusIcon string
		var style = styleRowNormal
		switch p.Status {
		case PhaseDone:
			statusIcon = styleRowDone.Render(iconDone)
			style = styleRowDone
		case PhaseWorking:
			statusIcon = styleRowWorking.Render(iconWorking) + " " + nv.Spinner.View()
			style = styleRowWorking
		case PhaseFailed:
			statusIcon = styleRowFailed.Render(iconFailed)
			style = styleRowFailed
		case PhaseGate:
			statusIcon = styleRowGate.Render(iconGate)
			style = styleRowGate
		case PhaseSkipped:
			statusIcon = styleRowWaiting.Render(iconSkipped)
			style = styleRowWaiting
		default:
			statusIcon = styleRowWaiting.Render(iconWaiting)
			style = styleRowWaiting
		}

		if selected {
			style = styleRowSelected
		}

		detail := ""
		if p.Status == PhaseDone {
			detail = fmt.Sprintf("W%d  $%.2f  %d cycle(s)", p.Wave, p.CostUSD, p.Cycles)
		} else if p.Status == PhaseWorking {
			elapsed := formatElapsed(p.StartedAt)
			detail = fmt.Sprintf("W%d  working… %s", p.Wave, elapsed)
		} else if p.BlockedBy != "" {
			detail = fmt.Sprintf("blocked: %s", p.BlockedBy)
		}

		// Truncate phase ID to fit available width.
		phaseID := p.ID
		idWidth := 24
		if nv.Width < CompactWidth && nv.Width > 0 {
			// In compact mode, shrink the ID column proportionally.
			idWidth = nv.Width / 3
			if idWidth < 8 {
				idWidth = 8
			}
		}
		phaseID = TruncateWithEllipsis(phaseID, idWidth)

		line := fmt.Sprintf("%s%s %-*s %s", indicator, statusIcon, idWidth, phaseID, detail)
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}
	return b.String()
}
