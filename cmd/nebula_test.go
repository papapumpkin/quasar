package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestWriteStatusJSON_WritesToWriter(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "json-test"},
		},
		Phases: []nebula.PhaseSpec{{ID: "p1"}, {ID: "p2"}},
	}
	state := &nebula.State{
		TotalCostUSD: 2.50,
		Phases: map[string]*nebula.PhaseState{
			"p1": {Status: nebula.PhaseStatusDone},
			"p2": {Status: nebula.PhaseStatusFailed},
		},
	}

	started := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	completed := started.Add(5 * time.Minute)
	m := &nebula.Metrics{
		NebulaName:     "json-test",
		StartedAt:      started,
		CompletedAt:    completed,
		TotalCostUSD:   3.00,
		TotalPhases:    2,
		TotalConflicts: 1,
		TotalRestarts:  0,
		Phases: []nebula.PhaseMetrics{
			{PhaseID: "p1", WaveNumber: 0, Duration: 2 * time.Minute, CostUSD: 1.50, CyclesUsed: 2, Satisfaction: "high"},
			{PhaseID: "p2", WaveNumber: 1, Duration: 3 * time.Minute, CostUSD: 1.50, CyclesUsed: 3, Conflict: true},
		},
		Waves: []nebula.WaveMetrics{
			{WaveNumber: 0, PhaseCount: 1, EffectiveParallelism: 1, TotalDuration: 2 * time.Minute},
			{WaveNumber: 1, PhaseCount: 1, EffectiveParallelism: 1, TotalDuration: 3 * time.Minute},
		},
	}

	history := []nebula.HistorySummary{
		{StartedAt: started.Add(-24 * time.Hour), TotalPhases: 2, TotalCostUSD: 2.00, Duration: 4 * time.Minute, TotalConflicts: 0},
	}

	var buf bytes.Buffer
	if err := writeStatusJSON(&buf, neb, state, m, history); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}

	// Verify it's valid JSON.
	var result statusJSON
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
	}

	if result.Name != "json-test" {
		t.Errorf("Name = %q, want %q", result.Name, "json-test")
	}
	if result.TotalPhases != 2 {
		t.Errorf("TotalPhases = %d, want 2", result.TotalPhases)
	}
	if result.Completed != 1 {
		t.Errorf("Completed = %d, want 1", result.Completed)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
	if result.TotalCost != 3.00 {
		t.Errorf("TotalCost = %f, want 3.00", result.TotalCost)
	}
	if result.Conflicts != 1 {
		t.Errorf("Conflicts = %d, want 1", result.Conflicts)
	}
	if len(result.Waves) != 2 {
		t.Errorf("len(Waves) = %d, want 2", len(result.Waves))
	}
	if len(result.Phases) != 2 {
		t.Errorf("len(Phases) = %d, want 2", len(result.Phases))
	}
	if len(result.History) != 1 {
		t.Errorf("len(History) = %d, want 1", len(result.History))
	}
}

func TestWriteStatusJSON_NilMetrics(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "nil-metrics"},
		},
		Phases: []nebula.PhaseSpec{{ID: "p1"}},
	}
	state := &nebula.State{
		TotalCostUSD: 1.00,
		Phases: map[string]*nebula.PhaseState{
			"p1": {Status: nebula.PhaseStatusDone},
		},
	}

	var buf bytes.Buffer
	if err := writeStatusJSON(&buf, neb, state, nil, nil); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}

	var result statusJSON
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if result.Name != "nil-metrics" {
		t.Errorf("Name = %q, want %q", result.Name, "nil-metrics")
	}
	if result.Completed != 1 {
		t.Errorf("Completed = %d, want 1", result.Completed)
	}
	if result.TotalCost != 1.00 {
		t.Errorf("TotalCost = %f, want 1.00", result.TotalCost)
	}
}
