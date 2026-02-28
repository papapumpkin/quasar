package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CockpitTab identifies a top-level tab in nebula cockpit mode.
type CockpitTab int

const (
	// TabBoard shows the task board (default).
	TabBoard CockpitTab = iota
	// TabEntanglements shows interface agreements from the fabric.
	TabEntanglements
	// TabGraph shows the DAG dependency graph.
	TabGraph
	// TabScratchpad shows telemetry-fed shared notes.
	TabScratchpad
)

// cockpitTabCount is the total number of cockpit tabs.
const cockpitTabCount = 4

// tabLabels maps each tab to its display label.
var tabLabels = [cockpitTabCount]string{
	TabBoard:         "board",
	TabEntanglements: "entanglements",
	TabGraph:         "graph",
	TabScratchpad:    "scratchpad",
}

// Label returns the display label for a tab.
func (t CockpitTab) Label() string {
	if int(t) >= 0 && int(t) < cockpitTabCount {
		return tabLabels[t]
	}
	return "unknown"
}

// Next cycles forward to the next tab, wrapping around.
func (t CockpitTab) Next() CockpitTab {
	return CockpitTab((int(t) + 1) % cockpitTabCount)
}

// Prev cycles backward to the previous tab, wrapping around.
func (t CockpitTab) Prev() CockpitTab {
	return CockpitTab((int(t) + cockpitTabCount - 1) % cockpitTabCount)
}

// TabFromNumber converts a 1-based number key to a CockpitTab.
// Returns the tab and true if valid, or TabBoard and false otherwise.
func TabFromNumber(n int) (CockpitTab, bool) {
	idx := n - 1
	if idx >= 0 && idx < cockpitTabCount {
		return CockpitTab(idx), true
	}
	return TabBoard, false
}

// TabBar renders a horizontal row of tab labels for cockpit mode.
type TabBar struct {
	ActiveTab CockpitTab
	Width     int
}

// View renders the tab bar as a single styled line.
// The active tab is highlighted with the nebula accent color and bold.
// Inactive tabs use the muted color.
func (tb TabBar) View() string {
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorNebula)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(colorMuted)

	var parts []string
	for i := 0; i < cockpitTabCount; i++ {
		tab := CockpitTab(i)
		label := fmt.Sprintf("[%d] %s", i+1, tab.Label())
		if tab == tb.ActiveTab {
			parts = append(parts, activeStyle.Render(label))
		} else {
			parts = append(parts, inactiveStyle.Render(label))
		}
	}

	line := strings.Join(parts, "  ")
	return lipgloss.NewStyle().
		Width(tb.Width).
		PaddingLeft(2).
		Render(line)
}
