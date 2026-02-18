package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// --- handlePauseKey tests ---

func TestHandlePauseKey(t *testing.T) {
	t.Parallel()

	t.Run("writes PAUSE file and sets Paused flag", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)

		m.handlePauseKey()

		if !m.Paused {
			t.Error("expected Paused to be true after handlePauseKey")
		}
		pausePath := filepath.Join(dir, "PAUSE")
		data, err := os.ReadFile(pausePath)
		if err != nil {
			t.Fatalf("expected PAUSE file to exist: %v", err)
		}
		if string(data) != "paused by TUI\n" {
			t.Errorf("unexpected PAUSE file content: %q", string(data))
		}
	})

	t.Run("resume removes PAUSE file and unsets Paused flag", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)

		// First press: pause.
		m.handlePauseKey()
		if !m.Paused {
			t.Fatal("expected Paused to be true after first press")
		}

		// Second press: resume.
		m.handlePauseKey()
		if m.Paused {
			t.Error("expected Paused to be false after second press")
		}
		pausePath := filepath.Join(dir, "PAUSE")
		if _, err := os.Stat(pausePath); !os.IsNotExist(err) {
			t.Error("expected PAUSE file to be removed after resume")
		}
	})

	t.Run("no-op in loop mode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)
		m.Mode = ModeLoop

		m.handlePauseKey()

		if m.Paused {
			t.Error("expected Paused to remain false in loop mode")
		}
		assertNoFile(t, filepath.Join(dir, "PAUSE"))
	})

	t.Run("no-op at wrong depth", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)
		m.Depth = DepthPhaseLoop

		m.handlePauseKey()

		if m.Paused {
			t.Error("expected Paused to remain false at DepthPhaseLoop")
		}
		assertNoFile(t, filepath.Join(dir, "PAUSE"))
	})

	t.Run("no-op when NebulaDir is empty", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModel("")

		m.handlePauseKey()

		if m.Paused {
			t.Error("expected Paused to remain false when NebulaDir is empty")
		}
	})

	t.Run("no-op when already stopping", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)
		m.Stopping = true

		m.handlePauseKey()

		if m.Paused {
			t.Error("expected Paused to remain false when Stopping is true")
		}
		assertNoFile(t, filepath.Join(dir, "PAUSE"))
	})
}

// --- handleStopKey tests ---

func TestHandleStopKey(t *testing.T) {
	t.Parallel()

	t.Run("writes STOP file and sets Stopping flag", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)

		m.handleStopKey()

		if !m.Stopping {
			t.Error("expected Stopping to be true after handleStopKey")
		}
		stopPath := filepath.Join(dir, "STOP")
		data, err := os.ReadFile(stopPath)
		if err != nil {
			t.Fatalf("expected STOP file to exist: %v", err)
		}
		if string(data) != "stopped by TUI\n" {
			t.Errorf("unexpected STOP file content: %q", string(data))
		}
	})

	t.Run("no-op when already stopping", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)
		m.Stopping = true

		m.handleStopKey()

		// Should still be stopping but no new file write attempt.
		if !m.Stopping {
			t.Error("expected Stopping to remain true")
		}
	})

	t.Run("no-op in loop mode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)
		m.Mode = ModeLoop

		m.handleStopKey()

		if m.Stopping {
			t.Error("expected Stopping to remain false in loop mode")
		}
		assertNoFile(t, filepath.Join(dir, "STOP"))
	})

	t.Run("no-op at wrong depth", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModel(dir)
		m.Depth = DepthAgentOutput

		m.handleStopKey()

		if m.Stopping {
			t.Error("expected Stopping to remain false at DepthAgentOutput")
		}
		assertNoFile(t, filepath.Join(dir, "STOP"))
	})

	t.Run("no-op when NebulaDir is empty", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModel("")

		m.handleStopKey()

		if m.Stopping {
			t.Error("expected Stopping to remain false when NebulaDir is empty")
		}
	})
}

// --- handleRetryKey tests ---

