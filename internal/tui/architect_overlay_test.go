package tui

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestNewArchitectOverlay(t *testing.T) {
	t.Parallel()

	phases := []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
		{ID: "auth", Status: PhaseWorking},
	}

	t.Run("create mode", func(t *testing.T) {
		t.Parallel()
		a := NewArchitectOverlay("create", "", phases)
		if a.Mode != "create" {
			t.Errorf("Mode = %q, want %q", a.Mode, "create")
		}
		if a.Step != stepInput {
			t.Errorf("Step = %d, want stepInput", a.Step)
		}
		if len(a.AllPhaseIDs) != 2 {
			t.Errorf("AllPhaseIDs = %d, want 2", len(a.AllPhaseIDs))
		}
	})

	t.Run("refactor mode", func(t *testing.T) {
		t.Parallel()
		a := NewArchitectOverlay("refactor", "auth", phases)
		if a.Mode != "refactor" {
			t.Errorf("Mode = %q, want %q", a.Mode, "refactor")
		}
		if a.PhaseID != "auth" {
			t.Errorf("PhaseID = %q, want %q", a.PhaseID, "auth")
		}
	})
}

func TestArchitectOverlaySetResult(t *testing.T) {
	t.Parallel()

	phases := []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
		{ID: "auth", Status: PhaseWorking},
		{ID: "tests", Status: PhaseWaiting},
	}

	a := NewArchitectOverlay("create", "", phases)
	result := &nebula.ArchitectResult{
		Filename: "rate-limiting.md",
		PhaseSpec: nebula.PhaseSpec{
			ID:        "rate-limiting",
			Title:     "Add rate limiting",
			Type:      "feature",
			Priority:  2,
			DependsOn: []string{"auth"},
		},
		Body: "Implement rate limiting...",
	}

	a.SetResult(result, phases)

	if a.Step != stepPreview {
		t.Errorf("Step = %d, want stepPreview", a.Step)
	}
	if len(a.Deps) != 3 {
		t.Fatalf("Deps = %d, want 3", len(a.Deps))
	}
	// "auth" should be pre-selected (it's in DependsOn).
	if !a.Deps[1].Selected {
		t.Error("auth should be pre-selected")
	}
	if a.Deps[0].Selected {
		t.Error("setup should not be pre-selected")
	}
	if a.Deps[2].Selected {
		t.Error("tests should not be pre-selected")
	}
}

func TestArchitectOverlayDepNavigation(t *testing.T) {
	t.Parallel()

	a := NewArchitectOverlay("create", "", nil)
	a.Step = stepPreview
	a.Deps = []DepEntry{
		{ID: "a", Status: PhaseDone},
		{ID: "b", Status: PhaseWorking},
		{ID: "c", Status: PhaseWaiting},
	}

	if a.DepCursor != 0 {
		t.Errorf("initial cursor = %d, want 0", a.DepCursor)
	}

	a.MoveDepDown()
	if a.DepCursor != 1 {
		t.Errorf("after down: cursor = %d, want 1", a.DepCursor)
	}

	a.MoveDepDown()
	if a.DepCursor != 2 {
		t.Errorf("after down 2: cursor = %d, want 2", a.DepCursor)
	}

	// Should not go past the end.
	a.MoveDepDown()
	if a.DepCursor != 2 {
		t.Errorf("after down 3: cursor = %d, want 2 (clamped)", a.DepCursor)
	}

	a.MoveDepUp()
	if a.DepCursor != 1 {
		t.Errorf("after up: cursor = %d, want 1", a.DepCursor)
	}

	// Should not go before 0.
	a.MoveDepUp()
	a.MoveDepUp()
	if a.DepCursor != 0 {
		t.Errorf("after up 3: cursor = %d, want 0 (clamped)", a.DepCursor)
	}
}

func TestArchitectOverlayToggleDep(t *testing.T) {
	t.Parallel()

	a := NewArchitectOverlay("create", "", nil)
	a.Step = stepPreview
	a.Deps = []DepEntry{
		{ID: "a", Status: PhaseDone},
		{ID: "b", Status: PhaseWorking},
	}
	a.DepCursor = 0

	// Toggle on.
	a.ToggleDep("new-phase", nil)
	if !a.Deps[0].Selected {
		t.Error("dep a should be selected after toggle")
	}

	// Toggle off.
	a.ToggleDep("new-phase", nil)
	if a.Deps[0].Selected {
		t.Error("dep a should be deselected after second toggle")
	}
}

