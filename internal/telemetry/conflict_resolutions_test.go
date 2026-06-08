package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestConflictResolutionLogRoundTrip writes a row and reads it back, asserting
// the log creates its parent directory lazily and preserves the event fields.
func TestConflictResolutionLogRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "conflict_resolutions.jsonl")
	log := NewConflictResolutionLog(path)
	ctx := context.Background()

	ev := ConflictResolutionEvent{
		SrcRun: "run-a", DstRun: "run-b", Mode: "markers", Cycles: 1,
		Status: "resolved", FilesChanged: 3, Files: []string{"a.go", "b.go"},
		LatencyMs: 18200, CostUSD: 0.42,
	}
	if err := log.Record(ctx, ev); err != nil {
		t.Fatalf("Record: %v", err)
	}

	events, err := log.ReadSince(ctx, time.Time{})
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("read %d events, want 1", len(events))
	}
	got := events[0]
	if got.SrcRun != "run-a" || got.DstRun != "run-b" || got.Mode != "markers" ||
		got.Status != "resolved" || got.FilesChanged != 3 || got.CostUSD != 0.42 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Timestamp.IsZero() {
		t.Error("Record did not stamp a timestamp")
	}
}

// TestConflictResolutionLogNilIsNoOp asserts a nil log is safe to call.
func TestConflictResolutionLogNilIsNoOp(t *testing.T) {
	t.Parallel()
	var log *ConflictResolutionLog
	if err := log.Record(context.Background(), ConflictResolutionEvent{}); err != nil {
		t.Errorf("nil Record: %v", err)
	}
	events, err := log.ReadSince(context.Background(), time.Time{})
	if err != nil || events != nil {
		t.Errorf("nil ReadSince = (%v, %v), want (nil, nil)", events, err)
	}
}

// TestConflictResolutionLogSince asserts ReadSince filters out rows older than
// the cutoff while keeping newer ones.
func TestConflictResolutionLogSince(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "conflict_resolutions.jsonl")
	log := NewConflictResolutionLog(path)
	ctx := context.Background()

	old := ConflictResolutionEvent{Mode: "markers", Status: "resolved", Timestamp: time.Now().Add(-48 * time.Hour)}
	fresh := ConflictResolutionEvent{Mode: "no_markers", Status: "needs_human", Timestamp: time.Now().Add(-1 * time.Hour)}
	for _, ev := range []ConflictResolutionEvent{old, fresh} {
		if err := log.Record(ctx, ev); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	events, err := log.ReadSince(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(events) != 1 || events[0].Mode != "no_markers" {
		t.Fatalf("ReadSince returned %+v, want only the fresh no_markers row", events)
	}
}

// TestAggregateConflictResolutions asserts the report computes the resolution
// rate, averages, and per-file conflict counts.
func TestAggregateConflictResolutions(t *testing.T) {
	t.Parallel()
	events := []ConflictResolutionEvent{
		{Status: "resolved", CostUSD: 0.40, LatencyMs: 10000, Files: []string{"a.go", "b.go"}},
		{Status: "resolved", CostUSD: 0.60, LatencyMs: 20000, Files: []string{"a.go"}},
		{Status: "needs_human", CostUSD: 0.50, LatencyMs: 30000, Files: []string{"go.mod"}},
	}
	rep := AggregateConflictResolutions(events)

	if rep.Total != 3 || rep.Resolved != 2 {
		t.Errorf("Total/Resolved = %d/%d, want 3/2", rep.Total, rep.Resolved)
	}
	if got := rep.ResolutionRate(); got < 0.66 || got > 0.67 {
		t.Errorf("ResolutionRate = %v, want ~0.667", got)
	}
	if rep.AvgCostUSD != 0.50 {
		t.Errorf("AvgCostUSD = %v, want 0.50", rep.AvgCostUSD)
	}
	if rep.AvgLatencyMs != 20000 {
		t.Errorf("AvgLatencyMs = %v, want 20000", rep.AvgLatencyMs)
	}
	if rep.FilePaths["a.go"] != 2 {
		t.Errorf("FilePaths[a.go] = %d, want 2", rep.FilePaths["a.go"])
	}
}

// TestAggregateConflictResolutionsEmpty asserts an empty report has a zero
// resolution rate without dividing by zero.
func TestAggregateConflictResolutionsEmpty(t *testing.T) {
	t.Parallel()
	rep := AggregateConflictResolutions(nil)
	if rep.Total != 0 || rep.ResolutionRate() != 0 {
		t.Errorf("empty report = %+v, want zero totals and rate", rep)
	}
}
