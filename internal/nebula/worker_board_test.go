package nebula

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/papapumpkin/quasar/internal/board"
)

// --- mock types ---------------------------------------------------------

// mockBoard implements board.Board with in-memory maps.
type mockBoard struct {
	mu         sync.Mutex
	states     map[string]string
	contracts  []board.Contract
	claims     map[string]string // filepath → owner
	phaseClaim map[string][]string
}

func newMockBoard() *mockBoard {
	return &mockBoard{
		states:     make(map[string]string),
		claims:     make(map[string]string),
		phaseClaim: make(map[string][]string),
	}
}

func (b *mockBoard) SetPhaseState(_ context.Context, phaseID, state string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.states[phaseID] = state
	return nil
}

func (b *mockBoard) GetPhaseState(_ context.Context, phaseID string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.states[phaseID], nil
}

func (b *mockBoard) PublishContract(_ context.Context, c board.Contract) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.contracts = append(b.contracts, c)
	return nil
}

func (b *mockBoard) PublishContracts(_ context.Context, cs []board.Contract) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.contracts = append(b.contracts, cs...)
	return nil
}

func (b *mockBoard) ContractsFor(_ context.Context, phaseID string) ([]board.Contract, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []board.Contract
	for _, c := range b.contracts {
		if c.Producer == phaseID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (b *mockBoard) AllContracts(_ context.Context) ([]board.Contract, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]board.Contract, len(b.contracts))
	copy(out, b.contracts)
	return out, nil
}

func (b *mockBoard) ClaimFile(_ context.Context, filepath, owner string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, ok := b.claims[filepath]; ok && existing != owner {
		return fmt.Errorf("file %q already claimed by %q", filepath, existing)
	}
	b.claims[filepath] = owner
	b.phaseClaim[owner] = append(b.phaseClaim[owner], filepath)
	return nil
}

func (b *mockBoard) ReleaseClaims(_ context.Context, owner string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, fp := range b.phaseClaim[owner] {
		delete(b.claims, fp)
	}
	delete(b.phaseClaim, owner)
	return nil
}

func (b *mockBoard) FileOwner(_ context.Context, filepath string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.claims[filepath], nil
}

func (b *mockBoard) ClaimsFor(_ context.Context, phaseID string) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.phaseClaim[phaseID], nil
}

func (b *mockBoard) Close() error { return nil }

// mockPoller implements board.Poller with configurable per-phase decisions.
type mockPoller struct {
	mu        sync.Mutex
	decisions map[string]board.PollResult
	pollCount map[string]int
}

func newMockPoller() *mockPoller {
	return &mockPoller{
		decisions: make(map[string]board.PollResult),
		pollCount: make(map[string]int),
	}
}

func (p *mockPoller) setDecision(phaseID string, result board.PollResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decisions[phaseID] = result
}

func (p *mockPoller) getPollCount(phaseID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pollCount[phaseID]
}

func (p *mockPoller) Poll(_ context.Context, phaseID string, _ board.BoardSnapshot) (board.PollResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pollCount[phaseID]++
	if r, ok := p.decisions[phaseID]; ok {
		return r, nil
	}
	return board.PollResult{Decision: board.PollProceed}, nil
}

// --- helpers ------------------------------------------------------------

// minimalWorkerGroup creates a WorkerGroup with board integration for testing.
func minimalWorkerGroup(t *testing.T, phases []PhaseSpec) *WorkerGroup {
	t.Helper()

	neb := &Nebula{
		Phases: phases,
		Manifest: Manifest{
			Nebula: Info{Name: "test"},
		},
	}
	state := &State{
		Phases: make(map[string]*PhaseState),
	}
	for _, p := range phases {
		state.Phases[p.ID] = &PhaseState{BeadID: "bead-" + p.ID}
	}

	wg := NewWorkerGroup(neb, state)
	wg.Logger = io.Discard
	wg.Gater = &autoAcceptGater{}
	return wg
}

type autoAcceptGater struct{}

