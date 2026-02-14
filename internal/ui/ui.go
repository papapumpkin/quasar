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
		}
		fmt.Fprintf(os.Stderr, "  "+color+symbol+" %-20s"+reset+" %s\n", a.TaskID, a.Reason)
	}
	fmt.Fprintln(os.Stderr)
}

func (p *Printer) NebulaApplyDone(plan *nebula.Plan) {
	var created, updated, closed, skipped int
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
		}
	}
	fmt.Fprintf(os.Stderr, green+bold+"✓ apply complete"+reset+" — created: %d, updated: %d, closed: %d, skipped: %d\n",
		created, updated, closed, skipped)
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
