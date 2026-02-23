package tycho

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// --- mock types ---

// mockFabric satisfies fabric.Fabric with in-memory state and call tracking.
type mockFabric struct {
	mu             sync.Mutex
	states         map[string]string
	entanglements  []fabric.Entanglement
	claims         map[string]fabric.Claim // filepath -> Claim
	setCalls       []string                // phase IDs passed to SetPhaseState
	releasedClaims []string                // phase IDs passed to ReleaseClaims
	discoveries    []fabric.Discovery
}

func newMockFabric() *mockFabric {
	return &mockFabric{
		states: make(map[string]string),
		claims: make(map[string]fabric.Claim),
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

func (m *mockFabric) AllPhaseStates(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]string, len(m.states))
	for k, v := range m.states {
		cp[k] = v
	}
	return cp, nil
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
	m.claims[filepath] = fabric.Claim{
		Filepath:  filepath,
		OwnerTask: ownerPhaseID,
		ClaimedAt: time.Now(),
	}
	return nil
}

func (m *mockFabric) ReleaseClaims(_ context.Context, ownerPhaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releasedClaims = append(m.releasedClaims, ownerPhaseID)
	for fp, c := range m.claims {
		if c.OwnerTask == ownerPhaseID {
			delete(m.claims, fp)
		}
	}
	return nil
}

func (m *mockFabric) ReleaseFileClaim(_ context.Context, filepath, ownerPhaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.claims[filepath]; ok && c.OwnerTask == ownerPhaseID {
		delete(m.claims, filepath)
	}
	return nil
}

func (m *mockFabric) FileOwner(_ context.Context, filepath string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.claims[filepath]; ok {
		return c.OwnerTask, nil
	}
	return "", nil
}

func (m *mockFabric) ClaimsFor(_ context.Context, phaseID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []string
	for fp, c := range m.claims {
		if c.OwnerTask == phaseID {
			result = append(result, fp)
		}
	}
	return result, nil
}

func (m *mockFabric) AllClaims(_ context.Context) ([]fabric.Claim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var claims []fabric.Claim
	for _, c := range m.claims {
		claims = append(claims, c)
	}
	return claims, nil
}

func (m *mockFabric) PostDiscovery(_ context.Context, d fabric.Discovery) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.discoveries = append(m.discoveries, d)
	return int64(len(m.discoveries)), nil
}

func (m *mockFabric) Discoveries(_ context.Context, _ string) ([]fabric.Discovery, error) {
	return nil, nil
}

func (m *mockFabric) AllDiscoveries(_ context.Context) ([]fabric.Discovery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]fabric.Discovery, len(m.discoveries))
	copy(cp, m.discoveries)
	return cp, nil
}

func (m *mockFabric) ResolveDiscovery(_ context.Context, _ int64) error { return nil }

func (m *mockFabric) UnresolvedDiscoveries(_ context.Context) ([]fabric.Discovery, error) {
	return nil, nil
}

func (m *mockFabric) AddBead(_ context.Context, _ fabric.Bead) error              { return nil }
func (m *mockFabric) BeadsFor(_ context.Context, _ string) ([]fabric.Bead, error) { return nil, nil }
func (m *mockFabric) Close() error                                                { return nil }

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

// mockSnapshotBuilder returns a fixed snapshot.
type mockSnapshotBuilder struct {
	snap fabric.FabricSnapshot
	err  error
}

func (b *mockSnapshotBuilder) BuildSnapshot(_ context.Context) (fabric.FabricSnapshot, error) {
	return b.snap, b.err
}

// --- helpers ---

func newTestScheduler() (*Scheduler, *mockFabric, *mockPoller, *bytes.Buffer) {
	mf := newMockFabric()
	mp := newMockPoller()
	var logBuf bytes.Buffer

	s := &Scheduler{
		Fabric:   mf,
		Poller:   mp,
		Blocked:  fabric.NewBlockedTracker(),
		Pushback: &fabric.PushbackHandler{Fabric: mf},
		Logger:   &logBuf,
	}
	return s, mf, mp, &logBuf
}

// --- Scan tests ---

