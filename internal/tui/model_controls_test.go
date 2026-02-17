package tui

import (
	"os"
	"path/filepath"
	"testing"
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