func (g *autoAcceptGater) PhaseGate(_ context.Context, _ *PhaseSpec, _ *Checkpoint) (GateAction, error) {
	return GateActionAccept, nil
}

func (g *autoAcceptGater) PlanGate(_ context.Context, _ *Checkpoint) error {
	return nil
}

// --- tests --------------------------------------------------------------

func TestPollEligible_NilBoard(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Title: "Phase A"}}
	wg := minimalWorkerGroup(t, phases)
	// Board is nil — pollEligible should never be called, but if called,
	// buildBoardSnapshot would panic. The Run loop guards with Board != nil.
	// Just verify the field defaults.
	if wg.Board != nil {
		t.Fatal("expected nil Board")
	}
	if wg.boardBlocked() != 0 {
		t.Fatal("expected 0 blocked phases with nil board")
	}
}

func TestPollEligible_AllProceed(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
	}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	mp := newMockPoller()
	wg.Board = mb
	wg.Poller = mp
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: mb}

	// Initialize tracker so buildBoardSnapshot can access maps.
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	eligible := []string{"a", "b"}
	result := wg.pollEligible(context.Background(), eligible)

	if len(result) != 2 {
		t.Fatalf("expected 2 eligible, got %d", len(result))
	}
	if mp.getPollCount("a") != 1 || mp.getPollCount("b") != 1 {
		t.Error("expected each phase to be polled once")
	}
	// Board state should be set to running for both.
	for _, id := range []string{"a", "b"} {
		s, _ := mb.GetPhaseState(context.Background(), id)
		if s != board.StateRunning {
			t.Errorf("expected board state %q for %q, got %q", board.StateRunning, id, s)
		}
	}
}

func TestPollEligible_NeedInfoBlocks(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
	}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	mp := newMockPoller()
	mp.setDecision("b", board.PollResult{
		Decision:    board.PollNeedInfo,
		Reason:      "missing types from phase a",
		MissingInfo: []string{"SomeType"},
	})
	wg.Board = mb
	wg.Poller = mp
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: mb}
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	eligible := []string{"a", "b"}
	result := wg.pollEligible(context.Background(), eligible)

	if len(result) != 1 || result[0] != "a" {
		t.Fatalf("expected only 'a' eligible, got %v", result)
	}
	if wg.blockedTracker.Len() != 1 {
		t.Fatalf("expected 1 blocked phase, got %d", wg.blockedTracker.Len())
	}
	bp := wg.blockedTracker.Get("b")
	if bp == nil {
		t.Fatal("expected 'b' to be blocked")
	}
	if bp.RetryCount != 0 {
		t.Errorf("expected retry count 0, got %d", bp.RetryCount)
	}
	// Board state should be blocked.
	s, _ := mb.GetPhaseState(context.Background(), "b")
	if s != board.StateBlocked {
		t.Errorf("expected board state %q for 'b', got %q", board.StateBlocked, s)
	}
}

func TestPollEligible_SkipsAlreadyBlocked(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Title: "Phase A"}}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	mp := newMockPoller()
	wg.Board = mb
	wg.Poller = mp
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: mb}
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	// Pre-block phase 'a'.
	wg.blockedTracker.Block("a", board.PollResult{Decision: board.PollNeedInfo})

	eligible := []string{"a"}
	result := wg.pollEligible(context.Background(), eligible)

	if len(result) != 0 {
		t.Fatalf("expected no eligible phases, got %v", result)
	}
	// Phase should NOT have been polled (already in blocked tracker).
	if mp.getPollCount("a") != 0 {
		t.Error("blocked phase should not be re-polled via pollEligible")
	}
}

