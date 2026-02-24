package nebula

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/tycho"
)

// --- helpers for integration tests ---

// testSQLiteFabricForNebula creates a real SQLite-backed fabric in a temp dir.
func testSQLiteFabricForNebula(t *testing.T) fabric.Fabric {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "integration.fabric.db")
	f, err := fabric.NewSQLiteFabric(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteFabric(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// integrationPoller is a Poller with canned per-phase responses and
// thread-safe mutation support.
type integrationPoller struct {
	mu        sync.Mutex
	decisions map[string]fabric.PollResult
	pollCount map[string]int
}

func newIntegrationPoller() *integrationPoller {
	return &integrationPoller{
		decisions: make(map[string]fabric.PollResult),
		pollCount: make(map[string]int),
	}
}

func (p *integrationPoller) setDecision(phaseID string, r fabric.PollResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decisions[phaseID] = r
}

func (p *integrationPoller) Poll(_ context.Context, phaseID string, _ fabric.Snapshot) (fabric.PollResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pollCount[phaseID]++
	if r, ok := p.decisions[phaseID]; ok {
		return r, nil
	}
	return fabric.PollResult{Decision: fabric.PollProceed}, nil
}

// newIntegrationWorkerGroup creates a WorkerGroup backed by a real SQLiteFabric.
func newIntegrationWorkerGroup(t *testing.T, phases []PhaseSpec) (*WorkerGroup, fabric.Fabric, *integrationPoller, *bytes.Buffer) {
	t.Helper()
	f := testSQLiteFabricForNebula(t)
	p := newIntegrationPoller()
	var logBuf bytes.Buffer

	state := &State{
		Version: 1,
		Phases:  make(map[string]*PhaseState),
	}

	neb := &Nebula{
		Phases: phases,
	}

	wg := NewWorkerGroup(neb, state,
		WithFabric(f),
		WithPoller(p),
		WithLogger(&logBuf),
	)

	wg.tracker = NewPhaseTracker(phases, state)
	bt := fabric.NewBlockedTracker()
	ph := &fabric.PushbackHandler{Fabric: f}
	wg.blockedTracker = bt
	wg.pushbackHandler = ph
	wg.tychoScheduler = &tycho.Scheduler{
		Fabric:   f,
		Poller:   p,
		Blocked:  bt,
		Pushback: ph,
		Logger:   &logBuf,
	}

	return wg, f, p, &logBuf
}

// --- Integration Tests ---

func TestIntegration_FabricPhaseComplete_PublishesToRealStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{{ID: "producer"}, {ID: "consumer"}}
	wg, f, _, _ := newIntegrationWorkerGroup(t, phases)

	// Simulate the producer phase claiming a file.
	if err := f.ClaimFile(ctx, "internal/api/handler.go", "producer"); err != nil {
		t.Fatalf("ClaimFile: %v", err)
	}

	// Manually publish entanglements (since Publisher requires git, we
	// write directly and verify fabricPhaseComplete handles the rest).
	if err := f.PublishEntanglements(ctx, []fabric.Entanglement{
		{Producer: "producer", Kind: fabric.KindType, Name: "Handler", Package: "api", Status: fabric.StatusFulfilled},
		{Producer: "producer", Kind: fabric.KindFunction, Name: "NewHandler", Package: "api", Status: fabric.StatusFulfilled},
	}); err != nil {
		t.Fatalf("PublishEntanglements: %v", err)
	}

	// Complete the phase through fabricPhaseComplete (Publisher is nil so
	// it only sets state and releases claims).
	wg.fabricPhaseComplete(ctx, "producer", nil)

	// Verify: phase state should be done.
	state, err := f.GetPhaseState(ctx, "producer")
	if err != nil {
		t.Fatalf("GetPhaseState: %v", err)
	}
	if state != fabric.StateDone {
		t.Errorf("expected fabric state %q, got %q", fabric.StateDone, state)
	}

	// Verify: file claims should be released.
	owner, err := f.FileOwner(ctx, "internal/api/handler.go")
	if err != nil {
		t.Fatalf("FileOwner: %v", err)
	}
	if owner != "" {
		t.Errorf("expected claim released, but file still owned by %q", owner)
	}

	// Verify: entanglements are still in the store (they survive claim release).
	entanglements, err := f.EntanglementsFor(ctx, "producer")
	if err != nil {
		t.Fatalf("EntanglementsFor: %v", err)
	}
	if len(entanglements) != 2 {
		t.Errorf("expected 2 entanglements, got %d", len(entanglements))
	}
}

func TestIntegration_PollEligible_FiltersBasedOnRealFabricState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{{ID: "phase-a"}, {ID: "phase-b"}, {ID: "phase-c"}}
	wg, f, poller, _ := newIntegrationWorkerGroup(t, phases)

	// Seed real fabric state: phase-a is done, has entanglements.
	if err := f.SetPhaseState(ctx, "phase-a", fabric.StateDone); err != nil {
		t.Fatalf("SetPhaseState: %v", err)
	}
	if err := f.PublishEntanglements(ctx, []fabric.Entanglement{
		{Producer: "phase-a", Kind: fabric.KindType, Name: "Config", Package: "config", Status: fabric.StatusFulfilled},
	}); err != nil {
		t.Fatalf("PublishEntanglements: %v", err)
	}

	// phase-b can proceed (default in poller). phase-c needs info.
	poller.setDecision("phase-c", fabric.PollResult{
		Decision:    fabric.PollNeedInfo,
		Reason:      "waiting for Database type",
		MissingInfo: []string{"Database"},
	})

	eligible := wg.pollEligible(ctx, []string{"phase-b", "phase-c"})

	got := make(map[string]bool)
	for _, id := range eligible {
		got[id] = true
	}

	if !got["phase-b"] {
		t.Error("expected phase-b to be eligible (polls PROCEED)")
	}
	if got["phase-c"] {
		t.Error("expected phase-c to be blocked (polls NEED_INFO)")
	}

	// Verify phase-c is in the blocked tracker.
	if wg.blockedTracker.Get("phase-c") == nil {
		t.Error("expected phase-c to be tracked as blocked")
	}

	// Verify phase-b got its fabric state set to running.
	stateB, _ := f.GetPhaseState(ctx, "phase-b")
	if stateB != fabric.StateRunning {
		t.Errorf("expected phase-b fabric state %q, got %q", fabric.StateRunning, stateB)
	}
}

