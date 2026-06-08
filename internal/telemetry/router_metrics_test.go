package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRouterMetricStore_Record(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "router_metrics.jsonl")
	store := NewRouterMetricStore(path)

	metric := RouterMetric{
		InvocationID:   "inv-1",
		NebulaID:       "neb-1",
		PhaseID:        "phase-a",
		SubKind:        "file_finder",
		HaikuInTokens:  120,
		HaikuOutTokens: 8,
		LatencyMs:      42,
		CacheHit:       false,
	}
	if err := store.RecordRouter(context.Background(), metric); err != nil {
		t.Fatalf("RecordRouter: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected one line in log, got none")
	}
	var got RouterMetric
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal line: %v", err)
	}
	if got.SubKind != "file_finder" || got.HaikuInTokens != 120 || got.HaikuOutTokens != 8 {
		t.Errorf("fields wrong: %+v", got)
	}
	if got.Timestamp.IsZero() {
		t.Error("Timestamp should be auto-filled")
	}
}

func TestRouterMetricStore_NilIsNoOp(t *testing.T) {
	var store *RouterMetricStore
	if err := store.RecordRouter(context.Background(), RouterMetric{}); err != nil {
		t.Errorf("nil store RecordRouter = %v, want nil", err)
	}
}

func TestRouterSavingsByPhase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router_metrics.jsonl")
	store := NewRouterMetricStore(path)
	ctx := context.Background()

	records := []RouterMetric{
		{NebulaID: "neb-1", PhaseID: "phase-a", HaikuInTokens: 100, HaikuOutTokens: 10},
		{NebulaID: "neb-1", PhaseID: "phase-a", HaikuInTokens: 50, HaikuOutTokens: 5, CacheHit: true},
		{NebulaID: "neb-1", PhaseID: "phase-b", HaikuInTokens: 200, HaikuOutTokens: 20},
		{NebulaID: "neb-2", PhaseID: "phase-a", HaikuInTokens: 999, HaikuOutTokens: 1},
	}
	for _, r := range records {
		if err := store.RecordRouter(ctx, r); err != nil {
			t.Fatalf("RecordRouter: %v", err)
		}
	}

	metrics, err := store.RouterMetricsByNebula(ctx, "neb-1")
	if err != nil {
		t.Fatalf("RouterMetricsByNebula: %v", err)
	}
	if len(metrics) != 3 {
		t.Fatalf("metrics for neb-1 = %d, want 3 (neb-2 must not leak)", len(metrics))
	}

	savings := RouterSavingsByPhaseFor(metrics)
	// phase-a: (100+10) + (50+5, a cache hit that still counts) = 165
	if savings["phase-a"] != 165 {
		t.Errorf("phase-a savings = %d, want 165", savings["phase-a"])
	}
	// phase-b: 200+20 = 220
	if savings["phase-b"] != 220 {
		t.Errorf("phase-b savings = %d, want 220", savings["phase-b"])
	}
	if len(savings) != 2 {
		t.Errorf("expected 2 phases, got %d", len(savings))
	}
}

func TestRouterMetricStore_MissingFile(t *testing.T) {
	store := NewRouterMetricStore(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	metrics, err := store.RouterMetricsByNebula(context.Background(), "neb-1")
	if err != nil {
		t.Fatalf("RouterMetricsByNebula on missing file: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("expected no metrics, got %d", len(metrics))
	}
}
