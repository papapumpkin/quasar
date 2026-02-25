package loop

import (
	"strings"
	"sync"
	"testing"
)

func TestValidateHailKind(t *testing.T) {
	t.Parallel()

	t.Run("valid kinds", func(t *testing.T) {
		t.Parallel()
		for _, kind := range []HailKind{
			HailDecisionNeeded,
			HailAmbiguity,
			HailBlocker,
			HailHumanReviewFlag,
		} {
			if err := ValidateHailKind(kind); err != nil {
				t.Errorf("ValidateHailKind(%q) = %v, want nil", kind, err)
			}
		}
	})

	t.Run("invalid kind", func(t *testing.T) {
		t.Parallel()
		err := ValidateHailKind("nonexistent")
		if err == nil {
			t.Fatal("ValidateHailKind(nonexistent) = nil, want error")
		}
		if !strings.Contains(err.Error(), "invalid hail kind") {
			t.Errorf("error = %q, want substring %q", err, "invalid hail kind")
		}
	})
}

func TestHailIsResolved(t *testing.T) {
	t.Parallel()

	t.Run("unresolved", func(t *testing.T) {
		t.Parallel()
		h := &Hail{ID: "h1", Kind: HailBlocker}
		if h.IsResolved() {
			t.Error("IsResolved() = true for fresh hail, want false")
		}
	})

	t.Run("resolved", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "need help"})
		_ = q.Resolve("h1", "do X")
		all := q.All()
		if len(all) != 1 {
			t.Fatalf("All() returned %d hails, want 1", len(all))
		}
		if !all[0].IsResolved() {
			t.Error("IsResolved() = false after Resolve, want true")
		}
	})
}

func TestMemoryHailQueue_Post(t *testing.T) {
	t.Parallel()

	t.Run("auto-generates ID and timestamp", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()

		err := q.Post(Hail{Kind: HailAmbiguity, Summary: "unclear spec"})
		if err != nil {
			t.Fatalf("Post() = %v, want nil", err)
		}

		all := q.All()
		if len(all) != 1 {
			t.Fatalf("All() returned %d hails, want 1", len(all))
		}
		if all[0].ID == "" {
			t.Error("Post() did not auto-generate ID")
		}
		if all[0].CreatedAt.IsZero() {
			t.Error("Post() did not set CreatedAt")
		}
	})

	t.Run("preserves explicit ID", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()

		err := q.Post(Hail{ID: "custom-id", Kind: HailBlocker, Summary: "blocked"})
		if err != nil {
			t.Fatalf("Post() = %v, want nil", err)
		}

		all := q.All()
		if all[0].ID != "custom-id" {
			t.Errorf("ID = %q, want %q", all[0].ID, "custom-id")
		}
	})

	t.Run("rejects invalid kind", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()

		err := q.Post(Hail{Kind: "invalid", Summary: "bad"})
		if err == nil {
			t.Fatal("Post() with invalid kind = nil, want error")
		}
		if !strings.Contains(err.Error(), "invalid hail kind") {
			t.Errorf("error = %q, want substring %q", err, "invalid hail kind")
		}
	})

	t.Run("sequential IDs are unique", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()

		_ = q.Post(Hail{Kind: HailBlocker, Summary: "first"})
		_ = q.Post(Hail{Kind: HailBlocker, Summary: "second"})

		all := q.All()
		if len(all) != 2 {
			t.Fatalf("All() returned %d hails, want 2", len(all))
		}
		if all[0].ID == all[1].ID {
			t.Errorf("sequential IDs are identical: %q", all[0].ID)
		}
	})
}

func TestMemoryHailQueue_Unresolved(t *testing.T) {
	t.Parallel()

	t.Run("empty queue", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()

		got := q.Unresolved()
		if len(got) != 0 {
			t.Errorf("Unresolved() = %d items, want 0", len(got))
		}
	})

	t.Run("filters resolved hails", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()

		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "one"})
		_ = q.Post(Hail{ID: "h2", Kind: HailAmbiguity, Summary: "two"})
		_ = q.Post(Hail{ID: "h3", Kind: HailDecisionNeeded, Summary: "three"})
		_ = q.Resolve("h2", "resolved")

		got := q.Unresolved()
		if len(got) != 2 {
			t.Fatalf("Unresolved() = %d items, want 2", len(got))
		}

		ids := make(map[string]bool)
		for _, h := range got {
			ids[h.ID] = true
		}
		if !ids["h1"] || !ids["h3"] {
			t.Errorf("Unresolved() returned IDs %v, want h1 and h3", ids)
		}
	})

	t.Run("returns copy not reference", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "one"})

		got := q.Unresolved()
		got[0].Summary = "mutated"

		fresh := q.Unresolved()
		if fresh[0].Summary == "mutated" {
			t.Error("Unresolved() returned a reference to internal state, want copy")
		}
	})
}

