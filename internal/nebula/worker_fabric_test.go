package nebula

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/tycho"
)

// --- mock types for fabric unit tests ---

// mockFabric satisfies fabric.Fabric with in-memory state and call tracking.
type mockFabric struct {
	mu             sync.Mutex
	states         map[string]string
	entanglements  []fabric.Entanglement
	claims         map[string]string // filepath -> ownerPhaseID
	setCalls       []string          // phase IDs passed to SetPhaseState
	releasedClaims []string          // phase IDs passed to ReleaseClaims
}

func newMockFabric() *mockFabric {
	return &mockFabric{
		states: make(map[string]string),
		claims: make(map[string]string),
	}
}

func (m *mockFabric) SetPhaseState(_ context.Context, phaseID, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[phaseID] = state
	m.setCalls = append(m.setCalls, phaseID+":"+state)
	return nil
}

func (m *mockFabric) GetPhaseState(_ context.Context, phaseID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[phaseID], nil
}

func (m *mockFabric) PublishEntanglement(_ context.Context, e fabric.Entanglement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entanglements = append(m.entanglements, e)
	return nil
}

func (m *mockFabric) PublishEntanglements(_ context.Context, es []fabric.Entanglement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entanglements = append(m.entanglements, es...)
	return nil
}

func (m *mockFabric) EntanglementsFor(_ context.Context, phaseID string) ([]fabric.Entanglement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []fabric.Entanglement
	for _, e := range m.entanglements {
		if e.Producer == phaseID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockFabric) AllEntanglements(_ context.Context) ([]fabric.Entanglement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]fabric.Entanglement, len(m.entanglements))
	copy(cp, m.entanglements)
	return cp, nil
}

func (m *mockFabric) ClaimFile(_ context.Context, filepath, ownerPhaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claims[filepath] = ownerPhaseID
	return nil
}

func (m *mockFabric) ReleaseClaims(_ context.Context, ownerPhaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releasedClaims = append(m.releasedClaims, ownerPhaseID)
	for fp, owner := range m.claims {
		if owner == ownerPhaseID {
			delete(m.claims, fp)
		}
	}
	return nil
}

func (m *mockFabric) ReleaseFileClaim(_ context.Context, filepath, ownerPhaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claims[filepath] == ownerPhaseID {
		delete(m.claims, filepath)
	}
	return nil
}

func (m *mockFabric) AllPhaseStates(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]string, len(m.states))
	for k, v := range m.states {
		cp[k] = v
	}
	return cp, nil
}

func (m *mockFabric) FileOwner(_ context.Context, filepath string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.claims[filepath], nil
}

func (m *mockFabric) ClaimsFor(_ context.Context, phaseID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []string
	for fp, owner := range m.claims {
		if owner == phaseID {
			result = append(result, fp)
		}
	}
	return result, nil
}

func (m *mockFabric) AllClaims(_ context.Context) ([]fabric.Claim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var claims []fabric.Claim
	for fp, owner := range m.claims {
		claims = append(claims, fabric.Claim{Filepath: fp, OwnerTask: owner})
	}
	return claims, nil
}

func (m *mockFabric) PostDiscovery(_ context.Context, _ fabric.Discovery) (int64, error) {
	return 0, nil
}
func (m *mockFabric) Discoveries(_ context.Context, _ string) ([]fabric.Discovery, error) {
	return nil, nil
}
func (m *mockFabric) AllDiscoveries(_ context.Context) ([]fabric.Discovery, error) {
	return nil, nil
}
func (m *mockFabric) ResolveDiscovery(_ context.Context, _ int64) error { return nil }
func (m *mockFabric) UnresolvedDiscoveries(_ context.Context) ([]fabric.Discovery, error) {
	return nil, nil
}
func (m *mockFabric) EmitPulse(_ context.Context, _ fabric.Pulse) error             { return nil }
func (m *mockFabric) PulsesFor(_ context.Context, _ string) ([]fabric.Pulse, error) { return nil, nil }
func (m *mockFabric) AllPulses(_ context.Context) ([]fabric.Pulse, error)           { return nil, nil }
func (m *mockFabric) PurgeAll(_ context.Context) error                              { return nil }
func (m *mockFabric) Close() error                                                  { return nil }