func TestIntegration_ReevaluateBlocked_WithRealFabricStateChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{{ID: "producer"}, {ID: "consumer"}}
	wg, f, poller, logBuf := newIntegrationWorkerGroup(t, phases)

	// Block the consumer — it needs producer's entanglements.
	wg.blockedTracker.Block("consumer", fabric.PollResult{
		Decision:    fabric.PollNeedInfo,
		Reason:      "waiting for Handler type from producer",
		MissingInfo: []string{"Handler"},
	})

	// Consumer still needs info on first re-evaluation.
	poller.setDecision("consumer", fabric.PollResult{
		Decision:    fabric.PollNeedInfo,
		Reason:      "still waiting for Handler",
		MissingInfo: []string{"Handler"},
	})

	wg.reevaluateBlocked(ctx)

	// Consumer should still be blocked.
	bp := wg.blockedTracker.Get("consumer")
	if bp == nil {
		t.Fatal("expected consumer to still be blocked after first re-evaluation")
	}
	if bp.RetryCount < 1 {
		t.Errorf("expected retry count >= 1, got %d", bp.RetryCount)
	}

	// Now simulate the producer completing and publishing entanglements.
	if err := f.SetPhaseState(ctx, "producer", fabric.StateDone); err != nil {
		t.Fatalf("SetPhaseState: %v", err)
	}
	if err := f.PublishEntanglements(ctx, []fabric.Entanglement{
		{Producer: "producer", Kind: fabric.KindType, Name: "Handler", Package: "api", Status: fabric.StatusFulfilled},
	}); err != nil {
		t.Fatalf("PublishEntanglements: %v", err)
	}

	// Change poller to return PROCEED for consumer now that entanglements exist.
	poller.setDecision("consumer", fabric.PollResult{Decision: fabric.PollProceed})

	wg.reevaluateBlocked(ctx)

	// Consumer should be unblocked now.
	if wg.blockedTracker.Get("consumer") != nil {
		t.Error("expected consumer to be unblocked after producer completed")
	}

	// Fabric state should be set to scanning.
	state, err := f.GetPhaseState(ctx, "consumer")
	if err != nil {
		t.Fatalf("GetPhaseState: %v", err)
	}
	if state != fabric.StateScanning {
		t.Errorf("expected consumer fabric state %q, got %q", fabric.StateScanning, state)
	}

	if !strings.Contains(logBuf.String(), "unblocked") {
		t.Errorf("expected log to mention 'unblocked', got: %s", logBuf.String())
	}
}