func TestScan(t *testing.T) {
	t.Parallel()

	t.Run("phases that poll PROCEED are returned", func(t *testing.T) {
		t.Parallel()
		s, _, mp, _ := newTestScheduler()

		mp.setDecision("b", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing type X",
		})

		sb := &mockSnapshotBuilder{snap: fabric.FabricSnapshot{}}
		proceed, err := s.Scan(context.Background(), []string{"a", "b", "c"}, sb)
		if err != nil {
			t.Fatalf("Scan error: %v", err)
		}

		got := make(map[string]bool)
		for _, id := range proceed {
			got[id] = true
		}
		if !got["a"] || !got["c"] {
			t.Errorf("expected a and c to proceed, got %v", proceed)
		}
		if got["b"] {
			t.Error("expected b to be blocked, but it proceeded")
		}
	})

	t.Run("blocked phases are skipped without re-polling", func(t *testing.T) {
		t.Parallel()
		s, _, mp, _ := newTestScheduler()

		// Pre-block "a".
		s.Blocked.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo})

		sb := &mockSnapshotBuilder{snap: fabric.FabricSnapshot{}}
		proceed, err := s.Scan(context.Background(), []string{"a", "b"}, sb)
		if err != nil {
			t.Fatalf("Scan error: %v", err)
		}

		got := make(map[string]bool)
		for _, id := range proceed {
			got[id] = true
		}
		if got["a"] {
			t.Error("expected already-blocked a to be skipped")
		}
		if !got["b"] {
			t.Error("expected b to proceed")
		}
		if mp.getPollCount("a") != 0 {
			t.Errorf("expected 0 polls for already-blocked a, got %d", mp.getPollCount("a"))
		}
	})

	t.Run("overridden phases skip polling and proceed", func(t *testing.T) {
		t.Parallel()
		s, _, mp, _ := newTestScheduler()

		s.Blocked.Override("a")

		sb := &mockSnapshotBuilder{snap: fabric.FabricSnapshot{}}
		proceed, err := s.Scan(context.Background(), []string{"a"}, sb)
		if err != nil {
			t.Fatalf("Scan error: %v", err)
		}

		if len(proceed) != 1 || proceed[0] != "a" {
			t.Errorf("expected overridden a to proceed, got %v", proceed)
		}
		if mp.getPollCount("a") != 0 {
			t.Errorf("expected 0 polls for overridden a, got %d", mp.getPollCount("a"))
		}
	})

	t.Run("snapshot error returns all eligible", func(t *testing.T) {
		t.Parallel()
		s, _, _, logBuf := newTestScheduler()

		sb := &mockSnapshotBuilder{err: context.DeadlineExceeded}
		proceed, err := s.Scan(context.Background(), []string{"a", "b"}, sb)
		if err != nil {
			t.Fatalf("Scan error: %v", err)
		}

		if len(proceed) != 2 {
			t.Errorf("expected all 2 phases on snapshot error, got %d", len(proceed))
		}
		if !strings.Contains(logBuf.String(), "warning") {
			t.Error("expected warning in log on snapshot error")
		}
	})
}

// --- handlePollBlock tests ---

