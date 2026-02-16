package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronsalm/quasar/internal/ui"
)

// collectingModel is a tea.Model that collects all messages for inspection.
type collectingModel struct {
	msgs []tea.Msg
}

func (m collectingModel) Init() tea.Cmd { return nil }
func (m collectingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.msgs = append(m.msgs, msg)
	return m, nil
}
func (m collectingModel) View() string { return "" }

// sendAndCollect runs a function that calls bridge methods, then returns
// the messages that were sent to the program.
func sendAndCollect(t *testing.T, fn func(b *UIBridge)) []tea.Msg {
	t.Helper()

	// Use a channel-based approach: the bridge calls Send which puts messages
	// into the program's channel. We verify by calling bridge methods and
	// checking they don't panic.
	msgs := make(chan tea.Msg, 100)

	// Create a minimal program just to test Send doesn't panic.
	model := NewAppModel(ModeLoop)
	model.Detail = NewDetailPanel(80, 10)
	p := tea.NewProgram(model, tea.WithoutSignalHandler())

	bridge := NewUIBridge(p)

	// Call the bridge function (sends messages to the program).
	fn(bridge)

	close(msgs)
	return nil
}

func TestUIBridgeImplementsInterface(t *testing.T) {
	// Compile-time check is in bridge.go, but verify at runtime too.
	model := NewAppModel(ModeLoop)
	model.Detail = NewDetailPanel(80, 10)
	p := tea.NewProgram(model, tea.WithoutSignalHandler())
	var iface ui.UI = NewUIBridge(p)
	_ = iface
}

func TestUIBridgeMethodsDoNotPanic(t *testing.T) {
	model := NewAppModel(ModeLoop)
	model.Detail = NewDetailPanel(80, 10)
	p := tea.NewProgram(model, tea.WithoutSignalHandler())
	b := NewUIBridge(p)

	// Run the program briefly in a goroutine so Send has a receiver.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Run for a short time, then quit.
		time.AfterFunc(200*time.Millisecond, func() { p.Quit() })
		_, _ = p.Run()
	}()

	// Give the program time to start.
	time.Sleep(50 * time.Millisecond)

	// None of these should panic.
	b.TaskStarted("bead-123", "test task")
	b.CycleStart(1, 5)
	b.AgentStart("coder")
	b.AgentDone("coder", 0.45, 12300)
	b.AgentOutput("coder", 1, "some output")
	b.CycleSummary(ui.CycleSummaryData{
		Cycle:     1,
		MaxCycles: 5,
		Phase:     "code_complete",
		CostUSD:   0.45,
	})
	b.IssuesFound(2)
	b.Approved()
	b.MaxCyclesReached(5)
	b.BudgetExceeded(10.0, 5.0)
	b.Error("test error")
	b.Info("test info")
	b.TaskComplete("bead-123", 1.23)

	<-done
}

func TestAppModelLoopLifecycle(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	// Simulate a task lifecycle.
	var tm tea.Model = m

	tm, _ = tm.Update(MsgTaskStarted{BeadID: "bead-1", Title: "hello world"})
	am := tm.(AppModel)
	if am.StatusBar.BeadID != "bead-1" {
		t.Errorf("BeadID = %q, want %q", am.StatusBar.BeadID, "bead-1")
	}

	tm, _ = tm.Update(MsgCycleStart{Cycle: 1, MaxCycles: 3})
	am = tm.(AppModel)
	if am.StatusBar.Cycle != 1 {
		t.Errorf("Cycle = %d, want 1", am.StatusBar.Cycle)
	}
	if len(am.LoopView.Cycles) != 1 {
		t.Errorf("Cycles = %d, want 1", len(am.LoopView.Cycles))
	}

	tm, _ = tm.Update(MsgAgentStart{Role: "coder"})
	am = tm.(AppModel)
	if len(am.LoopView.Cycles[0].Agents) != 1 {
		t.Errorf("Agents = %d, want 1", len(am.LoopView.Cycles[0].Agents))
	}

	tm, _ = tm.Update(MsgAgentDone{Role: "coder", CostUSD: 0.5, DurationMs: 5000})
	am = tm.(AppModel)
	if !am.LoopView.Cycles[0].Agents[0].Done {
		t.Error("Agent should be done")
	}

	tm, _ = tm.Update(MsgAgentOutput{Role: "coder", Cycle: 1, Output: "wrote code"})

	tm, _ = tm.Update(MsgApproved{})
	am = tm.(AppModel)
	if !am.LoopView.Approved {
		t.Error("LoopView should be approved")
	}

	tm, _ = tm.Update(MsgTaskComplete{BeadID: "bead-1", TotalCost: 1.0})
	am = tm.(AppModel)
	if !am.Done {
		t.Error("AppModel should be done")
	}
}