func TestReevaluateBlocked_UnblocksOnProceed(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Title: "Phase A"}}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	mp := newMockPoller()
	// Start with NEED_INFO.
	mp.setDecision("a", board.PollResult{Decision: board.PollNeedInfo, Reason: "waiting"})
	wg.Board = mb
	wg.Poller = mp
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: mb}
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	// Block the phase.
	wg.blockedTracker.Block("a", board.PollResult{Decision: board.PollNeedInfo})

	// Now change the poller to return PROCEED.
	mp.setDecision("a", board.PollResult{Decision: board.PollProceed})

	wg.reevaluateBlocked(context.Background())

	if wg.blockedTracker.Len() != 0 {
		t.Fatalf("expected 0 blocked after re-evaluation, got %d", wg.blockedTracker.Len())
	}
	s, _ := mb.GetPhaseState(context.Background(), "a")
	if s != board.StatePolling {
		t.Errorf("expected board state %q after unblock, got %q", board.StatePolling, s)
	}
}

func TestReevaluateBlocked_StillBlockedIncrementsRetry(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Title: "Phase A"}}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	mp := newMockPoller()
	mp.setDecision("a", board.PollResult{Decision: board.PollNeedInfo, Reason: "still waiting"})
	wg.Board = mb
	wg.Poller = mp
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: mb}
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	// Block with initial result.
	wg.blockedTracker.Block("a", board.PollResult{Decision: board.PollNeedInfo})

	wg.reevaluateBlocked(context.Background())

	if wg.blockedTracker.Len() != 1 {
		t.Fatalf("expected 1 blocked, got %d", wg.blockedTracker.Len())
	}
	bp := wg.blockedTracker.Get("a")
	if bp == nil {
		t.Fatal("expected 'a' to still be blocked")
	}
	// RetryCount should have incremented: once in Block() during reevaluate's
	// handlePollBlock call (the initial Block() sets count=0, then the second
	// Block() call increments to 1).
	if bp.RetryCount < 1 {
		t.Errorf("expected retry count >= 1, got %d", bp.RetryCount)
	}
}

func TestBoardPhaseComplete_PublishesAndReleases(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Title: "Phase A"}}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	wg.Board = mb
	// No Publisher — test that board state and claims are still updated.
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	// Claim a file first.
	if err := mb.ClaimFile(context.Background(), "foo.go", "a"); err != nil {
		t.Fatal(err)
	}

	result := &PhaseRunnerResult{
		BaseCommitSHA:  "abc123",
		FinalCommitSHA: "def456",
	}
	wg.boardPhaseComplete(context.Background(), "a", result)

	s, _ := mb.GetPhaseState(context.Background(), "a")
	if s != board.StateDone {
		t.Errorf("expected board state %q, got %q", board.StateDone, s)
	}

	// File claims should be released.
	owner, _ := mb.FileOwner(context.Background(), "foo.go")
	if owner != "" {
		t.Errorf("expected claims released, but foo.go still claimed by %q", owner)
	}
}

func TestBoardPhaseComplete_NilBoardNoOp(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Title: "Phase A"}}
	wg := minimalWorkerGroup(t, phases)
	// Board is nil — should be a no-op without panic.
	wg.boardPhaseComplete(context.Background(), "a", nil)
}

func TestEscalatePhase_SetsHumanDecisionAndGateSignal(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Title: "Phase A"}}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	wg.Board = mb
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: mb}
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	bp := &board.BlockedPhase{
		PhaseID:    "a",
		RetryCount: 3,
		LastResult: board.PollResult{
			Decision: board.PollNeedInfo,
			Reason:   "missing dependency",
		},
	}

	wg.escalatePhase(context.Background(), "a", bp)

	s, _ := mb.GetPhaseState(context.Background(), "a")
	if s != board.StateHumanDecision {
		t.Errorf("expected board state %q, got %q", board.StateHumanDecision, s)
	}
	// Phase should be marked as done+failed in tracker.
	if !wg.tracker.Done()["a"] {
		t.Error("expected phase 'a' in done set after escalation")
	}
	if !wg.tracker.Failed()["a"] {
		t.Error("expected phase 'a' in failed set after escalation")
	}
	// Should have a gate signal.
	if len(wg.gateSignals) != 1 {
		t.Fatalf("expected 1 gate signal, got %d", len(wg.gateSignals))
	}
	if wg.gateSignals[0].action != GateActionReject {
		t.Errorf("expected gate action %q, got %q", GateActionReject, wg.gateSignals[0].action)
	}
}

