package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthEventStoreRecordAndSince(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "health_events.jsonl")
	store := NewHealthEventStore(path)
	ctx := context.Background()

	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)

	mustRecord(t, store, HealthEvent{Timestamp: old, InvocationID: "a", Event: HealthEventDegraded, Signal: "token_rate"})
	mustRecord(t, store, HealthEvent{Timestamp: recent, InvocationID: "b", Event: HealthEventDead, Signals: []string{"token_rate", "write_idle"}})

	got, err := store.Since(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Since returned %d events, want 1 (only the recent one)", len(got))
	}
	if got[0].InvocationID != "b" {
		t.Errorf("got invocation %q, want b", got[0].InvocationID)
	}
}

func TestHealthEventStoreMissingFile(t *testing.T) {
	t.Parallel()
	store := NewHealthEventStore(filepath.Join(t.TempDir(), "absent.jsonl"))
	got, err := store.Since(context.Background(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Since on missing file: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty slice for missing file, got %d", len(got))
	}
}

func TestNilHealthEventStoreRecordIsNoop(t *testing.T) {
	t.Parallel()
	var store *HealthEventStore
	if err := store.Record(context.Background(), HealthEvent{Event: HealthEventDead}); err != nil {
		t.Fatalf("nil store Record = %v, want nil", err)
	}
}

func TestTerminationHistogram(t *testing.T) {
	t.Parallel()
	events := []HealthEvent{
		{Event: HealthEventDegraded, Signal: "token_rate"},
		{Event: HealthEventDegraded, Signal: "token_rate"},
		{Event: HealthEventDead, Signals: []string{"token_rate", "write_idle"}},
		{Event: HealthEventDead, Signals: []string{"write_idle", "cpu_idle"}},
		{Event: HealthEventSigterm}, // non-terminal, ignored
	}
	hist := TerminationHistogram(events)

	if hist["degraded:token_rate"] != 2 {
		t.Errorf("degraded:token_rate = %d, want 2", hist["degraded:token_rate"])
	}
	if hist["dead:write_idle"] != 2 {
		t.Errorf("dead:write_idle = %d, want 2", hist["dead:write_idle"])
	}
	if hist["dead:token_rate"] != 1 {
		t.Errorf("dead:token_rate = %d, want 1", hist["dead:token_rate"])
	}

	// SortedHistogram orders by descending count, ties alphabetical.
	order := SortedHistogram(hist)
	if len(order) == 0 {
		t.Fatal("empty sorted histogram")
	}
	for i := 1; i < len(order); i++ {
		if hist[order[i-1]] < hist[order[i]] {
			t.Fatalf("histogram not descending: %v", order)
		}
	}
}

func mustRecord(t *testing.T, s *HealthEventStore, e HealthEvent) {
	t.Helper()
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatalf("Record: %v", err)
	}
}