func TestHandleRetryKey(t *testing.T) {
	t.Parallel()

	t.Run("writes RETRY file with phase ID when phase is failed at DepthPhases", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseFailed},
			{ID: "phase-2", Title: "Phase 2", Status: PhaseDone},
		})
		m.NebulaView.Cursor = 0 // select the failed phase

		m.handleRetryKey()

		retryPath := filepath.Join(dir, "RETRY")
		data, err := os.ReadFile(retryPath)
		if err != nil {
			t.Fatalf("expected RETRY file to exist: %v", err)
		}
		if string(data) != "phase-1\n" {
			t.Errorf("expected RETRY file to contain phase ID, got: %q", string(data))
		}
		// Phase status should be reset to PhaseWaiting.
		if m.NebulaView.Phases[0].Status != PhaseWaiting {
			t.Errorf("expected phase status to be reset to PhaseWaiting, got: %v", m.NebulaView.Phases[0].Status)
		}
	})

	t.Run("writes RETRY file when at DepthPhaseLoop with failed focused phase", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseFailed},
		})
		m.Depth = DepthPhaseLoop
		m.FocusedPhase = "phase-1"

		m.handleRetryKey()

		retryPath := filepath.Join(dir, "RETRY")
		data, err := os.ReadFile(retryPath)
		if err != nil {
			t.Fatalf("expected RETRY file to exist: %v", err)
		}
		if string(data) != "phase-1\n" {
			t.Errorf("expected RETRY file to contain phase ID, got: %q", string(data))
		}
	})

	t.Run("clears per-phase loop view on retry", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseFailed},
		})
		// Simulate an existing per-phase loop view.
		lv := NewLoopView()
		m.PhaseLoops["phase-1"] = &lv

		m.handleRetryKey()

		if _, exists := m.PhaseLoops["phase-1"]; exists {
			t.Error("expected per-phase loop view to be cleared on retry")
		}
	})

	t.Run("adds message on retry", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseFailed},
		})

		m.handleRetryKey()

		found := false
		for _, msg := range m.Messages {
			if msg == "retrying phase phase-1" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected message 'retrying phase phase-1', got: %v", m.Messages)
		}
	})

	t.Run("no-op when selected phase is not failed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseDone},
		})

		m.handleRetryKey()

		assertNoFile(t, filepath.Join(dir, "RETRY"))
	})

	t.Run("no-op when phase is waiting", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseWaiting},
		})

		m.handleRetryKey()

		assertNoFile(t, filepath.Join(dir, "RETRY"))
	})

	t.Run("no-op when phase is working", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseWorking},
		})

		m.handleRetryKey()

		assertNoFile(t, filepath.Join(dir, "RETRY"))
	})

	t.Run("no-op in loop mode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseFailed},
		})
		m.Mode = ModeLoop

		m.handleRetryKey()

		assertNoFile(t, filepath.Join(dir, "RETRY"))
	})

	t.Run("no-op when NebulaDir is empty", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseFailed},
		})

		m.handleRetryKey()

		// Can't check file since there's no dir, but state should not crash.
		if m.NebulaView.Phases[0].Status != PhaseFailed {
			t.Error("expected phase status to remain PhaseFailed")
		}
	})

	t.Run("no-op at DepthAgentOutput", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		m := newNebulaModelWithPhases(dir, []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseFailed},
		})
		m.Depth = DepthAgentOutput

		m.handleRetryKey()

		assertNoFile(t, filepath.Join(dir, "RETRY"))
	})
}

// --- handleInfoKey tests ---

