package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/nebula"
)

// --- Nebula-specific output ---

// NebulaValidateResult prints the validation outcome for a nebula.
func (p *Printer) NebulaValidateResult(name string, phaseCount int, errs []nebula.ValidationError) {
	if len(errs) == 0 {
		fmt.Fprintf(os.Stderr, green+bold+"✓ nebula %q"+reset+" — %d phase(s), no errors\n", name, phaseCount)
		return
	}
	fmt.Fprintf(os.Stderr, red+bold+"✗ nebula %q"+reset+" — %d error(s):\n", name, len(errs))
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "  "+red+"• "+reset+"%s\n", e.Error())
	}
}

// NebulaPlan prints a formatted plan of nebula actions to stderr.
func (p *Printer) NebulaPlan(plan *nebula.Plan) {
	fmt.Fprintf(os.Stderr, "\n"+bold+cyan+"nebula plan: %s"+reset+"\n", plan.NebulaName)
	if len(plan.Actions) == 0 {
		fmt.Fprintln(os.Stderr, dim+"  (no actions)"+reset)
		return
	}
	for _, a := range plan.Actions {
		var symbol, color string
		switch a.Type {
		case nebula.ActionCreate:
			symbol, color = "+", green
		case nebula.ActionUpdate:
			symbol, color = "~", yellow
		case nebula.ActionSkip:
			symbol, color = "-", dim
		case nebula.ActionClose:
			symbol, color = "×", red
		case nebula.ActionRetry:
			symbol, color = "↻", yellow
		}
		fmt.Fprintf(os.Stderr, "  "+color+symbol+" %-20s"+reset+" %s\n", a.PhaseID, a.Reason)
	}
	fmt.Fprintln(os.Stderr)
}

// NebulaApplyDone prints a summary of completed apply actions.
func (p *Printer) NebulaApplyDone(plan *nebula.Plan) {
	var created, updated, closed, skipped, retried int
	for _, a := range plan.Actions {
		switch a.Type {
		case nebula.ActionCreate:
			created++
		case nebula.ActionUpdate:
			updated++
		case nebula.ActionClose:
			closed++
		case nebula.ActionSkip:
			skipped++
		case nebula.ActionRetry:
			retried++
		}
	}
	fmt.Fprintf(os.Stderr, green+bold+"✓ apply complete"+reset+" — created: %d, updated: %d, retried: %d, closed: %d, skipped: %d\n",
		created, updated, retried, closed, skipped)
}

// NebulaWorkerResults prints the outcome of each worker task execution.
func (p *Printer) NebulaWorkerResults(results []nebula.WorkerResult) {
	fmt.Fprintln(os.Stderr, "\n"+bold+"worker results:"+reset)
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "  "+red+"✗ %s"+reset+" — %v\n", r.PhaseID, r.Err)
		} else {
			fmt.Fprintf(os.Stderr, "  "+green+"✓ %s"+reset+" (bead %s)\n", r.PhaseID, r.BeadID)
			if r.Report != nil {
				p.ReviewReport(r.PhaseID, r.Report)
			}
		}
	}
}

// ReviewReport prints structured review metadata for a phase.
func (p *Printer) ReviewReport(phaseID string, report *agent.ReviewReport) {
	fmt.Fprintf(os.Stderr, dim+"  report for %s:"+reset+"\n", phaseID)
	fmt.Fprintf(os.Stderr, "    satisfaction:  %s\n", report.Satisfaction)
	fmt.Fprintf(os.Stderr, "    risk:          %s\n", report.Risk)
	humanReview := "no"
	if report.NeedsHumanReview {
		humanReview = yellow + "yes" + reset
	}
	fmt.Fprintf(os.Stderr, "    human review:  %s\n", humanReview)
	fmt.Fprintf(os.Stderr, "    summary:       %s\n", report.Summary)
}

