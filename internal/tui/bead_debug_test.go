package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBeadViewPipelineDebug(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 30
	m.ShowBeads = true

	var tm tea.Model = m
	tm, _ = tm.Update(MsgBeadUpdate{
		TaskBeadID: "bead-1",
		Root: BeadInfo{
			ID:     "bead-1",
			Title:  "Fix authentication bug",
			Status: "in_progress",
			Children: []BeadInfo{
				{ID: "bead-1.1", Title: "Missing session validation", Status: "closed", Cycle: 1},
				{ID: "bead-1.2", Title: "Token expiry edge case", Status: "closed", Cycle: 1},
				{ID: "bead-1.3", Title: "Race condition in refresh flow", Status: "open", Cycle: 2},
				{ID: "bead-1.4", Title: "Login redirect breaks on subdomain", Status: "open", Cycle: 2},
				{ID: "bead-1.5", Title: "Session cleanup doesn't fire", Status: "open", Cycle: 3},
			},
		},
	})
	am := tm.(AppModel)
	view := am.Detail.View()
	fmt.Printf("=== Detail View Output ===\n%s\n=== End ===\n", view)

	// Check tree connectors survive the pipeline
	if !strings.Contains(view, "├─") {
		t.Error("Missing mid-tree connector '├─' in detail panel output")
	}
	if !strings.Contains(view, "└─") {
		t.Error("Missing last-tree connector '└─' in detail panel output")
	}
	if !strings.Contains(view, "[2/5 resolved]") {
		t.Error("Missing progress fraction '[2/5 resolved]'")
	}
	if !strings.Contains(view, "█") {
		t.Error("Missing progress bar filled characters")
	}
	if !strings.Contains(view, "░") {
		t.Error("Missing progress bar empty characters")
	}
}
