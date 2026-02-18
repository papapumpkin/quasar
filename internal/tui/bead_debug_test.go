package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestBeadViewPipelineTreeConnectors verifies that the full rendering pipeline
// (BeadView → DetailPanel → viewport) preserves tree connectors, status icons,
// and progress bar when displaying the bead hierarchy through the detail panel.
func TestBeadViewPipelineTreeConnectors(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 20)
	m.Width = 80
	m.Height = 40
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

	// Tree connectors must survive the detail panel rendering pipeline.
	if !strings.Contains(view, "├─") {
		t.Error("Expected mid-tree connector '├─' in detail panel output")
	}
	if !strings.Contains(view, "└─") {
		t.Error("Expected last-tree connector '└─' in detail panel output")
	}

	// Progress fraction.
	if !strings.Contains(view, "[2/5 resolved]") {
		t.Error("Expected progress fraction '[2/5 resolved]'")
	}

	// Progress bar characters.
	if !strings.Contains(view, "█") {
		t.Error("Expected filled progress bar characters")
	}
	if !strings.Contains(view, "░") {
		t.Error("Expected empty progress bar characters")
	}

	// All child titles must appear.
	for _, title := range []string{
		"Missing session validation",
		"Token expiry edge case",
		"Race condition in refresh flow",
		"Login redirect breaks on subdomain",
		"Session cleanup doesn't fire",
	} {
		if !strings.Contains(view, title) {
			t.Errorf("Expected child title %q in detail panel output", title)
		}
	}

	// Status icons must appear.
	if !strings.Contains(view, beadIconClosed) {
		t.Error("Expected closed icon in detail panel output")
	}
	if !strings.Contains(view, beadIconOpen) {
		t.Error("Expected open icon in detail panel output")
	}
}

// TestBeadViewPipelineNebulaModeTreeConnectors verifies that the pipeline
// works correctly in nebula mode with phase-specific bead data.
func TestBeadViewPipelineNebulaModeTreeConnectors(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 20)
	m.Width = 80
	m.Height = 40
	m.ShowBeads = true

	// Add a phase so SelectedPhase returns something.
	m.NebulaView.InitPhases([]PhaseInfo{
		{ID: "setup", Title: "Setup phase"},
	})

	// Populate PhaseBeads.
	var tm tea.Model = m
	tm, _ = tm.Update(MsgPhaseBeadUpdate{
		PhaseID:    "setup",
		TaskBeadID: "bead-2",
		Root: BeadInfo{
			ID:     "bead-2",
			Title:  "Setup task",
			Status: "in_progress",
			Children: []BeadInfo{
				{ID: "bead-2.1", Title: "Config validation", Status: "closed", Cycle: 1},
				{ID: "bead-2.2", Title: "Environment setup", Status: "open", Cycle: 1},
			},
		},
	})
	am := tm.(AppModel)
	view := am.Detail.View()

	if !strings.Contains(view, "├─") {
		t.Error("Expected mid-tree connector in nebula mode bead view")
	}
	if !strings.Contains(view, "└─") {
		t.Error("Expected last-tree connector in nebula mode bead view")
	}
	if !strings.Contains(view, "[1/2 resolved]") {
		t.Error("Expected progress fraction '[1/2 resolved]'")
	}
}