func TestHandleInfoKey(t *testing.T) {
	t.Parallel()

	t.Run("toggles plan on at DepthPhases", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan\nDo stuff."},
		})

		m.handleInfoKey()

		if !m.ShowPlan {
			t.Error("expected ShowPlan to be true after handleInfoKey")
		}
	})

	t.Run("toggles plan off when already on", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan\nDo stuff."},
		})
		m.ShowPlan = true

		m.handleInfoKey()

		if m.ShowPlan {
			t.Error("expected ShowPlan to be false after second handleInfoKey")
		}
	})

	t.Run("works at DepthPhaseLoop", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan"},
		})
		m.Depth = DepthPhaseLoop
		m.FocusedPhase = "phase-1"

		m.handleInfoKey()

		if !m.ShowPlan {
			t.Error("expected ShowPlan to be true at DepthPhaseLoop")
		}
	})

	t.Run("no-op in loop mode", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan"},
		})
		m.Mode = ModeLoop

		m.handleInfoKey()

		if m.ShowPlan {
			t.Error("expected ShowPlan to remain false in loop mode")
		}
	})

	t.Run("no-op at DepthAgentOutput", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan"},
		})
		m.Depth = DepthAgentOutput

		m.handleInfoKey()

		if m.ShowPlan {
			t.Error("expected ShowPlan to remain false at DepthAgentOutput")
		}
	})

	t.Run("drillDown dismisses plan", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan"},
		})
		m.ShowPlan = true

		m.drillDown()

		if m.ShowPlan {
			t.Error("expected ShowPlan to be false after drillDown")
		}
	})

	t.Run("drillUp dismisses plan without changing depth", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan"},
		})
		m.Depth = DepthPhaseLoop
		m.FocusedPhase = "phase-1"
		m.ShowPlan = true

		m.drillUp()

		if m.ShowPlan {
			t.Error("expected ShowPlan to be false after drillUp")
		}
		if m.Depth != DepthPhaseLoop {
			t.Errorf("expected depth to remain DepthPhaseLoop, got %d", m.Depth)
		}
	})

	t.Run("drillUp dismisses diff without changing depth", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1"},
		})
		m.Depth = DepthAgentOutput
		m.FocusedPhase = "phase-1"
		m.ShowDiff = true
		m.DiffFileList = &FileListView{Files: []FileStatEntry{{Path: "a.go"}}}

		m.drillUp()

		if m.ShowDiff {
			t.Error("expected ShowDiff to be false after drillUp")
		}
		if m.DiffFileList != nil {
			t.Error("expected DiffFileList to be nil after drillUp")
		}
		if m.Depth != DepthAgentOutput {
			t.Errorf("expected depth to remain DepthAgentOutput, got %d", m.Depth)
		}
	})

	t.Run("showDetailPanel true when plan is toggled on at DepthPhases", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan"},
		})
		m.ShowPlan = true

		if !m.showDetailPanel() {
			t.Error("expected showDetailPanel to return true when ShowPlan is true at DepthPhases")
		}
	})

	t.Run("toggles plan on dismisses beads", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan"},
		})
		m.ShowBeads = true

		m.handleInfoKey()

		if !m.ShowPlan {
			t.Error("expected ShowPlan to be true")
		}
		if m.ShowBeads {
			t.Error("expected ShowBeads to be false when plan toggled on")
		}
	})

	t.Run("toggles plan on dismisses diff and file list", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", PlanBody: "# Plan"},
		})
		m.ShowDiff = true
		m.DiffFileList = &FileListView{} // non-nil sentinel

		m.handleInfoKey()

		if !m.ShowPlan {
			t.Error("expected ShowPlan to be true")
		}
		if m.ShowDiff {
			t.Error("expected ShowDiff to be false when plan toggled on")
		}
		if m.DiffFileList != nil {
			t.Error("expected DiffFileList to be nil when plan toggled on")
		}
	})
}

// --- handleDiffKey mutual exclusivity tests ---

func TestHandleDiffKeyMutualExclusivity(t *testing.T) {
	t.Parallel()

	newLoopModelWithDiff := func() *AppModel {
		m := NewAppModel(ModeLoop)
		m.Detail = NewDetailPanel(80, 10)
		m.Width = 80
		m.Height = 24
		m.LoopView.StartCycle(1)
		m.LoopView.StartAgent("coder")
		m.LoopView.FinishAgent("coder", 0.5, 5000)
		m.LoopView.SetAgentOutput("coder", 1, "wrote code")
		m.LoopView.SetAgentDiff("coder", 1, "diff --git a/f.go b/f.go\n+line\n")
		m.Depth = DepthAgentOutput
		m.LoopView.Cursor = 1
		return &m
	}

	t.Run("toggles diff on dismisses plan", func(t *testing.T) {
		t.Parallel()
		m := newLoopModelWithDiff()
		m.ShowPlan = true

		m.handleDiffKey()

		if m.ShowPlan {
			t.Error("expected ShowPlan to be false when diff toggled on")
		}
	})

	t.Run("toggles diff on dismisses beads", func(t *testing.T) {
		t.Parallel()
		m := newLoopModelWithDiff()
		m.ShowBeads = true

		m.handleDiffKey()

		if m.ShowBeads {
			t.Error("expected ShowBeads to be false when diff toggled on")
		}
	})
}

