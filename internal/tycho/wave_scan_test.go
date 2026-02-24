package tycho

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// buildDAG creates a DAG from edges (from depends on to) and returns
// the DAG and its computed waves.
func buildDAG(t *testing.T, edges [][2]string) (*dag.DAG, []dag.Wave) {
	t.Helper()
	d := dag.New()
	nodeSet := make(map[string]bool)
	for _, e := range edges {
		nodeSet[e[0]] = true
		nodeSet[e[1]] = true
	}
	for id := range nodeSet {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatalf("AddNode(%q): %v", id, err)
		}
	}
	for _, e := range edges {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatalf("AddEdge(%q, %q): %v", e[0], e[1], err)
		}
	}
	waves, err := d.ComputeWaves()
	if err != nil {
		t.Fatalf("ComputeWaves: %v", err)
	}
	return d, waves
}

// buildLinearDAG creates a linear chain A→B→C→D where each depends on the prior.
func buildLinearDAG(t *testing.T, ids []string) (*dag.DAG, []dag.Wave) {
	t.Helper()
	var edges [][2]string
	for i := 1; i < len(ids); i++ {
		edges = append(edges, [2]string{ids[i], ids[i-1]})
	}
	return buildDAG(t, edges)
}

func newTestWaveScanner(d *dag.DAG) (*WaveScanner, *mockFabric, *mockPoller, *bytes.Buffer) {
	mf := newMockFabric()
	mp := newMockPoller()
	var logBuf bytes.Buffer

	ws := &WaveScanner{
		Poller:   mp,
		Blocked:  fabric.NewBlockedTracker(),
		Pushback: &fabric.PushbackHandler{Fabric: mf},
		Fabric:   mf,
		DAG:      d,
		Logger:   &logBuf,
	}
	return ws, mf, mp, &logBuf
}