func TestIntegration_FullFlow_BlockPollUnblock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{
		{ID: "setup"},
		{ID: "api-types"},
		{ID: "api-handlers", DependsOn: []string{"api-types"}},
	}
	wg, f, poller, _ := newIntegrationWorkerGroup(t, phases)

	// Mark setup as done in the tracker (simulating prior completion).
	wg.tracker.Done()["setup"] = true

	// api-types can proceed, api-handlers needs info.
	poller.setDecision("api-handlers", fabric.PollResult{
		Decision:    fabric.PollNeedInfo,
		Reason:      "waiting for Router type",
		MissingInfo: []string{"Router"},
	})

	// Step 1: Poll eligible phases.
	eligible := wg.pollEligible(ctx, []string{"api-types", "api-handlers"})

	gotEligible := make(map[string]bool)
	for _, id := range eligible {
		gotEligible[id] = true
	}
	if !gotEligible["api-types"] {
		t.Error("expected api-types to be eligible")
	}
	if gotEligible["api-handlers"] {
		t.Error("expected api-handlers to be blocked")
	}

	// Step 2: Simulate api-types completing.
	if err := f.SetPhaseState(ctx, "api-types", fabric.StateDone); err != nil {
		t.Fatalf("SetPhaseState: %v", err)
	}
	if err := f.PublishEntanglements(ctx, []fabric.Entanglement{
		{Producer: "api-types", Kind: fabric.KindType, Name: "Router", Package: "api", Status: fabric.StatusFulfilled},
		{Producer: "api-types", Kind: fabric.KindInterface, Name: "Handler", Package: "api", Status: fabric.StatusFulfilled},
	}); err != nil {
		t.Fatalf("PublishEntanglements: %v", err)
	}
	wg.fabricPhaseComplete(ctx, "api-types", nil)
	wg.tracker.Done()["api-types"] = true

	// Step 3: Re-evaluate blocked phases.
	poller.setDecision("api-handlers", fabric.PollResult{Decision: fabric.PollProceed})

	wg.reevaluateBlocked(ctx)

	if wg.blockedTracker.Get("api-handlers") != nil {
		t.Error("expected api-handlers to be unblocked after re-evaluation")
	}

	// Verify fabric state progression for api-types.
	stateTypes, _ := f.GetPhaseState(ctx, "api-types")
	if stateTypes != fabric.StateDone {
		t.Errorf("expected api-types fabric state %q, got %q", fabric.StateDone, stateTypes)
	}

	// Verify entanglements persisted in real store.
	allEnts, err := f.AllEntanglements(ctx)
	if err != nil {
		t.Fatalf("AllEntanglements: %v", err)
	}
	if len(allEnts) != 2 {
		t.Errorf("expected 2 entanglements in store, got %d", len(allEnts))
	}
}

func TestIntegration_EscalateAllBlocked_WithRealFabric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{{ID: "stuck-a"}, {ID: "stuck-b"}}
	wg, f, _, logBuf := newIntegrationWorkerGroup(t, phases)

	// Block both phases.
	wg.blockedTracker.Block("stuck-a", fabric.PollResult{
		Decision: fabric.PollNeedInfo,
		Reason:   "missing everything",
	})
	wg.blockedTracker.Block("stuck-b", fabric.PollResult{
		Decision: fabric.PollConflict,
		Reason:   "unresolvable conflict",
	})

	wg.escalateAllBlocked(ctx)

	// Both should be unblocked from the tracker.
	if wg.blockedTracker.Len() != 0 {
		t.Errorf("expected 0 blocked after escalation, got %d", wg.blockedTracker.Len())
	}

	// Both should have fabric state human_decision in the real store.
	stateA, _ := f.GetPhaseState(ctx, "stuck-a")
	stateB, _ := f.GetPhaseState(ctx, "stuck-b")
	if stateA != fabric.StateHumanDecision {
		t.Errorf("expected stuck-a state %q, got %q", fabric.StateHumanDecision, stateA)
	}
	if stateB != fabric.StateHumanDecision {
		t.Errorf("expected stuck-b state %q, got %q", fabric.StateHumanDecision, stateB)
	}

	// Both should be marked as failed in the tracker.
	if !wg.tracker.Failed()["stuck-a"] || !wg.tracker.Failed()["stuck-b"] {
		t.Error("expected both phases to be marked as failed")
	}

	// Escalation messages should be in the log.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "Fabric Escalation") {
		t.Errorf("expected escalation log output, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "stuck-a") || !strings.Contains(logOutput, "stuck-b") {
		t.Errorf("expected both phase IDs in escalation log, got: %s", logOutput)
	}
}
