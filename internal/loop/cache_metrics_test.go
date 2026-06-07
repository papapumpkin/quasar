package loop

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

func TestRecordCacheMetric_PersistsToStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache_metrics.jsonl")
	store := telemetry.NewCacheMetricStore(path)

	l := &Loop{CacheMetrics: store, NebulaID: "neb-1"}
	state := &CycleState{TaskID: "phase-a", Cycle: 2}
	result := &agent.InvocationResult{
		InputTokens:         100,
		CacheCreationTokens: 900,
		CacheReadTokens:     1800,
	}

	l.recordCacheMetric(context.Background(), state, result)

	metrics, err := store.MetricsByNebula(context.Background(), "neb-1")
	if err != nil {
		t.Fatalf("MetricsByNebula: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 recorded metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.PhaseID != "phase-a" || m.CycleN != 2 {
		t.Errorf("identity wrong: phase=%q cycle=%d", m.PhaseID, m.CycleN)
	}
	if m.InputTokens != 100 || m.CacheCreate != 900 || m.CacheRead != 1800 {
		t.Errorf("token counts wrong: %+v", m)
	}
	// Derived ratio: 1800 / (1800 + 100) ≈ 0.947.
	if m.CacheHitRatio <= 0.9 {
		t.Errorf("CacheHitRatio = %v, want > 0.9", m.CacheHitRatio)
	}
}

func TestRecordCacheMetric_NilStoreIsNoop(t *testing.T) {
	l := &Loop{CacheMetrics: nil}
	state := &CycleState{TaskID: "phase-a", Cycle: 0}
	// Must not panic when no store is configured.
	l.recordCacheMetric(context.Background(), state, &agent.InvocationResult{})
}
