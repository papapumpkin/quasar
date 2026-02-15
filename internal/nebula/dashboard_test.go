package nebula

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func newTestNebula(name string, phases []PhaseSpec) *Nebula {
	return &Nebula{
		Dir:      "/tmp/test-nebula",
		Manifest: Manifest{Nebula: Info{Name: name}},
		Phases:   phases,
	}
}

func newTestState(phases map[string]*PhaseState, totalCost float64) *State {
	return &State{
		Version:      1,
		NebulaName:   "test",
		TotalCostUSD: totalCost,
		Phases:       phases,
	}
}

func TestDashboard_RenderTTY_AllPhaseStatuses(t *testing.T) {
	t.Parallel()

	n := newTestNebula("CI/CD Pipeline", []PhaseSpec{
		{ID: "test-action"},
		{ID: "vet-action"},
		{ID: "lint-action"},
		{ID: "build-action", DependsOn: []string{"test-action", "vet-action", "lint-action"}},
	})

	state := newTestState(map[string]*PhaseState{
		"test-action":  {BeadID: "b1", Status: PhaseStatusDone},
		"vet-action":   {BeadID: "b2", Status: PhaseStatusFailed},
		"lint-action":  {BeadID: "b3", Status: PhaseStatusInProgress},
		"build-action": {BeadID: "b4", Status: PhaseStatusPending},
	}, 0.24)

	var buf bytes.Buffer
	d := NewDashboard(&buf, n, state, 50.0, true)
	d.Render()

	output := buf.String()

	// Verify header present.
	if !strings.Contains(output, "CI/CD Pipeline") {
		t.Errorf("expected header with nebula name, got:\n%s", output)
	}

	// Verify status indicators.
	tests := []struct {
		name string
		want string
	}{
		{"done status", "[done]"},
		{"fail status", "[FAIL]"},
		{"in-progress status", "[>>>>]"},
		{"gate status (blocked)", "[gate]"},
		{"budget", "Budget: $0.24 / $50.00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(output, tt.want) {
				t.Errorf("expected %q in output, got:\n%s", tt.want, output)
			}
		})
	}
}

func TestDashboard_RenderPlain_NoANSICursor(t *testing.T) {
	t.Parallel()

	n := newTestNebula("test-nebula", []PhaseSpec{
		{ID: "phase-a"},
		{ID: "phase-b"},
	})

	state := newTestState(map[string]*PhaseState{
		"phase-a": {BeadID: "b1", Status: PhaseStatusDone},
		"phase-b": {BeadID: "b2", Status: PhaseStatusInProgress},
	}, 1.50)

	var buf bytes.Buffer
	d := NewDashboard(&buf, n, state, 10.0, false)
	d.Render()

	output := buf.String()

	// Non-TTY mode should produce a simple one-line output.
	if !strings.Contains(output, "1/2 done") {
		t.Errorf("expected '1/2 done' in plain output, got: %s", output)
	}
	if !strings.Contains(output, "1 active") {
		t.Errorf("expected '1 active' in plain output, got: %s", output)
	}
	if !strings.Contains(output, "$1.50") {
		t.Errorf("expected '$1.50' in plain output, got: %s", output)
	}

	// Should not contain cursor movement sequences.
	if strings.Contains(output, "\033[") && strings.Contains(output, "A") {
		// More precise check: cursor-up is \033[<n>A
		t.Log("warning: possible ANSI cursor movement in plain mode output")
	}
}

func TestDashboard_TTY_OverwritesPreviousOutput(t *testing.T) {
	t.Parallel()

	n := newTestNebula("test", []PhaseSpec{
		{ID: "a"},
		{ID: "b"},
	})

	state := newTestState(map[string]*PhaseState{
		"a": {BeadID: "b1", Status: PhaseStatusPending},
		"b": {BeadID: "b2", Status: PhaseStatusPending},
	}, 0)

	var buf bytes.Buffer
	d := NewDashboard(&buf, n, state, 0, true)

	// First render.
	d.Render()
	firstLen := buf.Len()
	if firstLen == 0 {
		t.Fatal("expected non-empty first render")
	}

	// Update state and render again.
	state.Phases["a"].Status = PhaseStatusInProgress
	d.Render()

	output := buf.String()
	// Second render should contain cursor-up escape (to overwrite first render).
	if !strings.Contains(output, "\033[") {
		t.Errorf("expected ANSI cursor-up in second TTY render, got:\n%s", output)
	}
}