// --- drillDown state preservation tests (Bug 3) ---

func TestDrillDownPreservesDiffStateAtAgentOutput(t *testing.T) {
	t.Parallel()

	t.Run("loop mode at DepthAgentOutput preserves diff state", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Detail = NewDetailPanel(80, 10)
		m.Width = 80
		m.Height = 24
		m.LoopView.StartCycle(1)
		m.LoopView.StartAgent("coder")
		m.LoopView.FinishAgent("coder", 0.5, 5000)
		m.LoopView.SetAgentOutput("coder", 1, "wrote code")
		m.LoopView.SetAgentDiff("coder", 1, "diff --git a/f.go b/f.go\n+line\n")
		m.Depth = DepthAgentOutput
		m.LoopView.Cursor = 1
		m.ShowDiff = true
		m.DiffFileList = &FileListView{} // non-nil sentinel

		m.drillDown()

		if !m.ShowDiff {
			t.Error("expected ShowDiff to remain true at DepthAgentOutput in loop mode")
		}
		if m.DiffFileList == nil {
			t.Error("expected DiffFileList to remain non-nil at DepthAgentOutput in loop mode")
		}
	})

	t.Run("loop mode at DepthAgentOutput preserves beads and plan state", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Detail = NewDetailPanel(80, 10)
		m.Width = 80
		m.Height = 24
		m.Depth = DepthAgentOutput
		m.ShowBeads = true
		m.ShowPlan = true

		m.drillDown()

		if !m.ShowBeads {
			t.Error("expected ShowBeads to remain true at DepthAgentOutput in loop mode")
		}
		if !m.ShowPlan {
			t.Error("expected ShowPlan to remain true at DepthAgentOutput in loop mode")
		}
	})

	t.Run("nebula mode clears state when transitioning from DepthPhases", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1"},
		})
		m.ShowDiff = true
		m.ShowBeads = true
		m.ShowPlan = true
		m.DiffFileList = &FileListView{}

		m.drillDown()

		if m.ShowDiff {
			t.Error("expected ShowDiff to be cleared on nebula depth transition")
		}
		if m.ShowBeads {
			t.Error("expected ShowBeads to be cleared on nebula depth transition")
		}
		if m.ShowPlan {
			t.Error("expected ShowPlan to be cleared on nebula depth transition")
		}
		if m.DiffFileList != nil {
			t.Error("expected DiffFileList to be nil on nebula depth transition")
		}
	})
}

// --- handleDiffKey no-diff-files guard tests (Bug 4) ---

func TestHandleDiffKeyNoDiffFiles(t *testing.T) {
	t.Parallel()

	t.Run("resets ShowDiff when no diff files and no raw diff text", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Detail = NewDetailPanel(80, 10)
		m.Width = 80
		m.Height = 24
		m.LoopView.StartCycle(1)
		m.LoopView.StartAgent("coder")
		m.LoopView.FinishAgent("coder", 0.5, 5000)
		m.LoopView.SetAgentOutput("coder", 1, "wrote code")
		// No diff set — agent has no diff files and no raw diff text.
		m.Depth = DepthAgentOutput
		m.LoopView.Cursor = 1

		m.handleDiffKey()

		if m.ShowDiff {
			t.Error("expected ShowDiff to be false when no diff files or raw diff text exist")
		}
		if m.DiffFileList != nil {
			t.Error("expected DiffFileList to remain nil when no diff data available")
		}
	})

	t.Run("keeps ShowDiff true when raw diff text exists but no file list", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Detail = NewDetailPanel(80, 10)
		m.Width = 80
		m.Height = 24
		m.LoopView.StartCycle(1)
		m.LoopView.StartAgent("coder")
		m.LoopView.FinishAgent("coder", 0.5, 5000)
		m.LoopView.SetAgentOutput("coder", 1, "wrote code")
		m.LoopView.SetAgentDiff("coder", 1, "diff --git a/f.go b/f.go\n+line\n")
		m.Depth = DepthAgentOutput
		m.LoopView.Cursor = 1

		m.handleDiffKey()

		if !m.ShowDiff {
			t.Error("expected ShowDiff to remain true when raw diff text exists")
		}
	})
}

