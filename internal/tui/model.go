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

	"github.com/papapumpkin/quasar/internal/chat"
	"github.com/papapumpkin/quasar/internal/dialog"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tycho"
	"github.com/papapumpkin/quasar/internal/ui"
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
	Mode              Mode
	StatusBar         StatusBar
	Banner            Banner
	LoopView          LoopView // used in loop mode (single task)
	NebulaView        NebulaView
	Detail            DetailPanel
	Gate              *GatePrompt
	PendingGates      []MsgGatePrompt // queued gate prompts waiting for the current gate to resolve
	Hail              *HailOverlay
	GumAvailable      bool          // true if gum binary is in PATH
	GumBinPath        string        // resolved path to gum binary
	gumHailResponseCh chan<- string // stashed response channel for active gum hail
	Overlay           *CompletionOverlay
	Toasts            []Toast
	Keys              KeyMap
	Width             int
	Height            int
	StartTime         time.Time
	Done              bool
	DoneErr           error
	Messages          []string // recent info/error messages

	// Nebula navigation state.
	Depth        ViewDepth            // current navigation depth
	ActiveTab    CockpitTab           // active cockpit tab (board, entanglements, scratchpad)
	FocusedPhase string               // phase ID we're drilled into
	PhaseLoops   map[string]*LoopView // per-phase cycle timelines

	// Detail panel state.
	ShowPlan       bool          // whether the plan viewer is toggled on
	ShowDiff       bool          // whether the diff viewer is toggled on (vs raw output)
	DiffFileList   *FileListView // navigable file list when diff view is active
	DiffFileOpen   bool          // whether user has opened a single file's diff (Enter on file list)
	DiffSideBySide bool          // whether diff is rendered side-by-side (true) or unified (false)
	DiffCollapsed  map[int]bool  // hunk index → collapsed state when viewing a single file diff
	DiffHunkCount  int           // number of hunks in the currently viewed file
	ShowBeads      bool          // whether the bead tracker is toggled on

	// Bead hierarchy state.
	LoopBeads  *BeadInfo            // bead hierarchy for loop mode
	PhaseBeads map[string]*BeadInfo // phaseID → latest bead hierarchy

	// Execution control state (nebula mode).
	Paused    bool   // whether execution is paused
	Stopping  bool   // whether a stop has been requested
	NebulaDir string // path to nebula directory for intervention files

	// Graph view state — live DAG visualization tab.
	Graph GraphView // DAG graph renderer

	// Board view state — columnar board as alternative to the NebulaView table.
	Board        BoardView // columnar board renderer
	BoardActive  bool      // true = columnar board, false = table view
	boardSizedAt bool      // true after the first WindowSizeMsg sets the default

	// Split-pane sidebar state — persistent phase list on the left.
	Sidebar   Sidebar   // phase list sidebar component
	PaneFocus PaneFocus // which pane (sidebar vs main) has keyboard focus

	// Worker card state — live detail cards for active quasars.
	WorkerCards   map[string]*WorkerCard // phaseID → live worker card
	nextQuasarNum int                    // counter for assigning quasar IDs (q-1, q-2, ...)

	// Fabric bridge state — stored for later rendering by cockpit components.
	Entanglements    []fabric.Entanglement // latest entanglement snapshot
	EntanglementView EntanglementView      // persistent entanglement viewer with cursor state
	Discoveries      []fabric.Discovery    // posted discoveries
	Scratchpad       []MsgScratchpadEntry  // timestamped scratchpad notes
	ScratchpadView   ScratchpadView        // persistent scratchpad viewer with viewport
	StaleItems       []tycho.StaleItem     // latest stale warning items

	// File impact state — aggregate file footprint across phases.
	ImpactView ImpactView                 // persistent impact viewer with viewport
	PhaseFiles map[string][]FileStatEntry // phaseID → file stats (populated from diffs)

	// Hail tracking — pending hails from agents that need human attention.
	PendingHails []ui.HailInfo    // unresolved hails tracked via MsgHailReceived/MsgHailResolved
	HailList     *HailListOverlay // non-nil when the hail list overlay is active

	// Interactive dialog — back-and-forth session with an agent/scheduler.
	Dialog *DialogOverlay // non-nil when a dialog overlay is active

	// Home mode state (landing page).
	HomeCursor      int            // cursor position in the home nebula list
	HomeOffset      int            // viewport scroll offset in the home nebula list
	HomeNebulae     []NebulaChoice // discovered nebulas for the home view
	HomeFilter      HomeFilter     // active filter for the home nebula list
	HomeDir         string         // the .nebulas/ parent directory
	SelectedNebula  string         // set when user selects a nebula from home; read after Run() returns
	ShowPlanPreview bool           // true when the plan preview is visible (between home and apply)
	PlanPreview     *PlanView      // plan preview state (non-nil when active)

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

	// Embedded phase chat state — non-nil when a contextual chat is active.
	PhaseChat *ChatModel // embedded chat model for phase-linked conversations
}

// NewAppModel creates a root model configured for the given mode.
// The binary-star splash animation is enabled by default; pass the model
// through DisableSplash to skip it (e.g. for --no-splash or CI).
func NewAppModel(mode Mode) AppModel {
	splash := NewSplash(DefaultSplashConfig())
	m := AppModel{
		Mode:        mode,
		LoopView:    NewLoopView(),
		NebulaView:  NewNebulaView(),
		Board:       NewBoardView(),
		BoardActive: false, // set to true on first WindowSizeMsg if terminal is wide enough
		Sidebar:     NewSidebar(),
		PaneFocus:   PaneFocusMain, // start with main area focused
		Keys:        DefaultKeyMap(),
		StartTime:   time.Now(),
		PhaseLoops:  make(map[string]*LoopView),
		PhaseBeads:  make(map[string]*BeadInfo),
		PhaseFiles:  make(map[string][]FileStatEntry),
		ImpactView:  NewImpactView(),
		Thresholds:  DefaultResourceThresholds(),
		Splash:      &splash,
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

// waitForDialogMsg returns a Cmd that blocks on the session's ToHuman
// channel and delivers the next agent message as MsgDialogAgentMsg.
// When the session is closed, it delivers MsgDialogClosed instead.
func waitForDialogMsg(sess *dialog.MemSession) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-sess.ToHuman():
			if !ok {
				return MsgDialogClosed{SessionID: sess.ID()}
			}
			return MsgDialogAgentMsg{SessionID: sess.ID(), Message: msg}
		case <-sess.Closed():
			return MsgDialogClosed{SessionID: sess.ID()}
		}
	}
}

