package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aaronsalm/quasar/internal/nebula"
)

// Mode indicates whether the TUI is in loop or nebula mode.
type Mode int

const (
	ModeLoop Mode = iota
	ModeNebula
)

// AppModel is the root BubbleTea model composing all sub-views.
type AppModel struct {
	Mode        Mode
	StatusBar   StatusBar
	LoopView    LoopView
	NebulaView  NebulaView
	Detail      DetailPanel
	Gate        *GatePrompt
	Keys        KeyMap
	Width       int
	Height      int
	StartTime   time.Time
	Done        bool
	DoneErr     error
	Messages    []string // recent info/error messages for detail
	showDetail  bool
}

// NewAppModel creates a root model configured for the given mode.
func NewAppModel(mode Mode) AppModel {
	m := AppModel{
		Mode:      mode,
		LoopView:  NewLoopView(),
		NebulaView: NewNebulaView(),
		Keys:      DefaultKeyMap(),
		StartTime: time.Now(),
	}
	m.StatusBar.StartTime = m.StartTime
	return m
}

// Init starts the spinner and tick timer.
func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.LoopView.Spinner.Tick,
		m.NebulaView.Spinner.Tick,
		tickCmd(),
	)
}

// tickCmd returns a command that sends a tick every second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return MsgTick{Time: t}
	})
}

// Update handles all messages.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.StatusBar.Width = msg.Width
		detailHeight := m.detailHeight()
		m.Detail.SetSize(msg.Width-2, detailHeight)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.LoopView.Spinner, cmd = m.LoopView.Spinner.Update(msg)
		cmds = append(cmds, cmd)
		m.NebulaView.Spinner, _ = m.NebulaView.Spinner.Update(msg)

	case MsgTick:
		cmds = append(cmds, tickCmd())

	// Loop lifecycle.
	case MsgTaskStarted:
		m.StatusBar.BeadID = msg.BeadID
		m.StatusBar.Name = msg.Title
	case MsgTaskComplete:
		m.StatusBar.CostUSD = msg.TotalCost
		m.Done = true
	case MsgCycleStart:
		m.StatusBar.Cycle = msg.Cycle
		m.StatusBar.MaxCycles = msg.MaxCycles
		m.LoopView.StartCycle(msg.Cycle)
	case MsgAgentStart:
		m.LoopView.StartAgent(msg.Role)
	case MsgAgentDone:
		m.LoopView.FinishAgent(msg.Role, msg.CostUSD, msg.DurationMs)
	case MsgCycleSummary:
		m.StatusBar.CostUSD = msg.Data.TotalCostUSD
		m.LoopView.Approved = msg.Data.Approved
	case MsgIssuesFound:
		m.LoopView.SetIssueCount(msg.Count)
	case MsgApproved:
		m.LoopView.Approved = true
	case MsgMaxCyclesReached:
		m.addMessage("Max cycles reached (%d)", msg.Max)
	case MsgBudgetExceeded:
		m.addMessage("Budget exceeded ($%.2f / $%.2f)", msg.Spent, msg.Limit)
	case MsgAgentOutput:
		m.LoopView.SetAgentOutput(msg.Role, msg.Cycle, msg.Output)
		m.updateDetailFromSelection()
	case MsgError:
		m.addMessage("error: %s", msg.Msg)
	case MsgInfo:
		m.addMessage("%s", msg.Msg)

	// Nebula lifecycle.
	case MsgNebulaProgress:
		m.StatusBar.Completed = msg.Completed
		m.StatusBar.Total = msg.Total
		m.StatusBar.CostUSD = msg.TotalCostUSD

	// Gate.
	case MsgGatePrompt:
		m.Gate = NewGatePrompt(msg.Checkpoint, msg.ResponseCh)
		m.Gate.Width = m.Width

	// Done signals.
	case MsgLoopDone:
		m.Done = true
		m.DoneErr = msg.Err
	case MsgNebulaDone:
		m.Done = true
		m.DoneErr = msg.Err
	}

	return m, tea.Batch(cmds...)
}

// handleKey processes keyboard input.
func (m AppModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Gate mode overrides normal keys.
	if m.Gate != nil {
		return m.handleGateKey(msg)
	}

	switch {
	case key.Matches(msg, m.Keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.Keys.Up):
		if m.showDetail {
			m.Detail.Update(msg)
		} else {
			m.moveUp()
		}
	case key.Matches(msg, m.Keys.Down):
		if m.showDetail {
			m.Detail.Update(msg)
		} else {
			m.moveDown()
		}
	case key.Matches(msg, m.Keys.Enter):
		if m.showDetail {
			// Already in detail, no-op.
		} else {
			m.showDetail = true
			m.updateDetailFromSelection()
		}
	case key.Matches(msg, m.Keys.Back):
		m.showDetail = false
	}

	return m, nil
}