// --- Diff file list navigation at DepthAgentOutput ---

func TestDiffFileListNavigationAtAgentOutput(t *testing.T) {
	t.Parallel()

	t.Run("up/down navigate file list instead of scrolling detail", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Splash = nil // Disable splash so handleKey processes navigation keys.
		m.Width = 120
		m.Height = 40
		m.Detail = NewDetailPanel(80, 10)
		m.Depth = DepthAgentOutput
		m.LoopView.StartCycle(1)
		m.LoopView.StartAgent("coder")
		m.LoopView.FinishAgent("coder", 0.01, 100)
		m.LoopView.SetAgentDiff("coder", 1, "diff --git a/f.go b/f.go\n+line\n")
		m.LoopView.Cursor = 1
		m.ShowDiff = true
		m.DiffFileList = &FileListView{
			Files: []FileStatEntry{
				{Path: "a.go", Additions: 1, Deletions: 0},
				{Path: "b.go", Additions: 2, Deletions: 1},
				{Path: "c.go", Additions: 0, Deletions: 3},
			},
			Cursor: 0,
			Width:  80,
		}

		// Simulate pressing "down" key.
		downMsg := tea.KeyMsg{Type: tea.KeyDown}
		result, _ := m.handleKey(downMsg)
		updated := result.(AppModel)

		if updated.DiffFileList.Cursor != 1 {
			t.Errorf("expected file list cursor to be 1, got %d", updated.DiffFileList.Cursor)
		}
	})
}

// --- handleBeadsKey mutual exclusivity tests ---

func TestHandleBeadsKeyMutualExclusivity(t *testing.T) {
	t.Parallel()

	t.Run("toggles beads on dismisses plan", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1"},
		})
		m.ShowPlan = true

		m.handleBeadsKey()

		if !m.ShowBeads {
			t.Error("expected ShowBeads to be true")
		}
		if m.ShowPlan {
			t.Error("expected ShowPlan to be false when beads toggled on")
		}
	})

	t.Run("toggles beads on dismisses diff and file list", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1"},
		})
		m.ShowDiff = true
		m.DiffFileList = &FileListView{}

		m.handleBeadsKey()

		if !m.ShowBeads {
			t.Error("expected ShowBeads to be true")
		}
		if m.ShowDiff {
			t.Error("expected ShowDiff to be false when beads toggled on")
		}
		if m.DiffFileList != nil {
			t.Error("expected DiffFileList to be nil when beads toggled on")
		}
	})
}

// --- MsgArchitectStart handler tests ---

func TestMsgArchitectStartWithFunc(t *testing.T) {
	t.Parallel()

	m := newNebulaModelWithPhases("", []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})
	m.Architect = NewArchitectOverlay("create", "", m.NebulaView.Phases)
	m.Architect.StartWorking()

	called := false
	m.ArchitectFunc = func(_ context.Context, msg MsgArchitectStart) (*nebula.ArchitectResult, error) {
		called = true
		if msg.Mode != "create" {
			t.Errorf("Mode = %q, want %q", msg.Mode, "create")
		}
		return &nebula.ArchitectResult{
			Filename: "test.md",
			PhaseSpec: nebula.PhaseSpec{
				ID:    "test",
				Title: "Test Phase",
			},
			Body: "test body",
		}, nil
	}

	msg := MsgArchitectStart{Mode: "create", Prompt: "build it"}
	result, cmd := m.Update(msg)
	updated := result.(AppModel)

	if cmd == nil {
		t.Fatal("expected a command to be returned for architect dispatch")
	}

	// Execute the command to trigger the ArchitectFunc.
	resultMsg := cmd()
	archResult, ok := resultMsg.(MsgArchitectResult)
	if !ok {
		t.Fatalf("expected MsgArchitectResult, got %T", resultMsg)
	}
	if !called {
		t.Error("ArchitectFunc was not called")
	}
	if archResult.Err != nil {
		t.Errorf("unexpected error: %v", archResult.Err)
	}
	if archResult.Result.PhaseSpec.ID != "test" {
		t.Errorf("Result.PhaseSpec.ID = %q, want %q", archResult.Result.PhaseSpec.ID, "test")
	}

	// Feed the result back to Update.
	result2, _ := updated.Update(archResult)
	updated2 := result2.(AppModel)
	if updated2.Architect == nil {
		t.Fatal("Architect should not be nil after successful result")
	}
	if updated2.Architect.Step != stepPreview {
		t.Errorf("Step = %d, want stepPreview", updated2.Architect.Step)
	}
}

