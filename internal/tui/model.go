package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/nebula"
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
	Banner     Banner
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

	// Detail panel state.
	ShowPlan     bool          // whether the plan viewer is toggled on
	ShowDiff     bool          // whether the diff viewer is toggled on (vs raw output)
	DiffFileList *FileListView // navigable file list when diff view is active
	ShowBeads    bool          // whether the bead tracker is toggled on

	// Bead hierarchy state.
	LoopBeads  *BeadInfo            // bead hierarchy for loop mode
	PhaseBeads map[string]*BeadInfo // phaseID → latest bead hierarchy

	// Execution control state (nebula mode).
	Paused    bool   // whether execution is paused
	Stopping  bool   // whether a stop has been requested
	NebulaDir string // path to nebula directory for intervention files

	// Nebula picker state (post-completion).
	AvailableNebulae []NebulaChoice // populated on MsgNebulaDone via discovery
	NextNebula       string         // set when user selects one; read after Run() returns
	PickerCursor     int            // cursor position in the nebula picker list

	// Resource monitoring.
	Resources  ResourceSnapshot   // latest resource usage snapshot
	Thresholds ResourceThresholds // thresholds for color-coding

	// Architect overlay state (nebula mode).
	Architect     *ArchitectOverlay
	ArchitectFunc func(ctx context.Context, msg MsgArchitectStart) (*nebula.ArchitectResult, error) // injected by caller

	// Splash screen state.
	Splash bool // true while the startup splash is visible
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
		PhaseBeads: make(map[string]*BeadInfo),
		Thresholds: DefaultResourceThresholds(),
		Splash:     true,
	}
	m.StatusBar.StartTime = m.StartTime
	m.StatusBar.Thresholds = m.Thresholds
	return m
}

// splashDuration is how long the splash screen is shown at startup.
const splashDuration = 1500 * time.Millisecond

// Init starts the spinner, tick timer, resource sampler, and splash timer.
func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.LoopView.Spinner.Tick,
		m.NebulaView.Spinner.Tick,
		tickCmd(),
		resourceTickCmd(),
		tea.Tick(splashDuration, func(time.Time) tea.Msg {
			return MsgSplashDone{}
		}),
	)
}

// tickCmd returns a command that sends a tick every second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return MsgTick{Time: t}
	})
}

// resourceTickCmd returns a command that samples resources every 5 seconds.
// Uses context.Background since tea.Tick callbacks don't carry a context.
func resourceTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return MsgResourceUpdate{Snapshot: SampleResourcesFromSelf(context.Background())}
	})
}