func TestAppModelNebulaProgress(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m

	tm, _ = tm.Update(MsgNebulaProgress{
		Completed: 3, Total: 10, TotalCostUSD: 2.50,
	})
	am := tm.(AppModel)
	if am.StatusBar.Completed != 3 {
		t.Errorf("Completed = %d, want 3", am.StatusBar.Completed)
	}
	if am.StatusBar.Total != 10 {
		t.Errorf("Total = %d, want 10", am.StatusBar.Total)
	}
	if am.StatusBar.CostUSD != 2.50 {
		t.Errorf("CostUSD = %f, want 2.50", am.StatusBar.CostUSD)
	}
}

func TestNebulaPhaseLifecycle(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m

	// Initialize with phases.
	tm, _ = tm.Update(MsgNebulaInit{
		Name: "test-nebula",
		Phases: []PhaseInfo{
			{ID: "setup", Title: "Setup models"},
			{ID: "auth", Title: "Auth middleware", DependsOn: []string{"setup"}},
		},
	})
	am := tm.(AppModel)
	if len(am.NebulaView.Phases) != 2 {
		t.Fatalf("Phases = %d, want 2", len(am.NebulaView.Phases))
	}
	if am.NebulaView.Phases[0].ID != "setup" {
		t.Errorf("Phase[0].ID = %q, want %q", am.NebulaView.Phases[0].ID, "setup")
	}

	// Start a phase — should create a per-phase LoopView.
	tm, _ = tm.Update(MsgPhaseTaskStarted{PhaseID: "setup", BeadID: "b-1", Title: "Setup"})
	am = tm.(AppModel)
	if am.NebulaView.Phases[0].Status != PhaseWorking {
		t.Errorf("Phase status = %d, want PhaseWorking(%d)", am.NebulaView.Phases[0].Status, PhaseWorking)
	}
	if _, ok := am.PhaseLoops["setup"]; !ok {
		t.Error("Expected PhaseLoops[\"setup\"] to exist")
	}

	// Phase cycle + agent events.
	tm, _ = tm.Update(MsgPhaseCycleStart{PhaseID: "setup", Cycle: 1, MaxCycles: 3})
	tm, _ = tm.Update(MsgPhaseAgentStart{PhaseID: "setup", Role: "coder"})
	tm, _ = tm.Update(MsgPhaseAgentDone{PhaseID: "setup", Role: "coder", CostUSD: 0.5, DurationMs: 5000})
	am = tm.(AppModel)
	lv := am.PhaseLoops["setup"]
	if lv == nil || len(lv.Cycles) != 1 {
		t.Fatal("Expected 1 cycle in setup LoopView")
	}
	if len(lv.Cycles[0].Agents) != 1 || !lv.Cycles[0].Agents[0].Done {
		t.Error("Expected coder agent to be done")
	}

	// Complete the phase.
	tm, _ = tm.Update(MsgPhaseTaskComplete{PhaseID: "setup", BeadID: "b-1", TotalCost: 0.5})
	am = tm.(AppModel)
	if am.NebulaView.Phases[0].Status != PhaseDone {
		t.Errorf("Phase status = %d, want PhaseDone(%d)", am.NebulaView.Phases[0].Status, PhaseDone)
	}
}

