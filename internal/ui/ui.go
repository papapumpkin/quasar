package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/aaronsalm/quasar/internal/nebula"
)

// ANSI color codes.
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	blue    = "\033[34m"
	yellow  = "\033[33m"
	green   = "\033[32m"
	red     = "\033[31m"
	cyan    = "\033[36m"
	magenta = "\033[35m"
)

type Printer struct{}

func New() *Printer {
	return &Printer{}
}

func (p *Printer) Banner() {
	fmt.Fprintln(os.Stderr, bold+cyan+"  ╔═══════════════════════════════════╗"+reset)
	fmt.Fprintln(os.Stderr, bold+cyan+"  ║"+reset+bold+"   QUASAR  "+dim+"dual-agent coordinator"+reset+bold+cyan+"  ║"+reset)
	fmt.Fprintln(os.Stderr, bold+cyan+"  ╚═══════════════════════════════════╝"+reset)
	fmt.Fprintln(os.Stderr)
}

func (p *Printer) Prompt() {
	fmt.Fprintf(os.Stderr, bold+cyan+"quasar> "+reset)
}

func (p *Printer) CycleStart(cycle, maxCycles int) {
	fmt.Fprintf(os.Stderr, "\n"+bold+magenta+"── cycle %d/%d ──"+reset+"\n", cycle, maxCycles)
}

func (p *Printer) AgentStart(role string) {
	color := blue
	if role == "reviewer" {
		color = yellow
	}
	fmt.Fprintf(os.Stderr, color+bold+"▶ %s"+reset+dim+" working..."+reset+"\n", role)
}

func (p *Printer) AgentDone(role string, costUSD float64, durationMs int64) {
	color := blue
	if role == "reviewer" {
		color = yellow
	}
	secs := float64(durationMs) / 1000.0
	fmt.Fprintf(os.Stderr, color+"✓ %s"+reset+dim+" done (%.1fs, $%.4f)"+reset+"\n", role, secs, costUSD)
}

func (p *Printer) IssuesFound(count int) {
	fmt.Fprintf(os.Stderr, yellow+bold+"⚠ %d issue(s) found"+reset+" — sending back to coder\n", count)
}

func (p *Printer) Approved() {
	fmt.Fprintln(os.Stderr, green+bold+"✓ APPROVED"+reset+" — reviewer is satisfied")
}

func (p *Printer) MaxCyclesReached(max int) {
	fmt.Fprintf(os.Stderr, red+bold+"✗ max cycles reached (%d)"+reset+" — stopping\n", max)
}

func (p *Printer) BudgetExceeded(spent, limit float64) {
	fmt.Fprintf(os.Stderr, red+bold+"✗ budget exceeded"+reset+" ($%.2f / $%.2f)\n", spent, limit)
}

func (p *Printer) Error(msg string) {
	fmt.Fprintf(os.Stderr, red+bold+"error: "+reset+"%s\n", msg)
}

func (p *Printer) Info(msg string) {
	fmt.Fprintf(os.Stderr, dim+"%s"+reset+"\n", msg)
}

func (p *Printer) TaskStarted(beadID, title string) {
	fmt.Fprintf(os.Stderr, cyan+"◆ task"+reset+" %s — %s\n", beadID, title)
}

func (p *Printer) TaskComplete(beadID string, totalCost float64) {
	fmt.Fprintf(os.Stderr, green+"◆ task complete"+reset+" %s "+dim+"(total: $%.4f)"+reset+"\n", beadID, totalCost)
}

func (p *Printer) ShowHelp() {
	lines := []string{
		bold + "Commands:" + reset,
		"  Type a task description to start a coder-reviewer cycle",
		"  " + bold + "help" + reset + "    — show this message",
		"  " + bold + "status" + reset + "  — show current config",
		"  " + bold + "quit" + reset + "    — exit quasar",
	}
	fmt.Fprintln(os.Stderr, strings.Join(lines, "\n"))
}

func (p *Printer) ShowStatus(maxCycles int, maxBudget float64, model string) {
	fmt.Fprintln(os.Stderr, dim+"config:"+reset)
	fmt.Fprintf(os.Stderr, "  max cycles:  %d\n", maxCycles)
	fmt.Fprintf(os.Stderr, "  max budget:  $%.2f\n", maxBudget)
	if model != "" {
		fmt.Fprintf(os.Stderr, "  model:       %s\n", model)
	} else {
		fmt.Fprintf(os.Stderr, "  model:       (default)\n")
	}
}

// --- Nebula-specific output ---

func (p *Printer) NebulaValidateResult(name string, taskCount int, errs []nebula.ValidationError) {
	if len(errs) == 0 {
		fmt.Fprintf(os.Stderr, green+bold+"✓ nebula %q"+reset+" — %d task(s), no errors\n", name, taskCount)
		return
	}
	fmt.Fprintf(os.Stderr, red+bold+"✗ nebula %q"+reset+" — %d error(s):\n", name, len(errs))
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "  "+red+"• "+reset+"%s\n", e.Error())
	}
}

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
		fmt.Fprintf(os.Stderr, "  "+color+symbol+" %-20s"+reset+" %s\n", a.TaskID, a.Reason)
	}
	fmt.Fprintln(os.Stderr)
}

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