func TestMemoryHailQueue_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("successful resolve", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "help"})

		err := q.Resolve("h1", "do this instead")
		if err != nil {
			t.Fatalf("Resolve() = %v, want nil", err)
		}

		all := q.All()
		if all[0].Resolution != "do this instead" {
			t.Errorf("Resolution = %q, want %q", all[0].Resolution, "do this instead")
		}
		if all[0].ResolvedAt.IsZero() {
			t.Error("ResolvedAt is zero after Resolve")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()

		err := q.Resolve("nonexistent", "answer")
		if err == nil {
			t.Fatal("Resolve(nonexistent) = nil, want error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want substring %q", err, "not found")
		}
	})

	t.Run("already resolved", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()
		_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "help"})
		_ = q.Resolve("h1", "first answer")

		err := q.Resolve("h1", "second answer")
		if err == nil {
			t.Fatal("double Resolve() = nil, want error")
		}
		if !strings.Contains(err.Error(), "already resolved") {
			t.Errorf("error = %q, want substring %q", err, "already resolved")
		}
	})
}

func TestMemoryHailQueue_All(t *testing.T) {
	t.Parallel()

	q := NewMemoryHailQueue()
	_ = q.Post(Hail{ID: "h1", Kind: HailBlocker, Summary: "one"})
	_ = q.Post(Hail{ID: "h2", Kind: HailAmbiguity, Summary: "two"})
	_ = q.Resolve("h1", "done")

	all := q.All()
	if len(all) != 2 {
		t.Fatalf("All() = %d items, want 2", len(all))
	}
	// Verify both resolved and unresolved are returned.
	if !all[0].IsResolved() {
		t.Error("All()[0] should be resolved")
	}
	if all[1].IsResolved() {
		t.Error("All()[1] should not be resolved")
	}
}

func TestMemoryHailQueue_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	q := NewMemoryHailQueue()
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = q.Post(Hail{Kind: HailBlocker, Summary: "concurrent"})
			_ = q.Unresolved()
			_ = q.All()
		}()
	}

	wg.Wait()

	all := q.All()
	if len(all) != goroutines {
		t.Errorf("All() = %d items after %d concurrent posts, want %d", len(all), goroutines, goroutines)
	}
}

func TestMemoryHailQueue_PreservesFields(t *testing.T) {
	t.Parallel()

	q := NewMemoryHailQueue()
	h := Hail{
		ID:         "h-full",
		PhaseID:    "phase-1",
		Cycle:      3,
		SourceRole: "reviewer",
		Kind:       HailHumanReviewFlag,
		Summary:    "needs human eyes",
		Detail:     "the implementation looks risky",
		Options:    []string{"approve", "reject", "revise"},
	}

	err := q.Post(h)
	if err != nil {
		t.Fatalf("Post() = %v, want nil", err)
	}

	got := q.All()[0]
	if got.PhaseID != "phase-1" {
		t.Errorf("PhaseID = %q, want %q", got.PhaseID, "phase-1")
	}
	if got.Cycle != 3 {
		t.Errorf("Cycle = %d, want 3", got.Cycle)
	}
	if got.SourceRole != "reviewer" {
		t.Errorf("SourceRole = %q, want %q", got.SourceRole, "reviewer")
	}
	if got.Kind != HailHumanReviewFlag {
		t.Errorf("Kind = %q, want %q", got.Kind, HailHumanReviewFlag)
	}
	if got.Summary != "needs human eyes" {
		t.Errorf("Summary = %q, want %q", got.Summary, "needs human eyes")
	}
	if got.Detail != "the implementation looks risky" {
		t.Errorf("Detail = %q, want %q", got.Detail, "the implementation looks risky")
	}
	if len(got.Options) != 3 {
		t.Fatalf("Options len = %d, want 3", len(got.Options))
	}
	if got.Options[0] != "approve" || got.Options[1] != "reject" || got.Options[2] != "revise" {
		t.Errorf("Options = %v, want [approve reject revise]", got.Options)
	}
}