func TestNebulaNavigationDrillDown(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m

	// Init phases and start one.
	tm, _ = tm.Update(MsgNebulaInit{
		Name:   "nav-test",
		Phases: []PhaseInfo{{ID: "alpha", Title: "Alpha"}},
	})
	tm, _ = tm.Update(MsgPhaseTaskStarted{PhaseID: "alpha", BeadID: "b-1", Title: "Alpha"})
	tm, _ = tm.Update(MsgPhaseCycleStart{PhaseID: "alpha", Cycle: 1, MaxCycles: 3})
	tm, _ = tm.Update(MsgPhaseAgentStart{PhaseID: "alpha", Role: "coder"})

	am := tm.(AppModel)
	if am.Depth != DepthPhases {
		t.Errorf("Depth = %d, want DepthPhases", am.Depth)
	}

	// Drill into alpha.
	am.drillDown()
	if am.Depth != DepthPhaseLoop {
		t.Errorf("After drillDown: Depth = %d, want DepthPhaseLoop", am.Depth)
	}
	if am.FocusedPhase != "alpha" {
		t.Errorf("FocusedPhase = %q, want %q", am.FocusedPhase, "alpha")
	}

	// Drill into agent output.
	am.drillDown()
	if am.Depth != DepthAgentOutput {
		t.Errorf("After second drillDown: Depth = %d, want DepthAgentOutput", am.Depth)
	}

	// Drill back up.
	am.drillUp()
	if am.Depth != DepthPhaseLoop {
		t.Errorf("After drillUp: Depth = %d, want DepthPhaseLoop", am.Depth)
	}

	am.drillUp()
	if am.Depth != DepthPhases {
		t.Errorf("After second drillUp: Depth = %d, want DepthPhases", am.Depth)
	}
	if am.FocusedPhase != "" {
		t.Errorf("FocusedPhase should be empty, got %q", am.FocusedPhase)
	}
}

func TestPhaseUIBridgeImplementsInterface(t *testing.T) {
	model := NewAppModel(ModeNebula)
	model.Detail = NewDetailPanel(80, 10)
	p := tea.NewProgram(model, tea.WithoutSignalHandler())
	var iface ui.UI = NewPhaseUIBridge(p, "test-phase")
	_ = iface
}

func TestNewNebulaModelPrePopulated(t *testing.T) {
	// Verify that creating a nebula model with pre-populated phases
	// results in the correct initial state (what NewNebulaProgram does).
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.StatusBar.Name = "test-neb"
	phases := []PhaseInfo{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B", DependsOn: []string{"a"}},
		{ID: "c", Title: "Phase C", DependsOn: []string{"a", "b"}},
	}
	m.StatusBar.Total = len(phases)
	m.NebulaView.InitPhases(phases)

	if m.Mode != ModeNebula {
		t.Errorf("Mode = %d, want ModeNebula", m.Mode)
	}
	if m.StatusBar.Name != "test-neb" {
		t.Errorf("Name = %q, want %q", m.StatusBar.Name, "test-neb")
	}
	if m.StatusBar.Total != 3 {
		t.Errorf("Total = %d, want 3", m.StatusBar.Total)
	}
	if len(m.NebulaView.Phases) != 3 {
		t.Fatalf("Phases = %d, want 3", len(m.NebulaView.Phases))
	}
	if m.NebulaView.Phases[0].ID != "a" {
		t.Errorf("Phase[0].ID = %q, want %q", m.NebulaView.Phases[0].ID, "a")
	}
	// All phases should start in PhaseWaiting.
	for i, p := range m.NebulaView.Phases {
		if p.Status != PhaseWaiting {
			t.Errorf("Phase[%d].Status = %d, want PhaseWaiting", i, p.Status)
		}
	}
	// PhaseLoops should be empty initially.
	if len(m.PhaseLoops) != 0 {
		t.Errorf("PhaseLoops = %d, want 0", len(m.PhaseLoops))
	}
}

