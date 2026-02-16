package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// ViewDepth tracks the navigation level in nebula mode.
type ViewDepth int

const (
	// DepthPhases shows the phase table (top level).
	DepthPhases ViewDepth = iota
	// DepthPhaseLoop shows a single phase's cycle timeline.
	DepthPhaseLoop
	// DepthAgentOutput shows agent output in the detail panel.
	DepthAgentOutput
)

// AppModel is the root BubbleTea model composing all sub-views.
type AppModel struct {
	Mode       Mode
	StatusBar  StatusBar
	LoopView   LoopView // used in loop mode (single task)
	NebulaView NebulaView
	Detail     DetailPanel
	Gate       *GatePrompt
	Overlay    *CompletionOverlay
	Toasts     []Toast
	Keys       KeyMap
	Width      int
	Height     int
	StartTime  time.Time
	Done       bool
	DoneErr    error
	Messages   []string // recent info/error messages

	// Nebula navigation state.
	Depth        ViewDepth            // current navigation depth
	FocusedPhase string               // phase ID we're drilled into
	PhaseLoops   map[string]*LoopView // per-phase cycle timelines

	// Execution control state (nebula mode).
	Paused    bool   // whether execution is paused
	Stopping  bool   // whether a stop has been requested
	NebulaDir string // path to nebula directory for intervention files
}

// NewAppModel creates a root model configured for the given mode.
func NewAppModel(mode Mode) AppModel {
	m := AppModel{
		Mode:       mode,
		LoopView:   NewLoopView(),
		NebulaView: NewNebulaView(),
		Keys:       DefaultKeyMap(),
		StartTime:  time.Now(),
		PhaseLoops: make(map[string]*LoopView),
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

		// Clamp cursors so they remain valid after a resize that may shrink lists.
		clampCursors(&m)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.LoopView.Spinner, cmd = m.LoopView.Spinner.Update(msg)
		cmds = append(cmds, cmd)
		m.NebulaView.Spinner, _ = m.NebulaView.Spinner.Update(msg)
		// Update spinners in per-phase loop views.
		for _, lv := range m.PhaseLoops {
			lv.Spinner, _ = lv.Spinner.Update(msg)
		}

	case MsgTick:
		cmds = append(cmds, tickCmd())

	// --- Loop mode (single task) ---
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
		toast, cmd := NewToast("error: "+msg.Msg, true)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)
	case MsgInfo:
		m.addMessage("%s", msg.Msg)

	// --- Nebula initialization ---
	case MsgNebulaInit:
		m.StatusBar.Name = msg.Name
		m.StatusBar.Total = len(msg.Phases)
		m.NebulaView.InitPhases(msg.Phases)

	// --- Nebula progress ---
	case MsgNebulaProgress:
		m.StatusBar.Completed = msg.Completed
		m.StatusBar.Total = msg.Total
		m.StatusBar.CostUSD = msg.TotalCostUSD

	// --- Phase-contextualized messages (nebula mode) ---
	case MsgPhaseTaskStarted:
		m.ensurePhaseLoop(msg.PhaseID)
		m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseWorking)
	case MsgPhaseTaskComplete:
		m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseDone)
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.Approved = true
		}
		m.NebulaView.SetPhaseCost(msg.PhaseID, msg.TotalCost)
	case MsgPhaseCycleStart:
		lv := m.ensurePhaseLoop(msg.PhaseID)
		lv.StartCycle(msg.Cycle)
		m.NebulaView.SetPhaseCycles(msg.PhaseID, msg.Cycle)
	case MsgPhaseAgentStart:
		lv := m.ensurePhaseLoop(msg.PhaseID)
		lv.StartAgent(msg.Role)
	case MsgPhaseAgentDone:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.FinishAgent(msg.Role, msg.CostUSD, msg.DurationMs)
		}
	case MsgPhaseAgentOutput:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.SetAgentOutput(msg.Role, msg.Cycle, msg.Output)
		}
		// If we're focused on this phase, refresh detail.
		if m.FocusedPhase == msg.PhaseID {
			m.updateDetailFromSelection()
		}
	case MsgPhaseCycleSummary:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.Approved = msg.Data.Approved
		}
		m.NebulaView.SetPhaseCost(msg.PhaseID, msg.Data.TotalCostUSD)
	case MsgPhaseIssuesFound:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.SetIssueCount(msg.Count)
		}
	case MsgPhaseApproved:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.Approved = true
		}
		m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseDone)
	case MsgPhaseError:
		m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseFailed)
		m.addMessage("[%s] %s", msg.PhaseID, msg.Msg)
		toast, cmd := NewToast(fmt.Sprintf("[%s] %s", msg.PhaseID, msg.Msg), true)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)
	case MsgPhaseInfo:
		// Informational — don't change phase status.

	// --- Gate ---
	case MsgGatePrompt:
		m.Gate = NewGatePrompt(msg.Checkpoint, msg.ResponseCh)
		m.Gate.Width = m.Width
		// Mark the phase as gated if we know which one.
		if msg.Checkpoint != nil {
			m.NebulaView.SetPhaseStatus(msg.Checkpoint.PhaseID, PhaseGate)
		}

	// --- Done signals ---
	case MsgLoopDone:
		m.Done = true
		m.DoneErr = msg.Err
		m.Overlay = NewCompletionFromLoopDone(msg, time.Since(m.StartTime), m.StatusBar.CostUSD)
	case MsgNebulaDone:
		m.Done = true
		m.DoneErr = msg.Err
		m.Overlay = NewCompletionFromNebulaDone(msg, time.Since(m.StartTime), m.StatusBar.CostUSD)

	// --- Toast auto-dismiss ---
	case MsgToastExpired:
		m.Toasts = removeToast(m.Toasts, msg.ID)
	}

	return m, tea.Batch(cmds...)
}

