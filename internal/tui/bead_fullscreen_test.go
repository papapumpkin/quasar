package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestBeadToggleFullView verifies that pressing 'b' at various depths in nebula mode
// produces a bounded, well-structured View() output — not a wall of text filling
// the entire screen.
func TestBeadToggleFullView(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Splash = nil
	m.Width = 120
	m.Height = 40

	// Initialize phases similar to the real nebula.
	m.NebulaView.InitPhases([]PhaseInfo{
		{ID: "header-solid-bg", Title: "Make header background solid"},
		{ID: "logo-no-bg", Title: "Remove background from logo"},
		{ID: "status-bar-colors", Title: "Restore per-segment coloring"},
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

	// Test at multiple depth levels.
	for _, tc := range []struct {
		name  string
		depth ViewDepth
	}{
		{"DepthPhases", DepthPhases},
		{"DepthPhaseLoop", DepthPhaseLoop},
		{"DepthAgentOutput", DepthAgentOutput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			am := tm.(AppModel)
			am.Depth = tc.depth
			am.FocusedPhase = "header-solid-bg"

			// Press 'b' to toggle beads view.
			var tm2 tea.Model = am
			tm2, _ = tm2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
			am2 := tm2.(AppModel)

			if !am2.ShowBeads {
				t.Fatal("Expected ShowBeads to be true after pressing 'b'")
			}

			view := am2.View()
			viewLines := strings.Split(view, "\n")
			t.Logf("Full View() has %d lines (terminal height: %d)", len(viewLines), am2.Height)

			if strings.Contains(view, "very long line of agent output") {
				t.Error("Agent output should NOT appear in the view when ShowBeads is active")
			}

			if len(viewLines) > am2.Height+5 {
				t.Errorf("View has %d lines but terminal is only %d tall — layout is overflowing", len(viewLines), am2.Height)
				t.Log("First 15 lines:")
				for i := 0; i < 15 && i < len(viewLines); i++ {
					t.Logf("  [%d] %q", i, viewLines[i])
				}
				t.Log("Last 15 lines:")
				start := len(viewLines) - 15
				if start < 0 {
					start = 0
				}
				for i := start; i < len(viewLines); i++ {
					t.Logf("  [%d] %q", i, viewLines[i])
				}
			}
		})
	}
}