// Update handles all messages.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Banner.Width = msg.Width
		m.Banner.Height = msg.Height
		m.StatusBar.Width = msg.Width
		contentWidth := msg.Width - m.Banner.SidePanelWidth()
		detailHeight := m.detailHeight()
		m.Detail.SetSize(contentWidth-2, detailHeight)

		// Clamp cursors so they remain valid after a resize that may shrink lists.
		clampCursors(&m)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		if m.showDetailPanel() {
			m.Detail.Update(msg)
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.LoopView.Spinner, cmd = m.LoopView.Spinner.Update(msg)
		cmds = append(cmds, cmd)
		m.NebulaView.Spinner, cmd = m.NebulaView.Spinner.Update(msg)
		cmds = append(cmds, cmd)
		// Update spinners in per-phase loop views.
		for _, lv := range m.PhaseLoops {
			lv.Spinner, cmd = lv.Spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		// Update architect overlay spinner.
		if m.Architect != nil && m.Architect.Step == stepWorking {
			m.Architect.Spinner, cmd = m.Architect.Spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case MsgTick:
		if !m.Done {
			cmds = append(cmds, tickCmd())
		}

	case MsgResourceUpdate:
		m.Resources = msg.Snapshot
		m.StatusBar.Resources = msg.Snapshot
		if !m.Done {
			cmds = append(cmds, resourceTickCmd())
		}

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
	case MsgAgentDiff:
		m.LoopView.SetAgentDiff(msg.Role, msg.Cycle, msg.Diff)
		m.LoopView.SetAgentDiffFiles(msg.Role, msg.Cycle, msg.Files, msg.BaseRef, msg.HeadRef, msg.WorkDir)
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
		m.NebulaView.SetPhaseCycles(msg.PhaseID, msg.Cycle, msg.MaxCycles)
		// Clear refactored indicator from previous cycle.
		m.NebulaView.SetPhaseRefactored(msg.PhaseID, false)
	case MsgPhaseAgentStart:
		lv := m.ensurePhaseLoop(msg.PhaseID)
		lv.StartAgent(msg.Role)
	case MsgPhaseAgentDone:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.FinishAgent(msg.Role, msg.CostUSD, msg.DurationMs)
		}
		if m.FocusedPhase == msg.PhaseID {
			m.updateDetailFromSelection()
		}
	case MsgPhaseAgentOutput:
		lv := m.ensurePhaseLoop(msg.PhaseID)
		lv.SetAgentOutput(msg.Role, msg.Cycle, msg.Output)
		// If we're focused on this phase, refresh detail.
		if m.FocusedPhase == msg.PhaseID {
			m.updateDetailFromSelection()
		}
	case MsgPhaseAgentDiff:
		lv := m.ensurePhaseLoop(msg.PhaseID)
		lv.SetAgentDiff(msg.Role, msg.Cycle, msg.Diff)
		lv.SetAgentDiffFiles(msg.Role, msg.Cycle, msg.Files, msg.BaseRef, msg.HeadRef, msg.WorkDir)
		if m.FocusedPhase == msg.PhaseID {
			m.updateDetailFromSelection()
		}
	case MsgPhaseCycleSummary:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.Approved = msg.Data.Approved
		}
		m.NebulaView.SetPhaseCost(msg.PhaseID, msg.Data.TotalCostUSD)
		if m.FocusedPhase == msg.PhaseID {
			m.updateDetailFromSelection()
		}
	case MsgPhaseIssuesFound:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.SetIssueCount(msg.Count)
		}
		if m.FocusedPhase == msg.PhaseID {
			m.updateDetailFromSelection()
		}
	case MsgPhaseApproved:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.Approved = true
		}
		m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseDone)
		// Clear refactored indicator on completion.
		m.NebulaView.SetPhaseRefactored(msg.PhaseID, false)
	case MsgPhaseRefactorPending:
		m.addMessage("[%s] refactor pending — will apply after current cycle", msg.PhaseID)
		toast, cmd := NewToast(fmt.Sprintf("[%s] refactor pending", msg.PhaseID), false)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)
	case MsgPhaseRefactorApplied:
		m.NebulaView.SetPhaseRefactored(msg.PhaseID, true)
		toast, cmd := NewToast(fmt.Sprintf("[%s] refactor applied", msg.PhaseID), false)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)
	case MsgPhaseError:
		m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseFailed)
		m.addMessage("[%s] %s", msg.PhaseID, msg.Msg)
		toast, cmd := NewToast(fmt.Sprintf("[%s] %s", msg.PhaseID, msg.Msg), true)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)
	case MsgPhaseInfo:
		// Informational — don't change phase status.

	// --- Hot-added phase ---
	case MsgPhaseHotAdded:
		m.NebulaView.AppendPhase(PhaseInfo{
			ID:        msg.PhaseID,
			Title:     msg.Title,
			DependsOn: msg.DependsOn,
		})
		m.StatusBar.Total = len(m.NebulaView.Phases)
		toast, cmd := NewToast(fmt.Sprintf("+ %s added to nebula", msg.PhaseID), false)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	// --- Bead hierarchy ---
	case MsgBeadUpdate:
		root := msg.Root
		m.LoopBeads = &root
		if m.ShowBeads {
			m.updateBeadDetail()
		}
	case MsgPhaseBeadUpdate:
		root := msg.Root
		m.PhaseBeads[msg.PhaseID] = &root
		if m.ShowBeads {
			m.updateBeadDetail()
		}

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
		m.StatusBar.FinalElapsed = time.Since(m.StartTime).Truncate(time.Second)
		m.Overlay = NewCompletionFromLoopDone(msg, time.Since(m.StartTime), m.StatusBar.CostUSD)
	case MsgNebulaDone:
		m.Done = true
		m.DoneErr = msg.Err
		m.StatusBar.FinalElapsed = time.Since(m.StartTime).Truncate(time.Second)
		m.Overlay = NewCompletionFromNebulaDone(msg, time.Since(m.StartTime), m.StatusBar.CostUSD, len(m.NebulaView.Phases))
		// Cancel and clear any active architect overlay.
		if m.Architect != nil {
			if m.Architect.CancelFunc != nil {
				m.Architect.CancelFunc()
			}
			m.Architect = nil
		}
		// Discover sibling nebulae in background.
		if m.NebulaDir != "" {
			nebulaDir := m.NebulaDir
			cmds = append(cmds, func() tea.Msg {
				choices, err := DiscoverNebulae(nebulaDir)
				if err != nil {
					return MsgError{Msg: fmt.Sprintf("nebula discovery: %v", err)}
				}
				return MsgNebulaChoicesLoaded{Choices: choices}
			})
		}

	case MsgNebulaChoicesLoaded:
		m.AvailableNebulae = msg.Choices
		if m.Overlay != nil {
			m.Overlay.NebulaChoices = msg.Choices
		}

	// --- Architect overlay ---
	case MsgArchitectConfirm:
		if m.NebulaDir != "" && msg.Result != nil {
			// Override the dependencies with the user's selection.
			msg.Result.PhaseSpec.DependsOn = msg.DependsOn

			// Reconstruct the full phase file (+++TOML+++ frontmatter + body).
			fileData, err := nebula.MarshalPhaseFile(msg.Result.PhaseSpec)
			if err != nil {
				m.addMessage("failed to marshal phase file: %s", err)
				toast, cmd := NewToast(fmt.Sprintf("marshal failed: %s", err), true)
				m.Toasts = append(m.Toasts, toast)
				cmds = append(cmds, cmd)
			} else {
				filePath := filepath.Join(m.NebulaDir, msg.Result.Filename)
				if err := os.WriteFile(filePath, fileData, 0644); err != nil {
					m.addMessage("failed to write phase file: %s", err)
					toast, cmd := NewToast(fmt.Sprintf("write failed: %s", err), true)
					m.Toasts = append(m.Toasts, toast)
					cmds = append(cmds, cmd)
				} else {
					m.addMessage("wrote %s — watcher will pick it up", msg.Result.Filename)
					toast, cmd := NewToast(fmt.Sprintf("wrote %s", msg.Result.Filename), false)
					m.Toasts = append(m.Toasts, toast)
					cmds = append(cmds, cmd)
				}
			}
		}

	case MsgArchitectStart:
		if m.Architect != nil && m.ArchitectFunc != nil {
			fn := m.ArchitectFunc
			startMsg := msg
			ctx, cancel := context.WithCancel(context.Background())
			m.Architect.CancelFunc = cancel
			cmds = append(cmds, func() tea.Msg {
				result, err := safeArchitectCall(ctx, fn, startMsg)
				return MsgArchitectResult{Result: result, Err: err}
			})
		} else if m.Architect != nil {
			// No architect function wired up — report an error.
			m.Architect = nil
			m.addMessage("architect not available (no invoker configured)")
			toast, cmd := NewToast("architect not available", true)
			m.Toasts = append(m.Toasts, toast)
			cmds = append(cmds, cmd)
		}

	case MsgArchitectResult:
		if m.Architect != nil {
			if msg.Err != nil {
				m.Architect = nil
				m.addMessage("architect error: %s", msg.Err)
				toast, cmd := NewToast(fmt.Sprintf("architect: %s", msg.Err), true)
				m.Toasts = append(m.Toasts, toast)
				cmds = append(cmds, cmd)
			} else {
				m.Architect.SetResult(msg.Result, m.NebulaView.Phases)
			}
		}

	// --- Toast auto-dismiss ---
	case MsgToastExpired:
		m.Toasts = removeToast(m.Toasts, msg.ID)

	// --- Splash screen ---
	case MsgSplashDone:
		m.Splash = false

	// --- External difftool ---
	case MsgDiffToolDone:
		if msg.Err != nil {
			m.addMessage("difftool: %s", msg.Err)
			toast, cmd := NewToast(fmt.Sprintf("difftool: %s", msg.Err), true)
			m.Toasts = append(m.Toasts, toast)
			cmds = append(cmds, cmd)
		}
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