// handleGateKey processes keys while a gate prompt is active.
func (m AppModel) handleGateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.Keys.Accept):
		m.resolveGate(nebula.GateActionAccept)
	case key.Matches(msg, m.Keys.Reject):
		m.resolveGate(nebula.GateActionReject)
	case key.Matches(msg, m.Keys.Retry):
		m.resolveGate(nebula.GateActionRetry)
	case key.Matches(msg, m.Keys.Skip):
		m.resolveGate(nebula.GateActionSkip)
	case key.Matches(msg, m.Keys.Enter):
		m.resolveGate(m.Gate.SelectedAction())
	case msg.String() == "left", msg.String() == "h":
		m.Gate.MoveLeft()
	case msg.String() == "right", msg.String() == "l":
		m.Gate.MoveRight()
	}
	return m, nil
}

// resolveGate sends the action and clears the gate.
func (m *AppModel) resolveGate(action nebula.GateAction) {
	if m.Gate != nil {
		m.Gate.Resolve(action)
		m.Gate = nil
	}
}

// moveUp delegates to the active view.
func (m *AppModel) moveUp() {
	switch m.Mode {
	case ModeLoop:
		m.LoopView.MoveUp()
	case ModeNebula:
		m.NebulaView.MoveUp()
	}
	m.updateDetailFromSelection()
}

// moveDown delegates to the active view.
func (m *AppModel) moveDown() {
	switch m.Mode {
	case ModeLoop:
		m.LoopView.MoveDown()
	case ModeNebula:
		m.NebulaView.MoveDown()
	}
	m.updateDetailFromSelection()
}

// updateDetailFromSelection updates the detail panel content
// based on the currently selected row.
func (m *AppModel) updateDetailFromSelection() {
	switch m.Mode {
	case ModeLoop:
		if agent := m.LoopView.SelectedAgent(); agent != nil {
			content := agent.Output
			if content == "" {
				content = "(no output captured)"
			}
			m.Detail.SetContent(agent.Role+" output", content)
		}
	case ModeNebula:
		if phase := m.NebulaView.SelectedPhase(); phase != nil {
			m.Detail.SetContent(phase.ID, phase.Title)
		}
	}
}

// addMessage appends a formatted message to the messages log.
func (m *AppModel) addMessage(format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	m.Messages = append(m.Messages, msg)
}

// detailHeight computes available height for the detail panel.
func (m AppModel) detailHeight() int {
	// status bar (1) + main view (variable) + footer (1) + borders
	used := 3
	mainH := m.Height - used
	if mainH < 4 {
		return 0
	}
	// Split: ~60% main view, ~40% detail.
	return mainH * 2 / 5
}

// mainViewHeight computes available height for the main list view.
func (m AppModel) mainViewHeight() int {
	used := 3 // status bar + footer + border
	if m.showDetail {
		used += m.detailHeight() + 1 // +1 for separator
	}
	h := m.Height - used
	if h < 1 {
		h = 1
	}
	return h
}

// View renders the full TUI.
func (m AppModel) View() string {
	if m.Width == 0 {
		return "initializing..."
	}

	var sections []string

	// Status bar.
	sections = append(sections, m.StatusBar.View())

	// Main view.
	switch m.Mode {
	case ModeLoop:
		m.LoopView.Width = m.Width
		sections = append(sections, m.LoopView.View())
	case ModeNebula:
		m.NebulaView.Width = m.Width
		sections = append(sections, m.NebulaView.View())
	}

	// Detail panel (when expanded).
	if m.showDetail {
		sep := styleSectionBorder.Width(m.Width).Render("")
		sections = append(sections, sep)
		sections = append(sections, m.Detail.View())
	}

	// Gate overlay.
	if m.Gate != nil {
		sections = append(sections, m.Gate.View())
	}

	// Footer.
	footer := m.buildFooter()
	sections = append(sections, footer.View())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// buildFooter creates the footer with appropriate bindings.
func (m AppModel) buildFooter() Footer {
	f := Footer{Width: m.Width}
	if m.Gate != nil {
		f.Bindings = GateFooterBindings(m.Keys)
	} else if m.Mode == ModeNebula {
		f.Bindings = NebulaFooterBindings(m.Keys)
	} else {
		f.Bindings = LoopFooterBindings(m.Keys)
	}
	return f
}