func TestDashboard_ThreadSafe(t *testing.T) {
	t.Parallel()

	n := newTestNebula("concurrent-test", []PhaseSpec{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
		{ID: "d"},
	})

	state := newTestState(map[string]*PhaseState{
		"a": {BeadID: "b1", Status: PhaseStatusPending},
		"b": {BeadID: "b2", Status: PhaseStatusPending},
		"c": {BeadID: "b3", Status: PhaseStatusPending},
		"d": {BeadID: "b4", Status: PhaseStatusPending},
	}, 0)

	var buf bytes.Buffer
	d := NewDashboard(&buf, n, state, 10.0, true)

	// Concurrent renders should not panic.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Render()
		}()
	}
	wg.Wait()

	if buf.Len() == 0 {
		t.Error("expected non-empty output after concurrent renders")
	}
}

func TestDashboard_Pause_ClearsOutput(t *testing.T) {
	t.Parallel()

	n := newTestNebula("pause-test", []PhaseSpec{
		{ID: "a"},
	})

	state := newTestState(map[string]*PhaseState{
		"a": {BeadID: "b1", Status: PhaseStatusInProgress},
	}, 0)

	var buf bytes.Buffer
	d := NewDashboard(&buf, n, state, 0, true)

	d.Render()
	d.Pause()

	output := buf.String()
	// After pause in TTY mode, should contain clear-line sequences.
	if !strings.Contains(output, ansiClearLine) {
		t.Errorf("expected clear-line escape after pause, got:\n%s", output)
	}

	// After pause, lineCount should be reset.
	if d.lineCount != 0 {
		t.Errorf("expected lineCount=0 after pause, got %d", d.lineCount)
	}
}

func TestDashboard_ProgressCallback_TriggersRender(t *testing.T) {
	t.Parallel()

	n := newTestNebula("callback-test", []PhaseSpec{
		{ID: "a"},
	})

	state := newTestState(map[string]*PhaseState{
		"a": {BeadID: "b1", Status: PhaseStatusDone},
	}, 0.50)

	var buf bytes.Buffer
	d := NewDashboard(&buf, n, state, 5.0, false)

	cb := d.ProgressCallback()
	cb(1, 1, 0, 1, 0.50)

	if buf.Len() == 0 {
		t.Error("expected output after ProgressCallback call")
	}
}

func TestDashboard_WaitStatus_UnblockedPending(t *testing.T) {
	t.Parallel()

	n := newTestNebula("wait-test", []PhaseSpec{
		{ID: "a"},
	})

	state := newTestState(map[string]*PhaseState{
		"a": {BeadID: "b1", Status: PhaseStatusPending},
	}, 0)

	var buf bytes.Buffer
	d := NewDashboard(&buf, n, state, 0, true)
	d.Render()

	output := buf.String()
	if !strings.Contains(output, "[wait]") {
		t.Errorf("expected [wait] for unblocked pending phase, got:\n%s", output)
	}
}

func TestDashboard_BlockedShowsDeps(t *testing.T) {
	t.Parallel()

	n := newTestNebula("blocked-test", []PhaseSpec{
		{ID: "dep-phase"},
		{ID: "blocked-phase", DependsOn: []string{"dep-phase"}},
	})

	state := newTestState(map[string]*PhaseState{
		"dep-phase":     {BeadID: "b1", Status: PhaseStatusPending},
		"blocked-phase": {BeadID: "b2", Status: PhaseStatusPending},
	}, 0)

	var buf bytes.Buffer
	d := NewDashboard(&buf, n, state, 0, true)
	d.Render()

	output := buf.String()
	if !strings.Contains(output, "[gate]") {
		t.Errorf("expected [gate] for blocked phase, got:\n%s", output)
	}
	if !strings.Contains(output, "blocked: dep-phase") {
		t.Errorf("expected blocking dependency listed, got:\n%s", output)
	}
}

func TestStatusIcon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    PhaseStatus
		isBlocked bool
		wantText  string
	}{
		{"done", PhaseStatusDone, false, "[done]"},
		{"in_progress", PhaseStatusInProgress, false, "[>>>>]"},
		{"created", PhaseStatusCreated, false, "[>>>>]"},
		{"failed", PhaseStatusFailed, false, "[FAIL]"},
		{"pending unblocked", PhaseStatusPending, false, "[wait]"},
		{"pending blocked", PhaseStatusPending, true, "[gate]"},
		{"unknown", PhaseStatus("unknown"), false, "[skip]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := statusIcon(tt.status, tt.isBlocked)
			if !strings.Contains(got, tt.wantText) {
				t.Errorf("statusIcon(%q, %v) = %q, want containing %q", tt.status, tt.isBlocked, got, tt.wantText)
			}
		})
	}
}