// safeArchitectCall wraps an architect function call with panic recovery.
// Any panic is converted to an error in the returned result.
func safeArchitectCall(ctx context.Context, fn func(context.Context, MsgArchitectStart) (*nebula.ArchitectResult, error), msg MsgArchitectStart) (result *nebula.ArchitectResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("architect panic: %v", r)
		}
	}()
	return fn(ctx, msg)
}

// handleKey processes keyboard input.
func (m AppModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Splash screen: only q quits; all other keys are ignored.
	if m.Splash {
		if key.Matches(msg, m.Keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	// Architect overlay takes priority — it intercepts all keys when active.
	if m.Architect != nil {
		return m.handleArchitectKey(msg)
	}

	// Completion overlay — q to exit, arrow keys for picker.
	if m.Overlay != nil {
		switch {
		case key.Matches(msg, m.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.Keys.Up):
			if len(m.AvailableNebulae) > 0 && m.PickerCursor > 0 {
				m.PickerCursor--
				m.Overlay.PickerCursor = m.PickerCursor
			}
		case key.Matches(msg, m.Keys.Down):
			if len(m.AvailableNebulae) > 0 && m.PickerCursor < len(m.AvailableNebulae)-1 {
				m.PickerCursor++
				m.Overlay.PickerCursor = m.PickerCursor
			}
		case key.Matches(msg, m.Keys.Enter):
			if len(m.AvailableNebulae) > 0 && m.PickerCursor < len(m.AvailableNebulae) {
				m.NextNebula = m.AvailableNebulae[m.PickerCursor].Path
				return m, tea.Quit
			}
		}
		return m, nil
	}

	// Gate mode overrides normal keys.
	if m.Gate != nil {
		return m.handleGateKey(msg)
	}

	// At DepthAgentOutput, reroute ↑/↓ to scroll the detail panel
	// instead of moving the list cursor. PageUp/PageDown/Home/End
	// also scroll the detail panel when it is visible.
	if m.Depth == DepthAgentOutput {
		switch {
		case key.Matches(msg, m.Keys.Up):
			m.Detail.Update(msg)
			return m, nil
		case key.Matches(msg, m.Keys.Down):
			m.Detail.Update(msg)
			return m, nil
		case key.Matches(msg, m.Keys.PageUp):
			m.Detail.Update(msg)
			return m, nil
		case key.Matches(msg, m.Keys.PageDown):
			m.Detail.Update(msg)
			return m, nil
		case key.Matches(msg, m.Keys.Home):
			m.Detail.Update(msg)
			return m, nil
		case key.Matches(msg, m.Keys.End):
			m.Detail.Update(msg)
			return m, nil
		}
	} else if m.showDetailPanel() {
		// At other depths with detail panel visible (e.g. beads/plan),
		// PageUp/PageDown/Home/End scroll the detail panel.
		switch {
		case key.Matches(msg, m.Keys.PageUp):
			m.Detail.Update(msg)
			return m, nil
		case key.Matches(msg, m.Keys.PageDown):
			m.Detail.Update(msg)
			return m, nil
		case key.Matches(msg, m.Keys.Home):
			m.Detail.Update(msg)
			return m, nil
		case key.Matches(msg, m.Keys.End):
			m.Detail.Update(msg)
			return m, nil
		}
	}

	// When the diff file list is active, Enter opens the external difftool
	// instead of drilling down into the loop view.
	if m.ShowDiff && m.DiffFileList != nil && key.Matches(msg, m.Keys.OpenDiff) {
		return m.openDiffTool()
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

	case key.Matches(msg, m.Keys.Info):
		m.handleInfoKey()

	case key.Matches(msg, m.Keys.Diff):
		m.handleDiffKey()

	case key.Matches(msg, m.Keys.Beads):
		m.handleBeadsKey()

	case key.Matches(msg, m.Keys.NewPhase):
		m.handleNewPhaseKey()

	case key.Matches(msg, m.Keys.EditPhase):
		m.handleEditPhaseKey()
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

// handleInfoKey toggles the phase plan viewer in the detail panel.
// Active in nebula mode at DepthPhases or DepthPhaseLoop.
func (m *AppModel) handleInfoKey() {
	if m.Mode != ModeNebula {
		return
	}
	if m.Depth != DepthPhases && m.Depth != DepthPhaseLoop {
		return
	}

	m.ShowPlan = !m.ShowPlan
	if m.ShowPlan {
		// Dismiss other panel modes for mutual exclusivity.
		m.ShowBeads = false
		m.ShowDiff = false
		m.DiffFileList = nil
		m.updatePlanDetail()
	}
}

// handleDiffKey toggles between output and diff view in the detail panel.
// Active at DepthAgentOutput when the selected agent has a diff.
func (m *AppModel) handleDiffKey() {
	if m.Depth != DepthAgentOutput {
		return
	}
	m.ShowDiff = !m.ShowDiff
	if m.ShowDiff {
		// Dismiss other panel modes for mutual exclusivity.
		m.ShowPlan = false
		m.ShowBeads = false
		m.DiffFileList = m.buildDiffFileList()
		// If no file list and no raw diff text, reset to prevent inconsistent state.
		if m.DiffFileList == nil && !m.hasSelectedAgentDiff() {
			m.ShowDiff = false
		}
	} else {
		m.DiffFileList = nil
	}
	m.updateDetailFromSelection()
}

// buildDiffFileList constructs a FileListView from the currently selected agent's diff metadata.
func (m *AppModel) buildDiffFileList() *FileListView {
	var agent *AgentEntry
	switch m.Mode {
	case ModeLoop:
		agent = m.LoopView.SelectedAgent()
	case ModeNebula:
		if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
			agent = lv.SelectedAgent()
		}
	}
	if agent == nil || len(agent.DiffFiles) == 0 {
		return nil
	}
	return NewFileListView(agent.DiffFiles, m.contentWidth()-4, agent.BaseRef, agent.HeadRef, agent.WorkDir)
}

// hasSelectedAgentDiff reports whether the currently selected agent has raw diff text.
func (m *AppModel) hasSelectedAgentDiff() bool {
	var agent *AgentEntry
	switch m.Mode {
	case ModeLoop:
		agent = m.LoopView.SelectedAgent()
	case ModeNebula:
		if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
			agent = lv.SelectedAgent()
		}
	}
	return agent != nil && agent.Diff != ""
}

// openDiffTool launches the user's configured git difftool for the selected file.
// It suspends the TUI via tea.ExecProcess and resumes when the tool exits.
// Returns a no-op when refs or files are unavailable.
func (m AppModel) openDiffTool() (tea.Model, tea.Cmd) {
	fl := m.DiffFileList
	if fl == nil || len(fl.Files) == 0 {
		return m, nil
	}
	if fl.BaseRef == "" || fl.HeadRef == "" {
		return m, nil
	}

	file := fl.SelectedFile()
	c := exec.Command("git", "difftool", "--no-prompt",
		fl.BaseRef+".."+fl.HeadRef,
		"--", file.Path)
	c.Dir = fl.WorkDir
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return MsgDiffToolDone{Err: err}
	})
}

