package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
)

// resumeMockRunner tracks whether RunFromCheckpoint or RunExistingPhase was called.
type resumeMockRunner struct {
	mu                 sync.Mutex
	existingPhaseCalls []string // phase IDs dispatched via RunExistingPhase
	resumeCalls        []string // phase IDs dispatched via RunFromCheckpoint
	result             *PhaseRunnerResult
	err                error
}

func (m *resumeMockRunner) RunExistingPhase(_ context.Context, phaseID, _, _, _ string, _ ResolvedExecution) (*PhaseRunnerResult, error) {
	m.mu.Lock()
	m.existingPhaseCalls = append(m.existingPhaseCalls, phaseID)
	m.mu.Unlock()
	return m.result, m.err
}

func (m *resumeMockRunner) GenerateCheckpoint(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (m *resumeMockRunner) RunFromCheckpoint(_ context.Context, _ any, phaseID, _, _, _ string, _ ResolvedExecution) (*PhaseRunnerResult, error) {
	m.mu.Lock()
	m.resumeCalls = append(m.resumeCalls, phaseID)
	m.mu.Unlock()
	return m.result, m.err
}

func (m *resumeMockRunner) getResumeCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.resumeCalls))
	copy(cp, m.resumeCalls)
	return cp
}

func (m *resumeMockRunner) getExistingCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.existingPhaseCalls))
	copy(cp, m.existingPhaseCalls)
	return cp
}