func TestScanWaves(t *testing.T) {
	t.Parallel()

	t.Run("all proceed in linear chain", func(t *testing.T) {
		t.Parallel()
		// A (wave 1) ← B (wave 2) ← C (wave 3)
		d, waves := buildLinearDAG(t, []string{"A", "B", "C"})
		ws, _, _, _ := newTestWaveScanner(d)

		eligible := map[string]bool{"A": true, "B": true, "C": true}
		snap := fabric.Snapshot{}

		proceed, pruned := ws.ScanWaves(context.Background(), waves, eligible, snap)

		if len(pruned) != 0 {
			t.Errorf("expected no pruned phases, got %v", pruned)
		}
		if len(proceed) != 3 {
			t.Errorf("expected 3 proceed phases, got %d: %v", len(proceed), proceed)
		}
		// Verify wave ordering: A should come before B, B before C.
		idxA, idxB, idxC := -1, -1, -1
		for i, id := range proceed {
			switch id {
			case "A":
				idxA = i
			case "B":
				idxB = i
			case "C":
				idxC = i
			}
		}
		if idxA > idxB || idxB > idxC {
			t.Errorf("expected wave order A < B < C, got indices A=%d B=%d C=%d", idxA, idxB, idxC)
		}
	})

	t.Run("first wave blocked prunes everything downstream", func(t *testing.T) {
		t.Parallel()
		// A (wave 1) ← B (wave 2) ← C (wave 3)
		d, waves := buildLinearDAG(t, []string{"A", "B", "C"})
		ws, _, mp, _ := newTestWaveScanner(d)

		mp.setDecision("A", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing dep X",
		})

		eligible := map[string]bool{"A": true, "B": true, "C": true}
		snap := fabric.Snapshot{}

		proceed, pruned := ws.ScanWaves(context.Background(), waves, eligible, snap)

		if len(proceed) != 0 {
			t.Errorf("expected 0 proceed phases, got %v", proceed)
		}
		if len(pruned) != 2 {
			t.Errorf("expected 2 pruned phases, got %d: %v", len(pruned), pruned)
		}
		if _, ok := pruned["B"]; !ok {
			t.Error("expected B to be pruned")
		}
		if _, ok := pruned["C"]; !ok {
			t.Error("expected C to be pruned")
		}

		// A should be polled, B and C should NOT be polled.
		if mp.getPollCount("A") != 1 {
			t.Errorf("expected A polled once, got %d", mp.getPollCount("A"))
		}
		if mp.getPollCount("B") != 0 {
			t.Errorf("expected B not polled, got %d", mp.getPollCount("B"))
		}
		if mp.getPollCount("C") != 0 {
			t.Errorf("expected C not polled, got %d", mp.getPollCount("C"))
		}
	})

	t.Run("mid-graph block partial prune", func(t *testing.T) {
		t.Parallel()
		// A (wave 1, proceeds)
		// B (wave 2, depends on A, blocked)
		// C (wave 3, depends on B, should be pruned)
		// D (wave 2, depends on A, proceeds)
		d, waves := buildDAG(t, [][2]string{
			{"B", "A"},
			{"C", "B"},
			{"D", "A"},
		})
		ws, _, mp, _ := newTestWaveScanner(d)

		mp.setDecision("B", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing interface",
		})

		eligible := map[string]bool{"A": true, "B": true, "C": true, "D": true}
		snap := fabric.Snapshot{}

		proceed, pruned := ws.ScanWaves(context.Background(), waves, eligible, snap)

		// A and D should proceed, B blocked, C pruned.
		proceedSet := make(map[string]bool)
		for _, id := range proceed {
			proceedSet[id] = true
		}
		if !proceedSet["A"] {
			t.Error("expected A to proceed")
		}
		if !proceedSet["D"] {
			t.Error("expected D to proceed")
		}
		if proceedSet["B"] {
			t.Error("expected B to be blocked, not proceed")
		}
		if proceedSet["C"] {
			t.Error("expected C to be pruned, not proceed")
		}

		if _, ok := pruned["C"]; !ok {
			t.Error("expected C to be in pruned set")
		}

		// C should never be polled.
		if mp.getPollCount("C") != 0 {
			t.Errorf("expected C not polled, got %d", mp.getPollCount("C"))
		}
	})

	t.Run("diamond dependency with one branch blocked", func(t *testing.T) {
		t.Parallel()
		// Diamond: A ← B, A ← C, B ← D, C ← D
		// A is the root (wave 1). B and C are wave 2. D is wave 3.
		// If B is blocked, D is pruned because D depends on B.
		// C still proceeds.
		d, waves := buildDAG(t, [][2]string{
			{"B", "A"},
			{"C", "A"},
			{"D", "B"},
			{"D", "C"},
		})
		ws, _, mp, _ := newTestWaveScanner(d)

		mp.setDecision("B", fabric.PollResult{
			Decision: fabric.PollConflict,
			Reason:   "file conflict with X",
		})

		eligible := map[string]bool{"A": true, "B": true, "C": true, "D": true}
		snap := fabric.Snapshot{}

		proceed, pruned := ws.ScanWaves(context.Background(), waves, eligible, snap)

		proceedSet := make(map[string]bool)
		for _, id := range proceed {
			proceedSet[id] = true
		}
		if !proceedSet["A"] {
			t.Error("expected A to proceed")
		}
		if !proceedSet["C"] {
			t.Error("expected C to proceed")
		}
		if proceedSet["B"] {
			t.Error("expected B to be blocked")
		}
		if proceedSet["D"] {
			t.Error("expected D to be pruned (depends on blocked B)")
		}

		if _, ok := pruned["D"]; !ok {
			t.Error("expected D to be in pruned set")
		}

		// D should never be polled.
		if mp.getPollCount("D") != 0 {
			t.Errorf("expected D not polled, got %d", mp.getPollCount("D"))
		}
	})

	t.Run("ineligible phases are skipped without polling", func(t *testing.T) {
		t.Parallel()
		d, waves := buildLinearDAG(t, []string{"A", "B", "C"})
		ws, _, mp, _ := newTestWaveScanner(d)

		// Only B is eligible — A and C are done/in-flight.
		eligible := map[string]bool{"B": true}
		snap := fabric.Snapshot{}

		proceed, _ := ws.ScanWaves(context.Background(), waves, eligible, snap)

		if len(proceed) != 1 || proceed[0] != "B" {
			t.Errorf("expected [B] to proceed, got %v", proceed)
		}
		if mp.getPollCount("A") != 0 {
			t.Errorf("expected A not polled (ineligible), got %d", mp.getPollCount("A"))
		}
		if mp.getPollCount("C") != 0 {
			t.Errorf("expected C not polled (ineligible), got %d", mp.getPollCount("C"))
		}
	})

	t.Run("already-blocked phases are skipped", func(t *testing.T) {
		t.Parallel()
		d, waves := buildLinearDAG(t, []string{"A", "B"})
		ws, _, mp, _ := newTestWaveScanner(d)

		// Pre-block A.
		ws.Blocked.Block("A", fabric.PollResult{Decision: fabric.PollNeedInfo})

		eligible := map[string]bool{"A": true, "B": true}
		snap := fabric.Snapshot{}

		proceed, _ := ws.ScanWaves(context.Background(), waves, eligible, snap)

		// A is already blocked (skipped, not polled). B proceeds.
		if mp.getPollCount("A") != 0 {
			t.Errorf("expected A not polled (already blocked), got %d", mp.getPollCount("A"))
		}
		proceedSet := make(map[string]bool)
		for _, id := range proceed {
			proceedSet[id] = true
		}
		if proceedSet["A"] {
			t.Error("expected already-blocked A to not proceed")
		}
		if !proceedSet["B"] {
			t.Error("expected B to proceed")
		}
	})

	t.Run("overridden phases skip polling and proceed", func(t *testing.T) {
		t.Parallel()
		d, waves := buildLinearDAG(t, []string{"A", "B"})
		ws, _, mp, _ := newTestWaveScanner(d)

		ws.Blocked.Override("A")

		eligible := map[string]bool{"A": true, "B": true}
		snap := fabric.Snapshot{}

		proceed, _ := ws.ScanWaves(context.Background(), waves, eligible, snap)

		if mp.getPollCount("A") != 0 {
			t.Errorf("expected A not polled (overridden), got %d", mp.getPollCount("A"))
		}
		proceedSet := make(map[string]bool)
		for _, id := range proceed {
			proceedSet[id] = true
		}
		if !proceedSet["A"] {
			t.Error("expected overridden A to proceed")
		}
		if !proceedSet["B"] {
			t.Error("expected B to proceed")
		}
	})

	t.Run("empty waves returns empty results", func(t *testing.T) {
		t.Parallel()
		ws := &WaveScanner{
			Poller:  newMockPoller(),
			Blocked: fabric.NewBlockedTracker(),
		}

		proceed, pruned := ws.ScanWaves(context.Background(), nil, nil, fabric.Snapshot{})
		if len(proceed) != 0 {
			t.Errorf("expected 0 proceed, got %d", len(proceed))
		}
		if len(pruned) != 0 {
			t.Errorf("expected 0 pruned, got %d", len(pruned))
		}
	})

	t.Run("poll error proceeds optimistically", func(t *testing.T) {
		t.Parallel()
		d := dag.New()
		_ = d.AddNode("A", 0)
		waves, _ := d.ComputeWaves()

		errPoller := &errorPoller{}
		var logBuf bytes.Buffer
		ws := &WaveScanner{
			Poller:  errPoller,
			Blocked: fabric.NewBlockedTracker(),
			Logger:  &logBuf,
		}

		eligible := map[string]bool{"A": true}
		proceed, _ := ws.ScanWaves(context.Background(), waves, eligible, fabric.Snapshot{})

		if len(proceed) != 1 || proceed[0] != "A" {
			t.Errorf("expected A to proceed on poll error, got %v", proceed)
		}
		if !strings.Contains(logBuf.String(), "warning") {
			t.Error("expected warning logged for poll error")
		}
	})

	t.Run("prune reason includes upstream info", func(t *testing.T) {
		t.Parallel()
		d, waves := buildLinearDAG(t, []string{"A", "B"})
		ws, _, mp, _ := newTestWaveScanner(d)

		mp.setDecision("A", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "need interface Foo",
		})

		eligible := map[string]bool{"A": true, "B": true}
		proceed, pruned := ws.ScanWaves(context.Background(), waves, eligible, fabric.Snapshot{})

		if len(proceed) != 0 {
			t.Errorf("expected 0 proceed, got %v", proceed)
		}
		reason, ok := pruned["B"]
		if !ok {
			t.Fatal("expected B to be pruned")
		}
		if !strings.Contains(reason, "upstream A blocked") {
			t.Errorf("expected prune reason to mention upstream A, got: %s", reason)
		}
		if !strings.Contains(reason, "need interface Foo") {
			t.Errorf("expected prune reason to include original reason, got: %s", reason)
		}
	})

	t.Run("escalate fires OnEscalate callback", func(t *testing.T) {
		t.Parallel()
		d := dag.New()
		_ = d.AddNode("A", 0)
		waves, _ := d.ComputeWaves()

		mf := newMockFabric()
		mp := newMockPoller()
		var logBuf bytes.Buffer

		// PollConflict with no matching file claim → handleConflict → ActionEscalate.
		mp.setDecision("A", fabric.PollResult{
			Decision:     fabric.PollConflict,
			Reason:       "interface conflict",
			ConflictWith: "nonexistent-phase",
		})

		var escalatedPhase string
		var escalatedBP *fabric.BlockedPhase

		ws := &WaveScanner{
			Poller:   mp,
			Blocked:  fabric.NewBlockedTracker(),
			Pushback: &fabric.PushbackHandler{Fabric: mf},
			Fabric:   mf,
			DAG:      d,
			Logger:   &logBuf,
			OnEscalate: func(_ context.Context, phaseID string, bp *fabric.BlockedPhase) {
				escalatedPhase = phaseID
				escalatedBP = bp
			},
		}

		eligible := map[string]bool{"A": true}
		proceed, _ := ws.ScanWaves(context.Background(), waves, eligible, fabric.Snapshot{})

		if len(proceed) != 0 {
			t.Errorf("expected 0 proceed when escalated, got %v", proceed)
		}
		if escalatedPhase != "A" {
			t.Errorf("expected OnEscalate called with phase A, got %q", escalatedPhase)
		}
		if escalatedBP == nil {
			t.Fatal("expected OnEscalate to receive non-nil BlockedPhase")
		}
		if escalatedBP.PhaseID != "A" {
			t.Errorf("expected BlockedPhase.PhaseID = A, got %q", escalatedBP.PhaseID)
		}
	})

	t.Run("escalate without OnEscalate logs message", func(t *testing.T) {
		t.Parallel()
		d := dag.New()
		_ = d.AddNode("A", 0)
		waves, _ := d.ComputeWaves()

		mf := newMockFabric()
		mp := newMockPoller()
		var logBuf bytes.Buffer

		mp.setDecision("A", fabric.PollResult{
			Decision:     fabric.PollConflict,
			Reason:       "interface conflict",
			ConflictWith: "nonexistent-phase",
		})

		ws := &WaveScanner{
			Poller:   mp,
			Blocked:  fabric.NewBlockedTracker(),
			Pushback: &fabric.PushbackHandler{Fabric: mf},
			Fabric:   mf,
			DAG:      d,
			Logger:   &logBuf,
			// OnEscalate intentionally nil.
		}

		eligible := map[string]bool{"A": true}
		proceed, _ := ws.ScanWaves(context.Background(), waves, eligible, fabric.Snapshot{})

		if len(proceed) != 0 {
			t.Errorf("expected 0 proceed when escalated, got %v", proceed)
		}
		if !strings.Contains(logBuf.String(), "escalated") {
			t.Errorf("expected log to mention 'escalated', got: %s", logBuf.String())
		}
		if !strings.Contains(logBuf.String(), "interface conflict") {
			t.Errorf("expected log to include reason, got: %s", logBuf.String())
		}
	})
}