// ensurePhaseLoop creates a LoopView for a phase if it doesn't exist.
func (m *AppModel) ensurePhaseLoop(phaseID string) *LoopView {
	if lv, ok := m.PhaseLoops[phaseID]; ok {
		return lv
	}
	lv := NewLoopView()
	m.PhaseLoops[phaseID] = &lv
	return &lv
}

// clampCursors ensures all cursors remain within valid bounds.
// This prevents panics after a resize or data change that shrinks a list.
func clampCursors(m *AppModel) {
	// Clamp NebulaView cursor.
	if max := len(m.NebulaView.Phases) - 1; max >= 0 {
		if m.NebulaView.Cursor > max {
			m.NebulaView.Cursor = max
		}
	} else {
		m.NebulaView.Cursor = 0
	}

	// Clamp LoopView cursor.
	if max := m.LoopView.TotalEntries() - 1; max >= 0 {
		if m.LoopView.Cursor > max {
			m.LoopView.Cursor = max
		}
	} else {
		m.LoopView.Cursor = 0
	}

	// Clamp per-phase LoopView cursors.
	for _, lv := range m.PhaseLoops {
		if max := lv.TotalEntries() - 1; max >= 0 {
			if lv.Cursor > max {
				lv.Cursor = max
			}
		} else {
			lv.Cursor = 0
		}
	}
}

