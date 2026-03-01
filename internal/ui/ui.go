package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/papapumpkin/quasar/internal/ansi"
)

// Package-level aliases for ANSI constants from the ansi package.
// These keep method bodies concise while using the single source of truth.
const (
	reset   = ansi.Reset
	bold    = ansi.Bold
	dim     = ansi.Dim
	blue    = ansi.Blue
	yellow  = ansi.Yellow
	green   = ansi.Green
	red     = ansi.Red
	cyan    = ansi.Cyan
	magenta = ansi.Magenta
)

// ANSIClearLine clears the entire current line.
// Deprecated: use ansi.ClearLine directly.
const ANSIClearLine = ansi.ClearLine

// ANSICursorUp returns an ANSI escape sequence to move the cursor up n lines.
// Deprecated: use ansi.CursorUp directly.
func ANSICursorUp(n int) string {
	return ansi.CursorUp(n)
}

// UI defines the interface for user-facing output during a coder-reviewer loop.
// Consumers (e.g. loop.Loop) depend on this interface rather than the concrete Printer.
type UI interface {
	TaskStarted(beadID, title string)
	TaskComplete(beadID string, totalCost float64)
	CycleStart(cycle, maxCycles int)
	AgentStart(role string)
	AgentDone(role string, costUSD float64, durationMs int64)
	CycleSummary(d CycleSummaryData)
	IssuesFound(count int)
	Approved()
	MaxCyclesReached(max int)
	BudgetExceeded(spent, limit float64)
	Error(msg string)
	Info(msg string)
	AgentOutput(role string, cycle int, output string)
	BeadUpdate(taskBeadID, title, status string, children []BeadChild)
	RefactorApplied(phaseID string)
	FindingLifecycle(cycle int, summary FindingLifecycleData)
	HailReceived(h HailInfo)
	HailResolved(id, resolution string)
}

// BeadChild carries display information for a child bead in the hierarchy.
type BeadChild struct {
	ID       string
	Title    string
	Status   string // "open", "in_progress", "closed"
	Severity string // "critical", "major", "minor"
	Cycle    int    // cycle in which this child was created
}

// HailInfo holds the data needed to display a hail notification. It mirrors
// the loop.Hail fields relevant for rendering without importing the loop
// package (which depends on ui).
type HailInfo struct {
	ID         string   // Unique hail identifier.
	Kind       string   // Classification (e.g. "decision_needed", "ambiguity").
	Cycle      int      // Cycle in which the hail was raised.
	SourceRole string   // "coder" or "reviewer".
	Summary    string   // One-line human-readable description.
	Detail     string   // Full context for the human decision.
	Options    []string // Optional choices the human can pick from.
}

// Verify that *Printer satisfies the UI interface at compile time.
var _ UI = (*Printer)(nil)

// Printer writes ANSI-colored status output to stderr.
type Printer struct{}

// New returns a new Printer.
func New() *Printer {
	return &Printer{}
}

// Banner prints the quasar ASCII banner to stderr.
func (p *Printer) Banner() {
	fmt.Fprintln(os.Stderr, bold+cyan+"  ╔═══════════════════════════════════╗"+reset)
	fmt.Fprintln(os.Stderr, bold+cyan+"  ║"+reset+bold+"   QUASAR  "+dim+"dual-agent coordinator"+reset+bold+cyan+"  ║"+reset)
	fmt.Fprintln(os.Stderr, bold+cyan+"  ╚═══════════════════════════════════╝"+reset)
	fmt.Fprintln(os.Stderr)
}

// Prompt prints the interactive prompt prefix to stderr.
func (p *Printer) Prompt() {
	fmt.Fprintf(os.Stderr, bold+cyan+"quasar> "+reset)
}

// CycleStart prints the cycle header line.
func (p *Printer) CycleStart(cycle, maxCycles int) {
	fmt.Fprintf(os.Stderr, "\n"+bold+magenta+"── cycle %d/%d ──"+reset+"\n", cycle, maxCycles)
}

// AgentStart prints a status line when an agent begins work.
func (p *Printer) AgentStart(role string) {
	color := blue
	if role == "reviewer" {
		color = yellow
	}
	fmt.Fprintf(os.Stderr, color+bold+"▶ %s"+reset+dim+" working..."+reset+"\n", role)
}

// AgentDone prints a completion line with cost and duration.
func (p *Printer) AgentDone(role string, costUSD float64, durationMs int64) {
	color := blue
	if role == "reviewer" {
		color = yellow
	}
	secs := float64(durationMs) / 1000.0
	fmt.Fprintf(os.Stderr, color+"✓ %s"+reset+dim+" done (%.1fs, $%.4f)"+reset+"\n", role, secs, costUSD)
}

