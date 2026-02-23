package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tycho"
)

// Mode indicates which top-level view the TUI is displaying.
type Mode int

const (
	ModeLoop Mode = iota
	ModeNebula
	ModeHome // landing page for browsing and selecting nebulas
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
	DiffFileOpen bool          // whether user has opened a single file's diff (Enter on file list)
	ShowBeads    bool          // whether the bead tracker is toggled on

	// Bead hierarchy state.
	LoopBeads  *BeadInfo            // bead hierarchy for loop mode
	PhaseBeads map[string]*BeadInfo // phaseID → latest bead hierarchy

	// Execution control state (nebula mode).
	Paused    bool   // whether execution is paused
	Stopping  bool   // whether a stop has been requested
	NebulaDir string // path to nebula directory for intervention files

	// Fabric bridge state — stored for later rendering by cockpit components.
	Entanglements []fabric.Entanglement // latest entanglement snapshot
	Discoveries   []fabric.Discovery    // posted discoveries
	Scratchpad    []MsgScratchpadEntry  // timestamped scratchpad notes
	StaleItems    []tycho.StaleItem     // latest stale warning items

	// Home mode state (landing page).
	HomeCursor     int            // cursor position in the home nebula list
	HomeOffset     int            // viewport scroll offset in the home nebula list
	HomeNebulae    []NebulaChoice // discovered nebulas for the home view
	HomeDir        string         // the .nebulas/ parent directory
	SelectedNebula string         // set when user selects a nebula from home; read after Run() returns

	// Nebula picker state (post-completion).
	AvailableNebulae []NebulaChoice // populated on MsgNebulaDone via discovery
	NextNebula       string         // set when user selects one; read after Run() returns
	PickerCursor     int            // cursor position in the nebula picker list
	ReturnToHome     bool           // set when user presses Esc on completion overlay to return to home

	// Resource monitoring.
	Resources  ResourceSnapshot   // latest resource usage snapshot
	Thresholds ResourceThresholds // thresholds for color-coding

	// Quit confirmation state.
	ShowQuitConfirm bool // whether the quit confirmation overlay is visible

	// Splash screen state — nil means splash is disabled (e.g. --no-splash).
	Splash *SplashModel
}

// NewAppModel creates a root model configured for the given mode.
// The binary-star splash animation is enabled by default; pass the model
// through DisableSplash to skip it (e.g. for --no-splash or CI).
func NewAppModel(mode Mode) AppModel {
	splash := NewSplash(DefaultSplashConfig())
	m := AppModel{
		Mode:       mode,
		LoopView:   NewLoopView(),
		NebulaView: NewNebulaView(),
		Keys:       DefaultKeyMap(),
		StartTime:  time.Now(),
		PhaseLoops: make(map[string]*LoopView),
		PhaseBeads: make(map[string]*BeadInfo),
		Thresholds: DefaultResourceThresholds(),
		Splash:     &splash,
	}
	m.StatusBar.StartTime = m.StartTime
	m.StatusBar.Thresholds = m.Thresholds
	// In home mode, show the detail panel by default.
	if mode == ModeHome {
		m.ShowPlan = true
	}
	return m
}

// DisableSplash clears the splash animation so the main view loads immediately.
func (m *AppModel) DisableSplash() {
	m.Splash = nil
}