// Update handles all messages.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate to embedded phase chat when active.
	if m.PhaseChat != nil {
		return m.updatePhaseChat(msg)
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Banner.Width = msg.Width
		m.Banner.Height = msg.Height
		m.StatusBar.Width = msg.Width

		// Sidebar sizing — updates dimensions for split-pane layout.
		sidebarW := m.sidebarWidth()
		if sidebarW > 0 {
			m.Sidebar.Width = sidebarW
			m.Sidebar.Height = m.sidebarHeight()
			m.Sidebar.Focus = m.PaneFocus == PaneFocusSidebar
		}

		contentWidth := m.contentWidth()
		detailHeight := m.detailHeight()
		if m.Mode == ModeHome {
			detailHeight = m.homeDetailHeight()
		}
		m.Detail.SetSize(contentWidth-2, detailHeight)
		m.ScratchpadView.SetSize(contentWidth, detailHeight)

		// Pass dimensions to the board view.
		m.Board.Width = contentWidth
		m.Board.Height = detailHeight
		m.EntanglementView.SetSize(contentWidth, detailHeight)
		m.ImpactView.SetSize(contentWidth, detailHeight)

		// Board view sizing: on the first resize, default to board if wide enough.
		// On subsequent resizes, auto-fallback to table if terminal shrinks below threshold.
		if !m.boardSizedAt {
			m.boardSizedAt = true
			m.BoardActive = msg.Width >= BoardMinWidth
		} else if msg.Width < BoardMinWidth {
			m.BoardActive = false
		}

		// Update gate overlay dimensions if it's currently visible.
		if m.Gate != nil {
			m.Gate.Width = m.contentWidth()
			m.Gate.Height = m.Height
		}

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
		m.StatusBar.TotalTokens += msg.Tokens
	case MsgCycleSummary:
		m.StatusBar.CostUSD = msg.Data.TotalCostUSD
		m.LoopView.Approved = msg.Data.Approved
		if msg.Data.Phase == "review_complete" {
			sat := SatisfactionFromReview(msg.Data.Approved, msg.Data.IssueCount)
			m.LoopView.SetSatisfaction(sat)
		}
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
		m.Graph = NewGraphView(msg.Phases, m.contentWidth(), m.detailHeight())

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
		m.Graph.SetPhaseStatus(msg.PhaseID, PhaseWorking)
		// Create a worker card for this active phase.
		wc := m.ensureWorkerCard(msg.PhaseID)
		wc.StartedAt = time.Now()
		// Populate phase title from the message or from the phase table.
		if msg.Title != "" {
			wc.PhaseTitle = msg.Title
		} else if p := m.findPhase(msg.PhaseID); p != nil {
			wc.PhaseTitle = p.Title
		}
	case MsgPhaseTaskComplete:
		m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseDone)
		m.Graph.SetPhaseStatus(msg.PhaseID, PhaseDone)
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.Approved = true
			// Extract completion note from last reviewer summary.
			if note := lastReviewerSummary(lv); note != "" {
				m.NebulaView.SetPhaseCompletionNote(msg.PhaseID, note)
			}
		}
		m.NebulaView.SetPhaseCost(msg.PhaseID, msg.TotalCost)
		// Remove worker card when phase completes.
		delete(m.WorkerCards, msg.PhaseID)
	case MsgPhaseCycleStart:
		lv := m.ensurePhaseLoop(msg.PhaseID)
		lv.StartCycle(msg.Cycle)
		m.NebulaView.SetPhaseCycles(msg.PhaseID, msg.Cycle, msg.MaxCycles)
		// Clear refactored indicator from previous cycle.
		m.NebulaView.SetPhaseRefactored(msg.PhaseID, false)
		// Update worker card cycle info.
		if wc := m.WorkerCards[msg.PhaseID]; wc != nil {
			wc.Cycle = msg.Cycle
			wc.MaxCycles = msg.MaxCycles
		}
	case MsgPhaseAgentStart:
		lv := m.ensurePhaseLoop(msg.PhaseID)
		lv.StartAgent(msg.Role)
		// Update worker card agent role and activity with phase context.
		if wc := m.WorkerCards[msg.PhaseID]; wc != nil {
			wc.AgentRole = msg.Role
			wc.Activity = richActivity(msg.Role, wc.PhaseTitle, wc.Claims)
		}
		// Propagate simple activity to board card.
		m.NebulaView.SetPhaseActivity(msg.PhaseID, activityFromRole(msg.Role))
	case MsgWorkerActivity:
		if msg.PhaseID != "" {
			if wc := m.WorkerCards[msg.PhaseID]; wc != nil {
				wc.Stream.Push(msg.Activity)
			}
		}
	case MsgPhaseAgentDone:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.FinishAgent(msg.Role, msg.CostUSD, msg.DurationMs)
		}
		m.StatusBar.TotalTokens += msg.Tokens
		if m.FocusedPhase == msg.PhaseID {
			m.updateDetailFromSelection()
		}
		// Update worker card token count and cost immediately on each agent
		// completion (coder + reviewer), not just at phase end.
		if wc := m.WorkerCards[msg.PhaseID]; wc != nil {
			wc.TokensUsed += msg.Tokens
			wc.CostUSD += msg.CostUSD
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
		// Update worker card claims from diff file list.
		if wc := m.WorkerCards[msg.PhaseID]; wc != nil && len(msg.Files) > 0 {
			claims := make([]string, len(msg.Files))
			for i, f := range msg.Files {
				claims[i] = f.Path
			}
			wc.Claims = claims
		}
		// Track file impacts per phase for the aggregate impact view.
		if len(msg.Files) > 0 {
			m.PhaseFiles[msg.PhaseID] = msg.Files
			m.ImpactView.Rebuild(m.PhaseFiles)
		}
	case MsgPhaseCycleSummary:
		if lv := m.PhaseLoops[msg.PhaseID]; lv != nil {
			lv.Approved = msg.Data.Approved
			if msg.Data.Phase == "review_complete" {
				sat := SatisfactionFromReview(msg.Data.Approved, msg.Data.IssueCount)
				lv.SetSatisfaction(sat)
			}
		}
		m.NebulaView.SetPhaseCost(msg.PhaseID, msg.Data.TotalCostUSD)
		m.NebulaView.SetPhaseReviewerSatisfied(msg.PhaseID, msg.Data.Approved)
		if msg.Data.MaxBudgetUSD > 0 {
			m.NebulaView.SetPhaseMaxBudget(msg.PhaseID, msg.Data.MaxBudgetUSD)
		}
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
		m.Graph.SetPhaseStatus(msg.PhaseID, PhaseDone)
		// Clear refactored indicator on completion.
		m.NebulaView.SetPhaseRefactored(msg.PhaseID, false)
		// Remove worker card on approval.
		delete(m.WorkerCards, msg.PhaseID)
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
		m.Graph.SetPhaseStatus(msg.PhaseID, PhaseFailed)
		// Set completion note from error message.
		m.NebulaView.SetPhaseCompletionNote(msg.PhaseID, msg.Msg)
		// Remove worker card on failure.
		delete(m.WorkerCards, msg.PhaseID)
		m.addMessage("[%s] %s", msg.PhaseID, msg.Msg)
		toast, cmd := NewToast(fmt.Sprintf("[%s] %s", msg.PhaseID, msg.Msg), true)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)
	case MsgPhaseInfo:
		// Informational — don't change phase status.

	// --- Hot-added phase ---
	case MsgPhaseHotAdded:
		pi := PhaseInfo{
			ID:        msg.PhaseID,
			Title:     msg.Title,
			DependsOn: msg.DependsOn,
		}
		m.NebulaView.AppendPhase(pi)
		m.Graph.AppendPhase(pi)
		m.StatusBar.Total = len(m.NebulaView.Phases)
		toast, cmd := NewToast(fmt.Sprintf("+ %s added to nebula", msg.PhaseID), false)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	// --- Fabric scanning ---
	case MsgPhaseScanning:
		m.addMessage("[%s] scanning entanglements", msg.PhaseID)
		toast, cmd := NewToast(fmt.Sprintf("[%s] scanning entanglements", msg.PhaseID), false)
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
		// Mark the phase as gated regardless of whether we show immediately.
		if msg.Checkpoint != nil {
			m.NebulaView.SetPhaseStatus(msg.Checkpoint.PhaseID, PhaseGate)
			m.Graph.SetPhaseStatus(msg.Checkpoint.PhaseID, PhaseGate)
		}
		if m.Gate == nil {
			// No active gate — show immediately.
			m.Gate = NewGatePrompt(msg.Checkpoint, msg.ResponseCh)
			m.Gate.Width = m.contentWidth()
			m.Gate.Height = m.Height
		} else {
			// Gate already active — queue for later.
			m.PendingGates = append(m.PendingGates, msg)
		}
		m.StatusBar.GateQueueCount = len(m.PendingGates)

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
		m.EntanglementView.Entanglements = msg.Entanglements
		m.EntanglementView.ClampCursor()

	case MsgDiscoveryPosted:
		m.Discoveries = append(m.Discoveries, msg.Discovery)
		toast, cmd := NewToast(fmt.Sprintf("discovery: %s", msg.Discovery.Kind), false)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	case MsgHail:
		// Show the hail overlay in any nebula view; queue if one is already active.
		if m.Mode == ModeNebula {
			if m.GumAvailable {
				// Use gum-backed hail resolution — suspend TUI, run gum subprocess.
				hailInfo := ui.HailInfo{
					ID:         fmt.Sprintf("hail-%d", len(m.PendingHails)),
					Kind:       msg.Discovery.Kind,
					Summary:    msg.Discovery.Detail,
					Detail:     msg.Discovery.Detail,
					SourceRole: msg.Discovery.SourceTask,
					Options:    extractOptions(msg.Discovery.Detail),
				}
				cmd := m.buildGumHailCmd(hailInfo)
				phaseID := msg.PhaseID
				responseCh := msg.ResponseCh
				m.gumHailResponseCh = responseCh
				cmds = append(cmds, tea.ExecProcess(cmd, func(err error) tea.Msg {
					// Read the response from the temp file.
					response := readGumResponseFile(cmd)
					return MsgGumHailResult{
						PhaseID:  phaseID,
						HailID:   hailInfo.ID,
						Response: response,
						Err:      err,
					}
				}))
			} else if m.Hail != nil {
				// Fallback: already showing a hail — queue for later.
				hailInfo := ui.HailInfo{
					Kind:    msg.Discovery.Kind,
					Summary: msg.Discovery.Detail,
					Detail:  msg.Discovery.Detail,
				}
				m.PendingHails = append(m.PendingHails, hailInfo)
				m.syncHailBadge()
				toast, cmd := NewToast("new hail queued — press H when ready", true)
				m.Toasts = append(m.Toasts, toast)
				cmds = append(cmds, cmd)
			} else {
				// Fallback: show BubbleTea overlay.
				m.Hail = NewHailOverlay(msg, msg.ResponseCh)
				cmds = append(cmds, m.Hail.Input.Focus())
			}
		} else {
			toast, cmd := NewToast(fmt.Sprintf("hail from %s: %s", msg.PhaseID, msg.Discovery.Detail), true)
			m.Toasts = append(m.Toasts, toast)
			cmds = append(cmds, cmd)
		}
		// Mark the phase card with an attention indicator.
		if msg.PhaseID != "" {
			m.NebulaView.SetPhaseHails(msg.PhaseID, true)
		}

	case MsgGumHailResult:
		// Gum hail subprocess completed — send response back to the agent.
		if msg.Err == nil && msg.Response != "" && m.gumHailResponseCh != nil {
			select {
			case m.gumHailResponseCh <- msg.Response:
			default:
			}
		}
		m.gumHailResponseCh = nil
		// Remove from pending hails if tracked.
		m.removePendingHail(msg.HailID)
		m.syncHailBadge()
		if msg.PhaseID != "" {
			m.syncPhaseHails(msg.PhaseID)
		}
		var toastMsg string
		if msg.Err != nil {
			toastMsg = fmt.Sprintf("hail error: %s", msg.Err)
		} else if msg.Response != "" {
			toastMsg = "response sent to agent"
		} else {
			toastMsg = "hail dismissed"
		}
		toast, cmd := NewToast(toastMsg, msg.Err != nil)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	case MsgHailReceived:
		m.PendingHails = append(m.PendingHails, msg.Hail)
		m.syncHailBadge()
		if msg.PhaseID != "" {
			m.syncPhaseHails(msg.PhaseID)
		}

	case MsgHailResolved:
		m.removePendingHail(msg.ID)
		m.syncHailBadge()
		if msg.PhaseID != "" {
			m.syncPhaseHails(msg.PhaseID)
		}

	// --- Interactive dialog ---
	case MsgDialogOpen:
		m.Dialog = NewDialogOverlay(msg.Session)
		cmds = append(cmds, m.Dialog.Input.Focus(), waitForDialogMsg(msg.Session))
		toast, cmd := NewToast("interactive dialog opened", true)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	case MsgDialogAgentMsg:
		if m.Dialog != nil && m.Dialog.Session.ID() == msg.SessionID {
			m.Dialog.AddAgentMessage(msg.Message)
			cmds = append(cmds, waitForDialogMsg(m.Dialog.Session))
		}

	case MsgDialogClosed:
		if m.Dialog != nil && m.Dialog.Session.ID() == msg.SessionID {
			m.Dialog = nil
		}

	case MsgGumDialogResult:
		// Gum dialog subprocess completed — close the dialog overlay.
		if m.Dialog != nil && m.Dialog.Session.ID() == msg.SessionID {
			if msg.Err == nil && msg.Response != "" {
				m.Dialog.Session.Close()
			}
			m.Dialog = nil
		}
		var toastMsg string
		if msg.Err != nil {
			toastMsg = fmt.Sprintf("dialog error: %s", msg.Err)
		} else if msg.Response != "" {
			toastMsg = "dialog response recorded"
		} else {
			toastMsg = "dialog dismissed"
		}
		toast, cmd := NewToast(toastMsg, msg.Err != nil)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	case MsgOpenPhaseChat:
		m.openEmbeddedPhaseChat(msg)

	case MsgScratchpadEntry:
		m.Scratchpad = append(m.Scratchpad, msg)
		m.ScratchpadView.AddEntry(msg)

	case MsgStaleWarning:
		m.StaleItems = msg.Items
		if len(msg.Items) > 0 {
			toast, cmd := NewToast(fmt.Sprintf("stale: %d items need attention", len(msg.Items)), true)
			m.Toasts = append(m.Toasts, toast)
			cmds = append(cmds, cmd)
		}

	// --- Plan preview ---
	case MsgPlanReady:
		if m.PlanPreview != nil {
			m.PlanPreview.SetPlan(msg.Plan, msg.Changes, msg.NebulaDir)
			w := m.contentWidth()
			h := m.homeMainHeight()
			m.PlanPreview.SetSize(w, h)
		}

	case MsgPlanAction:
		switch msg.Action {
		case PlanActionApply:
			m.SelectedNebula = msg.NebulaDir
			m.NextNebula = msg.NebulaDir
			return m, tea.Quit
		case PlanActionCancel:
			m.ShowPlanPreview = false
			m.PlanPreview = nil
		case PlanActionSave:
			if msg.Plan != nil {
				path, err := SavePlan(msg.Plan, msg.NebulaDir)
				if err != nil {
					toast, cmd := NewToast("save failed: "+err.Error(), true)
					m.Toasts = append(m.Toasts, toast)
					cmds = append(cmds, cmd)
				} else {
					toast, cmd := NewToast("Plan saved: "+path, false)
					m.Toasts = append(m.Toasts, toast)
					cmds = append(cmds, cmd)
				}
			}
		}

	case MsgPlanError:
		m.ShowPlanPreview = false
		m.PlanPreview = nil
		toast, cmd := NewToast("plan error: "+msg.Err.Error(), true)
		m.Toasts = append(m.Toasts, toast)
		cmds = append(cmds, cmd)

	}

	// Forward non-key messages (e.g. cursor.BlinkMsg) to active overlays'
	// textinput so the cursor blinks while the overlay is open.
	if _, isKey := msg.(tea.KeyMsg); !isKey {
		if m.Dialog != nil {
			var cmd tea.Cmd
			m.Dialog.Input, cmd = m.Dialog.Input.Update(msg)
			cmds = append(cmds, cmd)
		}
		if m.Hail != nil {
			var cmd tea.Cmd
			m.Hail.Input, cmd = m.Hail.Input.Update(msg)
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

// ensureWorkerCard creates or returns the worker card for the given phase.
// A new quasar ID is assigned when the card is first created.
func (m *AppModel) ensureWorkerCard(phaseID string) *WorkerCard {
	if m.WorkerCards == nil {
		m.WorkerCards = make(map[string]*WorkerCard)
	}
	if wc, ok := m.WorkerCards[phaseID]; ok {
		return wc
	}
	m.nextQuasarNum++
	wc := &WorkerCard{
		PhaseID:  phaseID,
		QuasarID: fmt.Sprintf("q-%d", m.nextQuasarNum),
	}
	m.WorkerCards[phaseID] = wc
	return wc
}

// clampCursors ensures all cursors remain within valid bounds.
// This prevents panics after a resize or data change that shrinks a list.
func clampCursors(m *AppModel) {
	// Clamp HomeCursor and HomeOffset against the filtered list.
	if max := len(m.filteredHomeNebulae()) - 1; max >= 0 {
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

	// Clamp sidebar cursor.
	if max := len(m.Sidebar.Phases) - 1; max >= 0 {
		if m.Sidebar.Cursor > max {
			m.Sidebar.Cursor = max
		}
	} else {
		m.Sidebar.Cursor = 0
	}

	// Clamp LoopView cursor.
	if max := m.LoopView.TotalEntries() - 1; max >= 0 {
		if m.LoopView.Cursor > max {
			m.LoopView.Cursor = max
		}
	} else {
		m.LoopView.Cursor = 0
	}

	// Clamp BoardView cursor.
	if max := len(m.Board.Phases) - 1; max >= 0 {
		if m.Board.Cursor > max {
			m.Board.Cursor = max
		}
	} else {
		m.Board.Cursor = 0
	}

	// Clamp EntanglementView cursor.
	m.EntanglementView.ClampCursor()

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

	// Dialog overlay takes highest priority — it renders on top of
	// everything (dimmed overlay) and must receive keys before the gate
	// or any other inline overlay.
	if m.Dialog != nil {
		return m.handleDialogKey(msg)
	}

	// Hail overlay — also a dimmed overlay, takes priority over the gate.
	if m.Hail != nil {
		return m.handleHailKey(msg)
	}

	// Hail list overlay.
	if m.HailList != nil {
		return m.handleHailListKey(msg)
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
			m.DiffCollapsed = nil
			m.DiffHunkCount = 0
			m.updateDetailFromSelection()
			return m, nil
		case key.Matches(msg, m.Keys.SideBySide):
			m.DiffSideBySide = !m.DiffSideBySide
			m.refreshFileDiff()
			return m, nil
		case key.Matches(msg, m.Keys.ToggleHunk):
			m.toggleCurrentHunk()
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
		case key.Matches(msg, m.Keys.SideBySide):
			m.DiffSideBySide = !m.DiffSideBySide
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

	// Sidebar focus toggle — when the sidebar is visible, left/right and Tab
	// toggle focus between the sidebar and the main content area.
	if m.Mode == ModeNebula && m.Depth == DepthPhases && m.sidebarWidth() > 0 {
		switch msg.String() {
		case "left", "h":
			if m.PaneFocus == PaneFocusSidebar {
				// Already in leftmost pane — no-op. Return early to prevent
				// the fallback board left/right handler from firing.
				return m, nil
			}
			// In main: try moving left in the board first, then switch to sidebar.
			if m.BoardActive && m.ActiveTab == TabBoard {
				m.Board.MoveLeft()
				return m, nil
			}
			m.PaneFocus = PaneFocusSidebar
			m.Sidebar.Focus = true
			// Reverse-sync: adopt main area's cursor so sidebar matches.
			m.Sidebar.Cursor = m.activePhaseCursor()
			m.Sidebar.ensureVisible()
			return m, nil
		case "right", "l":
			if m.PaneFocus == PaneFocusSidebar {
				m.PaneFocus = PaneFocusMain
				m.Sidebar.Focus = false
				return m, nil
			}
			// If already in main and board is active, move right in board.
			if m.BoardActive && m.ActiveTab == TabBoard {
				m.Board.MoveRight()
				return m, nil
			}
		}
	}

	// Tree view key handling — when sidebar has focus and tree is active.
	if m.Mode == ModeNebula && m.Depth == DepthPhases && m.sidebarWidth() > 0 && m.PaneFocus == PaneFocusSidebar && m.Sidebar.Tree != nil {
		switch {
		case key.Matches(msg, m.Keys.Enter):
			m.Sidebar.ToggleExpand()
			m.updateDetailFromSelection()
			return m, nil
		case key.Matches(msg, m.Keys.TreeCollapse):
			m.Sidebar.CollapseChildren()
			return m, nil
		case key.Matches(msg, m.Keys.TreeZoom):
			m.Sidebar.Zoom()
			return m, nil
		case key.Matches(msg, m.Keys.TreeSortCost):
			m.Sidebar.SortTree(TreeSortCost)
			return m, nil
		case key.Matches(msg, m.Keys.TreeSortDuration):
			m.Sidebar.SortTree(TreeSortDuration)
			return m, nil
		}
	}

	// Tab navigation and board toggle — only active in nebula mode at DepthPhases.
	if m.Mode == ModeNebula && m.Depth == DepthPhases {
		switch msg.String() {
		case "v":
			// Toggle between columnar board and table view.
			// Only allow board if terminal is wide enough.
			if !m.BoardActive && m.Width >= BoardMinWidth {
				m.BoardActive = true
			} else {
				m.BoardActive = false
			}
			return m, nil
		case "tab":
			// When sidebar is visible and focused, Tab moves focus to main.
			if m.sidebarWidth() > 0 && m.PaneFocus == PaneFocusSidebar {
				m.PaneFocus = PaneFocusMain
				m.Sidebar.Focus = false
				return m, nil
			}
			m.ActiveTab = m.ActiveTab.Next()
			return m, nil
		case "shift+tab":
			m.ActiveTab = m.ActiveTab.Prev()
			return m, nil
		case "1", "2", "3", "4", "5":
			n := int(msg.String()[0] - '0')
			if tab, ok := TabFromNumber(n); ok {
				m.ActiveTab = tab
			}
			return m, nil
		case "left", "h":
			// Left/right navigation for board columns (fallback when sidebar not visible).
			if m.BoardActive && m.ActiveTab == TabBoard {
				m.Board.MoveLeft()
				return m, nil
			}
		case "right", "l":
			if m.BoardActive && m.ActiveTab == TabBoard {
				m.Board.MoveRight()
				return m, nil
			}
		}
	}

	// Board tab: contextual chat keybinding.
	if m.Mode == ModeNebula && m.Depth == DepthPhases && m.ActiveTab == TabBoard {
		if key.Matches(msg, m.Keys.ChatPhase) {
			return m.openPhaseChat()
		}
	}

	// Graph tab key handling — toggle tracks, critical path, and scroll viewport.
	if m.Mode == ModeNebula && m.Depth == DepthPhases && m.ActiveTab == TabGraph {
		switch msg.String() {
		case "t":
			m.Graph.ToggleTracks()
			return m, nil
		case "c":
			m.Graph.ToggleCriticalPath()
			return m, nil
		}
		// Route scroll keys to the graph viewport.
		switch {
		case key.Matches(msg, m.Keys.PageUp),
			key.Matches(msg, m.Keys.PageDown),
			key.Matches(msg, m.Keys.Home),
			key.Matches(msg, m.Keys.End):
			m.Graph.Update(msg)
			return m, nil
		}
		if msg.String() == "g" || msg.String() == "G" {
			m.Graph.Update(msg)
			return m, nil
		}
	}

	// Scratchpad viewport scrolling — when the scratchpad tab is active,
	// route scroll keys to the viewport instead of the phase list.
	if m.Mode == ModeNebula && m.Depth == DepthPhases && m.ActiveTab == TabScratchpad {
		switch {
		case key.Matches(msg, m.Keys.Up),
			key.Matches(msg, m.Keys.Down),
			key.Matches(msg, m.Keys.PageUp),
			key.Matches(msg, m.Keys.PageDown),
			key.Matches(msg, m.Keys.Home),
			key.Matches(msg, m.Keys.End):
			m.ScratchpadView.Update(msg)
			return m, nil
		}
		// Also handle g/G for top/bottom (not in KeyMap but standard viewport keys).
		if msg.String() == "g" || msg.String() == "G" {
			m.ScratchpadView.Update(msg)
			return m, nil
		}
	}

	// Entanglement viewport scrolling — when the entanglements tab is active,
	// route page up/down, home/end, and g/G to the viewport.
	if m.Mode == ModeNebula && m.Depth == DepthPhases && m.ActiveTab == TabEntanglements {
		switch {
		case key.Matches(msg, m.Keys.PageUp),
			key.Matches(msg, m.Keys.PageDown),
			key.Matches(msg, m.Keys.Home),
			key.Matches(msg, m.Keys.End):
			m.EntanglementView.Update(msg)
			return m, nil
		}
		if msg.String() == "g" || msg.String() == "G" {
			m.EntanglementView.Update(msg)
			return m, nil
		}
	}

	// Impact tab key handling — scroll viewport and navigate phase groups.
	if m.Mode == ModeNebula && m.Depth == DepthPhases && m.ActiveTab == TabImpact {
		switch {
		case key.Matches(msg, m.Keys.Up):
			m.ImpactView.MoveUp()
			return m, nil
		case key.Matches(msg, m.Keys.Down):
			m.ImpactView.MoveDown()
			return m, nil
		case key.Matches(msg, m.Keys.PageUp),
			key.Matches(msg, m.Keys.PageDown),
			key.Matches(msg, m.Keys.Home),
			key.Matches(msg, m.Keys.End):
			m.ImpactView.Update(msg)
			return m, nil
		}
		if msg.String() == "g" || msg.String() == "G" {
			m.ImpactView.Update(msg)
			return m, nil
		}
	}

	// Home mode: plan preview key handling.
	if m.Mode == ModeHome && m.ShowPlanPreview && m.PlanPreview != nil {
		return m.handlePlanKey(msg)
	}

	// Home mode: Tab cycles through status filters.
	if m.Mode == ModeHome && !m.ShowPlanPreview && msg.String() == "tab" {
		m.HomeFilter = m.HomeFilter.Next()
		m.HomeCursor = 0
		m.HomeOffset = 0
		m.updateHomeDetail()
		return m, nil
	}

	// Home mode: Enter selects a nebula and launches plan preview.
	if m.Mode == ModeHome && key.Matches(msg, m.Keys.Enter) {
		filtered := m.filteredHomeNebulae()
		if m.HomeCursor >= 0 && m.HomeCursor < len(filtered) {
			selected := filtered[m.HomeCursor]
			pv := NewPlanView()
			m.PlanPreview = &pv
			m.ShowPlanPreview = true
			nebulaDir := selected.Path
			nebulaName := selected.Name
			return m, func() tea.Msg {
				return computePlan(nebulaDir, nebulaName)
			}
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

	case key.Matches(msg, m.Keys.HailList):
		cmd := m.openHailList()
		return m, cmd
	}

	return m, nil
}

// handlePlanKey processes keyboard input while the plan preview is active.
func (m AppModel) handlePlanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.Keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.Keys.Back):
		// Esc returns to home.
		m.ShowPlanPreview = false
		m.PlanPreview = nil
		return m, nil

	case key.Matches(msg, m.Keys.Enter):
		action := m.PlanPreview.SelectedAction()
		return m, func() tea.Msg {
			return MsgPlanAction{
				Action:    action,
				Plan:      m.PlanPreview.Plan,
				NebulaDir: m.PlanPreview.NebulaDir,
			}
		}

	case msg.String() == "left" || msg.String() == "h":
		m.PlanPreview.MoveLeft()
		return m, nil

	case msg.String() == "right" || msg.String() == "l":
		m.PlanPreview.MoveRight()
		return m, nil

	case key.Matches(msg, m.Keys.Up):
		m.PlanPreview.ScrollUp()
		return m, nil

	case key.Matches(msg, m.Keys.Down):
		m.PlanPreview.ScrollDown()
		return m, nil

	case key.Matches(msg, m.Keys.PageUp),
		key.Matches(msg, m.Keys.PageDown),
		key.Matches(msg, m.Keys.Home),
		key.Matches(msg, m.Keys.End):
		m.PlanPreview.Update(msg)
		return m, nil
	}

	return m, nil
}

// computePlan loads and analyzes a nebula, producing an ExecutionPlan message.
// This runs in a goroutine via tea.Cmd and returns either MsgPlanReady or MsgPlanError.
func computePlan(nebulaDir, nebulaName string) tea.Msg {
	n, err := nebula.Load(nebulaDir)
	if err != nil {
		return MsgPlanError{Err: fmt.Errorf("loading nebula: %w", err)}
	}

	errs := nebula.Validate(n)
	if len(errs) > 0 {
		return MsgPlanError{Err: fmt.Errorf("validation: %s", errs[0].Error())}
	}

	pe := &nebula.PlanEngine{
		Scanner: &fabric.StaticScanner{},
	}
	plan, err := pe.Plan(n)
	if err != nil {
		return MsgPlanError{Err: fmt.Errorf("plan engine: %w", err)}
	}

	// Load previous plan for diff comparison.
	var changes []nebula.PlanChange
	prev := LoadPreviousPlan(nebulaDir, nebulaName)
	if prev != nil {
		changes = nebula.Diff(prev, plan)
	}

	return MsgPlanReady{
		Plan:      plan,
		Changes:   changes,
		NebulaDir: nebulaDir,
	}
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
	m.Graph.SetPhaseStatus(phaseID, PhaseWaiting)
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

	m.DiffCollapsed = make(map[int]bool)
	m.DiffHunkCount = HunkCount(rawDiff, file.Path)
	opts := DiffRenderOpts{
		SideBySide:     m.DiffSideBySide,
		CollapsedHunks: m.DiffCollapsed,
	}
	body := RenderSingleFileDiffWithOpts(rawDiff, file.Path, m.contentWidth()-4, opts)
	m.Detail.SetContent(file.Path, body)
	m.DiffFileOpen = true
	return m, nil
}

// refreshFileDiff re-renders the currently open file diff with current state.
func (m *AppModel) refreshFileDiff() {
	fl := m.DiffFileList
	if fl == nil || len(fl.Files) == 0 {
		return
	}
	file := fl.SelectedFile()
	rawDiff := m.selectedAgentDiff()
	if rawDiff == "" {
		return
	}
	opts := DiffRenderOpts{
		SideBySide:     m.DiffSideBySide,
		CollapsedHunks: m.DiffCollapsed,
	}
	body := RenderSingleFileDiffWithOpts(rawDiff, file.Path, m.contentWidth()-4, opts)
	m.Detail.SetContent(file.Path, body)
}

// toggleCurrentHunk toggles the collapse state of hunks sequentially.
// Collapses the first expanded hunk, or expands all if all are collapsed.
func (m *AppModel) toggleCurrentHunk() {
	if m.DiffCollapsed == nil {
		m.DiffCollapsed = make(map[int]bool)
	}
	if m.DiffHunkCount <= 0 {
		return
	}
	toggled := false
	for i := 0; i < m.DiffHunkCount; i++ {
		if !m.DiffCollapsed[i] {
			m.DiffCollapsed[i] = true
			toggled = true
			break
		}
	}
	if !toggled {
		// All collapsed — expand all.
		for i := 0; i < m.DiffHunkCount; i++ {
			delete(m.DiffCollapsed, i)
		}
	}
	m.refreshFileDiff()
}

// selectedAgentDiff returns the raw diff text for the currently selected agent.
func (m *AppModel) selectedAgentDiff() string {
	switch m.Mode {
	case ModeLoop:
		if agent := m.LoopView.SelectedAgent(); agent != nil {
			return agent.Diff
		}
	case ModeNebula:
		if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
			if agent := lv.SelectedAgent(); agent != nil {
				return agent.Diff
			}
		}
	}
	return ""
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
			// Use the sidebar cursor when it has focus, otherwise the active tab.
			var phaseID string
			if m.sidebarWidth() > 0 && m.PaneFocus == PaneFocusSidebar {
				if p := m.Sidebar.SelectedPhase(); p != nil {
					phaseID = p.ID
				}
			} else if m.ActiveTab == TabGraph {
				phaseID = m.Graph.SelectedPhaseID()
			} else if m.BoardActive && m.ActiveTab == TabBoard {
				m.Board.Phases = m.NebulaView.Phases
				if p := m.Board.SelectedPhase(); p != nil {
					phaseID = p.ID
				}
			}
			if phaseID == "" {
				if p := m.NebulaView.SelectedPhase(); p != nil {
					phaseID = p.ID
				}
			}
			if phaseID != "" {
				m.FocusedPhase = phaseID
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
	case msg.String() == "up", msg.String() == "k":
		m.Gate.ScrollUp()
	case msg.String() == "down", msg.String() == "j":
		m.Gate.ScrollDown(m.Gate.contentLineCount(), m.Gate.viewportHeight())
	}
	return m, nil
}

// resolveGate sends the action, updates the phase status, clears the gate,
// and promotes the next queued gate prompt if one is pending.
func (m *AppModel) resolveGate(action nebula.GateAction) {
	if m.Gate != nil {
		phaseID := m.Gate.PhaseID
		m.Gate.Resolve(action)
		m.Gate = nil

		// Transition the phase out of PhaseGate based on the decision.
		// Update both NebulaView (board) and Graph (DAG) to keep them in sync.
		switch action {
		case nebula.GateActionAccept:
			m.NebulaView.SetPhaseStatus(phaseID, PhaseDone)
			m.Graph.SetPhaseStatus(phaseID, PhaseDone)
		case nebula.GateActionReject:
			m.NebulaView.SetPhaseStatus(phaseID, PhaseFailed)
			m.Graph.SetPhaseStatus(phaseID, PhaseFailed)
		case nebula.GateActionRetry:
			m.NebulaView.SetPhaseStatus(phaseID, PhaseWorking)
			m.Graph.SetPhaseStatus(phaseID, PhaseWorking)
		case nebula.GateActionSkip:
			m.NebulaView.SetPhaseStatus(phaseID, PhaseSkipped)
			m.Graph.SetPhaseStatus(phaseID, PhaseSkipped)
		}

		// Promote the next queued gate prompt, if any.
		if len(m.PendingGates) > 0 {
			next := m.PendingGates[0]
			m.PendingGates = m.PendingGates[1:]
			m.Gate = NewGatePrompt(next.Checkpoint, next.ResponseCh)
			m.Gate.Width = m.contentWidth()
			m.Gate.Height = m.Height
		}
		m.StatusBar.GateQueueCount = len(m.PendingGates)
	}
}

// handleDialogKey delegates key events to the dialog overlay and
// interprets the resulting action.
func (m AppModel) handleDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action, cmd := m.Dialog.HandleKey(msg, m.Keys)
	if action == DialogClosed {
		m.Dialog = nil
		toast, toastCmd := NewToast("dialog closed", false)
		m.Toasts = append(m.Toasts, toast)
		return m, toastCmd
	}
	return m, cmd
}

