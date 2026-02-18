package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

// AgentEntry represents one agent invocation within a cycle.
type AgentEntry struct {
	Role       string
	Done       bool
	CostUSD    float64
	DurationMs int64
	IssueCount int
	Output     string
	Diff       string
	DiffFiles  []FileStatEntry // parsed file stats for the diff
	BaseRef    string          // git ref before this cycle
	HeadRef    string          // git ref after this cycle
	WorkDir    string          // working directory for git operations
	StartedAt  time.Time
}

// CycleEntry represents one coder-reviewer cycle.
type CycleEntry struct {
	Number int
	Agents []AgentEntry
}

// LoopView renders the cycle timeline for single-task loop mode.
type LoopView struct {
	Cycles   []CycleEntry
	Cursor   int
	Spinner  spinner.Model
	Width    int
	Approved bool
}

// NewLoopView creates an empty loop view.
func NewLoopView() LoopView {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(colorBlue)
	return LoopView{
		Spinner: s,
	}
}

// TotalEntries returns the total number of selectable rows
// (cycle headers + agent entries).
func (lv LoopView) TotalEntries() int {
	count := 0
	for _, c := range lv.Cycles {
		count++ // cycle header
		count += len(c.Agents)
	}
	return count
}

// SelectedAgent returns the agent entry at the cursor, if any.
func (lv LoopView) SelectedAgent() *AgentEntry {
	idx := 0
	for i := range lv.Cycles {
		idx++ // skip cycle header
		for j := range lv.Cycles[i].Agents {
			if idx == lv.Cursor {
				return &lv.Cycles[i].Agents[j]
			}
			idx++
		}
	}
	return nil
}

// SelectedCycleNumber returns the cycle number for the currently selected agent.
// Returns 0 if no agent is selected.
func (lv LoopView) SelectedCycleNumber() int {
	idx := 0
	for i := range lv.Cycles {
		idx++ // skip cycle header
		for range lv.Cycles[i].Agents {
			if idx == lv.Cursor {
				return lv.Cycles[i].Number
			}
			idx++
		}
	}
	return 0
}

// StartCycle begins a new cycle.
func (lv *LoopView) StartCycle(number int) {
	lv.Cycles = append(lv.Cycles, CycleEntry{Number: number})
}

// StartAgent adds a working agent entry to the current cycle.
func (lv *LoopView) StartAgent(role string) {
	if len(lv.Cycles) == 0 {
		return
	}
	c := &lv.Cycles[len(lv.Cycles)-1]
	c.Agents = append(c.Agents, AgentEntry{Role: role, StartedAt: time.Now()})
}

// FinishAgent marks the last agent in the current cycle as done.
func (lv *LoopView) FinishAgent(role string, costUSD float64, durationMs int64) {
	if len(lv.Cycles) == 0 {
		return
	}
	c := &lv.Cycles[len(lv.Cycles)-1]
	for i := len(c.Agents) - 1; i >= 0; i-- {
		if c.Agents[i].Role == role && !c.Agents[i].Done {
			c.Agents[i].Done = true
			c.Agents[i].CostUSD = costUSD
			c.Agents[i].DurationMs = durationMs
			return
		}
	}
}

// SetAgentOutput stores agent output for drill-down.
// It first tries to find an exact cycle+role match. If no match is found
// (e.g. due to message ordering or off-by-one), it falls back to the most
// recent agent with the given role across all cycles, ensuring output is
// never silently dropped.
func (lv *LoopView) SetAgentOutput(role string, cycle int, output string) {
	// Try exact cycle match first.
	for i := range lv.Cycles {
		if lv.Cycles[i].Number != cycle {
			continue
		}
		for j := range lv.Cycles[i].Agents {
			if lv.Cycles[i].Agents[j].Role == role {
				lv.Cycles[i].Agents[j].Output = output
				return
			}
		}
	}

	// Fallback: store on the most recent agent with this role.
	for i := len(lv.Cycles) - 1; i >= 0; i-- {
		for j := len(lv.Cycles[i].Agents) - 1; j >= 0; j-- {
			if lv.Cycles[i].Agents[j].Role == role {
				lv.Cycles[i].Agents[j].Output = output
				return
			}
		}
	}
}

// SetAgentDiff stores a git diff for the given agent in the given cycle.
func (lv *LoopView) SetAgentDiff(role string, cycle int, diff string) {
	// Try exact cycle match first.
	for i := range lv.Cycles {
		if lv.Cycles[i].Number != cycle {
			continue
		}
		for j := range lv.Cycles[i].Agents {
			if lv.Cycles[i].Agents[j].Role == role {
				lv.Cycles[i].Agents[j].Diff = diff
				return
			}
		}
	}

	// Fallback: store on the most recent agent with this role.
	for i := len(lv.Cycles) - 1; i >= 0; i-- {
		for j := len(lv.Cycles[i].Agents) - 1; j >= 0; j-- {
			if lv.Cycles[i].Agents[j].Role == role {
				lv.Cycles[i].Agents[j].Diff = diff
				return
			}
		}
	}
}