// mockPoller satisfies fabric.Poller with canned per-phase responses.
type mockPoller struct {
	mu        sync.Mutex
	decisions map[string]fabric.PollResult
	pollCount map[string]int
}

func newMockPoller() *mockPoller {
	return &mockPoller{
		decisions: make(map[string]fabric.PollResult),
		pollCount: make(map[string]int),
	}
}

func (p *mockPoller) setDecision(phaseID string, r fabric.PollResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decisions[phaseID] = r
}

func (p *mockPoller) getPollCount(phaseID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pollCount[phaseID]
}

func (p *mockPoller) Poll(_ context.Context, phaseID string, _ fabric.FabricSnapshot) (fabric.PollResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pollCount[phaseID]++
	if r, ok := p.decisions[phaseID]; ok {
		return r, nil
	}
	return fabric.PollResult{Decision: fabric.PollProceed}, nil
}

// --- helpers ---

// newFabricTestWorkerGroup creates a minimal WorkerGroup wired for fabric unit tests.
// It sets up tracker, blockedTracker, pushbackHandler, tychoScheduler, and a log buffer.
func newFabricTestWorkerGroup(phases []PhaseSpec) (*WorkerGroup, *mockFabric, *mockPoller, *bytes.Buffer) {
	mf := newMockFabric()
	mp := newMockPoller()
	var logBuf bytes.Buffer

	state := &State{
		Version: 1,
		Phases:  make(map[string]*PhaseState),
	}

	neb := &Nebula{
		Phases: phases,
	}

	wg := NewWorkerGroup(neb, state,
		WithFabric(mf),
		WithPoller(mp),
		WithLogger(&logBuf),
	)

	// Initialize collaborators that Run() would normally create.
	wg.tracker = NewPhaseTracker(phases, state)
	bt := fabric.NewBlockedTracker()
	ph := &fabric.PushbackHandler{Fabric: mf}
	wg.blockedTracker = bt
	wg.pushbackHandler = ph
	wg.tychoScheduler = &tycho.Scheduler{
		Fabric:   mf,
		Poller:   mp,
		Blocked:  bt,
		Pushback: ph,
		Logger:   &logBuf,
	}

	return wg, mf, mp, &logBuf
}

// --- fabricBlocked tests ---

func TestFabricBlocked(t *testing.T) {
	t.Parallel()

	t.Run("returns 0 when tracker is nil", func(t *testing.T) {
		t.Parallel()
		wg := &WorkerGroup{}
		if got := wg.fabricBlocked(); got != 0 {
			t.Errorf("fabricBlocked() = %d, want 0 (nil tracker)", got)
		}
	})

	t.Run("returns correct count when phases are blocked", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}, {ID: "b"}, {ID: "c"}}
		wg, _, _, _ := newFabricTestWorkerGroup(phases)

		// No blocked phases initially.
		if got := wg.fabricBlocked(); got != 0 {
			t.Errorf("fabricBlocked() = %d, want 0 (no blocked)", got)
		}

		// Block two phases.
		wg.blockedTracker.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "missing X"})
		wg.blockedTracker.Block("b", fabric.PollResult{Decision: fabric.PollConflict, Reason: "conflict Y"})

		if got := wg.fabricBlocked(); got != 2 {
			t.Errorf("fabricBlocked() = %d, want 2", got)
		}

		// Unblock one.
		wg.blockedTracker.Unblock("a")
		if got := wg.fabricBlocked(); got != 1 {
			t.Errorf("fabricBlocked() = %d, want 1 after unblock", got)
		}
	})
}

// --- pollEligible tests ---