func TestBuildBoardSnapshot(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
	}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	ctx := context.Background()
	if err := mb.PublishContract(ctx, board.Contract{
		Producer: "a", Kind: "function", Name: "Foo", Package: "pkg",
	}); err != nil {
		t.Fatal(err)
	}
	if err := mb.ClaimFile(ctx, "main.go", "b"); err != nil {
		t.Fatal(err)
	}
	wg.Board = mb
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	// Mark 'a' as done, 'b' as in-flight.
	wg.tracker.Done()["a"] = true
	wg.tracker.InFlight()["b"] = true

	wg.mu.Lock()
	snap, err := wg.buildBoardSnapshot(ctx)
	wg.mu.Unlock()

	if err != nil {
		t.Fatalf("buildBoardSnapshot: %v", err)
	}
	if len(snap.Contracts) != 1 {
		t.Errorf("expected 1 contract, got %d", len(snap.Contracts))
	}
	if len(snap.Completed) != 1 || snap.Completed[0] != "a" {
		t.Errorf("expected completed=[a], got %v", snap.Completed)
	}
	if len(snap.InProgress) != 1 || snap.InProgress[0] != "b" {
		t.Errorf("expected in-progress=[b], got %v", snap.InProgress)
	}
	if snap.FileClaims["main.go"] != "b" {
		t.Errorf("expected file claim main.go→b, got %v", snap.FileClaims)
	}
}

func TestEscalateAllBlocked(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
	}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	wg.Board = mb
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: mb}
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	// Block both phases.
	wg.blockedTracker.Block("a", board.PollResult{Decision: board.PollNeedInfo, Reason: "x"})
	wg.blockedTracker.Block("b", board.PollResult{Decision: board.PollConflict, Reason: "y"})

	wg.escalateAllBlocked(context.Background())

	// Both should now be unblocked (escalated removes from tracker).
	if wg.blockedTracker.Len() != 0 {
		t.Errorf("expected 0 blocked after escalation, got %d", wg.blockedTracker.Len())
	}
	// Both should have gate signals.
	if len(wg.gateSignals) != 2 {
		t.Errorf("expected 2 gate signals, got %d", len(wg.gateSignals))
	}
	// Both should be in human_decision state.
	for _, id := range []string{"a", "b"} {
		s, _ := mb.GetPhaseState(context.Background(), id)
		if s != board.StateHumanDecision {
			t.Errorf("expected %q in %q state, got %q", id, board.StateHumanDecision, s)
		}
	}
}

func TestPollEligible_ConflictEscalates(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Title: "Phase A"}}
	wg := minimalWorkerGroup(t, phases)

	mb := newMockBoard()
	mp := newMockPoller()
	// Conflict without a file-claim conflict → immediate escalation.
	mp.setDecision("a", board.PollResult{
		Decision:     board.PollConflict,
		Reason:       "interface conflict",
		ConflictWith: "other-phase",
	})
	wg.Board = mb
	wg.Poller = mp
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: mb}
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)

	eligible := []string{"a"}
	result := wg.pollEligible(context.Background(), eligible)

	// Phase should not be eligible (escalated, not just blocked).
	if len(result) != 0 {
		t.Fatalf("expected 0 eligible after conflict, got %v", result)
	}
	// Should be escalated (unblocked from tracker, marked as failed+done).
	if wg.blockedTracker.Len() != 0 {
		t.Error("expected 0 blocked — escalation should unblock")
	}
	s, _ := mb.GetPhaseState(context.Background(), "a")
	if s != board.StateHumanDecision {
		t.Errorf("expected board state %q, got %q", board.StateHumanDecision, s)
	}
	if len(wg.gateSignals) != 1 {
		t.Errorf("expected 1 gate signal, got %d", len(wg.gateSignals))
	}
}