// SetAgentDiffFiles stores structured diff metadata (file stats, refs, workdir)
// for the given agent in the given cycle. It uses the same lookup strategy as
// SetAgentDiff: exact cycle match first, then fallback to the most recent agent
// with the given role.
func (lv *LoopView) SetAgentDiffFiles(role string, cycle int, files []FileStatEntry, baseRef, headRef, workDir string) {
	// Try exact cycle match first.
	for i := range lv.Cycles {
		if lv.Cycles[i].Number != cycle {
			continue
		}
		for j := range lv.Cycles[i].Agents {
			if lv.Cycles[i].Agents[j].Role == role {
				lv.Cycles[i].Agents[j].DiffFiles = files
				lv.Cycles[i].Agents[j].BaseRef = baseRef
				lv.Cycles[i].Agents[j].HeadRef = headRef
				lv.Cycles[i].Agents[j].WorkDir = workDir
				return
			}
		}
	}

	// Fallback: store on the most recent agent with this role.
	for i := len(lv.Cycles) - 1; i >= 0; i-- {
		for j := len(lv.Cycles[i].Agents) - 1; j >= 0; j-- {
			if lv.Cycles[i].Agents[j].Role == role {
				lv.Cycles[i].Agents[j].DiffFiles = files
				lv.Cycles[i].Agents[j].BaseRef = baseRef
				lv.Cycles[i].Agents[j].HeadRef = headRef
				lv.Cycles[i].Agents[j].WorkDir = workDir
				return
			}
		}
	}
}

// SetIssueCount sets the issue count on the last reviewer in the current cycle.
func (lv *LoopView) SetIssueCount(count int) {
	if len(lv.Cycles) == 0 {
		return
	}
	c := &lv.Cycles[len(lv.Cycles)-1]
	for i := len(c.Agents) - 1; i >= 0; i-- {
		if c.Agents[i].Role == "reviewer" {
			c.Agents[i].IssueCount = count
			return
		}
	}
}

// MoveUp moves the cursor up.
func (lv *LoopView) MoveUp() {
	if lv.Cursor > 0 {
		lv.Cursor--
	}
}

// MoveDown moves the cursor down.
func (lv *LoopView) MoveDown() {
	max := lv.TotalEntries() - 1
	if max < 0 {
		max = 0
	}
	if lv.Cursor < max {
		lv.Cursor++
	}
}

// View renders the cycle timeline with tree connector lines.
func (lv LoopView) View() string {
	var b strings.Builder
	idx := 0
	for _, c := range lv.Cycles {
		// Cycle header.
		selected := idx == lv.Cursor
		indicator := "  "
		if selected {
			indicator = styleSelectionIndicator.Render(selectionIndicator) + " "
		}
		header := fmt.Sprintf("%sCycle %d", indicator, c.Number)
		if selected {
			b.WriteString(styleRowSelected.Render(header))
		} else {
			b.WriteString(styleRowNormal.Render(header))
		}
		b.WriteString("\n")
		idx++

		// Agent entries with tree connectors.
		for j, a := range c.Agents {
			selected = idx == lv.Cursor
			indicator = "  "
			if selected {
				indicator = styleSelectionIndicator.Render(selectionIndicator) + " "
			}

			// Tree connector: └── for last agent, ├── for others.
			isLast := j == len(c.Agents)-1
			connector := "├── "
			if isLast {
				connector = "└── "
			}
			styledConnector := styleTreeConnector.Render(connector)

			if a.Done {
				secs := float64(a.DurationMs) / 1000.0
				icon := styleRowDone.Render(iconDone)
				var styledRole string
				if selected {
					styledRole = styleRowSelected.Render(a.Role)
				} else {
					styledRole = stylePhaseID.Render(a.Role)
				}
				stats := fmt.Sprintf("%.1fs  $%.4f", secs, a.CostUSD)
				if a.Role == "reviewer" && a.IssueCount > 0 {
					stats += fmt.Sprintf("  → %d issue(s)", a.IssueCount)
				}
				styledStats := stylePhaseDetail.Render(stats)
				line := fmt.Sprintf("%s%s%s %s  %s", indicator, styledConnector, icon, styledRole, styledStats)
				b.WriteString(line)
			} else {
				icon := styleRowWorking.Render(iconWorking)
				elapsed := formatElapsed(a.StartedAt)
				spinnerStr := roleColoredSpinner(a.Role, lv.Spinner)
				styledRole := stylePhaseID.Render(a.Role)
				styledDetail := stylePhaseDetail.Render(fmt.Sprintf("working… %s", elapsed))
				line := fmt.Sprintf("%s%s%s %s  %s  %s", indicator, styledConnector, icon, styledRole, styledDetail, spinnerStr)
				b.WriteString(line)
			}
			b.WriteString("\n")
			idx++
		}
	}
	return b.String()
}

// formatElapsed returns a human-readable elapsed time string from a start time.
// Returns an empty string if the start time is zero.
func formatElapsed(start time.Time) string {
	if start.IsZero() {
		return ""
	}
	d := time.Since(start).Truncate(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

// roleColoredSpinner renders the spinner frame with a color matching the agent role.
// Coder gets blue, reviewer gets yellow/gold.
func roleColoredSpinner(role string, s spinner.Model) string {
	frame := s.View()
	switch role {
	case "reviewer":
		return lipgloss.NewStyle().Foreground(colorReviewer).Render(frame)
	default:
		return lipgloss.NewStyle().Foreground(colorBlue).Render(frame)
	}
}