func TestMsgArchitectStartWithoutFunc(t *testing.T) {
	t.Parallel()

	m := newNebulaModelWithPhases("", []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})
	m.Architect = NewArchitectOverlay("create", "", m.NebulaView.Phases)
	m.Architect.StartWorking()
	// Do not set ArchitectFunc — it should handle gracefully.

	msg := MsgArchitectStart{Mode: "create", Prompt: "build it"}
	result, _ := m.Update(msg)
	updated := result.(AppModel)

	// Architect should be cleared and an error message added.
	if updated.Architect != nil {
		t.Error("Architect should be nil when no ArchitectFunc is set")
	}
	found := false
	for _, m := range updated.Messages {
		if strings.Contains(m, "not available") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'not available' message in Messages")
	}
}

// --- handleGateKey Esc tests ---

func TestHandleGateKeyEscDismissesGate(t *testing.T) {
	t.Parallel()

	t.Run("Esc resolves gate with skip action", func(t *testing.T) {
		t.Parallel()
		m := newNebulaModelWithPhases("", []PhaseEntry{
			{ID: "phase-1", Title: "Phase 1", Status: PhaseGate},
		})
		m.Splash = nil

		ch := make(chan nebula.GateAction, 1)
		m.Gate = NewGatePrompt(nil, ch)

		escMsg := tea.KeyMsg{Type: tea.KeyEscape}
		result, _ := m.handleKey(escMsg)
		updated := result.(AppModel)

		if updated.Gate != nil {
			t.Error("expected Gate to be nil after Esc")
		}

		select {
		case action := <-ch:
			if action != nebula.GateActionSkip {
				t.Errorf("expected GateActionSkip, got %q", action)
			}
		default:
			t.Error("expected gate response channel to receive an action")
		}
	})
}

// --- Completion overlay Esc tests ---

func TestCompletionOverlayEscQuits(t *testing.T) {
	t.Parallel()

	t.Run("Esc on completion overlay triggers quit", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Splash = nil
		m.Overlay = &CompletionOverlay{Kind: CompletionSuccess, Message: "done"}

		escMsg := tea.KeyMsg{Type: tea.KeyEscape}
		_, cmd := m.handleKey(escMsg)

		if cmd == nil {
			t.Fatal("expected a command to be returned for Esc on completion overlay")
		}
		resultMsg := cmd()
		if _, ok := resultMsg.(tea.QuitMsg); !ok {
			t.Errorf("expected tea.QuitMsg, got %T", resultMsg)
		}
	})
}

// --- Test helpers ---

// newNebulaModel creates an AppModel in nebula mode at DepthPhases with
// the given directory for intervention files.
func newNebulaModel(nebulaDir string) *AppModel {
	m := NewAppModel(ModeNebula)
	m.NebulaDir = nebulaDir
	m.Depth = DepthPhases
	return &m
}

// newNebulaModelWithPhases creates a nebula model with pre-populated phases.
func newNebulaModelWithPhases(nebulaDir string, phases []PhaseEntry) *AppModel {
	m := newNebulaModel(nebulaDir)
	m.NebulaView.Phases = phases
	m.NebulaView.Cursor = 0
	return m
}

// assertNoFile asserts that the given file path does not exist.
func assertNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file %s to not exist", path)
	}
}