// NebulaShow prints a detailed overview of a nebula and its phase states.
func (p *Printer) NebulaShow(n *nebula.Nebula, state *nebula.State) {
	fmt.Fprintf(os.Stderr, bold+cyan+"nebula: %s"+reset+"\n", n.Manifest.Nebula.Name)
	if n.Manifest.Nebula.Description != "" {
		fmt.Fprintf(os.Stderr, dim+"%s"+reset+"\n", n.Manifest.Nebula.Description)
	}
	fmt.Fprintf(os.Stderr, "phases: %d\n\n", len(n.Phases))

	// Display execution config if any fields are set.
	exec := n.Manifest.Execution
	if exec.MaxWorkers > 0 || exec.MaxReviewCycles > 0 || exec.MaxBudgetUSD > 0 || exec.Model != "" {
		fmt.Fprintf(os.Stderr, bold+"execution:"+reset+"\n")
		if exec.MaxWorkers > 0 {
			fmt.Fprintf(os.Stderr, "  max workers:       %d\n", exec.MaxWorkers)
		}
		if exec.MaxReviewCycles > 0 {
			fmt.Fprintf(os.Stderr, "  max review cycles: %d\n", exec.MaxReviewCycles)
		}
		if exec.MaxBudgetUSD > 0 {
			fmt.Fprintf(os.Stderr, "  max budget:        $%.2f\n", exec.MaxBudgetUSD)
		}
		if exec.Model != "" {
			fmt.Fprintf(os.Stderr, "  model:             %s\n", exec.Model)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Display context if any fields are set.
	ctx := n.Manifest.Context
	if ctx.Repo != "" || len(ctx.Goals) > 0 || len(ctx.Constraints) > 0 {
		fmt.Fprintf(os.Stderr, bold+"context:"+reset+"\n")
		if ctx.Repo != "" {
			fmt.Fprintf(os.Stderr, "  repo: %s\n", ctx.Repo)
		}
		if ctx.WorkingDir != "" {
			fmt.Fprintf(os.Stderr, "  working dir: %s\n", ctx.WorkingDir)
		}
		if len(ctx.Goals) > 0 {
			fmt.Fprintf(os.Stderr, "  goals:\n")
			for _, g := range ctx.Goals {
				fmt.Fprintf(os.Stderr, "    - %s\n", g)
			}
		}
		if len(ctx.Constraints) > 0 {
			fmt.Fprintf(os.Stderr, "  constraints:\n")
			for _, c := range ctx.Constraints {
				fmt.Fprintf(os.Stderr, "    - %s\n", c)
			}
		}
		fmt.Fprintln(os.Stderr)
	}

	// Display dependencies if any are set.
	deps := n.Manifest.Dependencies
	if len(deps.RequiresBeads) > 0 || len(deps.RequiresNebulae) > 0 {
		fmt.Fprintf(os.Stderr, bold+"dependencies:"+reset+"\n")
		if len(deps.RequiresBeads) > 0 {
			fmt.Fprintf(os.Stderr, "  requires beads:   %s\n", strings.Join(deps.RequiresBeads, ", "))
		}
		if len(deps.RequiresNebulae) > 0 {
			fmt.Fprintf(os.Stderr, "  requires nebulae: %s\n", strings.Join(deps.RequiresNebulae, ", "))
		}
		fmt.Fprintln(os.Stderr)
	}

	for _, t := range n.Phases {
		ts, hasState := state.Phases[t.ID]
		status := "pending"
		beadID := ""
		if hasState {
			status = string(ts.Status)
			beadID = ts.BeadID
		}

		var deps string
		if len(t.DependsOn) > 0 {
			deps = " depends:[" + strings.Join(t.DependsOn, ",") + "]"
		}

		var beadStr string
		if beadID != "" {
			beadStr = " bead:" + beadID
		}

		fmt.Fprintf(os.Stderr, "  %-20s %-12s %s%s%s\n", t.ID, status, t.Title, deps, beadStr)
		if hasState && ts.Report != nil {
			fmt.Fprintf(os.Stderr, "    "+dim+"satisfaction:%s risk:%s human-review:%v"+reset+"\n",
				ts.Report.Satisfaction, ts.Report.Risk, ts.Report.NeedsHumanReview)
		}
	}
}

// NebulaProgressBarLine formats a progress line string (without ANSI escape prefix).
// Format matches the spec: [nebula] 3/7 phases complete | $2.34 spent
// This is exported for testing.
func NebulaProgressBarLine(completed, total, openBeads, closedBeads int, totalCostUSD float64) string {
	return fmt.Sprintf("[nebula] %d/%d phases complete | $%.2f spent", completed, total, totalCostUSD)
}

// NebulaProgressBar writes a carriage-return-overwritten progress line to stderr.
// It uses \r to overwrite the current line (no newline) so the bar updates in place.
func (p *Printer) NebulaProgressBar(completed, total, openBeads, closedBeads int, totalCostUSD float64) {
	line := NebulaProgressBarLine(completed, total, openBeads, closedBeads, totalCostUSD)
	// \r returns to start of line; padding clears any leftover characters from previous line.
	fmt.Fprintf(os.Stderr, "\r"+cyan+"%s"+reset+"   ", line)
}

// NebulaProgressBarDone writes a final newline after the progress bar so
// subsequent output doesn't overwrite it.
func (p *Printer) NebulaProgressBarDone() {
	fmt.Fprintln(os.Stderr)
}

// NebulaStatus renders a metrics summary for a nebula run to stderr.
// It gracefully handles nil metrics by falling back to state-only information.
func (p *Printer) NebulaStatus(n *nebula.Nebula, state *nebula.State, m *nebula.Metrics, history []nebula.HistorySummary) {
	name := n.Manifest.Nebula.Name

	if m != nil && !m.CompletedAt.IsZero() {
		fmt.Fprintf(os.Stderr, bold+cyan+"nebula %q"+reset+" — last run %s\n\n", name, m.CompletedAt.Format(time.RFC3339))
	} else if m != nil && !m.StartedAt.IsZero() {
		fmt.Fprintf(os.Stderr, bold+cyan+"nebula %q"+reset+" — started %s (in progress)\n\n", name, m.StartedAt.Format(time.RFC3339))
	} else {
		fmt.Fprintf(os.Stderr, bold+cyan+"nebula %q"+reset+" — no metrics recorded\n\n", name)
	}

	// Phase counts from state.
	completed, failed := 0, 0
	for _, ps := range state.Phases {
		switch ps.Status {
		case nebula.PhaseStatusDone:
			completed++
		case nebula.PhaseStatusFailed:
			failed++
		}
	}

	restarts := 0
	if m != nil {
		restarts = m.TotalRestarts
	}
	fmt.Fprintf(os.Stderr, "  Phases:  %d completed, %d failed, %d restarts\n", completed, failed, restarts)

	// Waves.
	if m != nil && len(m.Waves) > 0 {
		avgParallelism := nebulaAvgParallelism(m.Waves)
		fmt.Fprintf(os.Stderr, "  Waves:   %d (avg effective parallelism: %.1f)\n", len(m.Waves), avgParallelism)
	} else {
		fmt.Fprintf(os.Stderr, "  Waves:   0\n")
	}

	// Cost.
	totalCost := state.TotalCostUSD
	if m != nil && m.TotalCostUSD > 0 {
		totalCost = m.TotalCostUSD
	}
	totalPhases := len(n.Phases)
	avgCost := 0.0
	if totalPhases > 0 {
		avgCost = totalCost / float64(totalPhases)
	}
	fmt.Fprintf(os.Stderr, "  Cost:    $%.2f (avg $%.2f/phase)\n", totalCost, avgCost)

	// Duration.
	if m != nil && !m.StartedAt.IsZero() && !m.CompletedAt.IsZero() {
		dur := m.CompletedAt.Sub(m.StartedAt)
		fmt.Fprintf(os.Stderr, "  Duration: %s (wall-clock)\n", formatDuration(dur))
	}

	// Conflicts.
	if m != nil {
		fmt.Fprintf(os.Stderr, "  Conflicts: %d\n", m.TotalConflicts)
	}

	// Wave breakdown.
	if m != nil && len(m.Waves) > 0 {
		fmt.Fprintf(os.Stderr, "\n  Wave breakdown:\n")
		for _, w := range m.Waves {
			note := ""
			if w.EffectiveParallelism < w.PhaseCount {
				note = " (scope serialization)"
			}
			fmt.Fprintf(os.Stderr, "    Wave %d: %d phases, parallelism %d/%d%s, %s\n",
				w.WaveNumber, w.PhaseCount, w.EffectiveParallelism, w.PhaseCount, note,
				formatDuration(w.TotalDuration))
		}
	}

	// Slowest phases.
	if m != nil && len(m.Phases) > 0 {
		sorted := make([]nebula.PhaseMetrics, len(m.Phases))
		copy(sorted, m.Phases)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Duration > sorted[j].Duration
		})
		limit := 5
		if len(sorted) < limit {
			limit = len(sorted)
		}
		fmt.Fprintf(os.Stderr, "\n  Slowest phases:\n")
		for _, pm := range sorted[:limit] {
			sat := pm.Satisfaction
			if sat == "" {
				sat = "-"
			}
			fmt.Fprintf(os.Stderr, "    %-24s %s  $%.2f  %d cycles  satisfaction: %s\n",
				pm.PhaseID, formatDuration(pm.Duration), pm.CostUSD, pm.CyclesUsed, sat)
		}
	}

	// History — entries are oldest-first, so take from the end for most recent.
	if len(history) > 0 {
		limit := 3
		if len(history) < limit {
			limit = len(history)
		}
		recent := history[len(history)-limit:]
		fmt.Fprintf(os.Stderr, "\n  History (last %d run%s):\n", limit, pluralS(limit))
		for _, h := range recent {
			fmt.Fprintf(os.Stderr, "    %s  %d phases  $%.2f  %s  %d conflict%s\n",
				h.StartedAt.Format("2006-01-02 15:04"),
				h.TotalPhases, h.TotalCostUSD,
				formatDuration(h.Duration),
				h.TotalConflicts, pluralS(h.TotalConflicts))
		}
	}

	fmt.Fprintln(os.Stderr)
}

// nebulaAvgParallelism computes the average effective parallelism across waves.
func nebulaAvgParallelism(waves []nebula.WaveMetrics) float64 {
	if len(waves) == 0 {
		return 0
	}
	total := 0
	for _, w := range waves {
		total += w.EffectiveParallelism
	}
	return float64(total) / float64(len(waves))
}

// formatDuration formats a duration as a human-readable string like "4m32s".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	totalSeconds := int(d.Seconds())
	h := totalSeconds / 3600
	m := (totalSeconds % 3600) / 60
	s := totalSeconds % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}

// pluralS returns "s" if n != 1, for simple English pluralization.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
