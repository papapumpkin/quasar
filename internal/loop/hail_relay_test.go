package loop

import (
	"strings"
	"testing"
	"time"
)

func TestMemoryHailQueue_UnrelayedResolved(t *testing.T) {
	t.Parallel()

	t.Run("empty queue", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		got := q.UnrelayedResolved()
		if len(got) != 0 {
			t.Errorf("UnrelayedResolved() = %d items, want 0", len(got))
		}
	})

	t.Run("excludes unresolved hails", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "pending"})
		got := q.UnrelayedResolved()
		if len(got) != 0 {
			t.Errorf("UnrelayedResolved() = %d items, want 0 (only unresolved)", len(got))
		}
	})

	t.Run("includes resolved unrelayed hails", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "blocked"})
		_ = q.Post(Hail{ID: "h2", Kind: HailAmbiguity, Summary: "unclear"})
		_ = q.Resolve("h1", "use channels")

		got := q.UnrelayedResolved()
		if len(got) != 1 {
			t.Fatalf("UnrelayedResolved() = %d items, want 1", len(got))
		}
		if got[0].ID != "h1" {
			t.Errorf("ID = %q, want %q", got[0].ID, "h1")
		}
		if got[0].Resolution != "use channels" {
			t.Errorf("Resolution = %q, want %q", got[0].Resolution, "use channels")
		}
	})

	t.Run("excludes already relayed hails", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "first"})
		_ = q.Post(Hail{ID: "h2", Kind: HailBlocker, Summary: "second"})
		_ = q.Resolve("h1", "answer one")
		_ = q.Resolve("h2", "answer two")
		_ = q.MarkRelayed([]string{"h1"})

		got := q.UnrelayedResolved()
		if len(got) != 1 {
			t.Fatalf("UnrelayedResolved() = %d items, want 1", len(got))
		}
		if got[0].ID != "h2" {
			t.Errorf("ID = %q, want %q", got[0].ID, "h2")
		}
	})

	t.Run("returns deep copy", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "orig", Options: []string{"a"}})
		_ = q.Resolve("h1", "answer")

		got := q.UnrelayedResolved()
		got[0].Summary = "mutated"
		got[0].Options[0] = "mutated"

		fresh := q.UnrelayedResolved()
		if fresh[0].Summary == "mutated" {
			t.Error("UnrelayedResolved() returned reference to internal state (Summary)")
		}
		if fresh[0].Options[0] == "mutated" {
			t.Error("UnrelayedResolved() returned reference to internal Options slice")
		}
	})
}

func TestMemoryHailQueue_MarkRelayed(t *testing.T) {
	t.Parallel()

	t.Run("marks hails as relayed", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "one"})
		_ = q.Post(Hail{ID: "h2", Kind: HailBlocker, Summary: "two"})
		_ = q.Resolve("h1", "a1")
		_ = q.Resolve("h2", "a2")

		err := q.MarkRelayed([]string{"h1", "h2"})
		if err != nil {
			t.Fatalf("MarkRelayed() = %v, want nil", err)
		}

		// After marking, UnrelayedResolved should return empty.
		got := q.UnrelayedResolved()
		if len(got) != 0 {
			t.Errorf("UnrelayedResolved() = %d items after MarkRelayed, want 0", len(got))
		}

		// Verify RelayedAt is set.
		all := q.All()
		for _, h := range all {
			if h.RelayedAt.IsZero() {
				t.Errorf("hail %q RelayedAt is zero after MarkRelayed", h.ID)
			}
		}
	})

	t.Run("error on unknown ID", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "one"})

		err := q.MarkRelayed([]string{"h1", "nonexistent"})
		if err == nil {
			t.Fatal("MarkRelayed(nonexistent) = nil, want error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want substring %q", err, "not found")
		}
	})

	t.Run("idempotent for already relayed", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "one"})
		_ = q.Resolve("h1", "answer")
		_ = q.MarkRelayed([]string{"h1"})

		// Mark again — should not error.
		all1 := q.All()
		firstRelayedAt := all1[0].RelayedAt

		// Small delay to ensure time differs if timestamp were updated.
		time.Sleep(time.Millisecond)

		err := q.MarkRelayed([]string{"h1"})
		if err != nil {
			t.Fatalf("second MarkRelayed() = %v, want nil", err)
		}

		// Original timestamp should be preserved.
		all2 := q.All()
		if !all2[0].RelayedAt.Equal(firstRelayedAt) {
			t.Error("MarkRelayed() updated RelayedAt on already-relayed hail; want idempotent")
		}
	})

	t.Run("empty IDs is no-op", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		err := q.MarkRelayed(nil)
		if err != nil {
			t.Fatalf("MarkRelayed(nil) = %v, want nil", err)
		}
		err = q.MarkRelayed([]string{})
		if err != nil {
			t.Fatalf("MarkRelayed([]) = %v, want nil", err)
		}
	})
}

