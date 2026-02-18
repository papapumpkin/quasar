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
	ID         string
	Title      string
	Status     PhaseStatus
	Wave       int
	CostUSD    float64
	Cycles     int
	MaxCycles  int
	BlockedBy  string
	DependsOn  []string // original dependency IDs from the phase spec
	StartedAt  time.Time
	PlanBody   string // markdown content from the phase file
	Refactored bool   // true when a mid-run refactor was applied this cycle
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
			DependsOn: p.DependsOn,
			PlanBody:  p.PlanBody,
		}
	}
}

// AppendPhase adds a hot-added phase to the end of the phase table.
func (nv *NebulaView) AppendPhase(info PhaseInfo) {
	blocked := ""
	if len(info.DependsOn) > 0 {
		blocked = info.DependsOn[0]
		if len(info.DependsOn) > 1 {
			blocked += fmt.Sprintf(" +%d", len(info.DependsOn)-1)
		}
	}
	nv.Phases = append(nv.Phases, PhaseEntry{
		ID:        info.ID,
		Title:     info.Title,
		Status:    PhaseWaiting,
		BlockedBy: blocked,
		DependsOn: info.DependsOn,
		PlanBody:  info.PlanBody,
	})
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

// SetPhaseCycles updates the cycle count and max cycles of a phase by ID.
func (nv *NebulaView) SetPhaseCycles(phaseID string, cycles, maxCycles int) {
	for i := range nv.Phases {
		if nv.Phases[i].ID == phaseID {
			nv.Phases[i].Cycles = cycles
			nv.Phases[i].MaxCycles = maxCycles
			return
		}
	}
}

// SetPhaseRefactored marks a phase as having received a mid-run refactor.
func (nv *NebulaView) SetPhaseRefactored(phaseID string, refactored bool) {
	for i := range nv.Phases {
		if nv.Phases[i].ID == phaseID {
			nv.Phases[i].Refactored = refactored
			return
		}
	}
}

// View renders the phase table with wave separators and aligned columns.
func (nv NebulaView) View() string {
	var b strings.Builder
	lastWave := -1
	for i, p := range nv.Phases {
		// Wave separator when wave changes.
		if p.Wave > 0 && p.Wave != lastWave {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(nv.renderWaveHeader(p.Wave))
			b.WriteString("\n")
		}
		lastWave = p.Wave

		b.WriteString(nv.renderPhaseRow(i, p))
		b.WriteString("\n")
	}
	return b.String()
}

// renderWaveHeader renders a subtle wave separator line.
func (nv NebulaView) renderWaveHeader(wave int) string {
	label := fmt.Sprintf("── Wave %d ──", wave)
	return "  " + styleWaveHeader.Render(label)
}

// renderPhaseRow renders a single phase row with aligned columns.
// The phase ID is rendered in a brighter/bolder style while status
// detail (cycles, elapsed, cost) uses a muted style for easy scanning.
// The selected row uses a blue indicator bar on the left edge — no full-width background.
func (nv NebulaView) renderPhaseRow(i int, p PhaseEntry) string {
	selected := i == nv.Cursor
	indicator := "  "
	if selected {
		indicator = styleSelectionIndicator.Render(selectionIndicator) + " "
	}

	statusIcon, _ := nv.phaseIconAndStyle(p)

	// Truncate phase ID to fit available width.
	idWidth := 24
	if nv.Width < CompactWidth && nv.Width > 0 {
		idWidth = nv.Width / 3
		if idWidth < 8 {
			idWidth = 8
		}
	}
	phaseID := TruncateWithEllipsis(p.ID, idWidth)
	paddedID := fmt.Sprintf("%-*s", idWidth, phaseID)

	// Style the phase name prominently so it stands out.
	var styledID string
	if selected {
		styledID = styleRowSelected.Render(paddedID)
	} else {
		styledID = stylePhaseID.Render(paddedID)
	}

	// Style the detail text (cycles, cost, elapsed) with a muted treatment.
	detail := nv.phaseDetail(p)
	var styledDetail string
	if detail != "" {
		styledDetail = "  " + stylePhaseDetail.Render(detail)
	}

	row := fmt.Sprintf("%s%s %s%s", indicator, statusIcon, styledID, styledDetail)

	return row
}

// phaseIconAndStyle returns the status icon and row style for a phase.
func (nv NebulaView) phaseIconAndStyle(p PhaseEntry) (string, lipgloss.Style) {
	switch p.Status {
	case PhaseDone:
		return styleRowDone.Render(iconDone), styleRowDone
	case PhaseWorking:
		return styleRowWorking.Render(iconWorking), styleRowWorking
	case PhaseFailed:
		return styleRowFailed.Render(iconFailed), styleRowFailed
	case PhaseGate:
		return styleRowGate.Render(iconGate), styleRowGate
	case PhaseSkipped:
		return styleRowWaiting.Render(iconSkipped), styleRowWaiting
	default:
		return styleRowWaiting.Render(iconWaiting), styleRowWaiting
	}
}

// phaseDetail builds the detail text for a phase row.
func (nv NebulaView) phaseDetail(p PhaseEntry) string {
	switch p.Status {
	case PhaseDone:
		elapsed := formatElapsed(p.StartedAt)
		if elapsed != "" {
			return fmt.Sprintf("$%.2f  %d cycle(s)  %s", p.CostUSD, p.Cycles, elapsed)
		}
		return fmt.Sprintf("$%.2f  %d cycle(s)", p.CostUSD, p.Cycles)
	case PhaseWorking:
		elapsed := formatElapsed(p.StartedAt)
		cycleProgress := ""
		if p.MaxCycles > 0 {
			cycleProgress = fmt.Sprintf("cycle %d/%d", p.Cycles, p.MaxCycles)
		} else if p.Cycles > 0 {
			cycleProgress = fmt.Sprintf("cycle %d", p.Cycles)
		}
		parts := []string{}
		if p.Refactored {
			parts = append(parts, "⟳ refactored")
		}
		if cycleProgress != "" {
			parts = append(parts, cycleProgress)
		}
		if elapsed != "" {
			parts = append(parts, elapsed)
		}
		parts = append(parts, nv.Spinner.View())
		return strings.Join(parts, "  ")
	default:
		if p.BlockedBy != "" {
			return fmt.Sprintf("blocked: %s", p.BlockedBy)
		}
		return ""
	}
}
