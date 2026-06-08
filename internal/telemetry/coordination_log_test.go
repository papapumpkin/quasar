package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCoordinationLogRecordAndRead(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "coordination_log.jsonl")
	log := NewCoordinationLog(path)
	ctx := context.Background()

	if err := log.RecordCheck(ctx, "run-1", "phase-a", 3, map[string]int{"in_flight": 1, "deprecated": 1, "declared": 1}); err != nil {
		t.Fatalf("RecordCheck: %v", err)
	}
	if err := log.RecordCheck(ctx, "run-2", "phase-b", 1, map[string]int{"declared": 1}); err != nil {
		t.Fatalf("RecordCheck: %v", err)
	}
	if err := log.RecordOverride(ctx, "phase-a", "FromTicket", "ignore_deprecations"); err != nil {
		t.Fatalf("RecordOverride: %v", err)
	}

	events, err := log.ReadSince(ctx, time.Time{})
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != CoordinationEventCheck || events[0].NotesCount != 3 {
		t.Errorf("unexpected first event: %+v", events[0])
	}
	if events[2].Type != CoordinationEventOverride || events[2].Symbol != "FromTicket" {
		t.Errorf("unexpected override event: %+v", events[2])
	}
}

func TestCoordinationLogReadSinceFilters(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "coordination_log.jsonl")
	log := NewCoordinationLog(path)
	ctx := context.Background()

	old := CoordinationEvent{Type: CoordinationEventCheck, PhaseID: "old", NotesCount: 1, Timestamp: time.Now().Add(-48 * time.Hour)}
	recent := CoordinationEvent{Type: CoordinationEventCheck, PhaseID: "new", NotesCount: 2, Timestamp: time.Now().Add(-1 * time.Hour)}
	if err := log.append(ctx, old); err != nil {
		t.Fatalf("append old: %v", err)
	}
	if err := log.append(ctx, recent); err != nil {
		t.Fatalf("append recent: %v", err)
	}

	events, err := log.ReadSince(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(events) != 1 || events[0].PhaseID != "new" {
		t.Fatalf("expected only the recent event, got %+v", events)
	}
}

func TestCoordinationLogNilIsNoOp(t *testing.T) {
	t.Parallel()

	var log *CoordinationLog
	ctx := context.Background()
	if err := log.RecordCheck(ctx, "r", "p", 1, nil); err != nil {
		t.Errorf("nil RecordCheck should be a no-op, got %v", err)
	}
	if err := log.RecordOverride(ctx, "p", "s", "reason"); err != nil {
		t.Errorf("nil RecordOverride should be a no-op, got %v", err)
	}
	events, err := log.ReadSince(ctx, time.Time{})
	if err != nil || events != nil {
		t.Errorf("nil ReadSince should yield (nil, nil), got %v, %v", events, err)
	}
}

func TestCoordinationLogMissingFile(t *testing.T) {
	t.Parallel()

	log := NewCoordinationLog(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	events, err := log.ReadSince(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected no events for missing file, got %d", len(events))
	}
}

func TestAggregateCoordination(t *testing.T) {
	t.Parallel()

	events := []CoordinationEvent{
		{Type: CoordinationEventCheck, PhaseID: "a", NotesCount: 3},
		{Type: CoordinationEventCheck, PhaseID: "a", NotesCount: 2},
		{Type: CoordinationEventCheck, PhaseID: "b", NotesCount: 1},
		{Type: CoordinationEventOverride, PhaseID: "a", Symbol: "Foo"},
		{Type: CoordinationEventOverride, PhaseID: "a", Symbol: "Foo"},
		{Type: CoordinationEventOverride, PhaseID: "b", Symbol: "Bar"},
	}
	rep := AggregateCoordination(events)

	if rep.NotesByPhase["a"] != 5 || rep.NotesByPhase["b"] != 1 {
		t.Errorf("unexpected NotesByPhase: %+v", rep.NotesByPhase)
	}
	if rep.OverridesByPhase["a"] != 2 || rep.OverridesByPhase["b"] != 1 {
		t.Errorf("unexpected OverridesByPhase: %+v", rep.OverridesByPhase)
	}
	if rep.ContendedSymbols["Foo"] != 2 || rep.ContendedSymbols["Bar"] != 1 {
		t.Errorf("unexpected ContendedSymbols: %+v", rep.ContendedSymbols)
	}
}
