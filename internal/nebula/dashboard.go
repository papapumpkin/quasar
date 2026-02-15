package nebula

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// ANSI escape codes for dashboard rendering.
const (
	ansiReset     = "\033[0m"
	ansiBold      = "\033[1m"
	ansiDim       = "\033[2m"
	ansiGreen     = "\033[32m"
	ansiYellow    = "\033[33m"
	ansiRed       = "\033[31m"
	ansiCyan      = "\033[36m"
	ansiCursorUp  = "\033[%dA" // Move cursor up N lines
	ansiClearLine = "\033[2K"  // Clear entire line
)

// Dashboard renders a live-updating progress view of a nebula execution to a writer.
// It is designed to be used as the OnProgress callback in WorkerGroup.
type Dashboard struct {
	Writer       io.Writer // typically stderr
	Nebula       *Nebula
	State        *State
	MaxBudgetUSD float64
	IsTTY        bool // controls whether to use ANSI cursor movement
	AppendOnly   bool // when true, never use cursor movement (watch mode scroll-back)

	mu        sync.Mutex
	lineCount int  // number of lines rendered in the last draw (for cursor-up in TTY mode)
	rendered  bool // whether the dashboard has been rendered at least once
}

// NewDashboard creates a new Dashboard wired to the given nebula and state.
func NewDashboard(w io.Writer, n *Nebula, state *State, maxBudgetUSD float64, isTTY bool) *Dashboard {
	return &Dashboard{
		Writer:       w,
		Nebula:       n,
		State:        state,
		MaxBudgetUSD: maxBudgetUSD,
		IsTTY:        isTTY,
	}
}

// ProgressCallback returns a ProgressFunc suitable for use as WorkerGroup.OnProgress.
// It re-renders the full dashboard on each call.
func (d *Dashboard) ProgressCallback() ProgressFunc {
	return func(completed, total, openBeads, closedBeads int, totalCostUSD float64) {
		d.Render()
	}
}

// Render draws the full dashboard. Thread-safe.
// In AppendOnly mode (watch), always uses plain rendering for scroll-back compatibility.
func (d *Dashboard) Render() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.AppendOnly || !d.IsTTY {
		d.renderPlain()
	} else {
		d.renderTTY()
	}
}

// Pause clears the dashboard state so that gate prompts or other output
// can write to stderr without visual conflicts. Thread-safe.
// In AppendOnly mode this is a no-op because there is no cursor movement to undo.
func (d *Dashboard) Pause() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.AppendOnly {
		return
	}

	if d.IsTTY && d.rendered && d.lineCount > 0 {
		// Move cursor up and clear each line to remove the dashboard.
		for i := 0; i < d.lineCount; i++ {
			fmt.Fprintf(d.Writer, "\033[1A"+ansiClearLine)
		}
	}
	d.rendered = false
	d.lineCount = 0
}

// Resume re-renders the dashboard after a pause.
func (d *Dashboard) Resume() {
	d.Render()
}

// renderTTY draws the dashboard using ANSI cursor movement to overwrite previous output.
func (d *Dashboard) renderTTY() {
	// Move cursor up to overwrite previous render.
	if d.rendered && d.lineCount > 0 {
		fmt.Fprintf(d.Writer, ansiCursorUp, d.lineCount)
	}

	lines := d.buildLines()
	for _, line := range lines {
		fmt.Fprintf(d.Writer, ansiClearLine+"%s\n", line)
	}
	d.lineCount = len(lines)
	d.rendered = true
}

// renderPlain prints a simple one-line status update per call (no cursor movement).
func (d *Dashboard) renderPlain() {
	completed, active, total := d.countStatuses()
	fmt.Fprintf(d.Writer, "[nebula] %d/%d done, %d active | $%.2f spent\n",
		completed, total, active, d.State.TotalCostUSD)
}