func TestPollEligible(t *testing.T) {
	t.Parallel()

	t.Run("phases that poll PROCEED are returned", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}, {ID: "b"}, {ID: "c"}}
		wg, _, mp, _ := newFabricTestWorkerGroup(phases)

		// a and c proceed, b needs info.
		mp.setDecision("b", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing type X",
		})

		eligible := wg.pollEligible(context.Background(), []string{"a", "b", "c"})

		got := make(map[string]bool)
		for _, id := range eligible {
			got[id] = true
		}
		if !got["a"] || !got["c"] {
			t.Errorf("expected a and c to proceed, got %v", eligible)
		}
		if got["b"] {
			t.Error("expected b to be blocked, but it proceeded")
		}
	})

	t.Run("blocked phases are skipped without re-polling", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}, {ID: "b"}}
		wg, _, mp, _ := newFabricTestWorkerGroup(phases)

		// Pre-block "a".
		wg.blockedTracker.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo})

		eligible := wg.pollEligible(context.Background(), []string{"a", "b"})

		got := make(map[string]bool)
		for _, id := range eligible {
			got[id] = true
		}
		if got["a"] {
			t.Error("expected already-blocked a to be skipped")
		}
		if !got["b"] {
			t.Error("expected b to proceed")
		}
		// a should not have been polled again.
		if mp.getPollCount("a") != 0 {
			t.Errorf("expected 0 polls for already-blocked a, got %d", mp.getPollCount("a"))
		}
	})

	t.Run("overridden phases skip polling and proceed", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		wg, _, mp, _ := newFabricTestWorkerGroup(phases)

		wg.blockedTracker.Override("a")

		eligible := wg.pollEligible(context.Background(), []string{"a"})
		if len(eligible) != 1 || eligible[0] != "a" {
			t.Errorf("expected overridden a to proceed, got %v", eligible)
		}
		if mp.getPollCount("a") != 0 {
			t.Errorf("expected 0 polls for overridden a, got %d", mp.getPollCount("a"))
		}
	})
}

// --- Tycho handlePollBlock tests (via the Tycho scheduler directly) ---

func TestTychoHandlePollBlock(t *testing.T) {
	t.Parallel()

	t.Run("retry action keeps phase blocked", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		wg, _, _, logBuf := newFabricTestWorkerGroup(phases)

		result := fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing interface Foo",
		}
		snap := fabric.FabricSnapshot{
			InProgress: []string{"producer-phase"},
		}

		wg.tychoScheduler.HandlePollBlock(context.Background(), "a", result, snap)

		bp := wg.blockedTracker.Get("a")
		if bp == nil {
			t.Fatal("expected a to be tracked as blocked")
		}
		if bp.RetryCount != 0 {
			t.Errorf("expected retry count 0 on first block, got %d", bp.RetryCount)
		}
		if !strings.Contains(logBuf.String(), "blocked") {
			t.Errorf("expected log to mention 'blocked', got: %s", logBuf.String())
		}
	})

	t.Run("escalate action marks phase as failed", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		wg, mf, _, _ := newFabricTestWorkerGroup(phases)

		// Exhaust retries so pushback handler escalates.
		wg.pushbackHandler.MaxRetries = 1
		wg.tychoScheduler.Pushback.MaxRetries = 1

		result := fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing dep",
		}
		snap := fabric.FabricSnapshot{}

		// First call: retry count=0, should retry.
		wg.tychoScheduler.HandlePollBlock(context.Background(), "a", result, snap)
		if wg.blockedTracker.Get("a") == nil {
			t.Fatal("expected a to be blocked after first handlePollBlock")
		}

		// Second call: retry count increments to 1 (>= MaxRetries=1), should escalate.
		wg.tychoScheduler.HandlePollBlock(context.Background(), "a", result, snap)

		// After escalation, phase should be unblocked from the blocked tracker.
		if wg.blockedTracker.Get("a") != nil {
			t.Error("expected a to be unblocked after escalation")
		}

		// Fabric state should be set to human_decision.
		state, _ := mf.GetPhaseState(context.Background(), "a")
		if state != fabric.StateHumanDecision {
			t.Errorf("expected fabric state %q, got %q", fabric.StateHumanDecision, state)
		}
	})

	t.Run("proceed action unblocks and overrides", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		wg, _, _, _ := newFabricTestWorkerGroup(phases)

		// A conflict with no file claims triggers ActionEscalate in the default
		// handler, so we use PollProceed-like behavior via a default decision.
		// The PushbackHandler returns ActionProceed for unknown decision types.
		result := fabric.PollResult{
			Decision: fabric.PollDecision("UNKNOWN"),
			Reason:   "something odd",
		}
		snap := fabric.FabricSnapshot{}

		wg.tychoScheduler.HandlePollBlock(context.Background(), "a", result, snap)

		if wg.blockedTracker.Get("a") != nil {
			t.Error("expected a to be unblocked after proceed action")
		}
		if !wg.blockedTracker.IsOverridden("a") {
			t.Error("expected a to be marked as overridden")
		}
	})
}

