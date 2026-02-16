package tui

import (
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
