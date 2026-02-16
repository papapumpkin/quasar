package nebula

import (
	"sync"
	"testing"
	"time"
)

func TestNewMetrics(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test-nebula")

	if m.NebulaName != "test-nebula" {
		t.Errorf("NebulaName = %q, want %q", m.NebulaName, "test-nebula")
	}
	if m.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
	if m.Phases == nil {
		t.Error("Phases should be initialized, not nil")
	}
	if m.Waves == nil {
		t.Error("Waves should be initialized, not nil")
	}
	if len(m.Phases) != 0 {
		t.Errorf("Phases length = %d, want 0", len(m.Phases))
	}
	if len(m.Waves) != 0 {
		t.Errorf("Waves length = %d, want 0", len(m.Waves))
	}
}

func TestZeroValueMetrics(t *testing.T) {
	t.Parallel()

	var m Metrics

	// Zero-value should not panic on any operation.
	m.RecordPhaseStart("phase-1", 0)
	m.RecordPhaseComplete("phase-1", PhaseRunnerResult{
		TotalCostUSD: 0.05,
		CyclesUsed:   2,
	})
	m.RecordConflict("phase-1")
	m.RecordRestart("phase-1")
	m.RecordLockWait("phase-1", 100*time.Millisecond)
	m.RecordWaveComplete(0, 3, 2)

	snap := m.Snapshot()
	if snap.TotalPhases != 1 {
		t.Errorf("TotalPhases = %d, want 1", snap.TotalPhases)
	}
}

func TestRecordPhaseStartAndComplete(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")
	m.RecordPhaseStart("p1", 0)

	// Small delay so duration is nonzero.
	time.Sleep(time.Millisecond)

	report := &ReviewReport{Satisfaction: "satisfied"}
	m.RecordPhaseComplete("p1", PhaseRunnerResult{
		TotalCostUSD: 0.10,
		CyclesUsed:   3,
		Report:       report,
	})

	snap := m.Snapshot()
	if snap.TotalPhases != 1 {
		t.Fatalf("TotalPhases = %d, want 1", snap.TotalPhases)
	}
	if len(snap.Phases) != 1 {
		t.Fatalf("len(Phases) = %d, want 1", len(snap.Phases))
	}

	p := snap.Phases[0]
	if p.PhaseID != "p1" {
		t.Errorf("PhaseID = %q, want %q", p.PhaseID, "p1")
	}
	if p.WaveNumber != 0 {
		t.Errorf("WaveNumber = %d, want 0", p.WaveNumber)
	}
	if p.CyclesUsed != 3 {
		t.Errorf("CyclesUsed = %d, want 3", p.CyclesUsed)
	}
	if p.CostUSD != 0.10 {
		t.Errorf("CostUSD = %f, want 0.10", p.CostUSD)
	}
	if p.Satisfaction != "satisfied" {
		t.Errorf("Satisfaction = %q, want %q", p.Satisfaction, "satisfied")
	}
	if p.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", p.Duration)
	}
	if p.CompletedAt.IsZero() {
		t.Error("CompletedAt should be set")
	}
	if snap.TotalCostUSD != 0.10 {
		t.Errorf("TotalCostUSD = %f, want 0.10", snap.TotalCostUSD)
	}
}

func TestRecordPhaseCompleteNilReport(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")
	m.RecordPhaseStart("p1", 0)
	m.RecordPhaseComplete("p1", PhaseRunnerResult{
		TotalCostUSD: 0.05,
		CyclesUsed:   1,
		Report:       nil,
	})

	snap := m.Snapshot()
	if snap.Phases[0].Satisfaction != "" {
		t.Errorf("Satisfaction = %q, want empty for nil report", snap.Phases[0].Satisfaction)
	}
}

func TestRecordConflict(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")
	m.RecordPhaseStart("p1", 0)
	m.RecordConflict("p1")

	snap := m.Snapshot()
	if snap.TotalConflicts != 1 {
		t.Errorf("TotalConflicts = %d, want 1", snap.TotalConflicts)
	}
	if !snap.Phases[0].Conflict {
		t.Error("Phase should be marked as conflicted")
	}
}

func TestRecordRestart(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")
	m.RecordPhaseStart("p1", 0)
	m.RecordRestart("p1")
	m.RecordRestart("p1")

	snap := m.Snapshot()
	if snap.TotalRestarts != 2 {
		t.Errorf("TotalRestarts = %d, want 2", snap.TotalRestarts)
	}
	if snap.Phases[0].Restarts != 2 {
		t.Errorf("Phase Restarts = %d, want 2", snap.Phases[0].Restarts)
	}
}