// handleKey processes keyboard input.
func (m AppModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Completion overlay takes precedence — only q to exit.
	if m.Overlay != nil {
		if key.Matches(msg, m.Keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	// Gate mode overrides normal keys.
	if m.Gate != nil {
		return m.handleGateKey(msg)
	}

	switch {
	case key.Matches(msg, m.Keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.Keys.Pause):
		m.handlePauseKey()

	case key.Matches(msg, m.Keys.Stop):
		m.handleStopKey()

	case key.Matches(msg, m.Keys.Retry):
		m.handleRetryKey()

	case key.Matches(msg, m.Keys.Up):
		m.moveUp()

	case key.Matches(msg, m.Keys.Down):
		m.moveDown()

	case key.Matches(msg, m.Keys.Enter):
		m.drillDown()

	case key.Matches(msg, m.Keys.Back):
		m.drillUp()
	}

	return m, nil
}

// handlePauseKey toggles pause state by writing/removing the PAUSE intervention file.
// Only active in nebula mode at the phase table level.
func (m *AppModel) handlePauseKey() {
	if m.Mode != ModeNebula || m.Depth != DepthPhases || m.NebulaDir == "" {
		return
	}
	if m.Stopping {
		return // can't pause while stopping
	}

	pausePath := filepath.Join(m.NebulaDir, "PAUSE")
	if m.Paused {
		// Resume: remove the PAUSE file.
		if err := os.Remove(pausePath); err != nil {
			m.addMessage("failed to remove PAUSE file: %s", err)
			return
		}
		m.Paused = false
	} else {
		// Pause: create the PAUSE file.
		if err := os.WriteFile(pausePath, []byte("paused by TUI\n"), 0644); err != nil {
			m.addMessage("failed to write PAUSE file: %s", err)
			return
		}
		m.Paused = true
	}
}

// handleStopKey writes the STOP intervention file.
// Only active in nebula mode at the phase table level.
func (m *AppModel) handleStopKey() {
	if m.Mode != ModeNebula || m.Depth != DepthPhases || m.NebulaDir == "" {
		return
	}
	if m.Stopping {
		return // already stopping
	}

	stopPath := filepath.Join(m.NebulaDir, "STOP")
	if err := os.WriteFile(stopPath, []byte("stopped by TUI\n"), 0644); err != nil {
		m.addMessage("failed to write STOP file: %s", err)
		return
	}
	m.Stopping = true
}

// handleRetryKey retries a failed phase by writing a RETRY intervention file
// and resetting the TUI's visual state. The WorkerGroup picks up the RETRY
// file and re-dispatches the phase.
// Only active in nebula mode when viewing a failed phase.
func (m *AppModel) handleRetryKey() {
	if m.Mode != ModeNebula || m.NebulaDir == "" {
		return
	}

	// Determine which phase to retry based on depth.
	var phaseID string
	switch m.Depth {
	case DepthPhases:
		if p := m.NebulaView.SelectedPhase(); p != nil && p.Status == PhaseFailed {
			phaseID = p.ID
		}
	case DepthPhaseLoop:
		if m.FocusedPhase != "" {
			for i := range m.NebulaView.Phases {
				if m.NebulaView.Phases[i].ID == m.FocusedPhase && m.NebulaView.Phases[i].Status == PhaseFailed {
					phaseID = m.FocusedPhase
					break
				}
			}
		}
	}

	if phaseID == "" {
		return // no failed phase selected
	}

	// Write a RETRY intervention file containing the phase ID.
	// The WorkerGroup monitors for this file and re-dispatches the phase.
	retryPath := filepath.Join(m.NebulaDir, "RETRY")
	if err := os.WriteFile(retryPath, []byte(phaseID+"\n"), 0644); err != nil {
		m.addMessage("failed to write RETRY file: %s", err)
		return
	}

	// Reset the TUI's visual state so it starts fresh.
	m.NebulaView.SetPhaseStatus(phaseID, PhaseWaiting)
	// Clear the per-phase loop view so it starts fresh.
	delete(m.PhaseLoops, phaseID)
	m.addMessage("retrying phase %s", phaseID)
}

// drillDown navigates deeper into the hierarchy.
func (m *AppModel) drillDown() {
	switch m.Mode {
	case ModeLoop:
		// In loop mode, enter toggles the detail panel.
		if m.Depth == DepthAgentOutput {
			return
		}
		m.Depth = DepthAgentOutput
		m.updateDetailFromSelection()

	case ModeNebula:
		switch m.Depth {
		case DepthPhases:
			// Drill into the selected phase's loop view.
			if p := m.NebulaView.SelectedPhase(); p != nil {
				m.FocusedPhase = p.ID
				m.Depth = DepthPhaseLoop
				m.updateDetailFromSelection()
			}
		case DepthPhaseLoop:
			// Drill into agent output.
			m.Depth = DepthAgentOutput
			m.updateDetailFromSelection()
		}
	}
}

// drillUp navigates back up the hierarchy.
func (m *AppModel) drillUp() {
	switch m.Mode {
	case ModeLoop:
		m.Depth = DepthPhases // collapse detail

	case ModeNebula:
		switch m.Depth {
		case DepthAgentOutput:
			m.Depth = DepthPhaseLoop
		case DepthPhaseLoop:
			m.Depth = DepthPhases
			m.FocusedPhase = ""
		}
	}
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

// moveUp delegates to the active view based on depth.
func (m *AppModel) moveUp() {
	switch m.Mode {
	case ModeLoop:
		m.LoopView.MoveUp()
	case ModeNebula:
		if m.Depth == DepthPhases {
			m.NebulaView.MoveUp()
		} else if m.Depth >= DepthPhaseLoop {
			if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
				lv.MoveUp()
			}
		}
	}
	m.updateDetailFromSelection()
}

// moveDown delegates to the active view based on depth.
func (m *AppModel) moveDown() {
	switch m.Mode {
	case ModeLoop:
		m.LoopView.MoveDown()
	case ModeNebula:
		if m.Depth == DepthPhases {
			m.NebulaView.MoveDown()
		} else if m.Depth >= DepthPhaseLoop {
			if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
				lv.MoveDown()
			}
		}
	}
	m.updateDetailFromSelection()
}

// updateDetailFromSelection updates the detail panel content
// based on the current view depth and selected item.
func (m *AppModel) updateDetailFromSelection() {
	switch m.Mode {
	case ModeLoop:
		agent := m.LoopView.SelectedAgent()
		if agent == nil {
			m.Detail.SetEmpty("Press enter to expand details")
			return
		}
		header := FormatAgentHeader(AgentContext{
			Role:       agent.Role,
			Cycle:      m.LoopView.SelectedCycleNumber(),
			DurationMs: agent.DurationMs,
			CostUSD:    agent.CostUSD,
			IssueCount: agent.IssueCount,
			Done:       agent.Done,
		})
		if agent.Output == "" {
			m.Detail.SetContentWithHeader(
				agent.Role+" output", header,
				"(output will appear when agent completes)",
			)
			return
		}
		body := FormatAgentOutput(agent.Output)
		m.Detail.SetContentWithHeader(agent.Role+" output", header, body)

	case ModeNebula:
		m.updateNebulaDetail()
	}
}

// updateNebulaDetail updates the detail panel for nebula mode based on depth.
func (m *AppModel) updateNebulaDetail() {
	// Build phase context header if we have a focused phase.
	var phaseHeader string
	if m.FocusedPhase != "" {
		if p := m.findPhase(m.FocusedPhase); p != nil {
			phaseHeader = FormatPhaseHeader(PhaseContext{
				ID:        p.ID,
				Title:     p.Title,
				Status:    p.Status,
				CostUSD:   p.CostUSD,
				Cycles:    p.Cycles,
				BlockedBy: p.BlockedBy,
			})
		}
	}

	switch m.Depth {
	case DepthPhaseLoop:
		// Show phase summary card in the detail panel.
		if phaseHeader != "" {
			m.Detail.SetContentWithHeader(m.FocusedPhase+" summary", phaseHeader, "(select an agent row and press enter to view output)")
		} else {
			m.Detail.SetEmpty("(select an agent row to view output)")
		}

	case DepthAgentOutput:
		lv := m.PhaseLoops[m.FocusedPhase]
		if lv == nil {
			m.Detail.SetEmpty("(no activity for this phase yet)")
			return
		}
		agent := lv.SelectedAgent()
		if agent == nil {
			m.Detail.SetContentWithHeader(m.FocusedPhase, phaseHeader, "(select an agent row to view output)")
			return
		}

		// Build combined header: phase context + agent context.
		agentHeader := FormatAgentHeader(AgentContext{
			Role:       agent.Role,
			Cycle:      lv.SelectedCycleNumber(),
			DurationMs: agent.DurationMs,
			CostUSD:    agent.CostUSD,
			IssueCount: agent.IssueCount,
			Done:       agent.Done,
		})
		header := agentHeader
		if phaseHeader != "" {
			header = phaseHeader + "\n" + agentHeader
		}

		title := fmt.Sprintf("%s → %s output", m.FocusedPhase, agent.Role)
		if agent.Output == "" {
			m.Detail.SetContentWithHeader(title, header, "(output will appear when agent completes)")
			return
		}
		body := FormatAgentOutput(agent.Output)
		m.Detail.SetContentWithHeader(title, header, body)

	default:
		m.Detail.SetEmpty("Press enter to expand details")
	}
}

// findPhase returns the PhaseEntry for a given phase ID, or nil.
func (m *AppModel) findPhase(phaseID string) *PhaseEntry {
	for i := range m.NebulaView.Phases {
		if m.NebulaView.Phases[i].ID == phaseID {
			return &m.NebulaView.Phases[i]
		}
	}
	return nil
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
	used := 3
	mainH := m.Height - used
	if mainH < 4 {
		return 0
	}
	return mainH * 2 / 5
}

// showDetailPanel returns whether the detail panel should be visible.
func (m AppModel) showDetailPanel() bool {
	if m.Mode == ModeLoop {
		return m.Depth == DepthAgentOutput
	}
	// In nebula mode, show detail at phase loop (summary) and agent output.
	return m.Depth >= DepthPhaseLoop
}

// View renders the full TUI.
func (m AppModel) View() string {
	if m.Width == 0 {
		return "initializing..."
	}

	// Terminal too small — show a centered message instead of broken layout.
	if m.Width < MinWidth || m.Height < MinHeight {
		return m.renderTooSmall()
	}

	var sections []string

	// Status bar — sync execution control state.
	m.StatusBar.Paused = m.Paused
	m.StatusBar.Stopping = m.Stopping
	sections = append(sections, m.StatusBar.View())

	// Breadcrumb (nebula drill-down) — hide if too narrow.
	if m.Mode == ModeNebula && m.Depth > DepthPhases && m.Width >= CompactWidth {
		sections = append(sections, m.renderBreadcrumb())
	}

	// Main view.
	sections = append(sections, m.renderMainView())

	// Detail panel (when drilled into agent output) — auto-collapse on short terminals.
	if m.showDetailPanel() && m.Height >= DetailCollapseHeight {
		sep := styleSectionBorder.Width(m.Width).Render("")
		sections = append(sections, sep)
		sections = append(sections, m.Detail.View())
	}

	// Gate overlay.
	if m.Gate != nil {
		sections = append(sections, m.Gate.View())
	}

	// Toast notifications (above footer).
	if len(m.Toasts) > 0 {
		sections = append(sections, RenderToasts(m.Toasts, m.Width))
	}

	// Footer.
	footer := m.buildFooter()
	sections = append(sections, footer.View())

	base := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Completion overlay — rendered over a dimmed placeholder background.
	if m.Overlay != nil {
		return m.Overlay.View(m.Width, m.Height)
	}

	return base
}

// renderTooSmall renders a centered "Terminal too small" message.
func (m AppModel) renderTooSmall() string {
	msg := fmt.Sprintf("Terminal too small (%dx%d)\nMinimum: %dx%d", m.Width, m.Height, MinWidth, MinHeight)
	style := lipgloss.NewStyle().
		Foreground(colorMuted).
		Align(lipgloss.Center).
		Width(m.Width).
		Height(m.Height)
	return style.Render(msg)
}

// renderBreadcrumb renders the navigation path for drill-down.
// Phase IDs are truncated with ellipsis if the breadcrumb would exceed the terminal width.
func (m AppModel) renderBreadcrumb() string {
	sep := " › "
	parts := []string{"phases"}
	if m.FocusedPhase != "" {
		// Reserve space for "phases › " + " › output" (if applicable) + padding.
		overhead := len("phases") + len(sep)
		if m.Depth == DepthAgentOutput {
			overhead += len(sep) + len("output")
		}
		// Leave 4 chars padding for the breadcrumb style padding.
		available := m.Width - overhead - 4
		if available < 4 {
			available = 4
		}
		parts = append(parts, TruncateWithEllipsis(m.FocusedPhase, available))
	}
	if m.Depth == DepthAgentOutput {
		parts = append(parts, "output")
	}
	renderedSep := styleBreadcrumbSep.Render(sep)
	path := strings.Join(parts, renderedSep)
	return styleBreadcrumb.Width(m.Width).Render(path)
}

// renderMainView renders the appropriate view for the current depth.
func (m AppModel) renderMainView() string {
	switch m.Mode {
	case ModeLoop:
		m.LoopView.Width = m.Width
		return m.LoopView.View()

	case ModeNebula:
		switch m.Depth {
		case DepthPhases:
			m.NebulaView.Width = m.Width
			return m.NebulaView.View()
		default:
			// Show the focused phase's loop view.
			if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
				lv.Width = m.Width
				return lv.View()
			}
			return styleDetailDim.Render("  (no activity for this phase yet)")
		}
	}
	return ""
}