// handleHailKey routes key events to the hail overlay's text input.
// Esc dismisses the overlay (empty response), Enter submits the response.
func (m AppModel) handleHailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.Keys.Back):
		// Esc: dismiss with empty response.
		m.resolveHail("")
		return m, nil

	case msg.String() == "ctrl+d":
		// Ctrl+D: submit all composed entries + remaining input.
		response := m.Hail.ComposeResolution()
		isInteractive := m.Hail.ResponseCh != nil
		if response != "" {
			m.resolveHail(response)
		} else {
			m.resolveHail("")
		}
		var toastMsg string
		if isInteractive {
			toastMsg = "response sent to agent"
		} else {
			toastMsg = "hail acknowledged"
		}
		toast, cmd := NewToast(toastMsg, false)
		m.Toasts = append(m.Toasts, toast)
		return m, cmd

	case msg.String() == "tab":
		// Tab: toggle between compose and scroll modes.
		m.Hail.ToggleMode()
		return m, nil

	case key.Matches(msg, m.Keys.Enter):
		val := strings.TrimSpace(m.Hail.Input.Value())
		if val == "" {
			return m, nil
		}
		// Single-letter option shortcut on first entry (no prior entries).
		if len(val) == 1 && len(m.Hail.Options) > 0 && len(m.Hail.Entries) == 0 {
			idx := int(val[0] - 'a')
			if idx >= 0 && idx < len(m.Hail.Options) {
				isInteractive := m.Hail.ResponseCh != nil
				m.resolveHail(m.Hail.Options[idx])
				var toastMsg string
				if isInteractive {
					toastMsg = "response sent to agent"
				} else {
					toastMsg = "hail acknowledged"
				}
				toast, cmd := NewToast(toastMsg, false)
				m.Toasts = append(m.Toasts, toast)
				return m, cmd
			}
		}
		// Add as dialog entry.
		m.Hail.AddEntry(val)
		return m, nil

	default:
		// In scroll mode, arrow keys scroll the context area.
		if m.Hail.Mode == HailModeScroll {
			switch {
			case key.Matches(msg, m.Keys.Up):
				if m.Hail.ScrollOffset > 0 {
					m.Hail.ScrollOffset--
				}
				return m, nil
			case key.Matches(msg, m.Keys.Down):
				m.Hail.ScrollOffset++
				return m, nil
			}
			return m, nil
		}
		// Compose mode: forward the key to the textinput widget.
		var cmd tea.Cmd
		m.Hail.Input, cmd = m.Hail.Input.Update(msg)
		return m, cmd
	}
}