// buildLines constructs the dashboard output as a slice of formatted lines.
func (d *Dashboard) buildLines() []string {
	completed, active, total := d.countStatuses()
	graph := NewGraph(d.Nebula.Phases)

	var lines []string

	// Header.
	header := fmt.Sprintf("%s%sNebula: %s%s          [%d/%d done, %d active]",
		ansiBold, ansiCyan, d.Nebula.Manifest.Nebula.Name, ansiReset,
		completed, total, active)
	lines = append(lines, header)

	// Separator.
	lines = append(lines, ansiDim+"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"+ansiReset)

	// Phase lines.
	for i, phase := range d.Nebula.Phases {
		ps := d.State.Phases[phase.ID]
		status := PhaseStatusPending
		if ps != nil {
			status = ps.Status
		}

		isBlocked := d.isBlocked(phase.ID, graph)
		icon := statusIcon(status, isBlocked)
		suffix := d.phaseSuffix(phase.ID, graph, status, isBlocked)

		line := fmt.Sprintf("  %s %02d %s%s",
			icon, i+1, phase.ID, suffix)
		lines = append(lines, line)
	}

	// Separator.
	lines = append(lines, ansiDim+"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"+ansiReset)

	// Budget line.
	budget := fmt.Sprintf("  Budget: $%.2f", d.State.TotalCostUSD)
	if d.MaxBudgetUSD > 0 {
		budget += fmt.Sprintf(" / $%.2f", d.MaxBudgetUSD)
	}
	lines = append(lines, budget)

	return lines
}

// countStatuses returns completed, active, and total phase counts.
func (d *Dashboard) countStatuses() (completed, active, total int) {
	total = len(d.Nebula.Phases)
	for _, phase := range d.Nebula.Phases {
		ps := d.State.Phases[phase.ID]
		if ps == nil {
			continue
		}
		switch ps.Status {
		case PhaseStatusDone, PhaseStatusFailed:
			completed++
		case PhaseStatusInProgress, PhaseStatusCreated:
			active++
		}
	}
	return completed, active, total
}

// isBlocked reports whether a phase has any unfinished dependency.
func (d *Dashboard) isBlocked(phaseID string, graph *Graph) bool {
	deps, ok := graph.adjacency[phaseID]
	if !ok {
		return false
	}
	for dep := range deps {
		ps := d.State.Phases[dep]
		if ps == nil || (ps.Status != PhaseStatusDone && ps.Status != PhaseStatusFailed) {
			return true
		}
	}
	return false
}

// phaseSuffix builds the trailing info for a phase line (cost, cycles, blocked deps).
func (d *Dashboard) phaseSuffix(phaseID string, graph *Graph, status PhaseStatus, isBlocked bool) string {
	var parts []string

	// Cost info for done/in-progress phases (future: track per-phase cost if available).
	// Currently we don't have per-phase cost tracking in PhaseState, so omit cost.

	if isBlocked && status == PhaseStatusPending {
		// Show which phases block this one.
		deps := graph.adjacency[phaseID]
		var blocking []string
		for dep := range deps {
			ps := d.State.Phases[dep]
			if ps == nil || (ps.Status != PhaseStatusDone && ps.Status != PhaseStatusFailed) {
				blocking = append(blocking, dep)
			}
		}
		if len(blocking) > 0 {
			parts = append(parts, fmt.Sprintf("(blocked: %s)", strings.Join(blocking, ", ")))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, "  ")
}

// statusIcon returns the colored status indicator for a phase.
func statusIcon(status PhaseStatus, isBlocked bool) string {
	switch status {
	case PhaseStatusDone:
		return ansiGreen + "[done]" + ansiReset
	case PhaseStatusInProgress:
		return ansiCyan + "[>>>>]" + ansiReset
	case PhaseStatusCreated:
		return ansiCyan + "[>>>>]" + ansiReset
	case PhaseStatusFailed:
		return ansiRed + ansiBold + "[FAIL]" + ansiReset
	case PhaseStatusPending:
		if isBlocked {
			return ansiYellow + "[gate]" + ansiReset
		}
		return ansiDim + "[wait]" + ansiReset
	default:
		return ansiDim + "[skip]" + ansiReset
	}
}