// buildFooter creates the footer with appropriate bindings.
func (m AppModel) buildFooter() Footer {
	f := Footer{Width: m.Width}
	if m.Gate != nil {
		f.Bindings = GateFooterBindings(m.Keys)
	} else if m.Mode == ModeNebula {
		if m.Depth > DepthPhases {
			f.Bindings = NebulaDetailFooterBindings(m.Keys)
			// Add retry if the focused phase is failed.
			if m.selectedPhaseFailed() {
				f.Bindings = append(f.Bindings, m.Keys.Retry)
			}
		} else {
			f.Bindings = NebulaFooterBindings(m.Keys)
			// Add retry if the selected phase is failed.
			if m.selectedPhaseFailed() {
				f.Bindings = append(f.Bindings, m.Keys.Retry)
			}
		}
	} else {
		f.Bindings = LoopFooterBindings(m.Keys)
	}
	return f
}

// selectedPhaseFailed reports whether the currently selected/focused phase is in PhaseFailed state.
func (m AppModel) selectedPhaseFailed() bool {
	if m.Mode != ModeNebula {
		return false
	}
	switch m.Depth {
	case DepthPhases:
		if p := m.NebulaView.SelectedPhase(); p != nil {
			return p.Status == PhaseFailed
		}
	case DepthPhaseLoop:
		for i := range m.NebulaView.Phases {
			if m.NebulaView.Phases[i].ID == m.FocusedPhase {
				return m.NebulaView.Phases[i].Status == PhaseFailed
			}
		}
	}
	return false
}