// resolveHail sends the response and clears the hail overlay.
func (m *AppModel) resolveHail(response string) {
	if m.Hail != nil {
		m.Hail.Resolve(response)
		m.Hail = nil
	}
}

// syncHailBadge updates the status bar hail counters from PendingHails.
func (m *AppModel) syncHailBadge() {
	m.StatusBar.HailCount = len(m.PendingHails)
	critical := 0
	for _, h := range m.PendingHails {
		if h.Kind == "blocker" {
			critical++
		}
	}
	m.StatusBar.CriticalHailCount = critical

	// Enable/disable the HailList keybinding based on pending hails.
	m.Keys.HailList.SetEnabled(len(m.PendingHails) > 0)
}

// syncPhaseHails updates the HasPendingHails flag on the PhaseEntry for the
// given phase. When a hail is resolved, this clears the flag if no active
// hail overlay references this phase.
func (m *AppModel) syncPhaseHails(phaseID string) {
	hasPending := m.Hail != nil && m.Hail.PhaseID == phaseID
	m.NebulaView.SetPhaseHails(phaseID, hasPending)
}

// removePendingHail removes a hail by ID from PendingHails.
func (m *AppModel) removePendingHail(id string) {
	for i, h := range m.PendingHails {
		if h.ID == id {
			m.PendingHails = append(m.PendingHails[:i], m.PendingHails[i+1:]...)
			return
		}
	}
}

