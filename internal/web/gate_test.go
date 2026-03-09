package web

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestWebGater_EnqueueAndResolve(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	req := &GateRequest{
		PhaseID:    "test-phase",
		PhaseTitle: "Test Phase",
		Options: []GateOption{
			{Label: "Accept", Action: "accept"},
			{Label: "Reject", Action: "reject"},
		},
		ResponseCh: make(chan nebula.GateAction, 1),
		CreatedAt:  time.Now(),
	}

	gater.Enqueue(req)

	// Verify it appears in pending.
	pending := gater.Pending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].PhaseID != "test-phase" {
		t.Errorf("expected phase ID test-phase, got %s", pending[0].PhaseID)
	}

	// Resolve it.
	if err := gater.Resolve("test-phase", "accept"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Verify response channel received the action.
	select {
	case action := <-req.ResponseCh:
		if action != nebula.GateActionAccept {
			t.Errorf("expected accept, got %s", action)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Verify pending is now empty.
	if len(gater.Pending()) != 0 {
		t.Error("expected 0 pending after resolve")
	}
}

func TestWebGater_DoubleResolveError(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	req := &GateRequest{
		PhaseID:    "double-phase",
		ResponseCh: make(chan nebula.GateAction, 1),
		CreatedAt:  time.Now(),
	}

	gater.Enqueue(req)

	if err := gater.Resolve("double-phase", "accept"); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}

	// Second resolve should fail.
	if err := gater.Resolve("double-phase", "accept"); err == nil {
		t.Error("expected error on double resolve, got nil")
	}
}

func TestWebGater_ResolveUnknownPhase(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	if err := gater.Resolve("nonexistent", "accept"); err == nil {
		t.Error("expected error resolving unknown phase, got nil")
	}
}

func TestWebGater_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	const numPhases = 20
	var wg sync.WaitGroup

	// Enqueue phases concurrently.
	for i := 0; i < numPhases; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := &GateRequest{
				PhaseID:    fmt.Sprintf("phase-%d", id),
				ResponseCh: make(chan nebula.GateAction, 1),
				CreatedAt:  time.Now(),
			}
			gater.Enqueue(req)
		}(i)
	}
	wg.Wait()

	pending := gater.Pending()
	if len(pending) != numPhases {
		t.Errorf("expected %d pending, got %d", numPhases, len(pending))
	}

	// Resolve all concurrently.
	for i := 0; i < numPhases; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := gater.Resolve(fmt.Sprintf("phase-%d", id), "accept"); err != nil {
				t.Errorf("Resolve phase-%d: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	if len(gater.Pending()) != 0 {
		t.Error("expected 0 pending after resolving all")
	}
}

func TestWebGater_MultiplePendingIndependent(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	reqA := &GateRequest{
		PhaseID:    "phase-a",
		PhaseTitle: "Phase A",
		ResponseCh: make(chan nebula.GateAction, 1),
		CreatedAt:  time.Now(),
	}
	reqB := &GateRequest{
		PhaseID:    "phase-b",
		PhaseTitle: "Phase B",
		ResponseCh: make(chan nebula.GateAction, 1),
		CreatedAt:  time.Now(),
	}

	gater.Enqueue(reqA)
	gater.Enqueue(reqB)

	if len(gater.Pending()) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(gater.Pending()))
	}

	// Resolve B first, A should remain.
	if err := gater.Resolve("phase-b", "reject"); err != nil {
		t.Fatalf("Resolve phase-b: %v", err)
	}

	pending := gater.Pending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].PhaseID != "phase-a" {
		t.Errorf("expected remaining phase to be phase-a, got %s", pending[0].PhaseID)
	}

	// Verify B's response channel.
	select {
	case action := <-reqB.ResponseCh:
		if action != nebula.GateActionReject {
			t.Errorf("expected reject, got %s", action)
		}
	default:
		t.Error("expected response on phase-b channel")
	}
}

func TestWebGater_Prompt(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	cp := &nebula.Checkpoint{
		PhaseID:      "prompt-phase",
		PhaseTitle:   "Prompt Test",
		Satisfaction: "high",
		Risk:         "low",
		ReviewCycles: 2,
		CostUSD:      0.1234,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Resolve in a goroutine since Prompt blocks.
	go func() {
		// Wait for the gate to appear in pending.
		for {
			if len(gater.Pending()) > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if err := gater.Resolve("prompt-phase", "accept"); err != nil {
			t.Errorf("Resolve: %v", err)
		}
	}()

	action, err := gater.Prompt(ctx, cp)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if action != nebula.GateActionAccept {
		t.Errorf("expected accept, got %s", action)
	}
}

func TestWebGater_PromptCancellation(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	cp := &nebula.Checkpoint{
		PhaseID:    "cancel-phase",
		PhaseTitle: "Cancel Test",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after the prompt is enqueued.
	go func() {
		for {
			if len(gater.Pending()) > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()

	action, err := gater.Prompt(ctx, cp)
	if err == nil {
		t.Error("expected context error, got nil")
	}
	if action != nebula.GateActionSkip {
		t.Errorf("expected skip on cancellation, got %s", action)
	}

	// Pending should be cleaned up.
	if len(gater.Pending()) != 0 {
		t.Error("expected 0 pending after cancellation cleanup")
	}
}

func TestBuildGateRequest(t *testing.T) {
	t.Parallel()

	t.Run("nil checkpoint", func(t *testing.T) {
		t.Parallel()
		req := buildGateRequest(nil)
		if req.PhaseID != "unknown" {
			t.Errorf("expected unknown phase ID, got %s", req.PhaseID)
		}
		if len(req.Options) != 4 {
			t.Errorf("expected 4 options for non-plan, got %d", len(req.Options))
		}
	})

	t.Run("plan checkpoint", func(t *testing.T) {
		t.Parallel()
		cp := &nebula.Checkpoint{
			PhaseID: nebula.PlanPhaseID,
		}
		req := buildGateRequest(cp)
		if len(req.Options) != 2 {
			t.Errorf("expected 2 options for plan, got %d", len(req.Options))
		}
	})

	t.Run("regular checkpoint", func(t *testing.T) {
		t.Parallel()
		cp := &nebula.Checkpoint{
			PhaseID:       "test-phase",
			PhaseTitle:    "Test",
			Satisfaction:  "high",
			Risk:          "low",
			ReviewSummary: "Looks good",
			ReviewCycles:  3,
			CostUSD:       1.5,
			FilesChanged: []nebula.FileChange{
				{Path: "foo.go", Operation: "modified"},
				{Path: "bar.go", Operation: "added"},
			},
		}
		req := buildGateRequest(cp)
		if req.PhaseID != "test-phase" {
			t.Errorf("expected test-phase, got %s", req.PhaseID)
		}
		if req.Satisfaction != "high" {
			t.Errorf("expected high satisfaction, got %s", req.Satisfaction)
		}
		if len(req.FilesChanged) != 2 {
			t.Errorf("expected 2 files changed, got %d", len(req.FilesChanged))
		}
		if len(req.Options) != 4 {
			t.Errorf("expected 4 options, got %d", len(req.Options))
		}
	})
}
