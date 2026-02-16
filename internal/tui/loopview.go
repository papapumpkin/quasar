package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
)

// AgentEntry represents one agent invocation within a cycle.
type AgentEntry struct {
	Role       string
	Done       bool
	CostUSD    float64
	DurationMs int64
	IssueCount int
	Output     string
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
	s.Spinner = spinner.Dot
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
	c.Agents = append(c.Agents, AgentEntry{Role: role})
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
func (lv *LoopView) SetAgentOutput(role string, cycle int, output string) {
	for i := range lv.Cycles {
		if lv.Cycles[i].Number != cycle {
			continue
		}
		for j := range lv.Cycles[i].Agents {
			if lv.Cycles[i].Agents[j].Role == role {
				lv.Cycles[i].Agents[j].Output = output
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

// View renders the cycle timeline.
func (lv LoopView) View() string {
	var b strings.Builder
	idx := 0
	for _, c := range lv.Cycles {
		// Cycle header.
		prefix := "  "
		if idx == lv.Cursor {
			prefix = "> "
		}
		header := fmt.Sprintf("%sCycle %d", prefix, c.Number)
		if idx == lv.Cursor {
			b.WriteString(styleRowSelected.Render(header))
		} else {
			b.WriteString(styleRowNormal.Render(header))
		}
		b.WriteString("\n")
		idx++

		// Agent entries.
		for _, a := range c.Agents {
			prefix = "    "
			if idx == lv.Cursor {
				prefix = "  > "
			}

			var line string
			if a.Done {
				secs := float64(a.DurationMs) / 1000.0
				line = fmt.Sprintf("%s%s %s  %.1fs  $%.4f", prefix, "✓", a.Role, secs, a.CostUSD)
				if a.Role == "reviewer" && a.IssueCount > 0 {
					line += fmt.Sprintf("  → %d issue(s)", a.IssueCount)
				}
				if idx == lv.Cursor {
					b.WriteString(styleRowSelected.Render(line))
				} else {
					b.WriteString(styleRowDone.Render(line))
				}
			} else {
				line = fmt.Sprintf("%s%s %s  working…  %s", prefix, "◎", a.Role, lv.Spinner.View())
				b.WriteString(styleRowWorking.Render(line))
			}
			b.WriteString("\n")
			idx++
		}
	}
	return b.String()
}