// handleHailListKey routes key events when the hail list overlay is active.
// Up/Down navigates the list, Enter selects, Esc dismisses.
func (m AppModel) handleHailListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.Keys.Back):
		m.HailList = nil
		return m, nil
	case key.Matches(msg, m.Keys.Up):
		m.HailList.MoveUp()
		return m, nil
	case key.Matches(msg, m.Keys.Down):
		m.HailList.MoveDown()
		return m, nil
	case key.Matches(msg, m.Keys.Enter):
		selected := m.HailList.Selected()
		if selected != nil {
			// Acknowledge (remove) the selected hail and dismiss the list.
			m.removePendingHail(selected.ID)
			m.syncHailBadge()
			m.HailList = nil
			toast, cmd := NewToast(fmt.Sprintf("✓ acknowledged: %s", selected.Summary), false)
			m.Toasts = append(m.Toasts, toast)
			return m, cmd
		}
		m.HailList = nil
		return m, nil
	}
	return m, nil
}

// openHailList creates and shows the hail list overlay. If only one hail
// is pending, it acknowledges it directly instead of showing a list.
func (m *AppModel) openHailList() tea.Cmd {
	if len(m.PendingHails) == 0 {
		return nil
	}
	if len(m.PendingHails) == 1 {
		// Single hail — acknowledge directly with a toast.
		hail := m.PendingHails[0]
		m.PendingHails = nil
		m.syncHailBadge()
		toast, cmd := NewToast(fmt.Sprintf("⚠ [%s] %s: %s", hail.Kind, hail.SourceRole, hail.Summary), true)
		m.Toasts = append(m.Toasts, toast)
		return cmd
	}
	m.HailList = NewHailListOverlay(m.PendingHails)
	m.HailList.Width = m.Width
	return nil
}

