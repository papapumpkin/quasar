package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		// Block until context is cancelled.
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