// handleBeadsKey toggles the bead tracker view in the detail panel.
// In loop mode, shows the single task's bead hierarchy.
// In nebula mode at DepthPhases or DepthPhaseLoop, shows the focused phase's beads.
func (m *AppModel) handleBeadsKey() {
	m.ShowBeads = !m.ShowBeads
	if m.ShowBeads {
		// Dismiss other panel modes.
		m.ShowPlan = false
		m.ShowDiff = false
		m.DiffFileList = nil
		m.updateBeadDetail()
	}
}

// handleNewPhaseKey opens the architect overlay in create mode.
// Only active in nebula mode at the phase table level.
func (m *AppModel) handleNewPhaseKey() {
	if m.Mode != ModeNebula || m.Depth != DepthPhases {
		return
	}
	m.Architect = NewArchitectOverlay("create", "", m.NebulaView.Phases)
}

// handleEditPhaseKey opens the architect overlay in refactor mode for the selected phase.
// Only active for in-progress or waiting phases.
func (m *AppModel) handleEditPhaseKey() {
	if m.Mode != ModeNebula || m.Depth != DepthPhases {
		return
	}
	p := m.NebulaView.SelectedPhase()
	if p == nil {
		return
	}
	if p.Status != PhaseWorking && p.Status != PhaseWaiting {
		return
	}
	m.Architect = NewArchitectOverlay("refactor", p.ID, m.NebulaView.Phases)
}

