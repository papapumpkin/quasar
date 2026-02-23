package board

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// --- helpers for integration tests ---

// testSQLiteBoard creates a real SQLite board backed by a temp file.
func testSQLiteBoard(t *testing.T) *SQLiteBoard {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "integration.board.db")
	b, err := NewSQLiteBoard(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteBoard(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// deterministicPoller is a Poller that returns canned responses keyed by
// phase ID. It supports thread-safe mutation of decisions via setDecision
// and tracks poll counts. A response function can be set for dynamic behavior.
type deterministicPoller struct {
	mu        sync.Mutex
	decisions map[string]PollResult
	pollCount map[string]int
	// responseFn is called if non-nil and takes precedence over decisions map.
	responseFn func(phaseID string, snap BoardSnapshot) PollResult
}

func newDeterministicPoller() *deterministicPoller {
	return &deterministicPoller{
		decisions: make(map[string]PollResult),
		pollCount: make(map[string]int),
	}
}

func (p *deterministicPoller) setDecision(phaseID string, r PollResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decisions[phaseID] = r
}

func (p *deterministicPoller) getPollCount(phaseID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pollCount[phaseID]
}

func (p *deterministicPoller) Poll(_ context.Context, phaseID string, snap BoardSnapshot) (PollResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pollCount[phaseID]++
	if p.responseFn != nil {
		return p.responseFn(phaseID, snap), nil
	}
	if r, ok := p.decisions[phaseID]; ok {
		return r, nil
	}
	return PollResult{Decision: PollProceed}, nil
}

// --- Integration Tests ---

// TestIntegration_LinearDependencyChain verifies the happy path: Phase A has no
// deps, runs and publishes contracts. Phase B depends on A, polls the board,
// finds A's contracts, and proceeds.
func TestIntegration_LinearDependencyChain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testSQLiteBoard(t)

	// Phase A: no deps, runs immediately.
	if err := b.SetPhaseState(ctx, "phase-a", StateRunning); err != nil {
		t.Fatalf("SetPhaseState(phase-a, running): %v", err)
	}

	// Phase A completes and publishes contracts.
	contractsA := []Contract{
		{Producer: "phase-a", Kind: KindInterface, Name: "Store", Signature: "type Store interface { Get(key string) string }", Package: "storage"},
		{Producer: "phase-a", Kind: KindFunction, Name: "NewStore", Signature: "func NewStore(dsn string) *Store", Package: "storage"},
	}
	if err := b.PublishContracts(ctx, contractsA); err != nil {
		t.Fatalf("PublishContracts: %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-a", StateDone); err != nil {
		t.Fatalf("SetPhaseState(phase-a, done): %v", err)
	}

	// Build snapshot for Phase B's poll.
	allContracts, err := b.AllContracts(ctx)
	if err != nil {
		t.Fatalf("AllContracts: %v", err)
	}
	snap := BoardSnapshot{
		Contracts: allContracts,
		Completed: []string{"phase-a"},
	}

	// Phase B polls — poller sees A's contracts and returns PROCEED.
	poller := newDeterministicPoller()
	poller.responseFn = func(phaseID string, s BoardSnapshot) PollResult {
		if phaseID == "phase-b" && len(s.Contracts) > 0 {
			return PollResult{Decision: PollProceed, Reason: "all deps satisfied"}
		}
		return PollResult{Decision: PollNeedInfo, Reason: "missing contracts"}
	}

	result, err := poller.Poll(ctx, "phase-b", snap)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if result.Decision != PollProceed {
		t.Errorf("expected PROCEED, got %s: %s", result.Decision, result.Reason)
	}

	// Verify B receives A's contracts in the snapshot.
	if len(snap.Contracts) != 2 {
		t.Fatalf("expected 2 contracts in snapshot, got %d", len(snap.Contracts))
	}
	foundStore := false
	foundNewStore := false
	for _, c := range snap.Contracts {
		if c.Name == "Store" && c.Kind == KindInterface && c.Package == "storage" {
			foundStore = true
		}
		if c.Name == "NewStore" && c.Kind == KindFunction {
			foundNewStore = true
		}
	}
	if !foundStore {
		t.Error("expected Store interface contract in snapshot")
	}
	if !foundNewStore {
		t.Error("expected NewStore function contract in snapshot")
	}
}

// TestIntegration_ParallelRootsPostToBoard verifies cascading resolution:
// Phases A and B run concurrently. Phase C depends on both. A completes first,
// C polls — NEED_INFO (missing B). B completes, C re-polls — PROCEED.
func TestIntegration_ParallelRootsPostToBoard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testSQLiteBoard(t)

	// A and B start concurrently.
	if err := b.SetPhaseState(ctx, "phase-a", StateRunning); err != nil {
		t.Fatalf("SetPhaseState(a): %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-b", StateRunning); err != nil {
		t.Fatalf("SetPhaseState(b): %v", err)
	}

	// Phase A completes first and publishes contracts.
	if err := b.PublishContract(ctx, Contract{
		Producer: "phase-a", Kind: KindType, Name: "Config",
		Signature: "type Config struct{}", Package: "config",
	}); err != nil {
		t.Fatalf("PublishContract(A): %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-a", StateDone); err != nil {
		t.Fatalf("SetPhaseState(a, done): %v", err)
	}

	// C polls after A completes but before B completes.
	// Poller checks that both phase-a and phase-b have contracts.
	poller := newDeterministicPoller()
	poller.responseFn = func(phaseID string, s BoardSnapshot) PollResult {
		if phaseID != "phase-c" {
			return PollResult{Decision: PollProceed}
		}
		hasA := false
		hasB := false
		for _, c := range s.Contracts {
			if c.Producer == "phase-a" {
				hasA = true
			}
			if c.Producer == "phase-b" {
				hasB = true
			}
		}
		if hasA && hasB {
			return PollResult{Decision: PollProceed, Reason: "both deps satisfied"}
		}
		missing := []string{}
		if !hasA {
			missing = append(missing, "phase-a contracts")
		}
		if !hasB {
			missing = append(missing, "phase-b contracts")
		}
		return PollResult{Decision: PollNeedInfo, Reason: "missing deps", MissingInfo: missing}
	}

	// First poll: only A's contracts available.
	snap1Contracts, _ := b.AllContracts(ctx)
	snap1 := BoardSnapshot{
		Contracts:  snap1Contracts,
		Completed:  []string{"phase-a"},
		InProgress: []string{"phase-b"},
	}

	result1, err := poller.Poll(ctx, "phase-c", snap1)
	if err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	if result1.Decision != PollNeedInfo {
		t.Errorf("first poll: expected NEED_INFO, got %s", result1.Decision)
	}
	if len(result1.MissingInfo) != 1 || result1.MissingInfo[0] != "phase-b contracts" {
		t.Errorf("expected missing [phase-b contracts], got %v", result1.MissingInfo)
	}

	// Phase B now completes and publishes.
	if err := b.PublishContract(ctx, Contract{
		Producer: "phase-b", Kind: KindInterface, Name: "Logger",
		Signature: "type Logger interface { Log(msg string) }", Package: "log",
	}); err != nil {
		t.Fatalf("PublishContract(B): %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-b", StateDone); err != nil {
		t.Fatalf("SetPhaseState(b, done): %v", err)
	}

	// Second poll: both A and B contracts available.
	snap2Contracts, _ := b.AllContracts(ctx)
	snap2 := BoardSnapshot{
		Contracts: snap2Contracts,
		Completed: []string{"phase-a", "phase-b"},
	}

	result2, err := poller.Poll(ctx, "phase-c", snap2)
	if err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if result2.Decision != PollProceed {
		t.Errorf("second poll: expected PROCEED, got %s: %s", result2.Decision, result2.Reason)
	}

	// Verify C sees both contract sets.
	if len(snap2.Contracts) != 2 {
		t.Fatalf("expected 2 contracts in final snapshot, got %d", len(snap2.Contracts))
	}
}

// TestIntegration_PushbackAutoRetry verifies: Phase B polls before A completes
// (NEED_INFO), A completes and publishes, B re-polls (PROCEED). Verifies retry
// count, blocked tracking, and automatic resume via the PushbackHandler.
func TestIntegration_PushbackAutoRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testSQLiteBoard(t)

	// Phase A starts running.
	if err := b.SetPhaseState(ctx, "phase-a", StateRunning); err != nil {
		t.Fatalf("SetPhaseState(a): %v", err)
	}

	tracker := NewBlockedTracker()
	handler := &PushbackHandler{Board: b, MaxRetries: 3}

	// Phase B polls — A not done yet → NEED_INFO.
	needInfoResult := PollResult{
		Decision:    PollNeedInfo,
		Reason:      "waiting for phase-a",
		MissingInfo: []string{"phase-a Store interface"},
	}

	// Block B.
	tracker.Block("phase-b", needInfoResult)
	bp := tracker.Get("phase-b")
	if bp == nil {
		t.Fatal("expected phase-b to be blocked")
	}
	if bp.RetryCount != 0 {
		t.Errorf("initial retry count: expected 0, got %d", bp.RetryCount)
	}

	// Run pushback handler — A is in-progress, plausible producer found.
	snap1 := BoardSnapshot{InProgress: []string{"phase-a"}}
	action := handler.Handle(ctx, bp, snap1.InProgress, snap1)
	if action != ActionRetry {
		t.Fatalf("expected ActionRetry, got %s", action)
	}

	// Simulate another re-evaluation cycle (still blocked, retry increments).
	tracker.Block("phase-b", needInfoResult)
	bp = tracker.Get("phase-b")
	if bp.RetryCount != 1 {
		t.Errorf("second retry count: expected 1, got %d", bp.RetryCount)
	}

	// Phase A completes and publishes contracts.
	if err := b.PublishContracts(ctx, []Contract{
		{Producer: "phase-a", Kind: KindInterface, Name: "Store",
			Signature: "type Store interface { Get(key string) string }", Package: "storage"},
	}); err != nil {
		t.Fatalf("PublishContracts: %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-a", StateDone); err != nil {
		t.Fatalf("SetPhaseState(a, done): %v", err)
	}

	// Re-poll: now contracts are available → PROCEED.
	allContracts, _ := b.AllContracts(ctx)
	snap2 := BoardSnapshot{
		Contracts: allContracts,
		Completed: []string{"phase-a"},
	}

	poller := newDeterministicPoller()
	poller.responseFn = func(phaseID string, s BoardSnapshot) PollResult {
		if phaseID == "phase-b" && len(s.Contracts) > 0 {
			return PollResult{Decision: PollProceed}
		}
		return PollResult{Decision: PollNeedInfo, Reason: "still waiting"}
	}

	result, err := poller.Poll(ctx, "phase-b", snap2)
	if err != nil {
		t.Fatalf("re-poll: %v", err)
	}
	if result.Decision != PollProceed {
		t.Errorf("re-poll: expected PROCEED, got %s", result.Decision)
	}

	// Unblock.
	tracker.Unblock("phase-b")
	if tracker.Len() != 0 {
		t.Errorf("expected 0 blocked after unblock, got %d", tracker.Len())
	}
}

// TestIntegration_PushbackEscalation verifies: Phase B needs a contract that no
// in-progress phase can provide. After MaxRetries, an escalation is produced.
func TestIntegration_PushbackEscalation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testSQLiteBoard(t)

	tracker := NewBlockedTracker()
	handler := &PushbackHandler{Board: b, MaxRetries: 2}

	needInfoResult := PollResult{
		Decision:    PollNeedInfo,
		Reason:      "missing contract from unknown phase",
		MissingInfo: []string{"UnknownService interface"},
	}

	// No in-progress phases — no plausible producer.
	snap := BoardSnapshot{InProgress: []string{}}

	// Retry loop until escalation.
	for i := range 3 {
		tracker.Block("phase-b", needInfoResult)
		bp := tracker.Get("phase-b")

		action := handler.Handle(ctx, bp, snap.InProgress, snap)

		if i < 2 {
			if action != ActionRetry {
				t.Fatalf("retry %d: expected ActionRetry, got %s", i, action)
			}
		} else {
			if action != ActionEscalate {
				t.Fatalf("retry %d: expected ActionEscalate, got %s", i, action)
			}
		}
	}

	// Verify escalation message contains useful info.
	bp := tracker.Get("phase-b")
	msg := EscalationMessage(bp, handler.MaxRetries)
	if msg == "" {
		t.Fatal("escalation message should not be empty")
	}
	if !strings.Contains(msg, "phase-b") {
		t.Error("escalation message should contain phase ID")
	}
	if !strings.Contains(msg, "NEED_INFO") {
		t.Error("escalation message should contain decision")
	}

	// Verify board state can be set to human_decision.
	if err := b.SetPhaseState(ctx, "phase-b", StateHumanDecision); err != nil {
		t.Fatalf("SetPhaseState(human_decision): %v", err)
	}
	state, _ := b.GetPhaseState(ctx, "phase-b")
	if state != StateHumanDecision {
		t.Errorf("expected state %q, got %q", StateHumanDecision, state)
	}
}

// TestIntegration_FileClaimConflict verifies: Phase A claims a file, Phase B
// (parallel) tries to claim the same file → CONFLICT. A completes and releases
// claims, B re-polls → PROCEED.
func TestIntegration_FileClaimConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testSQLiteBoard(t)

	// Phase A claims a file.
	if err := b.ClaimFile(ctx, "internal/api/routes.go", "phase-a"); err != nil {
		t.Fatalf("ClaimFile(A): %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-a", StateRunning); err != nil {
		t.Fatalf("SetPhaseState(a): %v", err)
	}

	// Phase B tries to claim the same file — fails.
	err := b.ClaimFile(ctx, "internal/api/routes.go", "phase-b")
	if err == nil {
		t.Fatal("expected error on conflicting file claim")
	}

	// Set up blocked tracker and handler.
	tracker := NewBlockedTracker()
	handler := &PushbackHandler{Board: b, MaxRetries: 3}

	// Phase B polls — gets CONFLICT because of file claim.
	conflictResult := PollResult{
		Decision:     PollConflict,
		Reason:       "file internal/api/routes.go claimed by phase-a",
		ConflictWith: "phase-a",
	}

	// Build snapshot with file claims.
	snap := BoardSnapshot{
		FileClaims: map[string]string{"internal/api/routes.go": "phase-a"},
		InProgress: []string{"phase-a"},
	}

	tracker.Block("phase-b", conflictResult)
	bp := tracker.Get("phase-b")
	action := handler.Handle(ctx, bp, snap.InProgress, snap)

	// File-claim conflicts are transient → retry.
	if action != ActionRetry {
		t.Fatalf("expected ActionRetry for file-claim conflict, got %s", action)
	}

	// Phase A completes and releases claims.
	if err := b.ReleaseClaims(ctx, "phase-a"); err != nil {
		t.Fatalf("ReleaseClaims(A): %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-a", StateDone); err != nil {
		t.Fatalf("SetPhaseState(a, done): %v", err)
	}

	// Verify the file is now unclaimed.
	owner, err := b.FileOwner(ctx, "internal/api/routes.go")
	if err != nil {
		t.Fatalf("FileOwner: %v", err)
	}
	if owner != "" {
		t.Errorf("expected file unclaimed after release, got owner %q", owner)
	}

	// Phase B can now claim the file.
	if err := b.ClaimFile(ctx, "internal/api/routes.go", "phase-b"); err != nil {
		t.Fatalf("ClaimFile(B) after release: %v", err)
	}

	// Re-poll returns PROCEED.
	tracker.Unblock("phase-b")
	snap2 := BoardSnapshot{
		FileClaims: map[string]string{"internal/api/routes.go": "phase-b"},
		Completed:  []string{"phase-a"},
	}
	poller := newDeterministicPoller()
	poller.setDecision("phase-b", PollResult{Decision: PollProceed})
	result, err := poller.Poll(ctx, "phase-b", snap2)
	if err != nil {
		t.Fatalf("re-poll: %v", err)
	}
	if result.Decision != PollProceed {
		t.Errorf("re-poll: expected PROCEED, got %s", result.Decision)
	}
}

// TestIntegration_ContradictoryContracts verifies: Phase A and B publish
// contradictory Store interfaces. Phase C depends on both, polls → CONFLICT
// (ambiguous Store). Immediate escalation, no auto-retry.
func TestIntegration_ContradictoryContracts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testSQLiteBoard(t)

	// Phase A publishes Store with Get() string.
	if err := b.PublishContract(ctx, Contract{
		Producer: "phase-a", Kind: KindInterface, Name: "Store",
		Signature: "type Store interface { Get() string }", Package: "storage",
	}); err != nil {
		t.Fatalf("PublishContract(A): %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-a", StateDone); err != nil {
		t.Fatalf("SetPhaseState(a): %v", err)
	}

	// Phase B publishes Store with Get() (string, error) — contradictory!
	if err := b.PublishContract(ctx, Contract{
		Producer: "phase-b", Kind: KindInterface, Name: "Store",
		Signature: "type Store interface { Get() (string, error) }", Package: "storage",
	}); err != nil {
		t.Fatalf("PublishContract(B): %v", err)
	}
	if err := b.SetPhaseState(ctx, "phase-b", StateDone); err != nil {
		t.Fatalf("SetPhaseState(b): %v", err)
	}

	// Phase C polls and detects the conflict.
	conflictResult := PollResult{
		Decision:     PollConflict,
		Reason:       "contradictory Store interfaces from phase-a and phase-b",
		ConflictWith: "phase-a", // non-empty but NOT a file-claim conflict
	}

	tracker := NewBlockedTracker()
	handler := &PushbackHandler{Board: b, MaxRetries: 3}

	// Build snapshot — no file claims for the conflicting phase.
	snap := BoardSnapshot{
		Completed: []string{"phase-a", "phase-b"},
		// No file claims for phase-a → not a file-claim conflict.
		FileClaims: map[string]string{},
	}

	tracker.Block("phase-c", conflictResult)
	bp := tracker.Get("phase-c")
	action := handler.Handle(ctx, bp, snap.InProgress, snap)

	// Interface conflict → immediate escalation (no retry).
	if action != ActionEscalate {
		t.Fatalf("expected ActionEscalate for contradictory contracts, got %s", action)
	}

	// Verify both contracts exist in the board (the conflict is real data).
	allContracts, err := b.AllContracts(ctx)
	if err != nil {
		t.Fatalf("AllContracts: %v", err)
	}

	storeCount := 0
	for _, c := range allContracts {
		if c.Name == "Store" && c.Kind == KindInterface {
			storeCount++
		}
	}
	// Both contracts exist because they have different producers (upsert key
	// is producer+kind+name, and producers differ).
	if storeCount != 2 {
		t.Errorf("expected 2 Store interface contracts (contradictory), got %d", storeCount)
	}
}

// TestIntegration_ConcurrentCompletions verifies that multiple phases completing
// concurrently and publishing contracts does not corrupt the board.
func TestIntegration_ConcurrentCompletions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testSQLiteBoard(t)

	const numPhases = 5
	const contractsPerPhase = 3

	var wg sync.WaitGroup
	errs := make(chan error, numPhases*contractsPerPhase)

	for i := range numPhases {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			phaseID := "phase-" + itoa(idx)

			if err := b.SetPhaseState(ctx, phaseID, StateRunning); err != nil {
				errs <- err
				return
			}

			var contracts []Contract
			for j := range contractsPerPhase {
				contracts = append(contracts, Contract{
					Producer:  phaseID,
					Kind:      KindFunction,
					Name:      "Func" + itoa(idx) + "_" + itoa(j),
					Signature: "func() error",
					Package:   "pkg" + itoa(idx),
				})
			}
			if err := b.PublishContracts(ctx, contracts); err != nil {
				errs <- err
				return
			}

			if err := b.SetPhaseState(ctx, phaseID, StateDone); err != nil {
				errs <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent completion error: %v", err)
	}

	// Verify all contracts were published.
	allContracts, err := b.AllContracts(ctx)
	if err != nil {
		t.Fatalf("AllContracts: %v", err)
	}
	expected := numPhases * contractsPerPhase
	if len(allContracts) != expected {
		t.Errorf("expected %d contracts, got %d", expected, len(allContracts))
	}

	// Verify all phases are done.
	for i := range numPhases {
		state, err := b.GetPhaseState(ctx, "phase-"+itoa(i))
		if err != nil {
			t.Errorf("GetPhaseState(phase-%d): %v", i, err)
		}
		if state != StateDone {
			t.Errorf("phase-%d: expected state %q, got %q", i, StateDone, state)
		}
	}
}

// TestIntegration_CascadingUnblocks verifies that when a phase completes and
// releases file claims, multiple previously-blocked phases can all proceed.
func TestIntegration_CascadingUnblocks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testSQLiteBoard(t)

	// Phase A claims two files.
	if err := b.ClaimFile(ctx, "file1.go", "phase-a"); err != nil {
		t.Fatalf("ClaimFile(file1): %v", err)
	}
	if err := b.ClaimFile(ctx, "file2.go", "phase-a"); err != nil {
		t.Fatalf("ClaimFile(file2): %v", err)
	}

	// Phases B and C are blocked because of file claims.
	tracker := NewBlockedTracker()
	handler := &PushbackHandler{Board: b, MaxRetries: 3}

	snap := BoardSnapshot{
		FileClaims: map[string]string{
			"file1.go": "phase-a",
			"file2.go": "phase-a",
		},
		InProgress: []string{"phase-a"},
	}

	// Both B and C blocked on file claim conflict with phase-a.
	for _, phaseID := range []string{"phase-b", "phase-c"} {
		tracker.Block(phaseID, PollResult{
			Decision: PollConflict, Reason: "file conflict",
			ConflictWith: "phase-a",
		})
		bp := tracker.Get(phaseID)
		action := handler.Handle(ctx, bp, snap.InProgress, snap)
		if action != ActionRetry {
			t.Fatalf("expected ActionRetry for %s, got %s", phaseID, action)
		}
	}

	if tracker.Len() != 2 {
		t.Fatalf("expected 2 blocked phases, got %d", tracker.Len())
	}

	// Phase A completes and releases all claims.
	if err := b.ReleaseClaims(ctx, "phase-a"); err != nil {
		t.Fatalf("ReleaseClaims: %v", err)
	}

	// Re-evaluate: both B and C should now be able to proceed.
	poller := newDeterministicPoller()
	poller.setDecision("phase-b", PollResult{Decision: PollProceed})
	poller.setDecision("phase-c", PollResult{Decision: PollProceed})

	snap2 := BoardSnapshot{
		Completed:  []string{"phase-a"},
		FileClaims: map[string]string{},
	}

	for _, bp := range tracker.All() {
		result, err := poller.Poll(ctx, bp.PhaseID, snap2)
		if err != nil {
			t.Fatalf("re-poll %s: %v", bp.PhaseID, err)
		}
		if result.Decision != PollProceed {
			t.Errorf("re-poll %s: expected PROCEED, got %s", bp.PhaseID, result.Decision)
		}
		tracker.Unblock(bp.PhaseID)
	}

	if tracker.Len() != 0 {
		t.Errorf("expected 0 blocked phases after cascading unblock, got %d", tracker.Len())
	}
}