// TestScanDelegatesWaveScanner verifies that Scheduler.Scan() delegates
// to WaveScanner when it is configured with waves.
func TestScanDelegatesWaveScanner(t *testing.T) {
	t.Parallel()

	t.Run("delegates to WaveScanner when configured", func(t *testing.T) {
		t.Parallel()

		d, waves := buildLinearDAG(t, []string{"A", "B", "C"})
		mf := newMockFabric()
		mp := newMockPoller()
		var logBuf bytes.Buffer

		ws := &WaveScanner{
			Poller:   mp,
			Blocked:  fabric.NewBlockedTracker(),
			Pushback: &fabric.PushbackHandler{Fabric: mf},
			Fabric:   mf,
			DAG:      d,
			Logger:   &logBuf,
		}

		// Block A so B and C are pruned.
		mp.setDecision("A", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing dep",
		})

		s := &Scheduler{
			Fabric:      mf,
			Poller:      mp,
			Blocked:     fabric.NewBlockedTracker(),
			Pushback:    &fabric.PushbackHandler{Fabric: mf},
			Logger:      &logBuf,
			WaveScanner: ws,
			Waves:       waves,
			DAG:         d,
		}

		sb := &mockSnapshotBuilder{snap: fabric.Snapshot{}}
		proceed, err := s.Scan(context.Background(), []string{"A", "B", "C"}, sb)
		if err != nil {
			t.Fatalf("Scan error: %v", err)
		}

		if len(proceed) != 0 {
			t.Errorf("expected 0 proceed with A blocked via WaveScanner, got %v", proceed)
		}

		// B and C should NOT have been polled (pruned by wave scanner).
		if mp.getPollCount("B") != 0 {
			t.Errorf("expected B not polled, got %d", mp.getPollCount("B"))
		}
		if mp.getPollCount("C") != 0 {
			t.Errorf("expected C not polled, got %d", mp.getPollCount("C"))
		}
	})

	t.Run("falls back to flat scan without WaveScanner", func(t *testing.T) {
		t.Parallel()

		s, _, mp, _ := newTestScheduler()

		mp.setDecision("B", fabric.PollResult{
			Decision: fabric.PollNeedInfo,
			Reason:   "missing",
		})

		sb := &mockSnapshotBuilder{snap: fabric.Snapshot{}}
		proceed, err := s.Scan(context.Background(), []string{"A", "B", "C"}, sb)
		if err != nil {
			t.Fatalf("Scan error: %v", err)
		}

		// Flat scan: A and C proceed, B is blocked.
		proceedSet := make(map[string]bool)
		for _, id := range proceed {
			proceedSet[id] = true
		}
		if !proceedSet["A"] || !proceedSet["C"] {
			t.Errorf("expected A and C to proceed in flat scan, got %v", proceed)
		}
		if proceedSet["B"] {
			t.Error("expected B to be blocked in flat scan")
		}
	})

	t.Run("falls back to flat scan with empty waves", func(t *testing.T) {
		t.Parallel()

		mf := newMockFabric()
		mp := newMockPoller()
		var logBuf bytes.Buffer

		ws := &WaveScanner{
			Poller:  mp,
			Blocked: fabric.NewBlockedTracker(),
		}

		s := &Scheduler{
			Fabric:      mf,
			Poller:      mp,
			Blocked:     fabric.NewBlockedTracker(),
			Pushback:    &fabric.PushbackHandler{Fabric: mf},
			Logger:      &logBuf,
			WaveScanner: ws,
			Waves:       nil, // empty waves triggers flat fallback
		}

		sb := &mockSnapshotBuilder{snap: fabric.Snapshot{}}
		proceed, err := s.Scan(context.Background(), []string{"A", "B"}, sb)
		if err != nil {
			t.Fatalf("Scan error: %v", err)
		}
		if len(proceed) != 2 {
			t.Errorf("expected 2 proceed (flat fallback), got %d", len(proceed))
		}
	})
}

// errorPoller always returns an error.
type errorPoller struct{}

func (p *errorPoller) Poll(_ context.Context, _ string, _ fabric.Snapshot) (fabric.PollResult, error) {
	return fabric.PollResult{}, context.DeadlineExceeded
}