// handleArchitectKey processes keyboard input while the architect overlay is active.
func (m AppModel) handleArchitectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.Architect

	switch a.Step {
	case stepInput:
		switch {
		case key.Matches(msg, m.Keys.Back):
			m.Architect = nil
			return m, nil
		case key.Matches(msg, m.Keys.Enter):
			prompt := a.InputValue()
			if prompt == "" {
				return m, nil
			}
			a.StartWorking()
			// The actual architect invocation is done via a command that the
			// caller wires up. We send MsgArchitectStart so the bridge can
			// dispatch the agent call.
			return m, func() tea.Msg {
				return MsgArchitectStart{
					Mode:    a.Mode,
					PhaseID: a.PhaseID,
					Prompt:  prompt,
				}
			}
		default:
			// Forward to textarea.
			var cmd tea.Cmd
			a.TextArea, cmd = a.TextArea.Update(msg)
			return m, cmd
		}

	case stepWorking:
		if key.Matches(msg, m.Keys.Back) {
			if a.CancelFunc != nil {
				a.CancelFunc()
			}
			m.Architect = nil
			return m, nil
		}
		return m, nil

	case stepPreview:
		switch {
		case key.Matches(msg, m.Keys.Back):
			m.Architect = nil
			return m, nil
		case key.Matches(msg, m.Keys.Enter):
			// Confirm: write the file.
			result := a.Result
			deps := a.SelectedDeps()
			m.Architect = nil
			return m, func() tea.Msg {
				return MsgArchitectConfirm{
					Result:    result,
					DependsOn: deps,
				}
			}
		case key.Matches(msg, m.Keys.Up):
			a.MoveDepUp()
		case key.Matches(msg, m.Keys.Down):
			a.MoveDepDown()
		case msg.String() == " ":
			newPhaseID := ""
			if a.Result != nil {
				newPhaseID = a.Result.PhaseSpec.ID
			}
			a.ToggleDep(newPhaseID, func(deps []string) bool {
				return WouldCreateCycle(m.NebulaView.Phases, newPhaseID, deps)
			})
		}
		return m, nil
	}

	return m, nil
}

