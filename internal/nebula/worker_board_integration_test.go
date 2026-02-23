package nebula

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/papapumpkin/quasar/internal/board"
)

// --- helpers for integration tests ---

// realBoard creates a real SQLite board backed by a temp file and registers cleanup.
func realBoard(t *testing.T) *board.SQLiteBoard {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "worker_integration.board.db")
	b, err := board.NewSQLiteBoard(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteBoard(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// intPoller implements board.Poller with thread-safe, dynamically-updatable
// decisions per phase. It tracks poll counts for verification.
type intPoller struct {
	mu        sync.Mutex
	decisions map[string]board.PollResult
	pollCount map[string]int
}

func newIntPoller() *intPoller {
	return &intPoller{
		decisions: make(map[string]board.PollResult),
		pollCount: make(map[string]int),
	}
}

func (p *intPoller) setDecision(phaseID string, r board.PollResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decisions[phaseID] = r
}

func (p *intPoller) getPollCount(phaseID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pollCount[phaseID]
}

func (p *intPoller) Poll(_ context.Context, phaseID string, _ board.BoardSnapshot) (board.PollResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pollCount[phaseID]++
	if r, ok := p.decisions[phaseID]; ok {
		return r, nil
	}
	return board.PollResult{Decision: board.PollProceed}, nil
}

// intWorkerGroup creates a WorkerGroup wired to a real SQLite board for
// integration testing.
func intWorkerGroup(t *testing.T, phases []PhaseSpec, b board.Board, poller board.Poller) *WorkerGroup {
	t.Helper()

	neb := &Nebula{
		Phases: phases,
		Manifest: Manifest{
			Nebula: Info{Name: "integration-test"},
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
	wg.Gater = &intAutoAcceptGater{}
	wg.Board = b
	wg.Poller = poller
	wg.blockedTracker = board.NewBlockedTracker()
	wg.pushbackHandler = &board.PushbackHandler{Board: b}
	wg.tracker = NewPhaseTracker(neb.Phases, state)

	return wg
}

type intAutoAcceptGater struct{}

func (g *intAutoAcceptGater) PhaseGate(_ context.Context, _ *PhaseSpec, _ *Checkpoint) (GateAction, error) {
	return GateActionAccept, nil
}

func (g *intAutoAcceptGater) PlanGate(_ context.Context, _ *Checkpoint) error {
	return nil
}

// --- Integration tests using real SQLite board ---

// TestIntBoardPipeline_LinearDependencyChain exercises the full board pipeline:
// Phase A completes and publishes contracts → Phase B polls and proceeds.
func TestIntBoardPipeline_LinearDependencyChain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B", DependsOn: []string{"a"}},
	}
	b := realBoard(t)
	poller := newIntPoller()
	wg := intWorkerGroup(t, phases, b, poller)

	// Phase A runs and completes — publish contracts directly to the board.
	if err := b.PublishContracts(ctx, []board.Contract{
		{Producer: "a", Kind: board.KindInterface, Name: "Store",
			Signature: "type Store interface{}", Package: "storage"},
	}); err != nil {
		t.Fatalf("PublishContracts: %v", err)
	}
	wg.boardPhaseComplete(ctx, "a", &PhaseRunnerResult{
		BaseCommitSHA:  "aaa",
		FinalCommitSHA: "bbb",
	})

	// Verify board state for A.
	stateA, err := b.GetPhaseState(ctx, "a")
	if err != nil {
		t.Fatalf("GetPhaseState(a): %v", err)
	}
	if stateA != board.StateDone {
		t.Errorf("expected board state %q for a, got %q", board.StateDone, stateA)
	}

	// Phase B polls — should PROCEED because A's contracts are on the board.
	eligible := wg.pollEligible(ctx, []string{"b"})
	if len(eligible) != 1 || eligible[0] != "b" {
		t.Fatalf("expected [b] eligible, got %v", eligible)
	}
	if poller.getPollCount("b") != 1 {
		t.Errorf("expected 1 poll for b, got %d", poller.getPollCount("b"))
	}

	// Verify board state for B is running.
	stateB, _ := b.GetPhaseState(ctx, "b")
	if stateB != board.StateRunning {
		t.Errorf("expected board state %q for b, got %q", board.StateRunning, stateB)
	}

	// Verify contracts are visible via the board.
	contracts, _ := b.AllContracts(ctx)
	if len(contracts) != 1 {
		t.Errorf("expected 1 contract on board, got %d", len(contracts))
	}
}

// TestIntBoardPipeline_BlockAndResume exercises: Phase B blocked (NEED_INFO),
// Phase A completes, re-evaluation unblocks B.
func TestIntBoardPipeline_BlockAndResume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
	}
	b := realBoard(t)
	poller := newIntPoller()
	wg := intWorkerGroup(t, phases, b, poller)

	// Mark A as in-flight in the tracker.
	wg.tracker.InFlight()["a"] = true

	// Phase B polls — NEED_INFO.
	poller.setDecision("b", board.PollResult{
		Decision:    board.PollNeedInfo,
		Reason:      "waiting for phase-a contracts",
		MissingInfo: []string{"phase-a Store interface"},
	})

	eligible := wg.pollEligible(ctx, []string{"b"})
	if len(eligible) != 0 {
		t.Fatalf("expected 0 eligible (b blocked), got %v", eligible)
	}
	if wg.blockedTracker.Len() != 1 {
		t.Fatalf("expected 1 blocked, got %d", wg.blockedTracker.Len())
	}

	// Verify board state shows blocked.
	stateB, _ := b.GetPhaseState(ctx, "b")
	if stateB != board.StateBlocked {
		t.Errorf("expected board state %q for b, got %q", board.StateBlocked, stateB)
	}

	// Phase A completes and publishes contracts.
	if err := b.PublishContracts(ctx, []board.Contract{
		{Producer: "a", Kind: board.KindInterface, Name: "Store",
			Signature: "type Store interface{}", Package: "storage"},
	}); err != nil {
		t.Fatalf("PublishContracts: %v", err)
	}
	wg.tracker.InFlight()["a"] = false
	delete(wg.tracker.InFlight(), "a")
	wg.tracker.Done()["a"] = true
	if err := b.SetPhaseState(ctx, "a", board.StateDone); err != nil {
		t.Fatalf("SetPhaseState(a, done): %v", err)
	}

	// Now change poller to return PROCEED for B.
	poller.setDecision("b", board.PollResult{Decision: board.PollProceed})

	// Re-evaluate blocked phases.
	wg.reevaluateBlocked(ctx)

	// B should be unblocked.
	if wg.blockedTracker.Len() != 0 {
		t.Fatalf("expected 0 blocked after reevaluation, got %d", wg.blockedTracker.Len())
	}

	// Board state should be polling (just unblocked, not yet running).
	stateB2, _ := b.GetPhaseState(ctx, "b")
	if stateB2 != board.StatePolling {
		t.Errorf("expected board state %q for b after unblock, got %q", board.StatePolling, stateB2)
	}
}