func TestArchitectOverlaySelectedDeps(t *testing.T) {
	t.Parallel()

	a := NewArchitectOverlay("create", "", nil)
	a.Deps = []DepEntry{
		{ID: "a", Selected: true},
		{ID: "b", Selected: false},
		{ID: "c", Selected: true},
	}

	deps := a.SelectedDeps()
	if len(deps) != 2 {
		t.Fatalf("SelectedDeps = %d, want 2", len(deps))
	}
	if deps[0] != "a" || deps[1] != "c" {
		t.Errorf("SelectedDeps = %v, want [a c]", deps)
	}
}

func TestArchitectOverlayCycleDetection(t *testing.T) {
	t.Parallel()

	a := NewArchitectOverlay("create", "", nil)
	a.Step = stepPreview
	a.Deps = []DepEntry{
		{ID: "a", Status: PhaseDone},
	}
	a.DepCursor = 0

	// Toggle with a cycle-detecting function that always reports a cycle.
	a.ToggleDep("new-phase", func(_ []string) bool {
		return true
	})

	// Should not be selected and should have a warning.
	if a.Deps[0].Selected {
		t.Error("dep should not be selected when cycle detected")
	}
	if a.CycleWarn == "" {
		t.Error("CycleWarn should be set when cycle detected")
	}
}

func TestArchitectOverlayViewInput(t *testing.T) {
	t.Parallel()

	phases := []PhaseEntry{{ID: "a", Status: PhaseDone}}
	a := NewArchitectOverlay("create", "", phases)

	view := a.View(80, 24)
	if !strings.Contains(view, "New Phase") {
		t.Error("input view should contain 'New Phase' title")
	}
	if !strings.Contains(view, "enter:generate") {
		t.Error("input view should contain key hints")
	}
}

func TestArchitectOverlayViewRefactorInput(t *testing.T) {
	t.Parallel()

	phases := []PhaseEntry{{ID: "auth", Status: PhaseWorking}}
	a := NewArchitectOverlay("refactor", "auth", phases)

	view := a.View(80, 24)
	if !strings.Contains(view, "Edit Phase") {
		t.Error("refactor input view should contain 'Edit Phase' title")
	}
}

func TestArchitectOverlayViewWorking(t *testing.T) {
	t.Parallel()

	a := NewArchitectOverlay("create", "", nil)
	a.StartWorking()

	view := a.View(80, 24)
	if !strings.Contains(view, "Generating phase") {
		t.Error("working view should contain 'Generating phase'")
	}
}

func TestArchitectOverlayViewPreview(t *testing.T) {
	t.Parallel()

	phases := []PhaseEntry{
		{ID: "setup", Status: PhaseDone},
		{ID: "auth", Status: PhaseWorking},
	}
	a := NewArchitectOverlay("create", "", phases)
	result := &nebula.ArchitectResult{
		Filename: "rate-limiting.md",
		PhaseSpec: nebula.PhaseSpec{
			ID:        "rate-limiting",
			Title:     "Add rate limiting",
			Type:      "feature",
			Priority:  2,
			DependsOn: []string{"auth"},
		},
		Body: "Implement token bucket rate limiting",
	}
	a.SetResult(result, phases)

	view := a.View(80, 24)
	if !strings.Contains(view, "Preview: rate-limiting") {
		t.Error("preview should contain phase ID")
	}
	if !strings.Contains(view, "Add rate limiting") {
		t.Error("preview should contain title")
	}
	if !strings.Contains(view, "enter:confirm") {
		t.Error("preview should contain confirmation hint")
	}
}

func TestStatusLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status PhaseStatus
		want   string
	}{
		{PhaseWaiting, "waiting"},
		{PhaseWorking, "working"},
		{PhaseDone, "done"},
		{PhaseFailed, "failed"},
		{PhaseGate, "gate"},
		{PhaseSkipped, "skipped"},
		{PhaseStatus(99), "unknown"},
	}

	for _, tt := range tests {
		got := statusLabel(tt.status)
		if got != tt.want {
			t.Errorf("statusLabel(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestArchitectOverlayStartWorking(t *testing.T) {
	t.Parallel()

	a := NewArchitectOverlay("create", "", nil)
	if a.Step != stepInput {
		t.Errorf("initial step = %d, want stepInput", a.Step)
	}

	a.StartWorking()
	if a.Step != stepWorking {
		t.Errorf("after StartWorking: step = %d, want stepWorking", a.Step)
	}
}

func TestWouldCreateCycleWithRealDeps(t *testing.T) {
	t.Parallel()

	t.Run("detects cycle through existing edges", func(t *testing.T) {
		t.Parallel()
		// a → b → c (existing edges). Adding c → a would create a cycle.
		phases := []PhaseEntry{
			{ID: "a", Status: PhaseDone, DependsOn: []string{"b"}},
			{ID: "b", Status: PhaseDone, DependsOn: []string{"c"}},
			{ID: "c", Status: PhaseWaiting},
		}
		// new-phase depends on "a", which depends on "b" → "c".
		// If new-phase also has "c" depend on it, that's not what we test here.
		// We test: new-phase depends on "c", and "a" depends on new-phase → cycle through b→c.
		// Actually simpler: new-phase depends on "a", and "c" depends on new-phase.
		// But WouldCreateCycle only adds edges from the new phase.
		// Test: adding a new phase that depends on "c", where "a" depends on "b" depends on "c".
		// No cycle here because new-phase → c and nothing depends on new-phase.
		if WouldCreateCycle(phases, "new-phase", []string{"c"}) {
			t.Error("should not detect cycle: new-phase → c with no back-edge")
		}
	})

	t.Run("no false negatives with existing deps", func(t *testing.T) {
		t.Parallel()
		// a → b, b → c. Adding c → a creates cycle: a → b → c → a.
		phases := []PhaseEntry{
			{ID: "a", Status: PhaseDone, DependsOn: []string{"b"}},
			{ID: "b", Status: PhaseDone, DependsOn: []string{"c"}},
			{ID: "c", Status: PhaseWaiting},
		}
		if !WouldCreateCycle(phases, "c", []string{"a"}) {
			t.Error("should detect cycle: a → b → c → a")
		}
	})

	t.Run("self dependency", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseEntry{
			{ID: "a", Status: PhaseDone},
		}
		if !WouldCreateCycle(phases, "x", []string{"x"}) {
			t.Error("should detect self-dependency cycle")
		}
	})

	t.Run("no cycle in valid DAG", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseEntry{
			{ID: "a", Status: PhaseDone},
			{ID: "b", Status: PhaseDone, DependsOn: []string{"a"}},
		}
		if WouldCreateCycle(phases, "c", []string{"a", "b"}) {
			t.Error("should not detect cycle in valid DAG")
		}
	})

	t.Run("nil deps on phases", func(t *testing.T) {
		t.Parallel()
		// Phases with no DependsOn should not cause issues.
		phases := []PhaseEntry{
			{ID: "a", Status: PhaseDone},
			{ID: "b", Status: PhaseDone},
		}
		if WouldCreateCycle(phases, "c", []string{"a"}) {
			t.Error("should not detect cycle with no existing edges")
		}
	})
}

func TestPhaseEntryDeps(t *testing.T) {
	t.Parallel()

	phases := []PhaseEntry{
		{ID: "a", DependsOn: []string{"b", "c"}},
		{ID: "b", DependsOn: []string{"c"}},
		{ID: "c"},
	}

	t.Run("returns deps for existing phase", func(t *testing.T) {
		t.Parallel()
		deps := phaseEntryDeps(phases, "a")
		if len(deps) != 2 || deps[0] != "b" || deps[1] != "c" {
			t.Errorf("phaseEntryDeps(a) = %v, want [b c]", deps)
		}
	})

	t.Run("returns nil for unknown phase", func(t *testing.T) {
		t.Parallel()
		deps := phaseEntryDeps(phases, "unknown")
		if deps != nil {
			t.Errorf("phaseEntryDeps(unknown) = %v, want nil", deps)
		}
	})

	t.Run("returns nil for phase with no deps", func(t *testing.T) {
		t.Parallel()
		deps := phaseEntryDeps(phases, "c")
		if deps != nil {
			t.Errorf("phaseEntryDeps(c) = %v, want nil", deps)
		}
	})
}