// updateBeadDetail populates the detail panel with the bead hierarchy.
func (m *AppModel) updateBeadDetail() {
	switch m.Mode {
	case ModeLoop:
		if m.LoopBeads == nil {
			m.Detail.SetEmpty("(no bead data yet)")
			return
		}
		bv := NewBeadView()
		bv.SetRoot(*m.LoopBeads)
		bv.Width = m.contentWidth() - 2
		m.Detail.SetContent("Beads: "+m.LoopBeads.Title, bv.View())

	case ModeNebula:
		phaseID := m.FocusedPhase
		if phaseID == "" {
			// At DepthPhases, use the selected phase.
			if p := m.NebulaView.SelectedPhase(); p != nil {
				phaseID = p.ID
			}
		}
		if phaseID == "" {
			m.Detail.SetEmpty("(select a phase to view beads)")
			return
		}
		root, ok := m.PhaseBeads[phaseID]
		if !ok || root == nil {
			m.Detail.SetEmpty("(no bead data for " + phaseID + ")")
			return
		}
		bv := NewBeadView()
		bv.SetRoot(*root)
		bv.Width = m.contentWidth() - 2
		m.Detail.SetContent("Beads: "+root.Title, bv.View())
	}
}

// updatePlanDetail populates the detail panel with the selected phase's plan body.
func (m *AppModel) updatePlanDetail() {
	var phase *PhaseEntry
	switch m.Depth {
	case DepthPhases:
		phase = m.NebulaView.SelectedPhase()
	case DepthPhaseLoop:
		phase = m.findPhase(m.FocusedPhase)
	}
	if phase == nil {
		m.Detail.SetEmpty("No phase selected")
		return
	}
	if phase.PlanBody == "" {
		m.Detail.SetContent("📋 Plan: "+phase.ID, "(no plan body available)")
		return
	}
	m.Detail.SetContent("📋 Plan: "+phase.ID, phase.PlanBody)
}

// drillDown navigates deeper into the hierarchy.
func (m *AppModel) drillDown() {
	switch m.Mode {
	case ModeLoop:
		// In loop mode at DepthAgentOutput, Enter is a no-op — don't clear state.
		if m.Depth == DepthAgentOutput {
			return
		}
		// Dismiss overlay viewers when actually transitioning.
		m.ShowPlan = false
		m.ShowDiff = false
		m.DiffFileList = nil
		m.ShowBeads = false
		m.Depth = DepthAgentOutput
		m.updateDetailFromSelection()

	case ModeNebula:
		switch m.Depth {
		case DepthPhases:
			// Dismiss overlay viewers when transitioning into a phase.
			m.ShowPlan = false
			m.ShowDiff = false
			m.DiffFileList = nil
			m.ShowBeads = false
			// Drill into the selected phase's loop view.
			if p := m.NebulaView.SelectedPhase(); p != nil {
				m.FocusedPhase = p.ID
				m.Depth = DepthPhaseLoop
				m.updateDetailFromSelection()
			}
		case DepthPhaseLoop:
			// Dismiss overlay viewers when transitioning to agent output.
			m.ShowPlan = false
			m.ShowDiff = false
			m.DiffFileList = nil
			m.ShowBeads = false
			m.Depth = DepthAgentOutput
			m.updateDetailFromSelection()
		}
	}
}