func (p *Printer) NebulaWorkerResults(results []nebula.WorkerResult) {
	fmt.Fprintln(os.Stderr, "\n"+bold+"worker results:"+reset)
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "  "+red+"✗ %s"+reset+" — %v\n", r.TaskID, r.Err)
		} else {
			fmt.Fprintf(os.Stderr, "  "+green+"✓ %s"+reset+" (bead %s)\n", r.TaskID, r.BeadID)
			if r.Report != nil {
				p.ReviewReport(r.TaskID, r.Report)
			}
		}
	}
}

func (p *Printer) ReviewReport(taskID string, report *nebula.ReviewReport) {
	fmt.Fprintf(os.Stderr, dim+"  report for %s:"+reset+"\n", taskID)
	fmt.Fprintf(os.Stderr, "    satisfaction:  %s\n", report.Satisfaction)
	fmt.Fprintf(os.Stderr, "    risk:          %s\n", report.Risk)
	humanReview := "no"
	if report.NeedsHumanReview {
		humanReview = yellow + "yes" + reset
	}
	fmt.Fprintf(os.Stderr, "    human review:  %s\n", humanReview)
	fmt.Fprintf(os.Stderr, "    summary:       %s\n", report.Summary)
}

func (p *Printer) NebulaShow(n *nebula.Nebula, state *nebula.State) {
	fmt.Fprintf(os.Stderr, bold+cyan+"nebula: %s"+reset+"\n", n.Manifest.Nebula.Name)
	if n.Manifest.Nebula.Description != "" {
		fmt.Fprintf(os.Stderr, dim+"%s"+reset+"\n", n.Manifest.Nebula.Description)
	}
	fmt.Fprintf(os.Stderr, "tasks: %d\n\n", len(n.Tasks))

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

	for _, t := range n.Tasks {
		ts, hasState := state.Tasks[t.ID]
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

// CycleSummaryData holds data needed to render a cycle summary.
// This struct lives in the ui package to avoid circular imports with loop.
type CycleSummaryData struct {
	Cycle        int
	MaxCycles    int
	Phase        string // e.g. "code_complete", "review_complete"
	CostUSD      float64
	TotalCostUSD float64
	MaxBudgetUSD float64
	DurationMs   int64
	Approved     bool
	IssueCount   int
}

// CycleSummary prints a structured summary after each coder/reviewer phase.
func (p *Printer) CycleSummary(d CycleSummaryData) {
	role := "coder"
	roleColor := blue
	if d.Phase == "review_complete" {
		role = "reviewer"
		roleColor = yellow
	}

	secs := float64(d.DurationMs) / 1000.0

	fmt.Fprintf(os.Stderr, "\n"+dim+"┌─ "+reset+bold+"Cycle %d/%d"+reset+dim+" ── %s%s%s%s ─────────────────"+reset+"\n",
		d.Cycle, d.MaxCycles, roleColor, bold, role, reset)

	// Cost line.
	budgetPct := 0.0
	if d.MaxBudgetUSD > 0 {
		budgetPct = (d.TotalCostUSD / d.MaxBudgetUSD) * 100
	}
	fmt.Fprintf(os.Stderr, dim+"│"+reset+"  cost: $%.4f this phase, "+bold+"$%.4f"+reset+" total",
		d.CostUSD, d.TotalCostUSD)
	if d.MaxBudgetUSD > 0 {
		fmt.Fprintf(os.Stderr, dim+" (%.0f%% of $%.2f budget)"+reset, budgetPct, d.MaxBudgetUSD)
	}
	fmt.Fprintln(os.Stderr)

	// Duration line.
	fmt.Fprintf(os.Stderr, dim+"│"+reset+"  duration: %.1fs\n", secs)

	// Outcome line (only for reviewer).
	if d.Phase == "review_complete" {
		if d.Approved {
			fmt.Fprintf(os.Stderr, dim+"│"+reset+"  outcome: "+green+bold+"approved"+reset+"\n")
		} else {
			fmt.Fprintf(os.Stderr, dim+"│"+reset+"  outcome: "+yellow+"%d issue(s) found"+reset+"\n", d.IssueCount)
		}
	}

	fmt.Fprintln(os.Stderr, dim+"└──────────────────────────────────────────"+reset)
}

// NebulaProgressBarLine formats a progress line string (without ANSI escape prefix).
// Format matches the spec: [nebula] 3/7 tasks complete | $2.34 spent
// This is exported for testing.
func NebulaProgressBarLine(completed, total, openBeads, closedBeads int, totalCostUSD float64) string {
	return fmt.Sprintf("[nebula] %d/%d tasks complete | $%.2f spent", completed, total, totalCostUSD)
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