func TestFormatHailRelay(t *testing.T) {
	t.Parallel()

	t.Run("empty hails returns empty string", func(t *testing.T) {
		t.Parallel()
		got := formatHailRelay(nil)
		if got != "" {
			t.Errorf("formatHailRelay(nil) = %q, want empty", got)
		}
	})

	t.Run("formats single hail", func(t *testing.T) {
		t.Parallel()
		hails := []Hail{{
			Kind:       HailAmbiguity,
			Summary:    "synchronization approach",
			Cycle:      3,
			Resolution: "Use channels — we prefer CSP style.",
		}}
		got := formatHailRelay(hails)

		if !strings.HasPrefix(got, "[HUMAN RESPONSES]") {
			t.Error("expected [HUMAN RESPONSES] header")
		}
		if !strings.Contains(got, "ambiguity") {
			t.Error("expected hail kind in output")
		}
		if !strings.Contains(got, "synchronization approach") {
			t.Error("expected hail summary in output")
		}
		if !strings.Contains(got, "cycle 3") {
			t.Error("expected cycle reference in output")
		}
		if !strings.Contains(got, "Use channels") {
			t.Error("expected resolution text in output")
		}
		if !strings.Contains(got, "Proceed with this guidance") {
			t.Error("expected closing instruction")
		}
	})

	t.Run("formats multiple hails", func(t *testing.T) {
		t.Parallel()
		hails := []Hail{
			{Kind: HailBlocker, Summary: "missing dep", Cycle: 1, Resolution: "Add it to go.mod"},
			{Kind: HailDecisionNeeded, Summary: "API design", Cycle: 2, Resolution: "Use REST"},
		}
		got := formatHailRelay(hails)

		if strings.Count(got, "was answered:") != 2 {
			t.Error("expected two 'was answered:' blocks")
		}
		if !strings.Contains(got, "missing dep") {
			t.Error("expected first hail summary")
		}
		if !strings.Contains(got, "API design") {
			t.Error("expected second hail summary")
		}
	})
}

func TestPendingHailRelay(t *testing.T) {
	t.Parallel()

	t.Run("nil queue returns empty", func(t *testing.T) {
		t.Parallel()
		l := &Loop{HailQueue: nil}
		block, ids := l.pendingHailRelay()
		if block != "" || ids != nil {
			t.Errorf("pendingHailRelay() with nil queue = (%q, %v), want empty", block, ids)
		}
	})

	t.Run("no pending hails returns empty", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		l := &Loop{HailQueue: q}
		block, ids := l.pendingHailRelay()
		if block != "" || ids != nil {
			t.Errorf("pendingHailRelay() with empty queue = (%q, %v), want empty", block, ids)
		}
	})

	t.Run("returns formatted block and IDs", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "help", Cycle: 2})
		_ = q.Resolve("h1", "do X")

		l := &Loop{HailQueue: q}
		block, ids := l.pendingHailRelay()

		if !strings.Contains(block, "[HUMAN RESPONSES]") {
			t.Error("expected relay block header")
		}
		if !strings.Contains(block, "do X") {
			t.Error("expected resolution in relay block")
		}
		if len(ids) != 1 || ids[0] != "h1" {
			t.Errorf("ids = %v, want [h1]", ids)
		}
	})

	t.Run("excludes already relayed hails", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "first", Cycle: 1})
		_ = q.Post(Hail{ID: "h2", Kind: HailBlocker, Summary: "second", Cycle: 2})
		_ = q.Resolve("h1", "answer1")
		_ = q.Resolve("h2", "answer2")
		_ = q.MarkRelayed([]string{"h1"})

		l := &Loop{HailQueue: q}
		block, ids := l.pendingHailRelay()

		if !strings.Contains(block, "answer2") {
			t.Error("expected h2 resolution in relay block")
		}
		if strings.Contains(block, "answer1") {
			t.Error("relay block should not contain already-relayed h1")
		}
		if len(ids) != 1 || ids[0] != "h2" {
			t.Errorf("ids = %v, want [h2]", ids)
		}
	})
}

func TestOneShotRelayBehavior(t *testing.T) {
	t.Parallel()

	t.Run("hail relayed exactly once across calls", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailAmbiguity, Summary: "question", Cycle: 1})
		_ = q.Resolve("h1", "the answer")

		l := &Loop{HailQueue: q, UI: &noopUI{}}

		// First call: should return the relay.
		block1, ids1 := l.pendingHailRelay()
		if block1 == "" || len(ids1) == 0 {
			t.Fatal("first pendingHailRelay() returned empty, want relay content")
		}

		// Simulate what runCoderPhase does: mark as relayed.
		l.markHailsRelayed(ids1)

		// Second call: should return nothing (already relayed).
		block2, ids2 := l.pendingHailRelay()
		if block2 != "" || len(ids2) != 0 {
			t.Errorf("second pendingHailRelay() = (%q, %v), want empty (already relayed)", block2, ids2)
		}
	})
}