// --- fabricPhaseComplete tests ---

func TestFabricPhaseComplete(t *testing.T) {
	t.Parallel()

	t.Run("nil fabric is a no-op", func(t *testing.T) {
		t.Parallel()
		wg := &WorkerGroup{}
		// Should not panic.
		wg.fabricPhaseComplete(context.Background(), "a", nil)
	})

	t.Run("sets done state and releases claims", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		wg, mf, _, _ := newFabricTestWorkerGroup(phases)

		// Pre-claim a file.
		_ = mf.ClaimFile(context.Background(), "main.go", "a")

		wg.fabricPhaseComplete(context.Background(), "a", nil)

		state, _ := mf.GetPhaseState(context.Background(), "a")
		if state != fabric.StateDone {
			t.Errorf("expected fabric state %q, got %q", fabric.StateDone, state)
		}

		// Claims should be released.
		owner, _ := mf.FileOwner(context.Background(), "main.go")
		if owner != "" {
			t.Errorf("expected claim released, but main.go still owned by %q", owner)
		}

		found := false
		for _, id := range mf.releasedClaims {
			if id == "a" {
				found = true
			}
		}
		if !found {
			t.Error("expected ReleaseClaims to be called for phase a")
		}
	})

	t.Run("does not call publisher when Publisher is nil", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		wg, mf, _, _ := newFabricTestWorkerGroup(phases)

		result := &PhaseRunnerResult{
			BaseCommitSHA:  "abc123",
			FinalCommitSHA: "def456",
		}

		// Publisher is nil — should still set done state without error.
		wg.fabricPhaseComplete(context.Background(), "a", result)

		state, _ := mf.GetPhaseState(context.Background(), "a")
		if state != fabric.StateDone {
			t.Errorf("expected fabric state %q, got %q", fabric.StateDone, state)
		}
	})
}

// --- reevaluateBlocked tests ---