// openPhaseChat builds a PhaseContext from the selected phase's execution
// state and emits MsgOpenPhaseChat so the cockpit can transition to chat mode.
func (m *AppModel) openPhaseChat() (tea.Model, tea.Cmd) {
	// Determine the selected phase based on view mode.
	var phase *PhaseEntry
	if m.BoardActive {
		phase = m.Board.SelectedPhase()
	} else {
		idx := m.NebulaView.Cursor
		if idx >= 0 && idx < len(m.NebulaView.Phases) {
			phase = &m.NebulaView.Phases[idx]
		}
	}
	if phase == nil {
		return m, nil
	}

	// Gather execution context from per-phase loop view.
	var lastSummary, reviewerFindings, diffStat string
	var cycle, maxCycles int
	var fileClaims []string

	cycle = phase.Cycles
	maxCycles = phase.MaxCycles

	if lv, ok := m.PhaseLoops[phase.ID]; ok {
		lastSummary = lastAgentSummary(lv)
		reviewerFindings = lastReviewerSummary(lv)
		files := aggregateFiles(lv)
		if len(files) > 0 {
			diffStat = FormatFileSummary(files)
			for _, f := range files {
				fileClaims = append(fileClaims, f.Path)
			}
		}
	}

	return m, func() tea.Msg {
		return MsgOpenPhaseChat{
			PhaseContext: chat.PhaseContext{
				PhaseID:          phase.ID,
				PhaseSpec:        phase.PlanBody,
				Cycle:            cycle,
				MaxCycles:        maxCycles,
				LastSummary:      lastSummary,
				DiffStat:         diffStat,
				ReviewerFindings: reviewerFindings,
				FileClaims:       fileClaims,
			},
		}
	}
}

// lastAgentSummary returns the summary from the most recent agent (coder or reviewer).
func lastAgentSummary(lv *LoopView) string {
	for i := len(lv.Cycles) - 1; i >= 0; i-- {
		for j := len(lv.Cycles[i].Agents) - 1; j >= 0; j-- {
			a := lv.Cycles[i].Agents[j]
			if a.Summary != "" {
				return a.Summary
			}
		}
	}
	return ""
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
		m.updateHomeDetail()
	case ModeLoop:
		m.LoopView.MoveUp()
	case ModeNebula:
		// When sidebar is visible and focused, navigate the sidebar.
		if m.Depth == DepthPhases && m.sidebarWidth() > 0 && m.PaneFocus == PaneFocusSidebar {
			m.Sidebar.MoveUp()
			m.syncSidebarSelection()
			m.updateDetailFromSelection()
			return
		}
		if m.Depth == DepthPhases {
			if m.ActiveTab == TabEntanglements {
				m.EntanglementView.MoveUp()
			} else if m.ActiveTab == TabGraph {
				m.Graph.MoveUp()
			} else if m.BoardActive && m.ActiveTab == TabBoard {
				m.Board.MoveUp()
			} else {
				m.NebulaView.MoveUp()
			}
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
		max := len(m.filteredHomeNebulae()) - 1
		if max < 0 {
			max = 0
		}
		if m.HomeCursor < max {
			m.HomeCursor++
		}
		m.adjustHomeOffset()
		m.updateHomeDetail()
	case ModeLoop:
		m.LoopView.MoveDown()
	case ModeNebula:
		// When sidebar is visible and focused, navigate the sidebar.
		if m.Depth == DepthPhases && m.sidebarWidth() > 0 && m.PaneFocus == PaneFocusSidebar {
			m.Sidebar.MoveDown()
			m.syncSidebarSelection()
			m.updateDetailFromSelection()
			return
		}
		if m.Depth == DepthPhases {
			if m.ActiveTab == TabEntanglements {
				m.EntanglementView.MoveDown()
			} else if m.ActiveTab == TabGraph {
				m.Graph.MoveDown()
			} else if m.BoardActive && m.ActiveTab == TabBoard {
				m.Board.MoveDown()
			} else {
				m.NebulaView.MoveDown()
			}
		} else if m.Depth >= DepthPhaseLoop {
			if lv := m.PhaseLoops[m.FocusedPhase]; lv != nil {
				lv.MoveDown()
			}
		}
	}
	m.updateDetailFromSelection()
}

// syncSidebarSelection synchronizes the sidebar selection with the main views.
// When the user navigates in the sidebar, the board/nebula view cursor follows.
func (m *AppModel) syncSidebarSelection() {
	if m.Sidebar.Cursor < 0 || m.Sidebar.Cursor >= len(m.Sidebar.Phases) {
		return
	}
	// Sync the NebulaView cursor.
	m.NebulaView.Cursor = m.Sidebar.Cursor
	// Sync the Board view cursor (it uses flat ordering, which may differ).
	// For simplicity, sync the NebulaView cursor — board cursor is separate.
	m.Board.Cursor = m.Sidebar.Cursor
}

// activePhaseCursor returns the current phase cursor from the active main view.
// Used for reverse-syncing the sidebar when focus moves from main → sidebar.
func (m AppModel) activePhaseCursor() int {
	if m.BoardActive && m.ActiveTab == TabBoard {
		return m.Board.Cursor
	}
	return m.NebulaView.Cursor
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
			Role:         agent.Role,
			Cycle:        m.LoopView.SelectedCycleNumber(),
			DurationMs:   agent.DurationMs,
			CostUSD:      agent.CostUSD,
			IssueCount:   agent.IssueCount,
			Done:         agent.Done,
			Summary:      agent.Summary,
			Satisfaction: agent.Satisfaction,
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
		body := FormatAgentOutput(agent.Output, m.Detail.viewport.Width)
		m.Detail.SetContentWithHeader(agent.Role+" output", header, body)

	case ModeNebula:
		m.updateNebulaDetail()
	}
}

// updateHomeDetail populates the detail panel with the selected nebula's
// description, phase count, and status breakdown.
func (m *AppModel) updateHomeDetail() {
	filtered := m.filteredHomeNebulae()
	if m.HomeCursor < 0 || m.HomeCursor >= len(filtered) {
		m.Detail.SetEmpty("No nebulas found")
		return
	}
	nc := filtered[m.HomeCursor]

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

	// Append file scope section to the phase header if available.
	if files, ok := m.PhaseFiles[m.FocusedPhase]; ok && len(files) > 0 {
		scopeSection := FileScopeSection(files, m.contentWidth()-4)
		if phaseHeader != "" {
			phaseHeader = phaseHeader + "\n" + scopeSection
		} else {
			phaseHeader = scopeSection
		}
	}

	switch m.Depth {
	case DepthPhaseLoop:
		// For completed/failed phases, show a structured completion summary.
		if fp := m.findPhase(m.FocusedPhase); fp != nil && (fp.Status == PhaseDone || fp.Status == PhaseFailed) {
			lv := m.PhaseLoops[m.FocusedPhase]
			summaryBody := FormatPhaseSummary(*fp, lv)
			m.Detail.SetContentWithHeader(m.FocusedPhase+" summary", phaseHeader, summaryBody)
			return
		}
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
			Role:         agent.Role,
			Cycle:        lv.SelectedCycleNumber(),
			DurationMs:   agent.DurationMs,
			CostUSD:      agent.CostUSD,
			IssueCount:   agent.IssueCount,
			Done:         agent.Done,
			Summary:      agent.Summary,
			Satisfaction: agent.Satisfaction,
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
		body := FormatAgentOutput(agent.Output, m.Detail.viewport.Width)
		m.Detail.SetContentWithHeader(title, header, body)

	case DepthPhases:
		// When tree view is active, show detail based on the selected node.
		m.updateTreeDetail()

	default:
		m.Detail.SetEmpty("Press enter to expand details")
	}
}