// drillUp navigates back up the hierarchy.
func (m *AppModel) drillUp() {
	// Pressing esc dismisses plan/beads viewers first (without changing depth).
	if m.ShowPlan {
		m.ShowPlan = false
		return
	}
	if m.ShowBeads {
		m.ShowBeads = false
		return
	}

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
// When the diff file list is active, navigation targets it instead of the main list.
func (m *AppModel) moveUp() {
	if m.ShowDiff && m.DiffFileList != nil {
		m.DiffFileList.MoveUp()
		m.updateDetailFromSelection()
		return
	}
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
// When the diff file list is active, navigation targets it instead of the main list.
func (m *AppModel) moveDown() {
	if m.ShowDiff && m.DiffFileList != nil {
		m.DiffFileList.MoveDown()
		m.updateDetailFromSelection()
		return
	}
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
	// Bead tracker takes precedence when toggled on.
	if m.ShowBeads {
		m.updateBeadDetail()
		return
	}
	// Plan viewer takes precedence when toggled on.
	if m.ShowPlan {
		m.updatePlanDetail()
		return
	}
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
		if m.ShowDiff && agent.Diff != "" {
			var body string
			if m.DiffFileList != nil {
				body = m.DiffFileList.View()
			} else {
				body = RenderDiffView(agent.Diff, m.contentWidth()-4)
			}
			m.Detail.SetContentWithHeader(agent.Role+" diff", header, body)
			return
		}
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

		if m.ShowDiff && agent.Diff != "" {
			title := fmt.Sprintf("%s → %s diff", m.FocusedPhase, agent.Role)
			var body string
			if m.DiffFileList != nil {
				body = m.DiffFileList.View()
			} else {
				body = RenderDiffView(agent.Diff, m.contentWidth()-4)
			}
			m.Detail.SetContentWithHeader(title, header, body)
			return
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
		return m.Depth == DepthAgentOutput || m.ShowBeads
	}
	// In nebula mode, show detail when plan/beads is toggled or drilled into a phase.
	if m.ShowPlan || m.ShowBeads {
		return true
	}
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

	// Splash screen — show centered quasar art for the first 1.5s.
	if m.Splash {
		return m.renderSplash()
	}

	// Compute content width: reduced by side panel in S-B mode.
	contentWidth := m.Width - m.Banner.SidePanelWidth()

	var sections []string

	// Status bar — always full terminal width; sync execution control state.
	m.StatusBar.Paused = m.Paused
	m.StatusBar.Stopping = m.Stopping
	sections = append(sections, m.StatusBar.View())

	// Top banner (S-A or XS-A modes) — between status bar and content.
	if bannerView := m.Banner.View(); bannerView != "" {
		sections = append(sections, bannerView)
	}

	// Build the "middle" section: breadcrumb + main view + detail + gate + toasts.
	// In side panel mode, this section sits to the right of the art panel.
	var middle []string

	// Breadcrumb (nebula drill-down) — hide if too narrow.
	if m.Mode == ModeNebula && m.Depth > DepthPhases && contentWidth >= CompactWidth {
		middle = append(middle, m.renderBreadcrumb())
	}

	// Main view.
	middle = append(middle, m.renderMainView())

	// Detail panel (when drilled into agent output) — auto-collapse on short terminals.
	if m.showDetailPanel() && m.Height >= DetailCollapseHeight {
		sep := styleSectionBorder.Width(contentWidth).Render("")
		middle = append(middle, sep)
		middle = append(middle, m.Detail.View())
	}

	// Gate overlay.
	if m.Gate != nil {
		middle = append(middle, m.Gate.View())
	}

	// Toast notifications (above footer).
	if len(m.Toasts) > 0 {
		middle = append(middle, RenderToasts(m.Toasts, contentWidth))
	}

	middleStr := lipgloss.JoinVertical(lipgloss.Left, middle...)

	// Side panel mode (S-B): join art panel horizontally with middle content.
	if m.Banner.Size() == BannerSB {
		middleHeight := lipgloss.Height(middleStr)
		artPanel := m.Banner.SidePanelView(middleHeight)
		middleStr = lipgloss.JoinHorizontal(lipgloss.Top, artPanel, middleStr)
	}

	sections = append(sections, middleStr)

	// Footer — always full terminal width.
	footer := m.buildFooter()
	sections = append(sections, footer.View())

	base := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Architect overlay — rendered over a dimmed background.
	if m.Architect != nil {
		dimmed := styleOverlayDimmed.Width(m.Width).Height(m.Height).Render(base)
		overlayBox := m.Architect.View(m.Width, m.Height)
		return compositeOverlay(dimmed, overlayBox, m.Width, m.Height)
	}

	// Completion overlay — rendered over a dimmed background.
	if m.Overlay != nil {
		dimmed := styleOverlayDimmed.Width(m.Width).Height(m.Height).Render(base)
		overlayBox := m.Overlay.View(m.Width, m.Height)
		return compositeOverlay(dimmed, overlayBox, m.Width, m.Height)
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

// renderSplash renders the splash screen with the best-fitting quasar art centered.
// Falls back to smaller art variants if the terminal is too narrow for XL.
func (m AppModel) renderSplash() string {
	var splash string
	switch {
	case m.Width >= 92:
		splash = m.Banner.SplashView()
	default:
		// Use the normal banner view (XS or S-A) for narrower terminals.
		splash = m.Banner.View()
	}
	if splash == "" {
		// Terminal too narrow for any art — show just the name.
		splash = lipgloss.NewStyle().
			Foreground(colorStarYellow).
			Bold(true).
			Render("Q  U  A  S  A  R")
	}
	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, splash)
}

// contentWidth returns the available width for main content, accounting for the
// side panel when in S-B mode.
func (m AppModel) contentWidth() int {
	return m.Width - m.Banner.SidePanelWidth()
}

// renderBreadcrumb renders the navigation path for drill-down.
// Phase IDs are truncated with ellipsis if the breadcrumb would exceed the content width.
func (m AppModel) renderBreadcrumb() string {
	w := m.contentWidth()
	sep := " › "
	parts := []string{"phases"}
	if m.FocusedPhase != "" {
		// Reserve space for "phases › " + " › output" (if applicable) + padding.
		overhead := len("phases") + len(sep)
		if m.Depth == DepthAgentOutput {
			overhead += len(sep) + len("output")
		}
		// Leave 4 chars padding for the breadcrumb style padding.
		available := w - overhead - 4
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
	return styleBreadcrumb.Width(w).Render(path)
}

// renderMainView renders the appropriate view for the current depth.
func (m AppModel) renderMainView() string {
	w := m.contentWidth()
	switch m.Mode {
	case ModeLoop:
		m.LoopView.Width = w
		return m.LoopView.View()

	case ModeNebula:
		switch m.Depth {
		case DepthPhases:
			m.NebulaView.Width = w
			return m.NebulaView.View()
		default:
			// Show the focused phase's loop view.
			if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
				lv.Width = w
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

	// When the diff file list is active, show dedicated diff-mode bindings.
	if m.ShowDiff && m.DiffFileList != nil {
		hasRefs := m.DiffFileList.BaseRef != "" && m.DiffFileList.HeadRef != ""
		f.Bindings = DiffFileListFooterBindings(m.Keys, hasRefs)
		return f
	}

	if m.Gate != nil {
		f.Bindings = GateFooterBindings(m.Keys)
	} else if m.Mode == ModeNebula {
		if m.Depth > DepthPhases {
			f.Bindings = NebulaDetailFooterBindings(m.Keys)
			if m.Depth == DepthAgentOutput {
				diffBind := m.Keys.Diff
				if m.ShowDiff {
					diffBind.SetHelp("d", "output")
				} else {
					diffBind.SetHelp("d", "diff")
				}
				f.Bindings = append(f.Bindings, diffBind)
			}
			if m.selectedPhaseFailed() {
				f.Bindings = append(f.Bindings, m.Keys.Retry)
			}
		} else {
			f.Bindings = NebulaFooterBindings(m.Keys)
			if m.selectedPhaseFailed() {
				f.Bindings = append(f.Bindings, m.Keys.Retry)
			}
		}
	} else {
		f.Bindings = LoopFooterBindings(m.Keys)
		if m.Depth == DepthAgentOutput {
			diffBind := m.Keys.Diff
			if m.ShowDiff {
				diffBind.SetHelp("d", "output")
			} else {
				diffBind.SetHelp("d", "diff")
			}
			f.Bindings = append(f.Bindings, diffBind)
		}
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
