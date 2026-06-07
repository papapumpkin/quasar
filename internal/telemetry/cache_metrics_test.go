package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheHitRatio(t *testing.T) {
	tests := []struct {
		name  string
		read  int
		fresh int
		want  float64
	}{
		{"all cached", 900, 0, 1.0},
		{"all fresh", 0, 900, 0.0},
		{"half", 100, 100, 0.5},
		{"no tokens", 0, 0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CacheHitRatio(tt.read, tt.fresh); got != tt.want {
				t.Errorf("CacheHitRatio(%d, %d) = %v, want %v", tt.read, tt.fresh, got, tt.want)
			}
		})
	}
}

func TestCacheMetricStore_Record(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cache_metrics.jsonl")
	store := NewCacheMetricStore(path)

	metric := CacheMetric{
		InvocationID: "inv-1",
		NebulaID:     "neb-1",
		PhaseID:      "phase-a",
		CycleN:       0,
		InputTokens:  100,
		CacheCreate:  900,
		CacheRead:    0,
	}
	if err := store.Record(context.Background(), metric); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// The line should exist and contain all required fields.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected one line in log, got none")
	}
	var got CacheMetric
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal line: %v", err)
	}

	if got.InvocationID != "inv-1" || got.NebulaID != "neb-1" || got.PhaseID != "phase-a" {
		t.Errorf("identity fields wrong: %+v", got)
	}
	if got.InputTokens != 100 || got.CacheCreate != 900 {
		t.Errorf("token fields wrong: %+v", got)
	}
	if got.Timestamp.IsZero() {
		t.Error("Timestamp should be auto-filled")
	}
	// Derived ratio: read 0 / (0 + 100) = 0.
	if got.CacheHitRatio != 0 {
		t.Errorf("CacheHitRatio = %v, want 0", got.CacheHitRatio)
	}
}

func TestCacheMetricStore_HitRateByPhase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache_metrics.jsonl")
	store := NewCacheMetricStore(path)
	ctx := context.Background()

	records := []CacheMetric{
		{NebulaID: "neb-1", PhaseID: "phase-a", CycleN: 0, InputTokens: 1000, CacheRead: 0},
		{NebulaID: "neb-1", PhaseID: "phase-a", CycleN: 1, InputTokens: 0, CacheRead: 1000},
		{NebulaID: "neb-1", PhaseID: "phase-b", CycleN: 0, InputTokens: 500, CacheRead: 500},
		{NebulaID: "neb-2", PhaseID: "phase-a", CycleN: 0, InputTokens: 0, CacheRead: 999},
	}
	for _, r := range records {
		if err := store.Record(ctx, r); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	rates, err := store.HitRateByPhase(ctx, "neb-1")
	if err != nil {
		t.Fatalf("HitRateByPhase: %v", err)
	}
	// phase-a pooled: read 1000 / (1000 + 1000) = 0.5
	if rates["phase-a"] != 0.5 {
		t.Errorf("phase-a hit rate = %v, want 0.5", rates["phase-a"])
	}
	// phase-b pooled: read 500 / (500 + 500) = 0.5
	if rates["phase-b"] != 0.5 {
		t.Errorf("phase-b hit rate = %v, want 0.5", rates["phase-b"])
	}
	// neb-2 must not leak into neb-1's report.
	if _, ok := rates["phase-c"]; ok {
		t.Error("unexpected phase in report")
	}
	if len(rates) != 2 {
		t.Errorf("expected 2 phases, got %d", len(rates))
	}
}

func TestCacheMetricStore_HitRateByCycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache_metrics.jsonl")
	store := NewCacheMetricStore(path)
	ctx := context.Background()

	records := []CacheMetric{
		{NebulaID: "neb-1", PhaseID: "phase-a", CycleN: 1, InputTokens: 0, CacheRead: 800},
		{NebulaID: "neb-1", PhaseID: "phase-a", CycleN: 0, InputTokens: 1000, CacheRead: 0},
		{NebulaID: "neb-1", PhaseID: "phase-b", CycleN: 0, InputTokens: 1, CacheRead: 1},
	}
	for _, r := range records {
		if err := store.Record(ctx, r); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	cycles, err := store.HitRateByCycle(ctx, "neb-1", "phase-a")
	if err != nil {
		t.Fatalf("HitRateByCycle: %v", err)
	}
	if len(cycles) != 2 {
		t.Fatalf("expected 2 cycles, got %d: %v", len(cycles), cycles)
	}
	// Ordered by cycle number: cycle 0 (all fresh) = 0, cycle 1 (all cached) = 1.
	if cycles[0] != 0 {
		t.Errorf("cycle 0 = %v, want 0", cycles[0])
	}
	if cycles[1] != 1 {
		t.Errorf("cycle 1 = %v, want 1", cycles[1])
	}
}

func TestCacheMetricStore_MissingFile(t *testing.T) {
	store := NewCacheMetricStore(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	metrics, err := store.MetricsByNebula(context.Background(), "neb-1")
	if err != nil {
		t.Fatalf("MetricsByNebula on missing file: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("expected no metrics, got %d", len(metrics))
	}
}
