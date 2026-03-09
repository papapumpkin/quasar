package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/nebula"
)

// DashboardData is the template context for the main dashboard page.
type DashboardData struct {
	NebulaName  string
	Phases      []PhaseRow
	Completed   int
	Total       int
	TotalCost   float64
	BudgetUSD   float64
	ElapsedSec  int
	ProgressPct int
}

// PhaseRow mirrors tui.PhaseEntry for HTML rendering.
type PhaseRow struct {
	ID         string
	Title      string
	Status     string // "pending", "in_progress", "done", "failed", "skipped", "decomposed"
	StatusIcon string // Unicode icon matching TUI: checkmark, spinner dot, X, etc.
	CostUSD    string // formatted to 4 decimal places
	Cycles     string // "2/5" format
	Wave       int
	BlockedBy  []string
	DependsOn  []string
}

// statusIcon maps a nebula PhaseStatus to a Unicode icon matching the TUI.
func statusIcon(status nebula.PhaseStatus) string {
	switch status {
	case nebula.PhaseStatusDone:
		return "\u2713" // ✓
	case nebula.PhaseStatusInProgress, nebula.PhaseStatusCreated:
		return "\u25ce" // ◎
	case nebula.PhaseStatusFailed:
		return "\u2717" // ✗
	case nebula.PhaseStatusSkipped:
		return "\u2013" // –
	case nebula.PhaseStatusDecomposed:
		return "\u2261" // ≡
	default:
		return "\u00b7" // ·
	}
}

// statusString normalises a PhaseStatus for CSS class usage.
// Defaults to "pending" for unknown or zero-value statuses.
func statusString(status nebula.PhaseStatus) string {
	switch status {
	case nebula.PhaseStatusDone, nebula.PhaseStatusFailed,
		nebula.PhaseStatusSkipped, nebula.PhaseStatusDecomposed,
		nebula.PhaseStatusInProgress, nebula.PhaseStatusCreated:
		return string(status)
	default:
		return "pending"
	}
}

// handleDashboard renders the main dashboard page.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	s.mu.RLock()
	neb := s.nebula
	state := s.state
	startTime := s.startTime
	s.mu.RUnlock()

	data := buildDashboardData(neb, state, startTime)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// buildDashboardData transforms nebula and state into a template-ready view model.
func buildDashboardData(neb *nebula.Nebula, state *nebula.State, startTime time.Time) DashboardData {
	if neb == nil {
		return DashboardData{}
	}
	if state == nil {
		state = &nebula.State{Phases: make(map[string]*nebula.PhaseState)}
	}

	// Build DAG for wave assignment and blocked-by computation.
	dg, waves := buildDAGAndWaves(neb.Phases)
	waveForPhase := mapPhasesToWaves(waves)

	total := len(neb.Phases)
	completed := 0
	rows := make([]PhaseRow, 0, total)

	for _, phase := range neb.Phases {
		ps := state.Phases[phase.ID]
		status := nebula.PhaseStatusPending
		if ps != nil {
			status = ps.Status
		}

		if status == nebula.PhaseStatusDone || status == nebula.PhaseStatusSkipped {
			completed++
		}

		maxCycles := phase.MaxReviewCycles
		if maxCycles == 0 {
			maxCycles = neb.Manifest.Execution.MaxReviewCycles
		}
		if maxCycles == 0 {
			maxCycles = 5
		}

		cycles := "0/" + fmt.Sprintf("%d", maxCycles)

		blockedBy := computeBlockedBy(phase.ID, phase.DependsOn, state, dg)

		rows = append(rows, PhaseRow{
			ID:         phase.ID,
			Title:      phase.Title,
			Status:     statusString(status),
			StatusIcon: statusIcon(status),
			CostUSD:    fmt.Sprintf("%.4f", 0.0),
			Cycles:     cycles,
			Wave:       waveForPhase[phase.ID],
			BlockedBy:  blockedBy,
			DependsOn:  phase.DependsOn,
		})
	}

	progressPct := 0
	if total > 0 {
		progressPct = (completed * 100) / total
	}

	elapsedSec := 0
	if !startTime.IsZero() {
		elapsedSec = int(time.Since(startTime).Seconds())
	}

	return DashboardData{
		NebulaName:  neb.Manifest.Nebula.Name,
		Phases:      rows,
		Completed:   completed,
		Total:       total,
		TotalCost:   state.TotalCostUSD,
		BudgetUSD:   neb.Manifest.Execution.MaxBudgetUSD,
		ElapsedSec:  elapsedSec,
		ProgressPct: progressPct,
	}
}

// buildDAGAndWaves constructs a DAG from phase specs and computes waves.
// Returns a nil DAG and empty waves on error (best-effort).
func buildDAGAndWaves(phases []nebula.PhaseSpec) (*dag.DAG, []dag.Wave) {
	dg := dag.New()
	for _, p := range phases {
		dg.AddNodeIdempotent(p.ID, p.Priority)
	}
	for _, p := range phases {
		for _, dep := range p.DependsOn {
			if err := dg.AddEdge(dep, p.ID); err != nil {
				return dg, nil
			}
		}
	}
	waves, _ := dg.ComputeWaves()
	return dg, waves
}

// mapPhasesToWaves creates a phaseID -> wave number mapping.
func mapPhasesToWaves(waves []dag.Wave) map[string]int {
	m := make(map[string]int)
	for _, w := range waves {
		for _, id := range w.NodeIDs {
			m[id] = w.Number
		}
	}
	return m
}

// computeBlockedBy returns the list of unfinished dependencies for a phase.
func computeBlockedBy(phaseID string, dependsOn []string, state *nebula.State, dg *dag.DAG) []string {
	var blocked []string
	deps := dependsOn
	if dg != nil {
		deps = dg.DepsFor(phaseID)
	}
	for _, dep := range deps {
		ps := state.Phases[dep]
		if ps == nil || (ps.Status != nebula.PhaseStatusDone &&
			ps.Status != nebula.PhaseStatusFailed &&
			ps.Status != nebula.PhaseStatusSkipped) {
			blocked = append(blocked, dep)
		}
	}
	return blocked
}