func TestRecordLockWait(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")
	m.RecordPhaseStart("p1", 0)
	m.RecordLockWait("p1", 50*time.Millisecond)
	m.RecordLockWait("p1", 30*time.Millisecond)

	snap := m.Snapshot()
	want := 80 * time.Millisecond
	if snap.Phases[0].LockWaitTime != want {
		t.Errorf("LockWaitTime = %v, want %v", snap.Phases[0].LockWaitTime, want)
	}
}

func TestRecordWaveComplete(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")
	m.RecordPhaseStart("p1", 0)
	m.RecordPhaseStart("p2", 0)
	m.RecordConflict("p2")

	m.RecordPhaseComplete("p1", PhaseRunnerResult{CyclesUsed: 1})
	time.Sleep(time.Millisecond)
	m.RecordPhaseComplete("p2", PhaseRunnerResult{CyclesUsed: 2})

	m.RecordWaveComplete(0, 4, 2)

	snap := m.Snapshot()
	if snap.TotalWaves != 1 {
		t.Fatalf("TotalWaves = %d, want 1", snap.TotalWaves)
	}
	if len(snap.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(snap.Waves))
	}

	w := snap.Waves[0]
	if w.WaveNumber != 0 {
		t.Errorf("WaveNumber = %d, want 0", w.WaveNumber)
	}
	if w.EffectiveParallelism != 4 {
		t.Errorf("EffectiveParallelism = %d, want 4", w.EffectiveParallelism)
	}
	if w.ActualParallelism != 2 {
		t.Errorf("ActualParallelism = %d, want 2", w.ActualParallelism)
	}
	if w.PhaseCount != 2 {
		t.Errorf("PhaseCount = %d, want 2", w.PhaseCount)
	}
	if w.Conflicts != 1 {
		t.Errorf("Conflicts = %d, want 1", w.Conflicts)
	}
	if w.TotalDuration <= 0 {
		t.Errorf("TotalDuration = %v, want > 0", w.TotalDuration)
	}
}

func TestSnapshotDeepCopy(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")
	m.RecordPhaseStart("p1", 0)
	m.RecordPhaseComplete("p1", PhaseRunnerResult{CyclesUsed: 1, TotalCostUSD: 0.05})
	m.RecordWaveComplete(0, 2, 1)

	snap := m.Snapshot()

	// Mutate original after snapshot.
	m.RecordPhaseStart("p2", 1)
	m.RecordWaveComplete(1, 3, 2)

	if len(snap.Phases) != 1 {
		t.Errorf("snap.Phases length = %d, want 1 (should not reflect later mutations)", len(snap.Phases))
	}
	if len(snap.Waves) != 1 {
		t.Errorf("snap.Waves length = %d, want 1 (should not reflect later mutations)", len(snap.Waves))
	}
	if snap.TotalPhases != 1 {
		t.Errorf("snap.TotalPhases = %d, want 1", snap.TotalPhases)
	}
}

func TestRecordUnknownPhase(t *testing.T) {
	t.Parallel()

	m := NewMetrics("test")

	// Recording against a non-existent phase should not panic.
	m.RecordPhaseComplete("nonexistent", PhaseRunnerResult{CyclesUsed: 1})
	m.RecordConflict("nonexistent")
	m.RecordRestart("nonexistent")
	m.RecordLockWait("nonexistent", time.Second)

	snap := m.Snapshot()
	if snap.TotalConflicts != 1 {
		t.Errorf("TotalConflicts = %d, want 1", snap.TotalConflicts)
	}
	if snap.TotalRestarts != 1 {
		t.Errorf("TotalRestarts = %d, want 1", snap.TotalRestarts)
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	m := NewMetrics("concurrent")
	var wg sync.WaitGroup

	// Simulate concurrent phase starts and completions.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			phaseID := "phase"
			m.RecordPhaseStart(phaseID, id%3)
			m.RecordLockWait(phaseID, time.Millisecond)
			m.RecordConflict(phaseID)
			m.RecordRestart(phaseID)
			m.RecordPhaseComplete(phaseID, PhaseRunnerResult{
				TotalCostUSD: 0.01,
				CyclesUsed:   1,
			})
			_ = m.Snapshot()
		}(i)
	}
	wg.Wait()

	snap := m.Snapshot()
	if snap.TotalPhases != 50 {
		t.Errorf("TotalPhases = %d, want 50", snap.TotalPhases)
	}
	if snap.TotalConflicts != 50 {
		t.Errorf("TotalConflicts = %d, want 50", snap.TotalConflicts)
	}
	if snap.TotalRestarts != 50 {
		t.Errorf("TotalRestarts = %d, want 50", snap.TotalRestarts)
	}
}