func TestHandlePollBlock(t *testing.T) {
	t.Parallel()

	t.Run("retry action keeps phase blocked", func(t *testing.T) {
		t.Parallel()
		s, _, _, logBuf := newTestScheduler()

		result := fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing interface Foo",
		}
		snap := fabric.FabricSnapshot{
			InProgress: []string{"producer-phase"},
		}

		s.HandlePollBlock(context.Background(), "a", result, snap)

		bp := s.Blocked.Get("a")
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

	t.Run("escalate action logs escalation", func(t *testing.T) {
		t.Parallel()
		s, mf, _, _ := newTestScheduler()

		// Exhaust retries so pushback handler escalates.
		s.Pushback.MaxRetries = 1

		result := fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing dep",
		}
		snap := fabric.FabricSnapshot{}

		// First call: retry count=0, should retry.
		s.HandlePollBlock(context.Background(), "a", result, snap)
		if s.Blocked.Get("a") == nil {
			t.Fatal("expected a to be blocked after first handlePollBlock")
		}

		// Second call: retry count increments to 1, should escalate.
		s.HandlePollBlock(context.Background(), "a", result, snap)

		// After escalation, phase should be unblocked from the blocked tracker.
		if s.Blocked.Get("a") != nil {
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
		s, _, _, _ := newTestScheduler()

		result := fabric.PollResult{
			Decision: fabric.PollDecision("UNKNOWN"),
			Reason:   "something odd",
		}
		snap := fabric.FabricSnapshot{}

		s.HandlePollBlock(context.Background(), "a", result, snap)

		if s.Blocked.Get("a") != nil {
			t.Error("expected a to be unblocked after proceed action")
		}
		if !s.Blocked.IsOverridden("a") {
			t.Error("expected a to be marked as overridden")
		}
	})
}

// --- Reevaluate tests ---

func TestReevaluate(t *testing.T) {
	t.Parallel()

	t.Run("nil tracker returns nil", func(t *testing.T) {
		t.Parallel()
		s := &Scheduler{Blocked: nil}
		unblocked, err := s.Reevaluate(context.Background(), &mockSnapshotBuilder{})
		if err != nil {
			t.Fatalf("Reevaluate error: %v", err)
		}
		if unblocked != nil {
			t.Errorf("expected nil unblocked, got %v", unblocked)
		}
	})

	t.Run("empty tracker returns nil", func(t *testing.T) {
		t.Parallel()
		s, _, mp, _ := newTestScheduler()
		sb := &mockSnapshotBuilder{snap: fabric.FabricSnapshot{}}

		unblocked, err := s.Reevaluate(context.Background(), sb)
		if err != nil {
			t.Fatalf("Reevaluate error: %v", err)
		}
		if unblocked != nil {
			t.Errorf("expected nil unblocked for empty tracker, got %v", unblocked)
		}
		if mp.getPollCount("a") != 0 {
			t.Error("expected no polling when no phases are blocked")
		}
	})

	t.Run("unblocks phases that poll PROCEED", func(t *testing.T) {
		t.Parallel()
		s, mf, mp, logBuf := newTestScheduler()

		// Block both phases.
		s.Blocked.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "need X"})
		s.Blocked.Block("b", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "need Y"})

		// On re-evaluation, a proceeds but b is still blocked.
		mp.setDecision("a", fabric.PollResult{Decision: fabric.PollProceed})
		mp.setDecision("b", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "still need Y"})

		sb := &mockSnapshotBuilder{snap: fabric.FabricSnapshot{}}
		unblocked, err := s.Reevaluate(context.Background(), sb)
		if err != nil {
			t.Fatalf("Reevaluate error: %v", err)
		}

		if len(unblocked) != 1 || unblocked[0] != "a" {
			t.Errorf("expected [a] unblocked, got %v", unblocked)
		}

		if s.Blocked.Get("a") != nil {
			t.Error("expected a to be unblocked after re-evaluation")
		}
		if s.Blocked.Get("b") == nil {
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
		s, _, mp, _ := newTestScheduler()

		s.Blocked.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "need X"})
		mp.setDecision("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "still need X"})

		sb := &mockSnapshotBuilder{snap: fabric.FabricSnapshot{}}
		_, err := s.Reevaluate(context.Background(), sb)
		if err != nil {
			t.Fatalf("Reevaluate error: %v", err)
		}

		bp := s.Blocked.Get("a")
		if bp == nil {
			t.Fatal("expected a to still be blocked")
		}
		if bp.RetryCount < 1 {
			t.Errorf("expected retry count >= 1, got %d", bp.RetryCount)
		}
	})
}

// --- BlockedCount tests ---