// TestIntBoardPipeline_EscalationWithRealBoard verifies that after MaxRetries
// of NEED_INFO with no plausible producer, the phase is escalated to
// HUMAN_DECISION_REQUIRED with a gate signal.
func TestIntBoardPipeline_EscalationWithRealBoard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{{ID: "stuck", Title: "Stuck Phase"}}
	b := realBoard(t)
	poller := newIntPoller()
	wg := intWorkerGroup(t, phases, b, poller)
	wg.pushbackHandler.MaxRetries = 2

	// Phase is blocked with NEED_INFO, no plausible producer.
	needInfo := board.PollResult{
		Decision:    board.PollNeedInfo,
		Reason:      "missing contract from unknown source",
		MissingInfo: []string{"UnknownService"},
	}
	poller.setDecision("stuck", needInfo)

	// First poll — blocks.
	eligible := wg.pollEligible(ctx, []string{"stuck"})
	if len(eligible) != 0 {
		t.Fatalf("expected 0 eligible, got %v", eligible)
	}

	// Re-evaluate twice more to exhaust retries (initial block + 2 re-evals = 3 total blocks).
	wg.reevaluateBlocked(ctx)
	wg.reevaluateBlocked(ctx)

	// After MaxRetries=2, the phase should be escalated.
	if wg.blockedTracker.Len() != 0 {
		t.Errorf("expected 0 blocked after escalation, got %d", wg.blockedTracker.Len())
	}

	// Verify board state is human_decision.
	state, _ := b.GetPhaseState(ctx, "stuck")
	if state != board.StateHumanDecision {
		t.Errorf("expected board state %q, got %q", board.StateHumanDecision, state)
	}

	// Verify gate signal was emitted.
	if len(wg.gateSignals) != 1 {
		t.Fatalf("expected 1 gate signal, got %d", len(wg.gateSignals))
	}
	if wg.gateSignals[0].action != GateActionReject {
		t.Errorf("expected gate action %q, got %q", GateActionReject, wg.gateSignals[0].action)
	}

	// Phase should be marked done+failed in tracker.
	if !wg.tracker.Done()["stuck"] {
		t.Error("expected phase in done set after escalation")
	}
	if !wg.tracker.Failed()["stuck"] {
		t.Error("expected phase in failed set after escalation")
	}
}