// updateTreeDetail populates the detail panel based on the selected tree node.
func (m *AppModel) updateTreeDetail() {
	node := m.Sidebar.SelectedTreeNode()
	if node == nil {
		m.Detail.SetEmpty("Navigate tree to see details")
		return
	}

	switch node.Kind {
	case TreeNodeNebula:
		m.Detail.SetContent("nebula: "+node.Label, m.buildNebulaOverview())

	case TreeNodePhase:
		if node.PhaseEntry == nil {
			m.Detail.SetEmpty("(no phase data)")
			return
		}
		p := node.PhaseEntry
		header := FormatPhaseHeader(PhaseContext{
			ID:        p.ID,
			Title:     p.Title,
			Status:    p.Status,
			CostUSD:   p.CostUSD,
			Cycles:    p.Cycles,
			BlockedBy: p.BlockedBy,
		})
		body := ""
		if p.PlanBody != "" {
			body = p.PlanBody
		} else {
			body = "(no plan body available)"
		}
		// If the phase is done/failed, show completion summary.
		if p.Status == PhaseDone || p.Status == PhaseFailed {
			lv := m.PhaseLoops[p.ID]
			body = FormatPhaseSummary(*p, lv)
		}
		m.Detail.SetContentWithHeader(p.ID, header, body)

	case TreeNodeCycle:
		if node.CycleEntry == nil {
			m.Detail.SetEmpty("(no cycle data)")
			return
		}
		c := node.CycleEntry
		var body strings.Builder
		body.WriteString(fmt.Sprintf("Cycle %d\n\n", c.Number))
		for _, a := range c.Agents {
			if a.Done {
				secs := float64(a.DurationMs) / 1000.0
				body.WriteString(fmt.Sprintf("  %s %s  %.1fs  $%.4f", iconDone, a.Role, secs, a.CostUSD))
				if a.IssueCount > 0 {
					body.WriteString(fmt.Sprintf("  %d issue(s)", a.IssueCount))
				}
			} else {
				body.WriteString(fmt.Sprintf("  %s %s  (in progress)", iconWorking, a.Role))
			}
			body.WriteString("\n")
			if a.Summary != "" {
				body.WriteString("    " + a.Summary + "\n")
			}
		}
		m.Detail.SetContent(fmt.Sprintf("cycle %d", c.Number), body.String())

	case TreeNodeAgent:
		if node.AgentEntry == nil {
			m.Detail.SetEmpty("(no agent data)")
			return
		}
		a := node.AgentEntry
		header := FormatAgentHeader(AgentContext{
			Role:       a.Role,
			DurationMs: a.DurationMs,
			CostUSD:    a.CostUSD,
			IssueCount: a.IssueCount,
			Done:       a.Done,
			Summary:    a.Summary,
		})
		if a.Output == "" {
			m.Detail.SetContentWithHeader(a.Role+" output", header, "(output will appear when agent completes)")
		} else {
			body := FormatAgentOutput(a.Output, m.Detail.viewport.Width)
			m.Detail.SetContentWithHeader(a.Role+" output", header, body)
		}

	default:
		m.Detail.SetEmpty("Press enter to expand details")
	}
}

// buildNebulaOverview builds a summary string for the nebula root node.
func (m *AppModel) buildNebulaOverview() string {
	var b strings.Builder
	phases := m.NebulaView.Phases
	done, working, waiting, failed := 0, 0, 0, 0
	var totalCost float64
	for _, p := range phases {
		totalCost += p.CostUSD
		switch p.Status {
		case PhaseDone, PhaseSkipped:
			done++
		case PhaseWorking, PhaseGate:
			working++
		case PhaseFailed:
			failed++
		default:
			waiting++
		}
	}
	b.WriteString(fmt.Sprintf("Progress: %d/%d phases complete\n", done, len(phases)))
	b.WriteString(fmt.Sprintf("Working: %d  Waiting: %d  Failed: %d\n", working, waiting, failed))
	b.WriteString(fmt.Sprintf("Total cost: $%.2f\n", totalCost))
	return b.String()
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

// openEmbeddedPhaseChat creates an embedded ChatModel seeded with phase context.
// The chat takes over the full screen until the user presses Esc.
func (m *AppModel) openEmbeddedPhaseChat(msg MsgOpenPhaseChat) {
	// Create a store for persistence. If it fails, proceed without persistence.
	chatDir, err := chat.DefaultDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "chat: failed to determine chat directory: %v\n", err)
		return
	}
	store, err := chat.NewFileStore(chatDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chat: failed to initialize chat store: %v\n", err)
		return
	}

	// Create the chat model without a provider — the user can type messages
	// but inference requires a provider to be wired. For now, use a nil-safe
	// model that shows the context.
	cm := NewChatModel(store, nil, "")
	cm.StartPhaseChat(msg.PhaseContext)
	cm.Width = m.Width
	cm.Height = m.Height
	cm.recalcLayout()
	m.PhaseChat = &cm
}

// updatePhaseChat delegates message handling to the embedded phase chat.
// It intercepts Esc to close the chat and return to the cockpit.
func (m AppModel) updatePhaseChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// Esc closes the embedded chat and returns to the board.
		if keyMsg.Type == tea.KeyEsc && m.PhaseChat.InputMode == ChatModeNormal {
			// Save conversation before closing.
			if m.PhaseChat.ActiveConv != nil && m.PhaseChat.Store != nil {
				if err := m.PhaseChat.Store.Save(m.PhaseChat.ActiveConv); err != nil {
					fmt.Fprintf(os.Stderr, "chat: failed to save conversation: %v\n", err)
				}
			}
			m.PhaseChat = nil
			return m, nil
		}
	}

	// Forward window size to the embedded chat.
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width = wsMsg.Width
		m.Height = wsMsg.Height
	}

	// Handle /refresh command: intercept before the chat model processes it.
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
		if m.PhaseChat.InputMode == ChatModeCompose {
			input := m.PhaseChat.ChatView.InputValue()
			if input == "/refresh" && m.PhaseChat.PhaseContext != nil {
				m.PhaseChat.ChatView.ClearInput()
				// Re-gather context from the current phase state.
				pc := m.gatherPhaseContext(m.PhaseChat.PhaseContext.PhaseID)
				m.PhaseChat.RefreshPhaseContext(pc)
				return m, nil
			}
		}
	}

	// Delegate to the embedded ChatModel.
	model, cmd := m.PhaseChat.Update(msg)
	if cm, ok := model.(ChatModel); ok {
		m.PhaseChat = &cm
	}
	return m, cmd
}

// gatherPhaseContext assembles the latest execution state for a phase.
func (m *AppModel) gatherPhaseContext(phaseID string) chat.PhaseContext {
	pc := chat.PhaseContext{PhaseID: phaseID}

	phase := m.findPhase(phaseID)
	if phase == nil {
		return pc
	}

	pc.PhaseSpec = phase.PlanBody
	pc.Cycle = phase.Cycles
	pc.MaxCycles = phase.MaxCycles

	if lv, ok := m.PhaseLoops[phaseID]; ok {
		pc.LastSummary = lastAgentSummary(lv)
		pc.ReviewerFindings = lastReviewerSummary(lv)
		files := aggregateFiles(lv)
		if len(files) > 0 {
			pc.DiffStat = FormatFileSummary(files)
			for _, f := range files {
				pc.FileClaims = append(pc.FileClaims, f.Path)
			}
		}
	}

	return pc
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
	// Fixed chrome: status bar (1) + spacing (1) + bottom bar (1) + footer (1).
	chrome := 4
	// Banner: only counted when the terminal is tall enough to show it.
	if m.Height >= BannerCollapseHeight {
		if bv := m.Banner.View(); bv != "" {
			chrome += lipgloss.Height(bv)
		}
	}
	// Detail panel — uses a higher collapse threshold and smaller fraction
	// than other modes because the banner competes for vertical space.
	if m.showDetailPanel() && m.Height >= HomeDetailCollapseHeight {
		chrome++ // separator line
		chrome += m.homeDetailHeight()
	}
	h := m.Height - chrome
	if h < 3 {
		h = 3
	}
	return h
}

// homeDetailHeight returns the detail panel height for home mode.
// Uses a smaller fraction (1/4) than the default detailHeight (2/5) to
// leave more room for the nebula list alongside the banner.
func (m AppModel) homeDetailHeight() int {
	mainH := m.Height - 4 // status + spacing + bottom bar + footer
	if mainH < 4 {
		return 0
	}
	return mainH / 4
}

// filteredHomeNebulae returns the subset of HomeNebulae matching the active filter.
func (m *AppModel) filteredHomeNebulae() []NebulaChoice {
	return m.HomeFilter.FilterNebulae(m.HomeNebulae)
}

// adjustHomeOffset updates HomeOffset so the cursor is visible within the
// approximate viewport height. Called after cursor changes in moveUp/moveDown.
func (m *AppModel) adjustHomeOffset() {
	filtered := m.filteredHomeNebulae()
	if len(filtered) == 0 {
		m.HomeOffset = 0
		return
	}
	hv := HomeView{
		Nebulae: filtered,
		Cursor:  m.HomeCursor,
		Offset:  m.HomeOffset,
		Height:  m.homeMainHeight(),
		Filter:  m.HomeFilter,
	}
	m.HomeOffset = hv.ensureCursorVisible()
}