func TestReevaluateBlocked(t *testing.T) {
	t.Parallel()

	t.Run("nil scheduler is a no-op", func(t *testing.T) {
		t.Parallel()
		wg := &WorkerGroup{}
		// Should not panic.
		wg.reevaluateBlocked(context.Background())
	})

	t.Run("empty tracker is a no-op", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		wg, _, mp, _ := newFabricTestWorkerGroup(phases)

		wg.reevaluateBlocked(context.Background())

		if mp.getPollCount("a") != 0 {
			t.Error("expected no polling when no phases are blocked")
		}
	})

	t.Run("unblocks phases that poll PROCEED", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}, {ID: "b"}}
		wg, mf, mp, logBuf := newFabricTestWorkerGroup(phases)

		// Block both phases.
		wg.blockedTracker.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "need X"})
		wg.blockedTracker.Block("b", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "need Y"})

		// On re-evaluation, a proceeds but b is still blocked.
		mp.setDecision("a", fabric.PollResult{Decision: fabric.PollProceed})
		mp.setDecision("b", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "still need Y"})

		wg.reevaluateBlocked(context.Background())

		if wg.blockedTracker.Get("a") != nil {
			t.Error("expected a to be unblocked after re-evaluation")
		}
		if wg.blockedTracker.Get("b") == nil {
			t.Error("expected b to remain blocked")
		}

		// Fabric state for a should be set to scanning.
		state, _ := mf.GetPhaseState(context.Background(), "a")
		if state != fabric.StateScanning {
			t.Errorf("expected fabric state %q for a, got %q", fabric.StateScanning, state)
		}

		if !strings.Contains(logBuf.String(), "unblocked") {
			t.Errorf("expected log to mention 'unblocked', got: %s", logBuf.String())
		}
	})

	t.Run("still-blocked phases go through pushback handler", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		wg, _, mp, _ := newFabricTestWorkerGroup(phases)

		wg.blockedTracker.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "need X"})

		// Still blocked on re-evaluation.
		mp.setDecision("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "still need X"})

		wg.reevaluateBlocked(context.Background())

		bp := wg.blockedTracker.Get("a")
		if bp == nil {
			t.Fatal("expected a to still be blocked")
		}
		// Retry count should have been incremented (Block was called again
		// internally by handlePollBlock for the existing entry).
		if bp.RetryCount < 1 {
			t.Errorf("expected retry count >= 1, got %d", bp.RetryCount)
		}
	})
}

// --- escalateAllBlocked tests ---

func TestEscalateAllBlocked(t *testing.T) {
	t.Parallel()

	t.Run("nil scheduler is a no-op", func(t *testing.T) {
		t.Parallel()
		wg := &WorkerGroup{}
		// Should not panic.
		wg.escalateAllBlocked(context.Background())
	})

	t.Run("escalates all blocked phases", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}, {ID: "b"}}
		wg, mf, _, logBuf := newFabricTestWorkerGroup(phases)

		wg.blockedTracker.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "need X"})
		wg.blockedTracker.Block("b", fabric.PollResult{Decision: fabric.PollConflict, Reason: "conflict Z"})

		wg.escalateAllBlocked(context.Background())

		// Both should be unblocked from the tracker (escalatePhase calls Unblock).
		if wg.blockedTracker.Get("a") != nil {
			t.Error("expected a to be unblocked after escalation")
		}
		if wg.blockedTracker.Get("b") != nil {
			t.Error("expected b to be unblocked after escalation")
		}

		// Both should have fabric state set to human_decision.
		stateA, _ := mf.GetPhaseState(context.Background(), "a")
		stateB, _ := mf.GetPhaseState(context.Background(), "b")
		if stateA != fabric.StateHumanDecision {
			t.Errorf("expected fabric state %q for a, got %q", fabric.StateHumanDecision, stateA)
		}
		if stateB != fabric.StateHumanDecision {
			t.Errorf("expected fabric state %q for b, got %q", fabric.StateHumanDecision, stateB)
		}

		// Both should be marked as failed in the tracker.
		if !wg.tracker.Failed()["a"] || !wg.tracker.Failed()["b"] {
			t.Error("expected both phases to be marked as failed")
		}
		if !wg.tracker.Done()["a"] || !wg.tracker.Done()["b"] {
			t.Error("expected both phases to be marked as done")
		}

		// Gate signals should be emitted.
		wg.mu.Lock()
		signals := wg.gateSignals
		wg.mu.Unlock()
		if len(signals) < 2 {
			t.Errorf("expected at least 2 gate signals, got %d", len(signals))
		}

		// Log should contain escalation messages.
		if !strings.Contains(logBuf.String(), "Fabric Escalation") {
			t.Errorf("expected escalation log output, got: %s", logBuf.String())
		}
	})
}

// --- workerEligibleResolver tests ---