func TestNebulaViewInitPhases(t *testing.T) {
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "x", Title: "X Phase"},
		{ID: "y", Title: "Y Phase", DependsOn: []string{"x"}},
		{ID: "z", Title: "Z Phase", DependsOn: []string{"x", "y"}},
	})

	if len(nv.Phases) != 3 {
		t.Fatalf("Phases = %d, want 3", len(nv.Phases))
	}
	if nv.Phases[0].ID != "x" || nv.Phases[0].Status != PhaseWaiting {
		t.Errorf("Phase[0] = {%q, %d}, want {x, PhaseWaiting}", nv.Phases[0].ID, nv.Phases[0].Status)
	}
	if nv.Phases[1].BlockedBy != "x" {
		t.Errorf("Phase[1].BlockedBy = %q, want %q", nv.Phases[1].BlockedBy, "x")
	}
	// z depends on x and y — should show "x +1"
	if nv.Phases[2].BlockedBy != "x +1" {
		t.Errorf("Phase[2].BlockedBy = %q, want %q", nv.Phases[2].BlockedBy, "x +1")
	}
}

func TestNebulaViewSetPhaseStatus(t *testing.T) {
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	})

	nv.SetPhaseStatus("a", PhaseWorking)
	if nv.Phases[0].Status != PhaseWorking {
		t.Errorf("Status = %d, want PhaseWorking", nv.Phases[0].Status)
	}
	nv.SetPhaseStatus("a", PhaseDone)
	if nv.Phases[0].Status != PhaseDone {
		t.Errorf("Status = %d, want PhaseDone", nv.Phases[0].Status)
	}
	// Unknown phase ID — should not panic.
	nv.SetPhaseStatus("unknown", PhaseFailed)
}

func TestNebulaViewSetPhaseCostAndCycles(t *testing.T) {
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{{ID: "p", Title: "P"}})

	nv.SetPhaseCost("p", 1.23)
	if nv.Phases[0].CostUSD != 1.23 {
		t.Errorf("CostUSD = %f, want 1.23", nv.Phases[0].CostUSD)
	}
	nv.SetPhaseCycles("p", 3)
	if nv.Phases[0].Cycles != 3 {
		t.Errorf("Cycles = %d, want 3", nv.Phases[0].Cycles)
	}
	// Unknown phase — should not panic.
	nv.SetPhaseCost("nope", 0.5)
	nv.SetPhaseCycles("nope", 1)
}

func TestNebulaViewCursorNavigation(t *testing.T) {
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	})

	if nv.Cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", nv.Cursor)
	}

	nv.MoveDown()
	if nv.Cursor != 1 {
		t.Errorf("after MoveDown: cursor = %d, want 1", nv.Cursor)
	}
	nv.MoveDown()
	if nv.Cursor != 2 {
		t.Errorf("after second MoveDown: cursor = %d, want 2", nv.Cursor)
	}
	// Can't go past end.
	nv.MoveDown()
	if nv.Cursor != 2 {
		t.Errorf("at end: cursor = %d, want 2", nv.Cursor)
	}

	nv.MoveUp()
	if nv.Cursor != 1 {
		t.Errorf("after MoveUp: cursor = %d, want 1", nv.Cursor)
	}
	// Can't go above 0.
	nv.MoveUp()
	nv.MoveUp()
	if nv.Cursor != 0 {
		t.Errorf("at start: cursor = %d, want 0", nv.Cursor)
	}

	// SelectedPhase at cursor.
	p := nv.SelectedPhase()
	if p == nil || p.ID != "a" {
		t.Errorf("SelectedPhase = %v, want phase a", p)
	}
}

func TestAppModelQuitKey(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	// q should return tea.Quit.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("Expected tea.Quit command from q key")
	}

	// ctrl+c should also return tea.Quit.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Expected tea.Quit command from ctrl+c")
	}
}