// TestIntBoardPipeline_FileConflictLifecycle exercises the full file claim
// lifecycle: A claims file → B gets CONFLICT → A releases → B proceeds.
func TestIntBoardPipeline_FileConflictLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
	}
	b := realBoard(t)
	poller := newIntPoller()
	wg := intWorkerGroup(t, phases, b, poller)

	// Phase A claims a file.
	if err := b.ClaimFile(ctx, "shared.go", "a"); err != nil {
		t.Fatalf("ClaimFile: %v", err)
	}
	wg.tracker.InFlight()["a"] = true

	// Phase B polls — CONFLICT with file claim.
	poller.setDecision("b", board.PollResult{
		Decision:     board.PollConflict,
		Reason:       "shared.go claimed by a",
		ConflictWith: "a",
	})

	eligible := wg.pollEligible(ctx, []string{"b"})
	if len(eligible) != 0 {
		t.Fatalf("expected 0 eligible (b blocked by conflict), got %v", eligible)
	}

	// Verify B is blocked (file-claim conflict → ActionRetry).
	if wg.blockedTracker.Len() != 1 {
		t.Fatalf("expected 1 blocked, got %d", wg.blockedTracker.Len())
	}

	// Phase A completes: release claims via boardPhaseComplete.
	wg.boardPhaseComplete(ctx, "a", &PhaseRunnerResult{
		BaseCommitSHA: "aaa", FinalCommitSHA: "bbb",
	})
	delete(wg.tracker.InFlight(), "a")
	wg.tracker.Done()["a"] = true

	// Verify claims are released.
	owner, _ := b.FileOwner(ctx, "shared.go")
	if owner != "" {
		t.Errorf("expected file unclaimed after release, got owner %q", owner)
	}

	// Now re-evaluate B: poller returns PROCEED.
	poller.setDecision("b", board.PollResult{Decision: board.PollProceed})
	wg.reevaluateBlocked(ctx)

	if wg.blockedTracker.Len() != 0 {
		t.Fatalf("expected 0 blocked after reevaluation, got %d", wg.blockedTracker.Len())
	}
}