// --- Overlay priority tests ---

func TestArchitectOverlayTakesPriorityOverCompletion(t *testing.T) {
	t.Parallel()

	m := newNebulaModelWithPhases("", []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})
	m.Splash = nil
	m.Architect = NewArchitectOverlay("create", "", m.NebulaView.Phases)
	m.Overlay = &CompletionOverlay{Kind: CompletionSuccess, Message: "done"}

	// Send a 'q' key — should be handled by architect (no quit), not completion.
	qKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	result, cmd := m.Update(qKey)
	updated := result.(AppModel)

	// The architect overlay should still be active (it doesn't quit on 'q' at input step).
	if updated.Architect == nil {
		t.Error("expected Architect overlay to still be active after key event")
	}
	// Critically, the completion overlay should NOT have triggered tea.Quit.
	if cmd != nil {
		resultMsg := cmd()
		if _, ok := resultMsg.(tea.QuitMsg); ok {
			t.Error("completion overlay stole 'q' key and triggered Quit; architect should have priority")
		}
	}
}

// --- MsgNebulaDone architect cleanup tests ---

func TestMsgNebulaDoneCleansUpArchitect(t *testing.T) {
	t.Parallel()

	m := newNebulaModelWithPhases("", []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})
	m.Architect = NewArchitectOverlay("create", "", m.NebulaView.Phases)

	canceled := false
	m.Architect.CancelFunc = func() { canceled = true }

	msg := MsgNebulaDone{}
	result, _ := m.Update(msg)
	updated := result.(AppModel)

	if updated.Architect != nil {
		t.Error("expected Architect to be nil after MsgNebulaDone")
	}
	if !canceled {
		t.Error("expected Architect CancelFunc to be called on MsgNebulaDone")
	}
	if updated.Overlay == nil {
		t.Error("expected Overlay to be set after MsgNebulaDone")
	}
}

func TestMsgNebulaDoneWithoutArchitect(t *testing.T) {
	t.Parallel()

	m := newNebulaModelWithPhases("", []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})
	// No architect set — should not panic.
	msg := MsgNebulaDone{}
	result, _ := m.Update(msg)
	updated := result.(AppModel)

	if updated.Overlay == nil {
		t.Error("expected Overlay to be set after MsgNebulaDone")
	}
}

// --- MsgArchitectConfirm handler tests ---

func TestMsgArchitectConfirmWritesFullPhaseFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := newNebulaModelWithPhases(dir, []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})

	msg := MsgArchitectConfirm{
		Result: &nebula.ArchitectResult{
			Filename: "rate-limiting.md",
			PhaseSpec: nebula.PhaseSpec{
				ID:    "rate-limiting",
				Title: "Add rate limiting",
				Type:  "feature",
				Body:  "Implement token bucket algorithm.",
			},
			Body: "Implement token bucket algorithm.",
		},
		DependsOn: []string{"setup"},
	}

	result, _ := m.Update(msg)
	_ = result.(AppModel)

	// Read the written file and verify it has proper frontmatter.
	filePath := filepath.Join(dir, "rate-limiting.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "+++\n") {
		t.Error("written file should start with +++ frontmatter delimiter")
	}
	if !strings.Contains(content, "id = ") || !strings.Contains(content, "rate-limiting") {
		t.Error("written file should contain phase ID in frontmatter")
	}
	if !strings.Contains(content, "title = ") || !strings.Contains(content, "Add rate limiting") {
		t.Error("written file should contain phase title in frontmatter")
	}
	if !strings.Contains(content, "depends_on") || !strings.Contains(content, "setup") {
		t.Errorf("written file should contain user-selected dependencies, got:\n%s", content)
	}
	if !strings.Contains(content, "Implement token bucket algorithm.") {
		t.Error("written file should contain body text after frontmatter")
	}

	// Verify it round-trips through the parser.
	spec, err := nebula.MarshalPhaseFile(msg.Result.PhaseSpec)
	if err != nil {
		t.Fatalf("MarshalPhaseFile: %v", err)
	}
	if !strings.HasPrefix(string(spec), "+++\n") {
		t.Error("marshaled spec should start with +++ delimiter")
	}
}

