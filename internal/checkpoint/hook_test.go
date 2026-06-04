package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/loop"
)

// makeState returns a sample CycleState for testing.
func makeState() *loop.CycleState {
	return &loop.CycleState{
		TaskID:        "bead-hook-test",
		TaskTitle:     "Test hook checkpoint",
		Phase:         loop.PhaseReviewing,
		Cycle:         2,
		MaxCycles:     5,
		TotalCostUSD:  1.5,
		MaxBudgetUSD:  10.0,
		CoderOutput:   "some code",
		ReviewOutput:  "some review",
		LintOutput:    "no lint issues",
		BaseCommitSHA: "abc123",
		CycleCommits:  []string{"commit-1"},
		FilterHistory: []string{"check-build"},
		Findings: []loop.ReviewFinding{
			{ID: "f1", Severity: "high", Description: "missing error handling", Cycle: 2, Status: loop.FindingStatusFound},
		},
		AllFindings: []loop.ReviewFinding{
			{ID: "f1", Severity: "high", Description: "missing error handling", Cycle: 2, Status: loop.FindingStatusFound},
		},
	}
}

func TestCheckpointHookWritesOnReviewComplete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "test-phase",
		NebulaName: "test-nebula",
		GitSHAFunc: func() string { return "sha-review-complete" },
		StateFunc:  func() *loop.CycleState { return state },
	}

	hook.OnEvent(context.Background(), loop.Event{
		Kind:  loop.EventReviewComplete,
		Cycle: 2,
	})

	cp := loadCheckpoint(t, dir, "test-phase")
	if cp.GitSHA != "sha-review-complete" {
		t.Errorf("GitSHA = %q, want %q", cp.GitSHA, "sha-review-complete")
	}
	if cp.PhaseID != "test-phase" {
		t.Errorf("PhaseID = %q, want %q", cp.PhaseID, "test-phase")
	}
	if cp.NebulaName != "test-nebula" {
		t.Errorf("NebulaName = %q, want %q", cp.NebulaName, "test-nebula")
	}
	if cp.Cycle != 2 {
		t.Errorf("Cycle = %d, want 2", cp.Cycle)
	}
	if cp.TaskID != "bead-hook-test" {
		t.Errorf("TaskID = %q, want %q", cp.TaskID, "bead-hook-test")
	}
}

func TestCheckpointHookWritesOnTaskSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "success-phase",
		NebulaName: "success-nebula",
		GitSHAFunc: func() string { return "sha-success" },
		StateFunc:  func() *loop.CycleState { return state },
	}

	hook.OnEvent(context.Background(), loop.Event{
		Kind: loop.EventTaskSuccess,
	})

	cp := loadCheckpoint(t, dir, "success-phase")
	if cp.GitSHA != "sha-success" {
		t.Errorf("GitSHA = %q, want %q", cp.GitSHA, "sha-success")
	}
}

func TestCheckpointHookWritesOnTaskFailed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "failed-phase",
		NebulaName: "failed-nebula",
		GitSHAFunc: func() string { return "sha-failed" },
		StateFunc:  func() *loop.CycleState { return state },
	}

	hook.OnEvent(context.Background(), loop.Event{
		Kind:    loop.EventTaskFailed,
		Message: "max cycles reached",
	})

	cp := loadCheckpoint(t, dir, "failed-phase")
	if cp.GitSHA != "sha-failed" {
		t.Errorf("GitSHA = %q, want %q", cp.GitSHA, "sha-failed")
	}
}

func TestCheckpointHookSkipsCycleStart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "skip-phase",
		NebulaName: "skip-nebula",
		GitSHAFunc: func() string { return "sha-skip" },
		StateFunc:  func() *loop.CycleState { return state },
	}

	hook.OnEvent(context.Background(), loop.Event{
		Kind:  loop.EventCycleStart,
		Cycle: 1,
	})

	path := Path(dir, "skip-phase")
	if _, err := os.Stat(path); err == nil {
		t.Error("checkpoint should not be written on EventCycleStart")
	}
}

func TestCheckpointHookSkipsAgentDone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "agent-done-phase",
		NebulaName: "agent-done-nebula",
		GitSHAFunc: func() string { return "sha-agent-done" },
		StateFunc:  func() *loop.CycleState { return state },
	}

	hook.OnEvent(context.Background(), loop.Event{
		Kind:  loop.EventAgentDone,
		Agent: "coder",
	})

	path := Path(dir, "agent-done-phase")
	if _, err := os.Stat(path); err == nil {
		t.Error("checkpoint should not be written on EventAgentDone")
	}
}

func TestCheckpointHookStandaloneNoPhaseID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "",
		NebulaName: "",
		GitSHAFunc: func() string { return "sha-standalone" },
		StateFunc:  func() *loop.CycleState { return state },
	}

	hook.OnEvent(context.Background(), loop.Event{
		Kind: loop.EventTaskSuccess,
	})

	// Standalone checkpoint has no phase ID → checkpoint.toml
	path := Path(dir, "")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected checkpoint.toml to exist: %v", err)
	}

	cp := loadCheckpoint(t, dir, "")
	if cp.GitSHA != "sha-standalone" {
		t.Errorf("GitSHA = %q, want %q", cp.GitSHA, "sha-standalone")
	}
}