func TestTryLoadCheckpoint(t *testing.T) {
	t.Parallel()

	t.Run("ResumeDisabled_ReturnsNil", func(t *testing.T) {
		t.Parallel()
		wg := &WorkerGroup{
			ResumeEnabled: false,
			CheckpointDir: "/tmp/checkpoints",
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil when resume is disabled, got %v", result)
		}
	})

	t.Run("EmptyCheckpointDir_ReturnsNil", func(t *testing.T) {
		t.Parallel()
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "",
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil when checkpoint dir is empty, got %v", result)
		}
	})

	t.Run("NoCheckpointLoader_ReturnsNil", func(t *testing.T) {
		t.Parallel()
		wg := &WorkerGroup{
			ResumeEnabled:    true,
			CheckpointDir:    "/tmp/checkpoints",
			CheckpointLoader: nil,
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil when no loader set, got %v", result)
		}
	})

	t.Run("NoCheckpointFound_ReturnsNil", func(t *testing.T) {
		t.Parallel()
		state := &State{Phases: map[string]*PhaseState{
			"phase-1": {Status: PhaseStatusInProgress, BeadID: "bead-1"},
		}}
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "/tmp/checkpoints",
			State:         state,
			CheckpointLoader: func(dir, phaseID string) (any, error) {
				return nil, nil // no checkpoint
			},
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil when no checkpoint found, got %v", result)
		}
	})

	t.Run("CheckpointFound_ReturnsData", func(t *testing.T) {
		t.Parallel()
		sentinel := "mock-checkpoint-data"
		state := &State{Phases: map[string]*PhaseState{
			"phase-1": {Status: PhaseStatusInProgress, BeadID: "bead-1"},
		}}
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "/tmp/checkpoints",
			State:         state,
			CheckpointLoader: func(dir, phaseID string) (any, error) {
				return sentinel, nil
			},
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != sentinel {
			t.Fatalf("expected sentinel checkpoint, got %v", result)
		}
	})

	t.Run("CheckpointLoadError_ReturnsNil", func(t *testing.T) {
		t.Parallel()
		state := &State{Phases: map[string]*PhaseState{
			"phase-1": {Status: PhaseStatusInProgress, BeadID: "bead-1"},
		}}
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "/tmp/checkpoints",
			State:         state,
			CheckpointLoader: func(dir, phaseID string) (any, error) {
				return nil, errors.New("disk read failed")
			},
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil on load error, got %v", result)
		}
	})

	t.Run("StaleCheckpoint_DonePhase_Removed", func(t *testing.T) {
		t.Parallel()
		var removedPhase string
		state := &State{Phases: map[string]*PhaseState{
			"phase-1": {Status: PhaseStatusDone, BeadID: "bead-1"},
		}}
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "/tmp/checkpoints",
			State:         state,
			CheckpointLoader: func(dir, phaseID string) (any, error) {
				return "should-not-be-used", nil
			},
			CheckpointRemover: func(dir, phaseID string) error {
				removedPhase = phaseID
				return nil
			},
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil for done phase, got %v", result)
		}
		if removedPhase != "phase-1" {
			t.Fatalf("expected checkpoint for phase-1 to be removed, got %q", removedPhase)
		}
	})

	t.Run("StaleCheckpoint_FailedPhase_Removed", func(t *testing.T) {
		t.Parallel()
		var removedPhase string
		state := &State{Phases: map[string]*PhaseState{
			"phase-1": {Status: PhaseStatusFailed, BeadID: "bead-1"},
		}}
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "/tmp/checkpoints",
			State:         state,
			CheckpointLoader: func(dir, phaseID string) (any, error) {
				return "should-not-be-used", nil
			},
			CheckpointRemover: func(dir, phaseID string) error {
				removedPhase = phaseID
				return nil
			},
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil for failed phase, got %v", result)
		}
		if removedPhase != "phase-1" {
			t.Fatalf("expected checkpoint for phase-1 to be removed, got %q", removedPhase)
		}
	})

	t.Run("ValidationFailure_FallsBackToFresh", func(t *testing.T) {
		t.Parallel()
		var removedPhase string
		state := &State{Phases: map[string]*PhaseState{
			"phase-1": {Status: PhaseStatusInProgress, BeadID: "bead-1"},
		}}
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "/tmp/checkpoints",
			State:         state,
			CheckpointLoader: func(dir, phaseID string) (any, error) {
				return "checkpoint-data", nil
			},
			CheckpointValidator: func(cp any, gitSHA string) error {
				return fmt.Errorf("git SHA mismatch: checkpoint=abc, current=%s", gitSHA)
			},
			CheckpointRemover: func(dir, phaseID string) error {
				removedPhase = phaseID
				return nil
			},
			GitSHAFunc: func(ctx context.Context) (string, error) {
				return "def456", nil
			},
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil on validation failure, got %v", result)
		}
		if removedPhase != "phase-1" {
			t.Fatalf("expected invalid checkpoint to be removed, got %q", removedPhase)
		}
	})

	t.Run("GitSHAError_FallsBackToFresh", func(t *testing.T) {
		t.Parallel()
		var removedPhase string
		state := &State{Phases: map[string]*PhaseState{
			"phase-1": {Status: PhaseStatusInProgress, BeadID: "bead-1"},
		}}
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "/tmp/checkpoints",
			State:         state,
			CheckpointLoader: func(dir, phaseID string) (any, error) {
				return "checkpoint-data", nil
			},
			CheckpointValidator: func(cp any, gitSHA string) error {
				return nil
			},
			CheckpointRemover: func(dir, phaseID string) error {
				removedPhase = phaseID
				return nil
			},
			GitSHAFunc: func(ctx context.Context) (string, error) {
				return "", errors.New("not a git repo")
			},
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != nil {
			t.Fatalf("expected nil on git SHA error, got %v", result)
		}
		if removedPhase != "phase-1" {
			t.Fatalf("expected checkpoint to be removed on SHA error, got %q", removedPhase)
		}
	})

	t.Run("ValidCheckpoint_WithValidation_ReturnsData", func(t *testing.T) {
		t.Parallel()
		sentinel := "valid-checkpoint"
		state := &State{Phases: map[string]*PhaseState{
			"phase-1": {Status: PhaseStatusInProgress, BeadID: "bead-1"},
		}}
		wg := &WorkerGroup{
			ResumeEnabled: true,
			CheckpointDir: "/tmp/checkpoints",
			State:         state,
			CheckpointLoader: func(dir, phaseID string) (any, error) {
				return sentinel, nil
			},
			CheckpointValidator: func(cp any, gitSHA string) error {
				return nil // validation passes
			},
			GitSHAFunc: func(ctx context.Context) (string, error) {
				return "abc123", nil
			},
		}
		result := wg.tryLoadCheckpoint(context.Background(), "phase-1")
		if result != sentinel {
			t.Fatalf("expected valid checkpoint, got %v", result)
		}
	})
}