// IssuesFound prints a warning that review issues were found.
func (p *Printer) IssuesFound(count int) {
	fmt.Fprintf(os.Stderr, yellow+bold+"⚠ %d issue(s) found"+reset+" — sending back to coder\n", count)
}

// Approved prints a success message indicating reviewer approval.
func (p *Printer) Approved() {
	fmt.Fprintln(os.Stderr, green+bold+"✓ APPROVED"+reset+" — reviewer is satisfied")
}

// MaxCyclesReached prints an error indicating the cycle limit was hit.
func (p *Printer) MaxCyclesReached(max int) {
	fmt.Fprintf(os.Stderr, red+bold+"✗ max cycles reached (%d)"+reset+" — stopping\n", max)
}

// BudgetExceeded prints an error indicating the cost budget was exceeded.
func (p *Printer) BudgetExceeded(spent, limit float64) {
	fmt.Fprintf(os.Stderr, red+bold+"✗ budget exceeded"+reset+" ($%.2f / $%.2f)\n", spent, limit)
}

// Error prints an error message to stderr.
func (p *Printer) Error(msg string) {
	fmt.Fprintf(os.Stderr, red+bold+"error: "+reset+"%s\n", msg)
}

// Info prints an informational message to stderr.
func (p *Printer) Info(msg string) {
	fmt.Fprintf(os.Stderr, dim+"%s"+reset+"\n", msg)
}

// AgentOutput is a no-op for the stderr printer; agent output is only
// displayed in the TUI drill-down view.
func (p *Printer) AgentOutput(role string, cycle int, output string) {}

// BeadUpdate is a no-op for the stderr printer; bead hierarchy is only
// displayed in the TUI bead tracker view.
func (p *Printer) BeadUpdate(taskBeadID, title, status string, children []BeadChild) {}

// RefactorApplied is a no-op for the stderr printer; refactor indicators
// are only displayed in the TUI phase view.
func (p *Printer) RefactorApplied(phaseID string) {}

// FindingLifecycle prints the verification summary for a cycle.
func (p *Printer) FindingLifecycle(cycle int, summary FindingLifecycleData) {
	fmt.Fprintf(os.Stderr, dim+"  findings: %s"+reset+"\n", summary.String())
}

// HailReceived prints an attention-grabbing block to stderr when an agent
// needs human input.
func (p *Printer) HailReceived(h HailInfo) {
	var b strings.Builder
	b.WriteString(yellow + bold + "⚠ AGENT NEEDS INPUT" + reset)
	b.WriteString(" [" + h.Kind + "]")
	if h.Cycle > 0 || h.SourceRole != "" {
		b.WriteString(dim + " — cycle " + fmt.Sprintf("%d", h.Cycle) + ", " + h.SourceRole + reset)
	}
	b.WriteString("\n")
	b.WriteString("  " + bold + "Summary:" + reset + " " + h.Summary + "\n")
	if h.Detail != "" {
		detail := h.Detail
		if len(detail) > 200 {
			detail = detail[:200] + "…"
		}
		b.WriteString("  " + dim + "Detail: " + detail + reset + "\n")
	}
	if len(h.Options) > 0 {
		b.WriteString("  " + bold + "Options:" + reset)
		for i, opt := range h.Options {
			fmt.Fprintf(&b, " %c) %s", 'A'+i, opt)
			if i < len(h.Options)-1 {
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	fmt.Fprint(os.Stderr, b.String())
}

// HailResolved prints a brief confirmation that a hail was resolved.
func (p *Printer) HailResolved(id, resolution string) {
	fmt.Fprintf(os.Stderr, green+"✓ hail resolved"+reset+" [%s] %s\n", id, resolution)
}

// TaskStarted prints a status line when a task begins.
func (p *Printer) TaskStarted(beadID, title string) {
	fmt.Fprintf(os.Stderr, cyan+"◆ task"+reset+" %s — %s\n", beadID, title)
}

// TaskComplete prints a success line when a task finishes.
func (p *Printer) TaskComplete(beadID string, totalCost float64) {
	fmt.Fprintf(os.Stderr, green+"◆ task complete"+reset+" %s "+dim+"(total: $%.4f)"+reset+"\n", beadID, totalCost)
}

// ShowHelp prints available interactive commands to stderr.
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

// ShowStatus prints the current configuration summary to stderr.
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

// FindingLifecycleData holds verification counts for a single cycle.
// This struct lives in the ui package to avoid circular imports with loop.
type FindingLifecycleData struct {
	Fixed        int
	StillPresent int
	Regressed    int
}

// String returns a compact summary like "2 fixed, 1 still present, 0 regressed".
func (d FindingLifecycleData) String() string {
	return fmt.Sprintf("%d fixed, %d still present, %d regressed",
		d.Fixed, d.StillPresent, d.Regressed)
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