func TestAppModelPhaseErrorSetsStatus(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	var tm tea.Model = m

	tm, _ = tm.Update(MsgNebulaInit{
		Name:   "err-test",
		Phases: []PhaseInfo{{ID: "fail-phase", Title: "Failing"}},
	})
	tm, _ = tm.Update(MsgPhaseTaskStarted{PhaseID: "fail-phase", BeadID: "b-1", Title: "Failing"})
	tm, _ = tm.Update(MsgPhaseError{PhaseID: "fail-phase", Msg: "something broke"})

	am := tm.(AppModel)
	if am.NebulaView.Phases[0].Status != PhaseFailed {
		t.Errorf("Status = %d, want PhaseFailed(%d)", am.NebulaView.Phases[0].Status, PhaseFailed)
	}
	if len(am.Messages) == 0 {
		t.Error("Expected error message in Messages")
	}
}

func TestAppModelDoneSignals(t *testing.T) {
	t.Run("MsgLoopDone", func(t *testing.T) {
		m := NewAppModel(ModeLoop)
		m.Detail = NewDetailPanel(80, 10)
		var tm tea.Model = m
		tm, _ = tm.Update(MsgLoopDone{Err: nil})
		am := tm.(AppModel)
		if !am.Done {
			t.Error("Expected Done = true after MsgLoopDone")
		}
	})

	t.Run("MsgNebulaDone", func(t *testing.T) {
		m := NewAppModel(ModeNebula)
		m.Detail = NewDetailPanel(80, 10)
		var tm tea.Model = m
		tm, _ = tm.Update(MsgNebulaDone{Err: nil})
		am := tm.(AppModel)
		if !am.Done {
			t.Error("Expected Done = true after MsgNebulaDone")
		}
	})

	t.Run("MsgNebulaDone with error", func(t *testing.T) {
		m := NewAppModel(ModeNebula)
		m.Detail = NewDetailPanel(80, 10)
		err := fmt.Errorf("workers failed")
		var tm tea.Model = m
		tm, _ = tm.Update(MsgNebulaDone{Err: err})
		am := tm.(AppModel)
		if !am.Done {
			t.Error("Expected Done = true")
		}
		if am.DoneErr == nil || am.DoneErr.Error() != "workers failed" {
			t.Errorf("DoneErr = %v, want 'workers failed'", am.DoneErr)
		}
	})
}

func TestAppModelViewDoesNotPanic(t *testing.T) {
	tests := []struct {
		name  string
		setup func() AppModel
	}{
		{
			name: "loop mode empty",
			setup: func() AppModel {
				m := NewAppModel(ModeLoop)
				m.Detail = NewDetailPanel(80, 10)
				m.Width = 80
				m.Height = 24
				return m
			},
		},
		{
			name: "nebula mode with phases",
			setup: func() AppModel {
				m := NewAppModel(ModeNebula)
				m.Detail = NewDetailPanel(80, 10)
				m.Width = 80
				m.Height = 24
				m.NebulaView.InitPhases([]PhaseInfo{
					{ID: "a", Title: "A"},
					{ID: "b", Title: "B", DependsOn: []string{"a"}},
				})
				return m
			},
		},
		{
			name: "nebula drilled into phase",
			setup: func() AppModel {
				m := NewAppModel(ModeNebula)
				m.Detail = NewDetailPanel(80, 10)
				m.Width = 80
				m.Height = 24
				m.NebulaView.InitPhases([]PhaseInfo{{ID: "x", Title: "X"}})
				lv := NewLoopView()
				lv.StartCycle(1)
				lv.StartAgent("coder")
				m.PhaseLoops["x"] = &lv
				m.FocusedPhase = "x"
				m.Depth = DepthPhaseLoop
				return m
			},
		},
		{
			name: "nebula at agent output depth",
			setup: func() AppModel {
				m := NewAppModel(ModeNebula)
				m.Detail = NewDetailPanel(80, 10)
				m.Width = 80
				m.Height = 24
				m.NebulaView.InitPhases([]PhaseInfo{{ID: "x", Title: "X"}})
				lv := NewLoopView()
				lv.StartCycle(1)
				lv.StartAgent("coder")
				lv.FinishAgent("coder", 0.5, 5000)
				m.PhaseLoops["x"] = &lv
				m.FocusedPhase = "x"
				m.Depth = DepthAgentOutput
				return m
			},
		},
		{
			name: "zero width (initializing)",
			setup: func() AppModel {
				m := NewAppModel(ModeLoop)
				m.Detail = NewDetailPanel(80, 10)
				return m
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setup()
			// Should not panic.
			_ = m.View()
		})
	}
}