// --- MsgArchitectStart cancellation tests ---

func TestMsgArchitectStartStoresCancelFunc(t *testing.T) {
	t.Parallel()

	m := newNebulaModelWithPhases("", []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})
	m.Architect = NewArchitectOverlay("create", "", m.NebulaView.Phases)
	m.Architect.StartWorking()

	m.ArchitectFunc = func(ctx context.Context, msg MsgArchitectStart) (*nebula.ArchitectResult, error) {
		// Block until context is canceled.
		<-ctx.Done()
		return nil, ctx.Err()
	}

	msg := MsgArchitectStart{Mode: "create", Prompt: "build it"}
	result, cmd := m.Update(msg)
	updated := result.(AppModel)

	if updated.Architect == nil {
		t.Fatal("Architect should not be nil after start")
	}
	if updated.Architect.CancelFunc == nil {
		t.Fatal("CancelFunc should be set on the overlay after MsgArchitectStart")
	}

	// Verify cancel actually stops the goroutine.
	updated.Architect.CancelFunc()
	if cmd == nil {
		t.Fatal("expected a command to be returned")
	}
	resultMsg := cmd()
	archResult, ok := resultMsg.(MsgArchitectResult)
	if !ok {
		t.Fatalf("expected MsgArchitectResult, got %T", resultMsg)
	}
	if archResult.Err == nil {
		t.Error("expected context cancellation error")
	}
}

// --- safeArchitectCall panic recovery tests ---

func TestSafeArchitectCallPanicRecovery(t *testing.T) {
	t.Parallel()

	m := newNebulaModelWithPhases("", []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})
	m.Architect = NewArchitectOverlay("create", "", m.NebulaView.Phases)
	m.Architect.StartWorking()

	m.ArchitectFunc = func(_ context.Context, _ MsgArchitectStart) (*nebula.ArchitectResult, error) {
		panic("boom")
	}

	msg := MsgArchitectStart{Mode: "create", Prompt: "build it"}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected a command to be returned")
	}

	// Execute the command — should not panic.
	resultMsg := cmd()
	archResult, ok := resultMsg.(MsgArchitectResult)
	if !ok {
		t.Fatalf("expected MsgArchitectResult, got %T", resultMsg)
	}
	if archResult.Err == nil {
		t.Fatal("expected error from panic recovery, got nil")
	}
	if !strings.Contains(archResult.Err.Error(), "architect panic") {
		t.Errorf("error = %q, want it to contain %q", archResult.Err, "architect panic")
	}
	if !strings.Contains(archResult.Err.Error(), "boom") {
		t.Errorf("error = %q, want it to contain %q", archResult.Err, "boom")
	}
	if archResult.Result != nil {
		t.Errorf("Result should be nil after panic, got %v", archResult.Result)
	}
}

func TestSafeArchitectCallNormalOperation(t *testing.T) {
	t.Parallel()

	m := newNebulaModelWithPhases("", []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
	})
	m.Architect = NewArchitectOverlay("create", "", m.NebulaView.Phases)
	m.Architect.StartWorking()

	expected := &nebula.ArchitectResult{
		Filename: "plan.md",
		PhaseSpec: nebula.PhaseSpec{
			ID:    "deploy",
			Title: "Deploy Phase",
		},
		Body: "deploy body",
	}

	m.ArchitectFunc = func(_ context.Context, msg MsgArchitectStart) (*nebula.ArchitectResult, error) {
		if msg.Prompt != "do it" {
			t.Errorf("Prompt = %q, want %q", msg.Prompt, "do it")
		}
		return expected, nil
	}

	msg := MsgArchitectStart{Mode: "create", Prompt: "do it"}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected a command to be returned")
	}

	resultMsg := cmd()
	archResult, ok := resultMsg.(MsgArchitectResult)
	if !ok {
		t.Fatalf("expected MsgArchitectResult, got %T", resultMsg)
	}
	if archResult.Err != nil {
		t.Errorf("unexpected error: %v", archResult.Err)
	}
	if archResult.Result != expected {
		t.Errorf("Result = %v, want %v", archResult.Result, expected)
	}
}