func TestWorkerEligibleResolver(t *testing.T) {
	t.Parallel()

	t.Run("resolves ready tasks from DAG", func(t *testing.T) {
		t.Parallel()
		// a has no deps, b depends on a.
		phases := []PhaseSpec{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a"}},
		}
		state := &State{Version: 1, Phases: make(map[string]*PhaseState)}
		neb := &Nebula{Phases: phases}
		wg := NewWorkerGroup(neb, state, WithLogger(&bytes.Buffer{}))
		wg.tracker = NewPhaseTracker(phases, state)

		scheduler, err := NewScheduler(phases)
		if err != nil {
			t.Fatalf("NewScheduler: %v", err)
		}
		resolver := &workerEligibleResolver{wg: wg, scheduler: scheduler}

		// Initially only "a" should be eligible (b depends on a).
		eligible := resolver.ResolveEligible()
		if len(eligible) != 1 || eligible[0] != "a" {
			t.Errorf("expected [a] eligible, got %v", eligible)
		}

		// After marking "a" as done, "b" should become eligible.
		wg.tracker.Done()["a"] = true
		eligible = resolver.ResolveEligible()
		if len(eligible) != 1 || eligible[0] != "b" {
			t.Errorf("expected [b] eligible after a is done, got %v", eligible)
		}
	})

	t.Run("excludes in-flight tasks", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}, {ID: "b"}}
		state := &State{Version: 1, Phases: make(map[string]*PhaseState)}
		neb := &Nebula{Phases: phases}
		wg := NewWorkerGroup(neb, state, WithLogger(&bytes.Buffer{}))
		wg.tracker = NewPhaseTracker(phases, state)

		scheduler, err := NewScheduler(phases)
		if err != nil {
			t.Fatalf("NewScheduler: %v", err)
		}
		resolver := &workerEligibleResolver{wg: wg, scheduler: scheduler}

		// Mark "a" as in-flight.
		wg.tracker.InFlight()["a"] = true

		eligible := resolver.ResolveEligible()
		got := make(map[string]bool)
		for _, id := range eligible {
			got[id] = true
		}
		if got["a"] {
			t.Error("expected in-flight a to be excluded")
		}
		if !got["b"] {
			t.Error("expected b to be eligible")
		}
	})

	t.Run("AnyInFlight reports correctly", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{{ID: "a"}}
		state := &State{Version: 1, Phases: make(map[string]*PhaseState)}
		neb := &Nebula{Phases: phases}
		wg := NewWorkerGroup(neb, state, WithLogger(&bytes.Buffer{}))
		wg.tracker = NewPhaseTracker(phases, state)

		scheduler, err := NewScheduler(phases)
		if err != nil {
			t.Fatalf("NewScheduler: %v", err)
		}
		resolver := &workerEligibleResolver{wg: wg, scheduler: scheduler}

		if resolver.AnyInFlight() {
			t.Error("expected no in-flight initially")
		}

		wg.tracker.InFlight()["a"] = true
		if !resolver.AnyInFlight() {
			t.Error("expected in-flight after marking a")
		}
	})

	t.Run("excludes tasks with failed dependencies", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a"}},
		}
		state := &State{Version: 1, Phases: make(map[string]*PhaseState)}
		neb := &Nebula{Phases: phases}
		wg := NewWorkerGroup(neb, state, WithLogger(&bytes.Buffer{}))
		wg.tracker = NewPhaseTracker(phases, state)

		scheduler, err := NewScheduler(phases)
		if err != nil {
			t.Fatalf("NewScheduler: %v", err)
		}
		resolver := &workerEligibleResolver{wg: wg, scheduler: scheduler}

		// Mark "a" as done and failed.
		wg.tracker.Done()["a"] = true
		wg.tracker.Failed()["a"] = true

		eligible := resolver.ResolveEligible()
		for _, id := range eligible {
			if id == "b" {
				t.Error("expected b to be excluded (failed dependency a)")
			}
		}
	})
}