func TestFooterBindings(t *testing.T) {
	km := DefaultKeyMap()

	loop := LoopFooterBindings(km)
	if len(loop) != 4 {
		t.Errorf("LoopFooterBindings = %d bindings, want 4", len(loop))
	}

	neb := NebulaFooterBindings(km)
	if len(neb) != 6 {
		t.Errorf("NebulaFooterBindings = %d bindings, want 6", len(neb))
	}

	detail := NebulaDetailFooterBindings(km)
	if len(detail) != 5 {
		t.Errorf("NebulaDetailFooterBindings = %d bindings, want 5", len(detail))
	}

	gate := GateFooterBindings(km)
	if len(gate) != 4 {
		t.Errorf("GateFooterBindings = %d bindings, want 4", len(gate))
	}
}

func TestLoopViewCursorNavigation(t *testing.T) {
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.FinishAgent("coder", 0.5, 5000)
	lv.StartAgent("reviewer")
	lv.FinishAgent("reviewer", 0.3, 3000)
	lv.StartCycle(2)
	lv.StartAgent("coder")

	// Total entries: cycle1 header + coder + reviewer + cycle2 header + coder = 5
	if got := lv.TotalEntries(); got != 5 {
		t.Errorf("TotalEntries = %d, want 5", got)
	}

	// Navigate down.
	lv.MoveDown() // cursor 1 -> coder (cycle 1)
	agent := lv.SelectedAgent()
	if agent == nil || agent.Role != "coder" {
		t.Error("Expected coder at cursor 1")
	}

	lv.MoveDown() // cursor 2 -> reviewer (cycle 1)
	agent = lv.SelectedAgent()
	if agent == nil || agent.Role != "reviewer" {
		t.Error("Expected reviewer at cursor 2")
	}

	// Navigate up.
	lv.MoveUp() // back to cursor 1
	agent = lv.SelectedAgent()
	if agent == nil || agent.Role != "coder" {
		t.Error("Expected coder at cursor 1 after MoveUp")
	}

	// Can't go above 0.
	lv.MoveUp()
	lv.MoveUp()
	if lv.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0 after multiple MoveUp", lv.Cursor)
	}
}

func TestPauseToggle(t *testing.T) {
	dir := t.TempDir()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.NebulaDir = dir
	m.NebulaView.InitPhases([]PhaseInfo{{ID: "a", Title: "Alpha"}})

	// Pause key at DepthPhases should write PAUSE file.
	cmd := m.handlePauseKey()
	if cmd == nil {
		t.Fatal("Expected command from handlePauseKey")
	}
	if !m.Paused {
		t.Error("Expected Paused = true")
	}

	pausePath := dir + "/PAUSE"
	if _, err := os.Stat(pausePath); os.IsNotExist(err) {
		t.Error("Expected PAUSE file to exist")
	}

	// The command should produce MsgPauseToggled{Paused: true}.
	msg := cmd()
	if pt, ok := msg.(MsgPauseToggled); !ok || !pt.Paused {
		t.Errorf("Expected MsgPauseToggled{Paused: true}, got %T %v", msg, msg)
	}

	// Second press should resume (remove PAUSE file).
	cmd = m.handlePauseKey()
	if cmd == nil {
		t.Fatal("Expected command from second handlePauseKey")
	}
	if m.Paused {
		t.Error("Expected Paused = false after toggle")
	}
	if _, err := os.Stat(pausePath); !os.IsNotExist(err) {
		t.Error("Expected PAUSE file to be removed")
	}

	msg = cmd()
	if pt, ok := msg.(MsgPauseToggled); !ok || pt.Paused {
		t.Errorf("Expected MsgPauseToggled{Paused: false}, got %T %v", msg, msg)
	}
}