func TestCheckpointHookNilGitSHAFunc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "nil-sha",
		NebulaName: "nil-sha-nebula",
		GitSHAFunc: nil, // no SHA provider
		StateFunc:  func() *loop.CycleState { return state },
	}

	hook.OnEvent(context.Background(), loop.Event{
		Kind: loop.EventReviewComplete,
	})

	cp := loadCheckpoint(t, dir, "nil-sha")
	if cp.GitSHA != "" {
		t.Errorf("GitSHA = %q, want empty when GitSHAFunc is nil", cp.GitSHA)
	}
}

func TestCheckpointHookNilStateFuncLogsAndSkips(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "nil-statefunc",
		NebulaName: "nil-statefunc-nebula",
		GitSHAFunc: func() string { return "sha-nil" },
		StateFunc:  nil, // no state provider
	}

	// Should not panic; logs to stderr and skips.
	hook.OnEvent(context.Background(), loop.Event{
		Kind: loop.EventTaskSuccess,
	})

	path := Path(dir, "nil-statefunc")
	if _, err := os.Stat(path); err == nil {
		t.Error("checkpoint should not be written when StateFunc is nil")
	}
}

func TestCheckpointHookNilStateLogsAndSkips(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	hook := &Hook{
		Dir:        dir,
		PhaseID:    "nil-state",
		NebulaName: "nil-state-nebula",
		GitSHAFunc: func() string { return "sha-nil" },
		StateFunc:  func() *loop.CycleState { return nil },
	}

	// Should not panic; logs to stderr and skips.
	hook.OnEvent(context.Background(), loop.Event{
		Kind: loop.EventTaskSuccess,
	})

	path := Path(dir, "nil-state")
	if _, err := os.Stat(path); err == nil {
		t.Error("checkpoint should not be written when StateFunc returns nil")
	}
}

// loadCheckpoint is a test helper that loads and parses a checkpoint file.
func loadCheckpoint(t *testing.T, dir, phaseID string) *Checkpoint {
	t.Helper()

	path := Path(dir, phaseID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading checkpoint: %v", err)
	}

	var cp Checkpoint
	if err := toml.Unmarshal(data, &cp); err != nil {
		t.Fatalf("parsing checkpoint: %v", err)
	}
	return &cp
}

func TestSaveAtomicWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	cp := FromCycleState(state, "atomic-phase", "atomic-nebula", "sha-atomic")

	if err := Save(dir, cp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify the file exists and no .tmp file remains.
	path := Path(dir, "atomic-phase")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("checkpoint file should exist: %v", err)
	}
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file should not remain after successful save")
	}

	// Verify the file is valid TOML that round-trips.
	loaded := loadCheckpoint(t, dir, "atomic-phase")
	if loaded.Version != Version {
		t.Errorf("Version = %d, want %d", loaded.Version, Version)
	}
	if loaded.PhaseID != "atomic-phase" {
		t.Errorf("PhaseID = %q, want %q", loaded.PhaseID, "atomic-phase")
	}
	if loaded.GitSHA != "sha-atomic" {
		t.Errorf("GitSHA = %q, want %q", loaded.GitSHA, "sha-atomic")
	}

	// Round-trip CycleState through Save + Load.
	got := loaded.ToCycleState()
	assertCycleStateEqual(t, state, got)
}

func TestSaveOverwritesExistingCheckpoint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := makeState()

	cp1 := FromCycleState(state, "overwrite-phase", "nebula", "sha-1")
	if err := Save(dir, cp1); err != nil {
		t.Fatalf("Save (first): %v", err)
	}

	// Update state and save again.
	state.Cycle = 3
	state.TotalCostUSD = 5.0
	cp2 := FromCycleState(state, "overwrite-phase", "nebula", "sha-2")
	if err := Save(dir, cp2); err != nil {
		t.Fatalf("Save (second): %v", err)
	}

	loaded := loadCheckpoint(t, dir, "overwrite-phase")
	if loaded.Cycle != 3 {
		t.Errorf("Cycle = %d, want 3", loaded.Cycle)
	}
	if loaded.GitSHA != "sha-2" {
		t.Errorf("GitSHA = %q, want %q", loaded.GitSHA, "sha-2")
	}
}

func TestCheckpointPathWithPhaseID(t *testing.T) {
	t.Parallel()

	got := Path("/tmp/dir", "my-phase")
	want := filepath.Join("/tmp/dir", "checkpoint.my-phase.toml")
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestCheckpointPathWithoutPhaseID(t *testing.T) {
	t.Parallel()

	got := Path("/tmp/dir", "")
	want := filepath.Join("/tmp/dir", "checkpoint.toml")
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}
