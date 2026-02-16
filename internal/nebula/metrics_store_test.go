package nebula

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadMetrics(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewMetrics("round-trip")
	m.CompletedAt = m.StartedAt.Add(5 * time.Second)
	m.RecordPhaseStart("p1", 0)
	m.RecordPhaseComplete("p1", PhaseRunnerResult{
		TotalCostUSD: 0.12,
		CyclesUsed:   3,
		Report:       &ReviewReport{Satisfaction: "satisfied"},
	})
	m.RecordConflict("p1")
	m.RecordRestart("p1")
	m.RecordLockWait("p1", 50*time.Millisecond)
	m.RecordWaveComplete(0, 4, 2)

	// Set agentmail coordination fields on the wave to verify round-trip.
	applyAgentmailMetrics(m, AgentmailMetrics{
		ConflictCount: 0, // don't double-count; just testing wave fields
		ChangeVolume:  42,
		ActiveClaims:  7,
		AvgClaimAge:   15 * time.Second,
	})

	if err := SaveMetrics(dir, m); err != nil {
		t.Fatalf("SaveMetrics: %v", err)
	}

	loaded, err := LoadMetrics(dir)
	if err != nil {
		t.Fatalf("LoadMetrics: %v", err)
	}

	snap := m.Snapshot()

	if loaded.NebulaName != snap.NebulaName {
		t.Errorf("NebulaName = %q, want %q", loaded.NebulaName, snap.NebulaName)
	}
	if loaded.TotalCostUSD != snap.TotalCostUSD {
		t.Errorf("TotalCostUSD = %f, want %f", loaded.TotalCostUSD, snap.TotalCostUSD)
	}
	if loaded.TotalPhases != snap.TotalPhases {
		t.Errorf("TotalPhases = %d, want %d", loaded.TotalPhases, snap.TotalPhases)
	}
	if loaded.TotalWaves != snap.TotalWaves {
		t.Errorf("TotalWaves = %d, want %d", loaded.TotalWaves, snap.TotalWaves)
	}
	if loaded.TotalConflicts != snap.TotalConflicts {
		t.Errorf("TotalConflicts = %d, want %d", loaded.TotalConflicts, snap.TotalConflicts)
	}
	if loaded.TotalRestarts != snap.TotalRestarts {
		t.Errorf("TotalRestarts = %d, want %d", loaded.TotalRestarts, snap.TotalRestarts)
	}

	if len(loaded.Phases) != 1 {
		t.Fatalf("len(Phases) = %d, want 1", len(loaded.Phases))
	}
	lp := loaded.Phases[0]
	sp := snap.Phases[0]
	if lp.PhaseID != sp.PhaseID {
		t.Errorf("Phase.PhaseID = %q, want %q", lp.PhaseID, sp.PhaseID)
	}
	if lp.CyclesUsed != sp.CyclesUsed {
		t.Errorf("Phase.CyclesUsed = %d, want %d", lp.CyclesUsed, sp.CyclesUsed)
	}
	if lp.CostUSD != sp.CostUSD {
		t.Errorf("Phase.CostUSD = %f, want %f", lp.CostUSD, sp.CostUSD)
	}
	if lp.Duration != sp.Duration {
		t.Errorf("Phase.Duration = %v, want %v", lp.Duration, sp.Duration)
	}
	if lp.LockWaitTime != sp.LockWaitTime {
		t.Errorf("Phase.LockWaitTime = %v, want %v", lp.LockWaitTime, sp.LockWaitTime)
	}
	if lp.Satisfaction != sp.Satisfaction {
		t.Errorf("Phase.Satisfaction = %q, want %q", lp.Satisfaction, sp.Satisfaction)
	}
	if lp.Conflict != sp.Conflict {
		t.Errorf("Phase.Conflict = %v, want %v", lp.Conflict, sp.Conflict)
	}
	if lp.Restarts != sp.Restarts {
		t.Errorf("Phase.Restarts = %d, want %d", lp.Restarts, sp.Restarts)
	}

	if len(loaded.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(loaded.Waves))
	}
	lw := loaded.Waves[0]
	sw := snap.Waves[0]
	if lw.WaveNumber != sw.WaveNumber {
		t.Errorf("Wave.WaveNumber = %d, want %d", lw.WaveNumber, sw.WaveNumber)
	}
	if lw.EffectiveParallelism != sw.EffectiveParallelism {
		t.Errorf("Wave.EffectiveParallelism = %d, want %d", lw.EffectiveParallelism, sw.EffectiveParallelism)
	}
	if lw.ActualParallelism != sw.ActualParallelism {
		t.Errorf("Wave.ActualParallelism = %d, want %d", lw.ActualParallelism, sw.ActualParallelism)
	}
	if lw.TotalDuration != sw.TotalDuration {
		t.Errorf("Wave.TotalDuration = %v, want %v", lw.TotalDuration, sw.TotalDuration)
	}
	if lw.Conflicts != sw.Conflicts {
		t.Errorf("Wave.Conflicts = %d, want %d", lw.Conflicts, sw.Conflicts)
	}
	if lw.ChangeVolume != sw.ChangeVolume {
		t.Errorf("Wave.ChangeVolume = %d, want %d", lw.ChangeVolume, sw.ChangeVolume)
	}
	if lw.ActiveClaims != sw.ActiveClaims {
		t.Errorf("Wave.ActiveClaims = %d, want %d", lw.ActiveClaims, sw.ActiveClaims)
	}
	if lw.AvgClaimAge != sw.AvgClaimAge {
		t.Errorf("Wave.AvgClaimAge = %v, want %v", lw.AvgClaimAge, sw.AvgClaimAge)
	}
}

func TestLoadMetricsNoFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	loaded, err := LoadMetrics(dir)
	if err != nil {
		t.Fatalf("LoadMetrics on empty dir: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadMetrics returned nil, want zero Metrics")
	}
	if loaded.NebulaName != "" {
		t.Errorf("NebulaName = %q, want empty", loaded.NebulaName)
	}
	if loaded.TotalPhases != 0 {
		t.Errorf("TotalPhases = %d, want 0", loaded.TotalPhases)
	}
	if loaded.Phases == nil {
		t.Error("Phases should be initialized, not nil")
	}
	if loaded.Waves == nil {
		t.Error("Waves should be initialized, not nil")
	}
}

func TestHistoryRotation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Perform multiple saves to build up history.
	for i := 0; i < 12; i++ {
		m := NewMetrics("history-test")
		m.TotalCostUSD = float64(i)
		m.TotalPhases = i
		m.CompletedAt = m.StartedAt.Add(time.Duration(i) * time.Second)
		if err := SaveMetrics(dir, m); err != nil {
			t.Fatalf("SaveMetrics run %d: %v", i, err)
		}
	}

	// Load the raw file to verify history is capped.
	file, err := loadMetricsFile(dir)
	if err != nil {
		t.Fatalf("loadMetricsFile: %v", err)
	}

	if len(file.History) > maxHistoryEntries {
		t.Errorf("history length = %d, want <= %d", len(file.History), maxHistoryEntries)
	}

	// Current should be the last run (i=11).
	if file.Current.TotalPhases != 11 {
		t.Errorf("Current.TotalPhases = %d, want 11", file.Current.TotalPhases)
	}

	// History should contain runs 1..10 (run 0 was pushed out).
	if len(file.History) != maxHistoryEntries {
		t.Fatalf("history length = %d, want %d", len(file.History), maxHistoryEntries)
	}
	// Oldest entry in history should be run 1.
	if file.History[0].TotalPhases != 1 {
		t.Errorf("oldest history TotalPhases = %d, want 1", file.History[0].TotalPhases)
	}
	// Newest entry in history should be run 10.
	if file.History[maxHistoryEntries-1].TotalPhases != 10 {
		t.Errorf("newest history TotalPhases = %d, want 10", file.History[maxHistoryEntries-1].TotalPhases)
	}
}

func TestHistorySummaryFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// First run.
	m1 := NewMetrics("summary-test")
	m1.TotalCostUSD = 0.50
	m1.TotalPhases = 3
	m1.TotalConflicts = 1
	m1.TotalRestarts = 2
	m1.CompletedAt = m1.StartedAt.Add(10 * time.Second)
	if err := SaveMetrics(dir, m1); err != nil {
		t.Fatalf("SaveMetrics run 1: %v", err)
	}

	// Second run (pushes first into history).
	m2 := NewMetrics("summary-test")
	m2.TotalCostUSD = 1.00
	if err := SaveMetrics(dir, m2); err != nil {
		t.Fatalf("SaveMetrics run 2: %v", err)
	}

	file, err := loadMetricsFile(dir)
	if err != nil {
		t.Fatalf("loadMetricsFile: %v", err)
	}

	if len(file.History) != 1 {
		t.Fatalf("history length = %d, want 1", len(file.History))
	}

	h := file.History[0]
	if h.TotalCostUSD != 0.50 {
		t.Errorf("history TotalCostUSD = %f, want 0.50", h.TotalCostUSD)
	}
	if h.TotalPhases != 3 {
		t.Errorf("history TotalPhases = %d, want 3", h.TotalPhases)
	}
	if h.TotalConflicts != 1 {
		t.Errorf("history TotalConflicts = %d, want 1", h.TotalConflicts)
	}
	if h.TotalRestarts != 2 {
		t.Errorf("history TotalRestarts = %d, want 2", h.TotalRestarts)
	}
	wantDuration := int64(10 * time.Second)
	if h.DurationNs != wantDuration {
		t.Errorf("history DurationNs = %d, want %d", h.DurationNs, wantDuration)
	}
}

func TestSaveMetricsAtomicWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewMetrics("atomic-test")
	if err := SaveMetrics(dir, m); err != nil {
		t.Fatalf("SaveMetrics: %v", err)
	}

	// Verify the file exists at the expected path.
	path := filepath.Join(dir, metricsFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat metrics file: %v", err)
	}
	if info.Size() == 0 {
		t.Error("metrics file is empty")
	}

	// Verify no temp file was left behind.
	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp file should not exist, got err: %v", err)
	}
}

func TestLoadMetricsCorruptFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, metricsFileName)
	if err := os.WriteFile(path, []byte("{{invalid toml"), 0644); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}

	_, err := LoadMetrics(dir)
	if err == nil {
		t.Fatal("LoadMetrics on corrupt file should return error")
	}
}

func TestSaveMetricsCreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewMetrics("create-test")
	m.RecordPhaseStart("p1", 0)
	m.RecordPhaseComplete("p1", PhaseRunnerResult{CyclesUsed: 1, TotalCostUSD: 0.01})

	if err := SaveMetrics(dir, m); err != nil {
		t.Fatalf("SaveMetrics: %v", err)
	}

	// Load it back and verify round-trip.
	loaded, err := LoadMetrics(dir)
	if err != nil {
		t.Fatalf("LoadMetrics: %v", err)
	}
	if loaded.TotalPhases != 1 {
		t.Errorf("TotalPhases = %d, want 1", loaded.TotalPhases)
	}
	if loaded.TotalCostUSD != 0.01 {
		t.Errorf("TotalCostUSD = %f, want 0.01", loaded.TotalCostUSD)
	}
}