// TestIntBoardPipeline_ContradictoryContracts exercises: Two phases publish
// contradictory interfaces → immediate CONFLICT escalation.
func TestIntBoardPipeline_ContradictoryContracts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
		{ID: "c", Title: "Phase C"},
	}
	b := realBoard(t)
	poller := newIntPoller()
	wg := intWorkerGroup(t, phases, b, poller)

	// Both A and B have completed with contradictory Store interfaces.
	if err := b.PublishContract(ctx, board.Contract{
		Producer: "a", Kind: board.KindInterface, Name: "Store",
		Signature: "type Store interface { Get() string }", Package: "storage",
	}); err != nil {
		t.Fatalf("PublishContract(a): %v", err)
	}
	if err := b.PublishContract(ctx, board.Contract{
		Producer: "b", Kind: board.KindInterface, Name: "Store",
		Signature: "type Store interface { Get() (string, error) }", Package: "storage",
	}); err != nil {
		t.Fatalf("PublishContract(b): %v", err)
	}
	wg.tracker.Done()["a"] = true
	wg.tracker.Done()["b"] = true
	if err := b.SetPhaseState(ctx, "a", board.StateDone); err != nil {
		t.Fatalf("SetPhaseState(a): %v", err)
	}
	if err := b.SetPhaseState(ctx, "b", board.StateDone); err != nil {
		t.Fatalf("SetPhaseState(b): %v", err)
	}

	// Phase C polls → CONFLICT (interface conflict, not file claim).
	poller.setDecision("c", board.PollResult{
		Decision:     board.PollConflict,
		Reason:       "contradictory Store interfaces",
		ConflictWith: "a", // no file claims for a → not a file-claim conflict
	})

	eligible := wg.pollEligible(ctx, []string{"c"})
	if len(eligible) != 0 {
		t.Fatalf("expected 0 eligible after conflict, got %v", eligible)
	}

	// Interface conflict → immediate escalation (not retry).
	if wg.blockedTracker.Len() != 0 {
		t.Error("expected 0 blocked — escalation should have removed from tracker")
	}

	stateC, _ := b.GetPhaseState(ctx, "c")
	if stateC != board.StateHumanDecision {
		t.Errorf("expected board state %q for c, got %q", board.StateHumanDecision, stateC)
	}

	if len(wg.gateSignals) != 1 {
		t.Fatalf("expected 1 gate signal, got %d", len(wg.gateSignals))
	}

	// Verify the actual contracts on the board show the contradiction.
	allContracts, _ := b.AllContracts(ctx)
	storeCount := 0
	for _, c := range allContracts {
		if c.Name == "Store" && c.Kind == board.KindInterface {
			storeCount++
		}
	}
	if storeCount != 2 {
		t.Errorf("expected 2 contradictory Store contracts on board, got %d", storeCount)
	}
}

// TestIntBoardPipeline_NilBoardBackwardCompat verifies that running with
// Board=nil produces identical behavior to legacy dispatch — no panics.
func TestIntBoardPipeline_NilBoardBackwardCompat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
	}
	neb := &Nebula{
		Phases: phases,
		Manifest: Manifest{
			Nebula: Info{Name: "nil-board-test"},
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
	wg.Gater = &intAutoAcceptGater{}
	// Board is intentionally nil.

	// boardBlocked should return 0, not panic.
	if wg.boardBlocked() != 0 {
		t.Fatal("expected 0 blocked with nil board")
	}

	// boardPhaseComplete should be a no-op, not panic.
	wg.boardPhaseComplete(ctx, "a", nil)
	wg.boardPhaseComplete(ctx, "b", &PhaseRunnerResult{
		BaseCommitSHA: "abc", FinalCommitSHA: "def",
	})

	// escalateAllBlocked should be a no-op, not panic.
	wg.escalateAllBlocked(ctx)

	// reevaluateBlocked should be a no-op, not panic.
	wg.reevaluateBlocked(ctx)
}

// TestIntBoardPipeline_EscalateAllBlocked verifies that when all ready phases
// are blocked and nothing is in-flight, escalateAllBlocked transitions them
// all to human_decision.
func TestIntBoardPipeline_EscalateAllBlocked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{
		{ID: "x", Title: "Phase X"},
		{ID: "y", Title: "Phase Y"},
	}
	b := realBoard(t)
	poller := newIntPoller()
	wg := intWorkerGroup(t, phases, b, poller)

	// Block both phases.
	poller.setDecision("x", board.PollResult{
		Decision: board.PollNeedInfo, Reason: "missing dep",
		MissingInfo: []string{"unknown"},
	})
	poller.setDecision("y", board.PollResult{
		Decision: board.PollConflict, Reason: "conflict",
		ConflictWith: "external",
	})

	// Poll both.
	eligible := wg.pollEligible(ctx, []string{"x"})
	if len(eligible) != 0 {
		t.Fatalf("expected 0 eligible for x, got %v", eligible)
	}

	// For y, the conflict with non-file-claim target causes immediate escalation.
	eligible = wg.pollEligible(ctx, []string{"y"})
	if len(eligible) != 0 {
		t.Fatalf("expected 0 eligible for y, got %v", eligible)
	}

	// x should still be blocked (NEED_INFO → retry), y was escalated immediately.
	// Escalate all remaining blocked.
	wg.escalateAllBlocked(ctx)

	// All should be escalated.
	if wg.blockedTracker.Len() != 0 {
		t.Errorf("expected 0 blocked after escalateAll, got %d", wg.blockedTracker.Len())
	}

	// Both should have human_decision board state.
	for _, id := range []string{"x", "y"} {
		s, _ := b.GetPhaseState(ctx, id)
		if s != board.StateHumanDecision {
			t.Errorf("expected %q in %q state, got %q", id, board.StateHumanDecision, s)
		}
	}
}