func TestPauseInLoopModeIsNoop(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)
	m.NebulaDir = t.TempDir()

	cmd := m.handlePauseKey()
	if cmd != nil {
		t.Error("Expected nil command in loop mode")
	}
	if m.Paused {
		t.Error("Should not be paused in loop mode")
	}
}

func TestPauseAtWrongDepthIsNoop(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.NebulaDir = t.TempDir()
	m.Depth = DepthPhaseLoop

	cmd := m.handlePauseKey()
	if cmd != nil {
		t.Error("Expected nil command at DepthPhaseLoop")
	}
}

func TestPauseWithoutNebulaDirIsNoop(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	// NebulaDir is empty.

	cmd := m.handlePauseKey()
	if cmd != nil {
		t.Error("Expected nil command without NebulaDir")
	}
}

func TestStopWritesFile(t *testing.T) {
	dir := t.TempDir()

	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.NebulaDir = dir
	m.NebulaView.InitPhases([]PhaseInfo{{ID: "a", Title: "Alpha"}})

	cmd := m.handleStopKey()
	if cmd == nil {
		t.Fatal("Expected command from handleStopKey")
	}
	if !m.Stopping {
		t.Error("Expected Stopping = true")
	}

	stopPath := dir + "/STOP"
	if _, err := os.Stat(stopPath); os.IsNotExist(err) {
		t.Error("Expected STOP file to exist")
	}

	msg := cmd()
	if _, ok := msg.(MsgStopRequested); !ok {
		t.Errorf("Expected MsgStopRequested, got %T", msg)
	}

	// Second stop should be a noop.
	cmd = m.handleStopKey()
	if cmd != nil {
		t.Error("Expected nil command when already stopping")
	}
}

func TestStopInLoopModeIsNoop(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)
	m.NebulaDir = t.TempDir()

	cmd := m.handleStopKey()
	if cmd != nil {
		t.Error("Expected nil command in loop mode")
	}
}

func TestPauseWhileStoppingIsNoop(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.NebulaDir = t.TempDir()
	m.Stopping = true

	cmd := m.handlePauseKey()
	if cmd != nil {
		t.Error("Expected nil command when stopping")
	}
}

func TestRetryFailedPhase(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.NebulaView.InitPhases([]PhaseInfo{
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Beta"},
	})

	// Mark phase "a" as failed.
	m.NebulaView.SetPhaseStatus("a", PhaseFailed)
	lv := NewLoopView()
	m.PhaseLoops["a"] = &lv

	// Cursor is on "a" (index 0).
	cmd := m.handleRetryKey()
	// handleRetryKey returns nil but resets state.
	_ = cmd
	if m.NebulaView.Phases[0].Status != PhaseWaiting {
		t.Errorf("Phase status = %d, want PhaseWaiting", m.NebulaView.Phases[0].Status)
	}
	if _, ok := m.PhaseLoops["a"]; ok {
		t.Error("Expected PhaseLoops[\"a\"] to be removed after retry")
	}
	if len(m.Messages) == 0 || !strings.Contains(m.Messages[len(m.Messages)-1], "retrying phase a") {
		t.Error("Expected retry message")
	}
}

func TestRetryNonFailedPhaseIsNoop(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.NebulaView.InitPhases([]PhaseInfo{{ID: "a", Title: "Alpha"}})

	// Phase is in PhaseWaiting (not failed) — retry should be noop.
	cmd := m.handleRetryKey()
	if cmd != nil {
		t.Error("Expected nil command for non-failed phase")
	}
}

func TestRetryInLoopModeIsNoop(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)

	cmd := m.handleRetryKey()
	if cmd != nil {
		t.Error("Expected nil command in loop mode")
	}
}