// showDetailPanel returns whether the detail panel should be visible.
func (m AppModel) showDetailPanel() bool {
	if m.Mode == ModeHome {
		// Hide detail panel when the plan preview is active (it's a full-page view).
		if m.ShowPlanPreview {
			return false
		}
		// Show the detail panel in home mode when there are nebulas to describe.
		// When ShowPlan is toggled off, hide the detail panel.
		return len(m.filteredHomeNebulae()) > 0 && m.ShowPlan
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

	// Embedded phase chat — full-screen takeover.
	if m.PhaseChat != nil {
		return m.PhaseChat.View()
	}

	// Splash screen — show binary-star animation until it finishes or is skipped.
	if m.Splash != nil {
		return m.renderSplash()
	}

	// Content width depends on whether the sidebar is visible.
	contentWidth := m.contentWidth()
	sidebarW := m.sidebarWidth()

	var sections []string

	// Status bar — always full terminal width; sync execution control state.
	m.StatusBar.Paused = m.Paused
	m.StatusBar.Stopping = m.Stopping
	if m.Mode == ModeHome {
		m.StatusBar.HomeMode = true
		m.StatusBar.HomeNebulaCount = len(m.filteredHomeNebulae())
		if m.ShowPlanPreview && m.PlanPreview != nil && m.PlanPreview.Plan != nil {
			m.StatusBar.Name = m.PlanPreview.Plan.Name + " (plan preview)"
		}
	}
	sections = append(sections, m.StatusBar.View())
	sections = append(sections, "") // Spacing between header and content.

	// Build the "middle" section: sidebar (if visible) + right area.
	var middle []string

	if sidebarW > 0 {
		// Split-pane layout: sidebar | right area (tabs + main + detail).
		// Use tree view for hierarchical expand/collapse in the sidebar.
		m.Sidebar.SyncTree(m.StatusBar.Name, m.NebulaView.Phases, m.PhaseLoops, m.StatusBar.CostUSD)
		m.Sidebar.Width = sidebarW
		m.Sidebar.Height = m.sidebarHeight()
		m.Sidebar.Focus = m.PaneFocus == PaneFocusSidebar

		// Right area: tabs + main + detail stacked vertically.
		rightArea := m.renderRightArea(contentWidth)

		sidebarStr := m.Sidebar.View()
		combined := lipgloss.JoinHorizontal(lipgloss.Top, sidebarStr, rightArea)
		middle = append(middle, combined)
	} else {
		// Traditional full-width vertical layout.

		// Tab bar — only in nebula mode at DepthPhases level.
		if m.Mode == ModeNebula && m.Depth == DepthPhases {
			tb := TabBar{ActiveTab: m.ActiveTab, Width: contentWidth}
			middle = append(middle, tb.View())
		}

		// Top banner (S-A or XS-A modes) — between status bar and content.
		// Skip when the terminal is too short to avoid pushing content off-screen.
		if m.Height >= BannerCollapseHeight {
			if bannerView := m.Banner.View(); bannerView != "" {
				middle = append(middle, bannerView)
			}
		}

		// Breadcrumb (nebula drill-down) — hide if too narrow.
		if m.Mode == ModeNebula && m.Depth > DepthPhases && contentWidth >= CompactWidth {
			middle = append(middle, m.renderBreadcrumb())
		}

		// Main view.
		middle = append(middle, m.renderMainView())

		// Detail panel (when drilled into agent output) — auto-collapse on short terminals.
		// Home mode uses a higher threshold because the banner also consumes vertical space.
		detailThreshold := DetailCollapseHeight
		if m.Mode == ModeHome {
			detailThreshold = HomeDetailCollapseHeight
		}
		if m.showDetailPanel() && m.Height >= detailThreshold {
			sep := styleSectionBorder.Width(contentWidth).Render("")
			middle = append(middle, sep)
			m.Detail.Focus = true // full-width layout: detail always has focus
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
	}

	middleStr := lipgloss.JoinVertical(lipgloss.Left, middle...)

	sections = append(sections, middleStr)

	// Bottom bar — aggregate stats line (tokens, cost, elapsed, progress).
	if bottomBar := m.StatusBar.BottomBar(); bottomBar != "" {
		sections = append(sections, bottomBar)
	}

	// Footer — always full terminal width.
	footer := m.buildFooter()
	sections = append(sections, footer.View())

	base := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Dialog overlay — rendered over a dimmed background for interactive sessions.
	if m.Dialog != nil {
		dimmed := styleOverlayDimmed.Width(m.Width).Height(m.Height).Render(base)
		overlayContent := m.Dialog.View(m.Width, m.Height)
		overlayBox := centerOverlay(overlayContent, m.Width, m.Height)
		return compositeOverlay(dimmed, overlayBox, m.Width, m.Height)
	}

	// Hail overlay — rendered over a dimmed background when a human decision is pending.
	if m.Hail != nil {
		dimmed := styleOverlayDimmed.Width(m.Width).Height(m.Height).Render(base)
		overlayContent := m.Hail.View(m.Width, m.Height)
		overlayBox := centerOverlay(overlayContent, m.Width, m.Height)
		return compositeOverlay(dimmed, overlayBox, m.Width, m.Height)
	}

	// Hail list overlay — rendered over a dimmed background for browsing pending hails.
	if m.HailList != nil {
		dimmed := styleOverlayDimmed.Width(m.Width).Height(m.Height).Render(base)
		overlayContent := m.HailList.View(m.Width, m.Height)
		overlayBox := centerOverlay(overlayContent, m.Width, m.Height)
		return compositeOverlay(dimmed, overlayBox, m.Width, m.Height)
	}

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

// contentWidth returns the available width for main content.
// In nebula mode at DepthPhases with a wide-enough terminal, this subtracts
// the sidebar width to leave room for the split-pane layout.
func (m AppModel) contentWidth() int {
	sw := m.sidebarWidth()
	if sw > 0 {
		return m.Width - sw
	}
	return m.Width
}

// sidebarWidth returns the sidebar width for the current terminal size.
// Returns 0 when the sidebar should be hidden (non-nebula mode, drilled in,
// or terminal too narrow).
func (m AppModel) sidebarWidth() int {
	if m.Mode != ModeNebula || m.Depth != DepthPhases {
		return 0
	}
	return ComputeSidebarWidth(m.Width)
}

// sidebarHeight returns the available height for the sidebar content area.
// This matches the total height between the status bar and bottom bar/footer.
func (m AppModel) sidebarHeight() int {
	// Chrome: status bar (1) + spacing (1) + bottom bar (1) + footer (1) = 4.
	chrome := 4
	h := m.Height - chrome
	if h < 3 {
		h = 3
	}
	return h
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

// renderRightArea renders the right side of the split-pane layout.
// This includes the tab bar, main content view, and detail panel stacked vertically.
func (m AppModel) renderRightArea(width int) string {
	var parts []string

	// Tab bar.
	tb := TabBar{ActiveTab: m.ActiveTab, Width: width}
	parts = append(parts, tb.View())

	// Main content.
	parts = append(parts, m.renderMainView())

	// Detail panel — show when applicable, splitting the right area ~60/40.
	detailThreshold := DetailCollapseHeight
	if m.showDetailPanel() && m.Height >= detailThreshold {
		sep := styleSectionBorder.Width(width).Render("")
		parts = append(parts, sep)
		m.Detail.Focus = m.PaneFocus == PaneFocusMain
		parts = append(parts, m.Detail.View())
	}

	// Gate overlay.
	if m.Gate != nil {
		parts = append(parts, m.Gate.View())
	}

	// Toast notifications.
	if len(m.Toasts) > 0 {
		parts = append(parts, RenderToasts(m.Toasts, width))
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderMainView renders the appropriate view for the current depth.
func (m AppModel) renderMainView() string {
	w := m.contentWidth()
	switch m.Mode {
	case ModeHome:
		// Plan preview takes over the home view when active.
		if m.ShowPlanPreview && m.PlanPreview != nil {
			m.PlanPreview.SetSize(w, m.homeMainHeight())
			return m.PlanPreview.View()
		}
		hv := HomeView{
			Nebulae: m.filteredHomeNebulae(),
			Cursor:  m.HomeCursor,
			Offset:  m.HomeOffset,
			Width:   w,
			Height:  m.homeMainHeight(),
			Filter:  m.HomeFilter,
		}
		return hv.View()

	case ModeLoop:
		m.LoopView.Width = w
		return m.LoopView.View()

	case ModeNebula:
		switch m.Depth {
		case DepthPhases:
			switch m.ActiveTab {
			case TabBoard:
				var boardStr string
				if m.BoardActive {
					// Columnar board view — sync phases and render.
					m.Board.Phases = m.NebulaView.Phases
					m.Board.Width = w
					boardStr = m.Board.View()
				} else {
					// Table view fallback.
					m.NebulaView.Width = w
					boardStr = m.NebulaView.View()
				}
				// Append worker cards beneath the board/table for active phases.
				active := ActiveWorkerCards(m.WorkerCards)
				if len(active) > 0 {
					cardsStr := RenderWorkerCards(active, w)
					boardStr = lipgloss.JoinVertical(lipgloss.Left, boardStr, cardsStr)
				}
				return boardStr
			case TabEntanglements:
				m.EntanglementView.SetSize(w, m.detailHeight())
				return m.EntanglementView.View()
			case TabGraph:
				m.Graph.SetSize(w, m.detailHeight())
				return m.Graph.View()
			case TabScratchpad:
				return m.ScratchpadView.View()
			case TabImpact:
				m.ImpactView.SetSize(w, m.detailHeight())
				return m.ImpactView.View()
			default:
				m.NebulaView.Width = w
				return m.NebulaView.View()
			}
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

	// When viewing a single file diff, show file-open bindings.
	if m.ShowDiff && m.DiffFileList != nil && m.DiffFileOpen {
		f.Bindings = DiffFileOpenFooterBindings(m.Keys)
		return f
	}
	// When the diff file list is active, show dedicated diff-mode bindings.
	if m.ShowDiff && m.DiffFileList != nil {
		f.Bindings = DiffFileListFooterBindings(m.Keys)
		return f
	}

	if m.Dialog != nil {
		f.Bindings = DialogFooterBindings()
		return f
	}

	if m.Hail != nil {
		f.Bindings = HailOverlayFooterBindings(m.Keys)
		return f
	}

	if m.HailList != nil {
		f.Bindings = HailListFooterBindings(m.Keys)
		return f
	}

	if m.Gate != nil {
		f.Bindings = GateFooterBindings(m.Keys)
	} else if m.Mode == ModeHome {
		if m.ShowPlanPreview && m.PlanPreview != nil {
			f.Bindings = PlanFooterBindings(m.Keys)
		} else {
			f.Bindings = HomeFooterBindings(m.Keys)
		}
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
		} else if m.sidebarWidth() > 0 && m.PaneFocus == PaneFocusSidebar {
			f.Bindings = SidebarFooterBindings(m.Keys)
		} else if m.BoardActive {
			f.Bindings = CockpitFooterBindings(m.Keys)
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

	// Append hail list binding when pending hails exist.
	if m.Keys.HailList.Enabled() {
		f.Bindings = append(f.Bindings, m.Keys.HailList)
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
