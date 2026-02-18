package tui

import (
	"fmt"
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

// TestBeadViewSurvivesInterleavedAgentOutput verifies that rapid MsgAgentOutput
// messages interleaved with a bead toggle do not corrupt the detail panel.
// When ShowBeads is true, updateDetailFromSelection routes to bead content,
// not agent output.
func TestBeadViewSurvivesInterleavedAgentOutput(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 20)
	m.Width = 80
	m.Height = 40

	// First, set up a loop view entry so agent output has somewhere to go.
	m.LoopView.StartCycle(1)
	m.LoopView.StartAgent("coder")

	// Enable beads and populate bead data.
	m.ShowBeads = true
	m.Depth = DepthAgentOutput

	var tm tea.Model = m

	// Send bead data.
	tm, _ = tm.Update(MsgBeadUpdate{
		TaskBeadID: "bead-1",
		Root: BeadInfo{
			ID:     "bead-1",
			Title:  "Auth bug fix",
			Status: "in_progress",
			Children: []BeadInfo{
				{ID: "c1", Title: "Session issue", Status: "closed", Cycle: 1},
				{ID: "c2", Title: "Token issue", Status: "open", Cycle: 1},
			},
		},
	})

	// Now fire rapid agent output messages — these should NOT overwrite bead content.
	for i := 0; i < 10; i++ {
		tm, _ = tm.Update(MsgAgentOutput{
			Role:   "coder",
			Cycle:  1,
			Output: fmt.Sprintf("agent output line %d — this should not appear in bead view", i),
		})
	}

	am := tm.(AppModel)
	view := am.Detail.View()

	// Bead content must be present.
	if !strings.Contains(view, "├─") {
		t.Error("Expected tree connector in detail panel after interleaved agent output")
	}
	if !strings.Contains(view, "Auth bug fix") {
		t.Error("Expected bead title in detail panel after interleaved agent output")
	}

	// Agent output must NOT be present.
	if strings.Contains(view, "agent output line") {
		t.Error("Agent output should not appear in detail panel when ShowBeads is true")
	}
}

// TestBeadViewSurvivesInterleavedNebulaOutput verifies the same interleaving
// protection in nebula mode at DepthPhaseLoop and DepthAgentOutput.
func TestBeadViewSurvivesInterleavedNebulaOutput(t *testing.T) {
	t.Parallel()

	for _, depth := range []ViewDepth{DepthPhaseLoop, DepthAgentOutput} {
		t.Run(fmt.Sprintf("depth_%d", depth), func(t *testing.T) {
			m := NewAppModel(ModeNebula)
			m.Detail = NewDetailPanel(80, 20)
			m.Width = 80
			m.Height = 40
			m.ShowBeads = true
			m.Depth = depth
			m.FocusedPhase = "setup"

			// Initialize phases and a loop view.
			m.NebulaView.InitPhases([]PhaseInfo{
				{ID: "setup", Title: "Setup phase"},
			})
			lv := m.ensurePhaseLoop("setup")
			lv.StartCycle(1)
			lv.StartAgent("coder")

			var tm tea.Model = m

			// Send phase bead data.
			tm, _ = tm.Update(MsgPhaseBeadUpdate{
				PhaseID:    "setup",
				TaskBeadID: "bead-3",
				Root: BeadInfo{
					ID:     "bead-3",
					Title:  "Setup task",
					Status: "in_progress",
					Children: []BeadInfo{
						{ID: "c1", Title: "Config check", Status: "closed", Cycle: 1},
					},
				},
			})

			// Fire rapid phase agent output.
			for i := 0; i < 5; i++ {
				tm, _ = tm.Update(MsgPhaseAgentOutput{
					PhaseID: "setup",
					Role:    "coder",
					Cycle:   1,
					Output:  fmt.Sprintf("phase output %d", i),
				})
			}

			am := tm.(AppModel)
			view := am.Detail.View()

			if !strings.Contains(view, "Setup task") {
				t.Error("Expected bead title in detail panel after interleaved phase output")
			}
			if strings.Contains(view, "phase output") {
				t.Error("Phase agent output should not appear when ShowBeads is true")
			}
		})
	}
}

// TestBeadViewChildOverflow verifies that the tree truncates large child lists
// with an overflow indicator.
func TestBeadViewChildOverflow(t *testing.T) {
	t.Parallel()

	bv := NewBeadView()
	bv.Width = 80

	// Create more children than maxBeadChildren.
	children := make([]BeadInfo, 40)
	for i := range children {
		children[i] = BeadInfo{
			ID:     fmt.Sprintf("c%d", i),
			Title:  fmt.Sprintf("Child issue %d", i),
			Status: "open",
			Cycle:  1,
		}
	}
	children[0].Status = "closed"

	bv.SetRoot(BeadInfo{
		ID:       "root",
		Title:    "Large task",
		Status:   "open",
		Children: children,
	})

	view := bv.View()

	// Overflow indicator must appear.
	overflow := len(children) - maxBeadChildren
	indicator := fmt.Sprintf("(and %d more...)", overflow)
	if !strings.Contains(view, indicator) {
		t.Errorf("Expected overflow indicator %q in view", indicator)
	}

	// The progress fraction should still reflect ALL children.
	expected := fmt.Sprintf("[1/%d resolved]", len(children))
	if !strings.Contains(view, expected) {
		t.Errorf("Expected progress fraction %q in view", expected)
	}

	// Only maxBeadChildren child titles should appear (not all 40).
	lastShown := fmt.Sprintf("Child issue %d", maxBeadChildren-1)
	firstHidden := fmt.Sprintf("Child issue %d", maxBeadChildren)
	if !strings.Contains(view, lastShown) {
		t.Errorf("Expected last shown child %q in view", lastShown)
	}
	if strings.Contains(view, firstHidden) {
		t.Errorf("Hidden child %q should not appear in view", firstHidden)
	}
}

// TestBeadToggleClearsStaleContent verifies that toggling beads on clears
// any stale content from the detail panel before populating bead data.
func TestBeadToggleClearsStaleContent(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 20)
	m.Width = 80
	m.Height = 40
	m.Depth = DepthAgentOutput

	// Pre-populate detail panel with agent output via the Update pathway.
	// StartCycle sets cursor to 0 (cycle header); MoveDown selects the agent.
	m.LoopView.StartCycle(1)
	m.LoopView.StartAgent("coder")
	m.LoopView.SetAgentOutput("coder", 1, "This is stale agent output that should vanish")
	m.LoopView.MoveDown() // Move cursor from cycle header to agent entry.
	m.updateDetailFromSelection()

	// Verify stale content is there before toggle.
	viewBefore := m.Detail.View()
	if !strings.Contains(viewBefore, "stale agent output") {
		t.Fatal("Precondition: detail panel should contain agent output before toggle")
	}

	// Toggle beads ON (no bead data yet).
	m.handleBeadsKey()

	viewAfter := m.Detail.View()

	// Stale agent output must NOT appear.
	if strings.Contains(viewAfter, "stale agent output") {
		t.Error("Stale agent output should be cleared when beads are toggled on")
	}

	// Should show empty state or loading hint.
	if !strings.Contains(viewAfter, "no bead data yet") && !strings.Contains(viewAfter, "Loading beads") {
		t.Error("Expected empty-state hint when no bead data is available")
	}
}