// TestIntBoardPipeline_SnapshotContents verifies that buildBoardSnapshot
// returns accurate contract content (not just counts) from a real SQLite board.
func TestIntBoardPipeline_SnapshotContents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	phases := []PhaseSpec{
		{ID: "a", Title: "Phase A"},
		{ID: "b", Title: "Phase B"},
	}
	b := realBoard(t)
	poller := newIntPoller()
	wg := intWorkerGroup(t, phases, b, poller)

	// Publish contracts with specific content.
	contracts := []board.Contract{
		{Producer: "a", Kind: board.KindInterface, Name: "Store",
			Signature: "type Store interface { Get(key string) ([]byte, error) }",
			Package:   "storage"},
		{Producer: "a", Kind: board.KindFunction, Name: "NewStore",
			Signature: "func NewStore(dsn string) (*Store, error)",
			Package:   "storage"},
	}
	if err := b.PublishContracts(ctx, contracts); err != nil {
		t.Fatalf("PublishContracts: %v", err)
	}

	// Claim a file.
	if err := b.ClaimFile(ctx, "storage/store.go", "b"); err != nil {
		t.Fatalf("ClaimFile: %v", err)
	}

	// Set tracker state.
	wg.tracker.Done()["a"] = true
	wg.tracker.InFlight()["b"] = true

	// Build snapshot.
	wg.mu.Lock()
	snap, err := wg.buildBoardSnapshot(ctx)
	wg.mu.Unlock()

	if err != nil {
		t.Fatalf("buildBoardSnapshot: %v", err)
	}

	// Verify contract content (not just count).
	if len(snap.Contracts) != 2 {
		t.Fatalf("expected 2 contracts, got %d", len(snap.Contracts))
	}

	foundStore := false
	foundNewStore := false
	for _, c := range snap.Contracts {
		if c.Name == "Store" && c.Kind == board.KindInterface {
			if !strings.Contains(c.Signature, "Get(key string)") {
				t.Errorf("Store contract signature mismatch: %q", c.Signature)
			}
			if c.Package != "storage" {
				t.Errorf("Store contract package: expected storage, got %q", c.Package)
			}
			foundStore = true
		}
		if c.Name == "NewStore" && c.Kind == board.KindFunction {
			if !strings.Contains(c.Signature, "NewStore(dsn string)") {
				t.Errorf("NewStore contract signature mismatch: %q", c.Signature)
			}
			foundNewStore = true
		}
	}
	if !foundStore {
		t.Error("Store interface contract not found in snapshot")
	}
	if !foundNewStore {
		t.Error("NewStore function contract not found in snapshot")
	}

	// Verify file claims.
	if snap.FileClaims["storage/store.go"] != "b" {
		t.Errorf("expected file claim storage/store.go → b, got %v", snap.FileClaims)
	}

	// Verify completed/in-progress lists.
	if len(snap.Completed) != 1 || snap.Completed[0] != "a" {
		t.Errorf("expected completed=[a], got %v", snap.Completed)
	}
	if len(snap.InProgress) != 1 || snap.InProgress[0] != "b" {
		t.Errorf("expected in-progress=[b], got %v", snap.InProgress)
	}
}