func TestRetryAtPhaseLoopDepth(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.NebulaView.InitPhases([]PhaseInfo{{ID: "a", Title: "Alpha"}})
	m.NebulaView.SetPhaseStatus("a", PhaseFailed)
	m.FocusedPhase = "a"
	m.Depth = DepthPhaseLoop

	cmd := m.handleRetryKey()
	_ = cmd
	if m.NebulaView.Phases[0].Status != PhaseWaiting {
		t.Errorf("Phase status = %d, want PhaseWaiting after retry at DepthPhaseLoop", m.NebulaView.Phases[0].Status)
	}
}

func TestStatusBarShowsPausedIndicator(t *testing.T) {
	s := StatusBar{
		Name:      "test",
		Total:     5,
		Completed: 2,
		Width:     80,
		Paused:    true,
	}
	view := s.View()
	if !strings.Contains(view, "PAUSED") {
		t.Error("Expected PAUSED in status bar view")
	}
}

func TestStatusBarShowsStoppingIndicator(t *testing.T) {
	s := StatusBar{
		Name:      "test",
		Total:     5,
		Completed: 2,
		Width:     80,
		Stopping:  true,
	}
	view := s.View()
	if !strings.Contains(view, "STOPPING") {
		t.Error("Expected STOPPING in status bar view")
	}
}

func TestStatusBarStoppingOverridesPaused(t *testing.T) {
	s := StatusBar{
		Name:      "test",
		Total:     5,
		Completed: 2,
		Width:     80,
		Paused:    true,
		Stopping:  true,
	}
	view := s.View()
	if !strings.Contains(view, "STOPPING") {
		t.Error("Expected STOPPING in status bar view")
	}
	// PAUSED should not be shown when stopping.
	if strings.Contains(view, "PAUSED") {
		t.Error("Expected PAUSED to NOT be in status bar when stopping")
	}
}

func TestFooterShowsRetryForFailedPhase(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.NebulaView.InitPhases([]PhaseInfo{{ID: "a", Title: "Alpha"}})
	m.NebulaView.SetPhaseStatus("a", PhaseFailed)

	f := m.buildFooter()
	found := false
	for _, b := range f.Bindings {
		h := b.Help()
		if h.Desc == "retry" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected retry binding in footer for failed phase")
	}
}

func TestFooterNoRetryForNonFailedPhase(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.NebulaView.InitPhases([]PhaseInfo{{ID: "a", Title: "Alpha"}})
	// Phase is PhaseWaiting — no retry.

	f := m.buildFooter()
	for _, b := range f.Bindings {
		h := b.Help()
		if h.Desc == "retry" {
			t.Error("Should not have retry binding for non-failed phase")
		}
	}
}

func TestSelectedPhaseFailedHelper(t *testing.T) {
	t.Run("at DepthPhases", func(t *testing.T) {
		m := NewAppModel(ModeNebula)
		m.NebulaView.InitPhases([]PhaseInfo{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B"},
		})
		m.NebulaView.SetPhaseStatus("a", PhaseFailed)
		if !m.selectedPhaseFailed() {
			t.Error("Expected true for failed selected phase")
		}
		m.NebulaView.MoveDown() // move to "b"
		if m.selectedPhaseFailed() {
			t.Error("Expected false for non-failed selected phase")
		}
	})

	t.Run("at DepthPhaseLoop", func(t *testing.T) {
		m := NewAppModel(ModeNebula)
		m.NebulaView.InitPhases([]PhaseInfo{{ID: "x", Title: "X"}})
		m.NebulaView.SetPhaseStatus("x", PhaseFailed)
		m.FocusedPhase = "x"
		m.Depth = DepthPhaseLoop
		if !m.selectedPhaseFailed() {
			t.Error("Expected true for failed focused phase at DepthPhaseLoop")
		}
	})

	t.Run("loop mode", func(t *testing.T) {
		m := NewAppModel(ModeLoop)
		if m.selectedPhaseFailed() {
			t.Error("Expected false in loop mode")
		}
	})
}
