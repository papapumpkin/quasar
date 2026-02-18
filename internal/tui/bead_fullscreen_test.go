package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestBeadToggleFullView verifies that pressing 'b' at DepthPhases in nebula mode
// produces a bounded, well-structured View() output — not a wall of text filling
// the entire screen.
func TestBeadToggleFullView(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40

	// Initialize phases similar to the real nebula.
	m.NebulaView.InitPhases([]PhaseInfo{
		{ID: "header-solid-bg", Title: "Make header background solid", Status: PhaseWorking},
		{ID: "logo-no-bg", Title: "Remove background from logo", Status: PhaseWaiting},
		{ID: "status-bar-colors", Title: "Restore per-segment coloring", Status: PhaseWaiting},
	})

	// Simulate WindowSizeMsg to initialize the detail panel.
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Simulate agent output arriving for a phase.
	longOutput := strings.Repeat("This is a very long line of agent output that would fill the screen. ", 50)
	tm, _ = tm.Update(MsgPhaseAgentOutput{
		PhaseID: "header-solid-bg",
		Role:    "coder",
		Cycle:   1,
		Output:  longOutput,
	})

	// Simulate bead data.
	tm, _ = tm.Update(MsgPhaseBeadUpdate{
		PhaseID:    "header-solid-bg",
		TaskBeadID: "bead-1",
		Root: BeadInfo{
			ID:     "bead-1",
			Title:  "Make header background solid",
			Status: "in_progress",
			Children: []BeadInfo{
				{ID: "c1", Title: "Missing background on segment styles", Status: "open", Cycle: 1},
				{ID: "c2", Title: "Padding gap in status bar", Status: "closed", Cycle: 1},
			},
		},
	})

	// Now press 'b' to toggle beads view.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})

	am := tm.(AppModel)
	if !am.ShowBeads {
		t.Fatal("Expected ShowBeads to be true after pressing 'b'")
	}

	// Capture the full view.
	view := am.View()
	viewLines := strings.Split(view, "\n")
	t.Logf("Full View() has %d lines (terminal height: %d)", len(viewLines), am.Height)
	t.Logf("ShowBeads=%v, Depth=%d", am.ShowBeads, am.Depth)

	// The view should NOT contain the agent output when ShowBeads is active.
	if strings.Contains(view, "very long line of agent output") {
		t.Error("Agent output should NOT appear in the view when ShowBeads is active")
	}

	// The view should contain bead tree elements.
	if !strings.Contains(view, "├─") && !strings.Contains(view, "└─") {
		t.Error("Expected tree connectors in the view when ShowBeads is active")
	}

	// The view height should not greatly exceed the terminal height.
	if len(viewLines) > am.Height+5 {
		t.Errorf("View has %d lines but terminal is only %d tall — layout is overflowing", len(viewLines), am.Height)
		// Print first and last few lines to diagnose what's filling the screen.
		t.Log("First 10 lines:")
		for i := 0; i < 10 && i < len(viewLines); i++ {
			t.Logf("  [%d] %s", i, viewLines[i])
		}
		t.Log("Last 10 lines:")
		for i := len(viewLines) - 10; i < len(viewLines); i++ {
			if i >= 0 {
				t.Logf("  [%d] %s", i, viewLines[i])
			}
		}
	}
}