// TestExecutePhase_ResumeRouting verifies that executePhase routes through
// RunFromCheckpoint when a valid checkpoint is found, and through
// RunExistingPhase when resume is disabled or no checkpoint exists.
func TestExecutePhase_ResumeRouting(t *testing.T) {
	t.Parallel()

	makeNebula := func() *Nebula {
		return &Nebula{
			Dir:      t.TempDir(),
			Manifest: Manifest{Nebula: Info{Name: "test"}},
			Phases:   []PhaseSpec{{ID: "phase-1", Title: "Test Phase"}},
		}
	}

	t.Run("ResumeEnabled_ValidCheckpoint_CallsRunFromCheckpoint", func(t *testing.T) {
		t.Parallel()
		n := makeNebula()
		state := &State{
			Version: 1,
			Phases: map[string]*PhaseState{
				"phase-1": {BeadID: "bead-1", Status: PhaseStatusInProgress},
			},
		}
		runner := &resumeMockRunner{
			result: &PhaseRunnerResult{TotalCostUSD: 0.01},
		}
		wg := NewWorkerGroup(n, state,
			WithRunner(runner),
			WithMaxWorkers(1),
			WithLogger(io.Discard),
			WithResumeEnabled(true),
			WithCheckpointDir("/tmp/test-cp"),
			WithCheckpointLoader(func(dir, phaseID string) (any, error) {
				return "mock-checkpoint", nil
			}),
		)
		wg.ensureGater()
		wg.tracker = NewPhaseTracker(n.Phases, state)
		wg.progress = NewProgressReporter(n, state, nil, nil, io.Discard)

		wg.executePhase(context.Background(), "phase-1", 0)

		resumeCalls := runner.getResumeCalls()
		existingCalls := runner.getExistingCalls()
		if len(resumeCalls) != 1 || resumeCalls[0] != "phase-1" {
			t.Fatalf("expected RunFromCheckpoint called for phase-1, got resume=%v existing=%v", resumeCalls, existingCalls)
		}
		if len(existingCalls) != 0 {
			t.Fatalf("expected no RunExistingPhase calls, got %v", existingCalls)
		}
	})

	t.Run("ResumeEnabled_NoCheckpoint_CallsRunExistingPhase", func(t *testing.T) {
		t.Parallel()
		n := makeNebula()
		state := &State{
			Version: 1,
			Phases: map[string]*PhaseState{
				"phase-1": {BeadID: "bead-1", Status: PhaseStatusInProgress},
			},
		}
		runner := &resumeMockRunner{
			result: &PhaseRunnerResult{TotalCostUSD: 0.01},
		}
		wg := NewWorkerGroup(n, state,
			WithRunner(runner),
			WithMaxWorkers(1),
			WithLogger(io.Discard),
			WithResumeEnabled(true),
			WithCheckpointDir("/tmp/test-cp"),
			WithCheckpointLoader(func(dir, phaseID string) (any, error) {
				return nil, nil // no checkpoint found
			}),
		)
		wg.ensureGater()
		wg.tracker = NewPhaseTracker(n.Phases, state)
		wg.progress = NewProgressReporter(n, state, nil, nil, io.Discard)

		wg.executePhase(context.Background(), "phase-1", 0)

		resumeCalls := runner.getResumeCalls()
		existingCalls := runner.getExistingCalls()
		if len(existingCalls) != 1 || existingCalls[0] != "phase-1" {
			t.Fatalf("expected RunExistingPhase called for phase-1, got existing=%v resume=%v", existingCalls, resumeCalls)
		}
		if len(resumeCalls) != 0 {
			t.Fatalf("expected no RunFromCheckpoint calls, got %v", resumeCalls)
		}
	})

	t.Run("ResumeDisabled_SkipsCheckpoint_CallsRunExistingPhase", func(t *testing.T) {
		t.Parallel()
		n := makeNebula()
		state := &State{
			Version: 1,
			Phases: map[string]*PhaseState{
				"phase-1": {BeadID: "bead-1", Status: PhaseStatusInProgress},
			},
		}
		runner := &resumeMockRunner{
			result: &PhaseRunnerResult{TotalCostUSD: 0.01},
		}
		wg := NewWorkerGroup(n, state,
			WithRunner(runner),
			WithMaxWorkers(1),
			WithLogger(io.Discard),
			// ResumeEnabled is false — even if loader is set, no checkpoint should load.
			WithCheckpointDir("/tmp/test-cp"),
			WithCheckpointLoader(func(dir, phaseID string) (any, error) {
				return "should-not-be-used", nil
			}),
		)
		wg.ensureGater()
		wg.tracker = NewPhaseTracker(n.Phases, state)
		wg.progress = NewProgressReporter(n, state, nil, nil, io.Discard)

		wg.executePhase(context.Background(), "phase-1", 0)

		resumeCalls := runner.getResumeCalls()
		existingCalls := runner.getExistingCalls()
		if len(existingCalls) != 1 {
			t.Fatalf("expected RunExistingPhase called, got existing=%v resume=%v", existingCalls, resumeCalls)
		}
		if len(resumeCalls) != 0 {
			t.Fatalf("expected no RunFromCheckpoint calls when resume disabled, got %v", resumeCalls)
		}
	})

	t.Run("ResumeEnabled_ValidationFails_FallsBackToFresh", func(t *testing.T) {
		t.Parallel()
		n := makeNebula()
		var removedPhase string
		state := &State{
			Version: 1,
			Phases: map[string]*PhaseState{
				"phase-1": {BeadID: "bead-1", Status: PhaseStatusInProgress},
			},
		}
		runner := &resumeMockRunner{
			result: &PhaseRunnerResult{TotalCostUSD: 0.01},
		}
		wg := NewWorkerGroup(n, state,
			WithRunner(runner),
			WithMaxWorkers(1),
			WithLogger(io.Discard),
			WithResumeEnabled(true),
			WithCheckpointDir("/tmp/test-cp"),
			WithCheckpointLoader(func(dir, phaseID string) (any, error) {
				return "stale-checkpoint", nil
			}),
			WithCheckpointValidator(func(cp any, gitSHA string) error {
				return fmt.Errorf("SHA mismatch")
			}),
			WithCheckpointRemover(func(dir, phaseID string) error {
				removedPhase = phaseID
				return nil
			}),
			WithGitSHAFunc(func(ctx context.Context) (string, error) {
				return "abc123", nil
			}),
		)
		wg.ensureGater()
		wg.tracker = NewPhaseTracker(n.Phases, state)
		wg.progress = NewProgressReporter(n, state, nil, nil, io.Discard)

		wg.executePhase(context.Background(), "phase-1", 0)

		resumeCalls := runner.getResumeCalls()
		existingCalls := runner.getExistingCalls()
		if len(existingCalls) != 1 {
			t.Fatalf("expected RunExistingPhase called after validation failure, got existing=%v resume=%v", existingCalls, resumeCalls)
		}
		if len(resumeCalls) != 0 {
			t.Fatalf("expected no RunFromCheckpoint calls after validation failure, got %v", resumeCalls)
		}
		if removedPhase != "phase-1" {
			t.Fatalf("expected stale checkpoint to be removed, got %q", removedPhase)
		}
	})
}