func TestBlockedCount(t *testing.T) {
	t.Parallel()

	t.Run("returns 0 when tracker is nil", func(t *testing.T) {
		t.Parallel()
		s := &Scheduler{}
		if got := s.BlockedCount(); got != 0 {
			t.Errorf("BlockedCount() = %d, want 0 (nil tracker)", got)
		}
	})

	t.Run("returns correct count", func(t *testing.T) {
		t.Parallel()
		s, _, _, _ := newTestScheduler()

		if got := s.BlockedCount(); got != 0 {
			t.Errorf("BlockedCount() = %d, want 0 (empty)", got)
		}

		s.Blocked.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo})
		s.Blocked.Block("b", fabric.PollResult{Decision: fabric.PollConflict})

		if got := s.BlockedCount(); got != 2 {
			t.Errorf("BlockedCount() = %d, want 2", got)
		}

		s.Blocked.Unblock("a")
		if got := s.BlockedCount(); got != 1 {
			t.Errorf("BlockedCount() = %d, want 1 after unblock", got)
		}
	})
}

// --- StaleCheck tests ---

func TestStaleCheck(t *testing.T) {
	t.Parallel()

	t.Run("detects stale claims", func(t *testing.T) {
		t.Parallel()
		s, mf, _, _ := newTestScheduler()

		// Add a claim with an old timestamp.
		mf.mu.Lock()
		mf.claims["old.go"] = fabric.Claim{
			Filepath:  "old.go",
			OwnerTask: "stale-task",
			ClaimedAt: time.Now().Add(-10 * time.Minute),
		}
		mf.states["stale-task"] = fabric.StateBlocked
		mf.mu.Unlock()

		items, err := s.StaleCheck(context.Background(), 5*time.Minute, 30*time.Minute)
		if err != nil {
			t.Fatalf("StaleCheck error: %v", err)
		}

		if len(items) != 1 {
			t.Fatalf("expected 1 stale item, got %d", len(items))
		}
		if items[0].Kind != "claim" {
			t.Errorf("expected kind 'claim', got %q", items[0].Kind)
		}
		if items[0].ID != "old.go" {
			t.Errorf("expected ID 'old.go', got %q", items[0].ID)
		}
	})

	t.Run("running claims are not stale", func(t *testing.T) {
		t.Parallel()
		s, mf, _, _ := newTestScheduler()

		mf.mu.Lock()
		mf.claims["active.go"] = fabric.Claim{
			Filepath:  "active.go",
			OwnerTask: "running-task",
			ClaimedAt: time.Now().Add(-10 * time.Minute),
		}
		mf.states["running-task"] = fabric.StateRunning
		mf.mu.Unlock()

		items, err := s.StaleCheck(context.Background(), 5*time.Minute, 30*time.Minute)
		if err != nil {
			t.Fatalf("StaleCheck error: %v", err)
		}

		if len(items) != 0 {
			t.Errorf("expected 0 stale items for running task, got %d", len(items))
		}
	})

	t.Run("detects stale blocked tasks", func(t *testing.T) {
		t.Parallel()
		s, _, _, _ := newTestScheduler()

		// Block a task and backdate the BlockedAt.
		s.Blocked.Block("old-task", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "waiting forever",
		})
		// We can't directly set BlockedAt, but we test with a very short staleTask duration.
		items, err := s.StaleCheck(context.Background(), time.Hour, 0)
		if err != nil {
			t.Fatalf("StaleCheck error: %v", err)
		}

		found := false
		for _, item := range items {
			if item.Kind == "task" && item.ID == "old-task" {
				found = true
			}
		}
		if !found {
			t.Error("expected stale blocked task 'old-task' to be detected")
		}
	})

	t.Run("no stale items for healthy state", func(t *testing.T) {
		t.Parallel()
		s, _, _, _ := newTestScheduler()

		items, err := s.StaleCheck(context.Background(), time.Hour, time.Hour)
		if err != nil {
			t.Fatalf("StaleCheck error: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("expected 0 stale items, got %d", len(items))
		}
	})
}

// --- EscalateAllBlocked tests ---