// Init starts the spinner, tick timer, resource sampler, and (if enabled)
// the binary-star splash animation.
func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.LoopView.Spinner.Tick,
		m.NebulaView.Spinner.Tick,
		tickCmd(),
		resourceTickCmd(),
	}
	if m.Splash != nil {
		cmds = append(cmds, m.Splash.Init())
	}
	return tea.Batch(cmds...)
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
		m.StatusBar.InProgress = msg.OpenBeads
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

	case MsgGitPostCompletion:
		if m.Overlay != nil {
			m.Overlay.GitResult = msg.Result
		}

	case MsgNebulaChoicesLoaded:
		m.AvailableNebulae = msg.Choices
		if m.Overlay != nil {
			m.Overlay.NebulaChoices = msg.Choices
		}

	// --- Toast auto-dismiss ---
	case MsgToastExpired:
		m.Toasts = removeToast(m.Toasts, msg.ID)

	// --- Splash animation ---
	case splashTickMsg:
		if m.Splash != nil {
			updated, cmd := m.Splash.Update(msg)
			sm := updated.(SplashModel)
			m.Splash = &sm
			if sm.Done() {
				m.Splash = nil
			}
			cmds = append(cmds, cmd)
		}

	// MsgSplashDone is kept for programmatic splash dismissal (e.g. tests).
	case MsgSplashDone:
		m.Splash = nil

	// --- Fabric bridge messages ---
	case MsgEntanglementUpdate:
		m.Entanglements = msg.Entanglements

	case MsgDiscoveryPosted:
		m.Discoveries = append(m.Discoveries, msg.Discovery)
		toast, cmd := NewToast(fmt.Sprintf("discovery: %s", msg.Discovery.Kind), false)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	case MsgHail:
		toast, cmd := NewToast(fmt.Sprintf("⚠ hail from %s: %s", msg.PhaseID, msg.Discovery.Detail), true)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	case MsgScratchpadEntry:
		m.Scratchpad = append(m.Scratchpad, msg)

	case MsgStaleWarning:
		m.StaleItems = msg.Items
		if len(msg.Items) > 0 {
			toast, cmd := NewToast(fmt.Sprintf("stale: %d items need attention", len(msg.Items)), true)
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
	// Clamp HomeCursor and HomeOffset.
	if max := len(m.HomeNebulae) - 1; max >= 0 {
		if m.HomeCursor > max {
			m.HomeCursor = max
		}
		if m.HomeOffset > max {
			m.HomeOffset = max
		}
	} else {
		m.HomeCursor = 0
		m.HomeOffset = 0
	}

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
	// Splash screen: q quits, any other key skips the animation.
	if m.Splash != nil {
		if key.Matches(msg, m.Keys.Quit) {
			return m, tea.Quit
		}
		// Any keypress dismisses the splash.
		m.Splash = nil
		return m, nil
	}

	// Quit confirmation overlay — y confirms, n/Esc dismisses.
	if m.ShowQuitConfirm {
		switch msg.String() {
		case "y", "Y":
			return m, tea.Quit
		case "n", "N", "esc":
			m.ShowQuitConfirm = false
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	// Completion overlay — q quits, Esc returns to home, arrow keys for picker.
	if m.Overlay != nil {
		switch {
		case key.Matches(msg, m.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.Keys.Back):
			// Esc: return to the home screen instead of quitting.
			m.ReturnToHome = true
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

	// When viewing a single file's diff, route scroll keys to the detail panel.
	// Esc returns to the file list.
	if m.ShowDiff && m.DiffFileList != nil && m.DiffFileOpen {
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
		case key.Matches(msg, m.Keys.Back):
			// Return to the file list without leaving diff mode.
			m.DiffFileOpen = false
			m.updateDetailFromSelection()
			return m, nil
		}
	}

	// When the diff file list is active (but not viewing a single file),
	// ↑/↓ navigate the file list instead of scrolling the detail panel.
	if m.ShowDiff && m.DiffFileList != nil && !m.DiffFileOpen {
		switch {
		case key.Matches(msg, m.Keys.Up):
			m.DiffFileList.MoveUp()
			m.updateDetailFromSelection()
			return m, nil
		case key.Matches(msg, m.Keys.Down):
			m.DiffFileList.MoveDown()
			m.updateDetailFromSelection()
			return m, nil
		}
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

	// When the diff file list is active, Enter shows the selected file's
	// diff inline instead of drilling down into the loop view.
	if m.ShowDiff && m.DiffFileList != nil && key.Matches(msg, m.Keys.OpenDiff) {
		return m.showFileDiff()
	}

	// Home mode: Enter selects a nebula and exits the TUI.
	if m.Mode == ModeHome && key.Matches(msg, m.Keys.Enter) {
		if m.HomeCursor >= 0 && m.HomeCursor < len(m.HomeNebulae) {
			selected := m.HomeNebulae[m.HomeCursor].Path
			m.SelectedNebula = selected
			m.NextNebula = selected
			return m, tea.Quit
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.Keys.Quit):
		// Ctrl+C always force-quits.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// Show confirmation if there are in-progress phases.
		if m.hasInProgressPhases() {
			m.ShowQuitConfirm = true
			return m, nil
		}
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

// handleInfoKey toggles the detail/plan viewer in the detail panel.
// Active in home mode and nebula mode at DepthPhases or DepthPhaseLoop.
func (m *AppModel) handleInfoKey() {
	if m.Mode == ModeHome {
		m.ShowPlan = !m.ShowPlan
		if m.ShowPlan {
			m.updateHomeDetail()
		}
		return
	}

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
		m.DiffFileOpen = false
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
		m.DiffFileOpen = false
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

// showFileDiff renders the selected file's diff inline in the detail panel.
func (m AppModel) showFileDiff() (tea.Model, tea.Cmd) {
	fl := m.DiffFileList
	if fl == nil || len(fl.Files) == 0 {
		return m, nil
	}

	file := fl.SelectedFile()

	// Get the agent's raw diff.
	var rawDiff string
	switch m.Mode {
	case ModeLoop:
		if agent := m.LoopView.SelectedAgent(); agent != nil {
			rawDiff = agent.Diff
		}
	case ModeNebula:
		if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
			if agent := lv.SelectedAgent(); agent != nil {
				rawDiff = agent.Diff
			}
		}
	}
	if rawDiff == "" {
		return m, nil
	}

	body := RenderSingleFileDiff(rawDiff, file.Path, m.contentWidth()-4)
	m.Detail.SetContent(file.Path, body)
	m.DiffFileOpen = true
	return m, nil
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
		m.DiffFileOpen = false
		m.updateBeadDetail()
	}
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
			m.DiffFileOpen = false
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
			m.DiffFileOpen = false
			m.ShowBeads = false
			m.Depth = DepthAgentOutput
			m.updateDetailFromSelection()
		}
	}
}

// drillUp navigates back up the hierarchy.
func (m *AppModel) drillUp() {
	// Pressing esc dismisses overlay viewers first (without changing depth).
	// If viewing a single file diff, return to the file list first.
	if m.ShowDiff {
		if m.DiffFileOpen {
			m.DiffFileOpen = false
			m.updateDetailFromSelection()
			return
		}
		m.ShowDiff = false
		m.DiffFileList = nil
		return
	}
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
// Esc dismisses the gate by sending GateActionSkip (least destructive default).
func (m AppModel) handleGateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.Keys.Back):
		m.resolveGate(nebula.GateActionSkip)
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
	case ModeHome:
		if m.HomeCursor > 0 {
			m.HomeCursor--
		}
		m.adjustHomeOffset()
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
	case ModeHome:
		max := len(m.HomeNebulae) - 1
		if max < 0 {
			max = 0
		}
		if m.HomeCursor < max {
			m.HomeCursor++
		}
		m.adjustHomeOffset()
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
	// Home mode always uses its own detail renderer.
	if m.Mode == ModeHome {
		m.updateHomeDetail()
		return
	}
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

// updateHomeDetail populates the detail panel with the selected nebula's
// description, phase count, and status breakdown.
func (m *AppModel) updateHomeDetail() {
	if m.HomeCursor < 0 || m.HomeCursor >= len(m.HomeNebulae) {
		m.Detail.SetEmpty("No nebulas found")
		return
	}
	nc := m.HomeNebulae[m.HomeCursor]

	var b strings.Builder
	if nc.Description != "" {
		b.WriteString(nc.Description)
		b.WriteString("\n\n")
	}
	PhasesMsg := fmt.Sprintf("Phases: %d", nc.Phases)
	b.WriteString(PhasesMsg)
	if nc.Done > 0 {
		DoneMsg := fmt.Sprintf("  (%d done)", nc.Done)
		b.WriteString(DoneMsg)
	}
	b.WriteString("\nStatus: ")
	b.WriteString(nc.Status)

	m.Detail.SetContent(nc.Name, b.String())
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

// homeMainHeight computes the available lines for the home nebula list.
func (m AppModel) homeMainHeight() int {
	// Fixed chrome: status bar (1) + spacing (1) + footer (1).
	chrome := 3
	// Banner: estimate height based on size tier.
	if bv := m.Banner.View(); bv != "" {
		chrome += lipgloss.Height(bv)
	}
	// Detail panel.
	if m.showDetailPanel() && m.Height >= DetailCollapseHeight {
		chrome++ // separator line
		chrome += m.detailHeight()
	}
	h := m.Height - chrome
	if h < 3 {
		h = 3
	}
	return h
}

// adjustHomeOffset updates HomeOffset so the cursor is visible within the
// approximate viewport height. Called after cursor changes in moveUp/moveDown.
func (m *AppModel) adjustHomeOffset() {
	if len(m.HomeNebulae) == 0 {
		m.HomeOffset = 0
		return
	}
	hv := HomeView{
		Nebulae: m.HomeNebulae,
		Cursor:  m.HomeCursor,
		Offset:  m.HomeOffset,
		Height:  m.homeMainHeight(),
	}
	m.HomeOffset = hv.ensureCursorVisible()
}

// showDetailPanel returns whether the detail panel should be visible.
func (m AppModel) showDetailPanel() bool {
	if m.Mode == ModeHome {
		// Show the detail panel in home mode when there are nebulas to describe.
		// When ShowPlan is toggled off, hide the detail panel.
		return len(m.HomeNebulae) > 0 && m.ShowPlan
	}
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

	// Splash screen — show binary-star animation until it finishes or is skipped.
	if m.Splash != nil {
		return m.renderSplash()
	}

	// Compute content width: reduced by side panel in S-B mode.
	contentWidth := m.Width - m.Banner.SidePanelWidth()

	var sections []string

	// Status bar — always full terminal width; sync execution control state.
	m.StatusBar.Paused = m.Paused
	m.StatusBar.Stopping = m.Stopping
	if m.Mode == ModeHome {
		m.StatusBar.HomeMode = true
		m.StatusBar.HomeNebulaCount = len(m.HomeNebulae)
	}
	sections = append(sections, m.StatusBar.View())
	sections = append(sections, "") // Spacing between header and content.

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

	// Quit confirmation overlay — rendered over a dimmed background.
	if m.ShowQuitConfirm {
		dimmed := styleOverlayDimmed.Width(m.Width).Height(m.Height).Render(base)
		overlayBox := RenderQuitConfirm(m.Width, m.Height)
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

// renderSplash renders the binary-star splash animation centered in the terminal.
func (m AppModel) renderSplash() string {
	if m.Splash == nil {
		return ""
	}
	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, m.Splash.View())
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
	case ModeHome:
		hv := HomeView{
			Nebulae: m.HomeNebulae,
			Cursor:  m.HomeCursor,
			Offset:  m.HomeOffset,
			Width:   w,
			Height:  m.homeMainHeight(),
		}
		return hv.View()

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
		f.Bindings = DiffFileListFooterBindings(m.Keys)
		return f
	}

	if m.Gate != nil {
		f.Bindings = GateFooterBindings(m.Keys)
	} else if m.Mode == ModeHome {
		f.Bindings = HomeFooterBindings(m.Keys)
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

// hasInProgressPhases reports whether any nebula phase is currently working.
func (m AppModel) hasInProgressPhases() bool {
	if m.Mode != ModeNebula {
		return false
	}
	for i := range m.NebulaView.Phases {
		if m.NebulaView.Phases[i].Status == PhaseWorking {
			return true
		}
	}
	return false
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