func TestEscalateAllBlocked(t *testing.T) {
	t.Parallel()

	t.Run("nil tracker is a no-op", func(t *testing.T) {
		t.Parallel()
		s := &Scheduler{}
		// Should not panic.
		s.EscalateAllBlocked(context.Background(), nil)
	})

	t.Run("escalates all blocked phases", func(t *testing.T) {
		t.Parallel()
		s, mf, _, logBuf := newTestScheduler()

		s.Blocked.Block("a", fabric.PollResult{Decision: fabric.PollNeedInfo, Reason: "need X"})
		s.Blocked.Block("b", fabric.PollResult{Decision: fabric.PollConflict, Reason: "conflict Z"})

		var failedIDs []string
		markFailed := func(phaseID string) {
			failedIDs = append(failedIDs, phaseID)
		}

		s.EscalateAllBlocked(context.Background(), markFailed)

		// Both should be unblocked from the tracker.
		if s.Blocked.Get("a") != nil {
			t.Error("expected a to be unblocked after escalation")
		}
		if s.Blocked.Get("b") != nil {
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

		// markFailed should have been called for both.
		if len(failedIDs) != 2 {
			t.Errorf("expected 2 markFailed calls, got %d", len(failedIDs))
		}

		if !strings.Contains(logBuf.String(), "Fabric Escalation") {
			t.Errorf("expected escalation log output, got: %s", logBuf.String())
		}
	})
}

// --- PhaseComplete tests ---

func TestPhaseComplete(t *testing.T) {
	t.Parallel()

	t.Run("nil fabric is a no-op", func(t *testing.T) {
		t.Parallel()
		s := &Scheduler{}
		// Should not panic.
		s.PhaseComplete(context.Background(), "a", nil, "", "")
	})

	t.Run("sets done state and releases claims", func(t *testing.T) {
		t.Parallel()
		s, mf, _, _ := newTestScheduler()

		// Pre-claim a file.
		_ = mf.ClaimFile(context.Background(), "main.go", "a")

		s.PhaseComplete(context.Background(), "a", nil, "", "")

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
}

// --- OnHail callback tests ---

func TestEscalatePhase_OnHail(t *testing.T) {
	t.Parallel()

	t.Run("OnHail fires on escalation", func(t *testing.T) {
		t.Parallel()
		s, mf, _, _ := newTestScheduler()

		var hailPhaseID string
		var hailDiscovery fabric.Discovery
		s.OnHail = func(phaseID string, d fabric.Discovery) {
			hailPhaseID = phaseID
			hailDiscovery = d
		}

		s.Blocked.Block("stuck", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing everything",
		})
		bp := s.Blocked.Get("stuck")

		s.escalatePhase(context.Background(), "stuck", bp, nil)

		if hailPhaseID != "stuck" {
			t.Errorf("expected OnHail called with phaseID 'stuck', got %q", hailPhaseID)
		}
		if hailDiscovery.Kind != fabric.DiscoveryRequirementsAmbiguity {
			t.Errorf("expected discovery kind %q, got %q", fabric.DiscoveryRequirementsAmbiguity, hailDiscovery.Kind)
		}
		if !strings.Contains(hailDiscovery.Detail, "stuck") {
			t.Errorf("expected discovery detail to mention 'stuck', got %q", hailDiscovery.Detail)
		}

		// Discovery should also be posted to the fabric.
		discs, _ := mf.AllDiscoveries(context.Background())
		if len(discs) != 1 {
			t.Errorf("expected 1 discovery posted, got %d", len(discs))
		}
	})

	t.Run("no OnHail does not panic", func(t *testing.T) {
		t.Parallel()
		s, _, _, _ := newTestScheduler()
		s.OnHail = nil

		s.Blocked.Block("phase", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "test",
		})
		bp := s.Blocked.Get("phase")

		// Should not panic.
		s.escalatePhase(context.Background(), "phase", bp, nil)
	})
}

// --- logger tests ---

func TestLogger(t *testing.T) {
	t.Parallel()

	t.Run("nil logger returns discard", func(t *testing.T) {
		t.Parallel()
		s := &Scheduler{}
		w := s.logger()
		if w == nil {
			t.Fatal("logger should never return nil")
		}
	})

	t.Run("custom logger is used", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		s := &Scheduler{Logger: &buf}
		w := s.logger()
		if w != &buf {
			t.Error("expected custom logger to be returned")
		}
	})
}
