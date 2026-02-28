package loop

import (
	"testing"
	"time"
)

func TestHailIsAutoResolved(t *testing.T) {
	t.Parallel()

	t.Run("human resolved is not auto-resolved", func(t *testing.T) {
		t.Parallel()
		h := &Hail{
			ID:         "h1",
			Resolution: "human answer",
			ResolvedAt: time.Now(),
		}
		if h.IsAutoResolved() {
			t.Error("IsAutoResolved() = true for human-resolved hail, want false")
		}
	})

	t.Run("auto-resolved hail", func(t *testing.T) {
		t.Parallel()
		h := &Hail{
			ID:           "h1",
			Resolution:   autoResolveMessage,
			ResolvedAt:   time.Now(),
			AutoResolved: true,
		}
		if !h.IsAutoResolved() {
			t.Error("IsAutoResolved() = false for auto-resolved hail, want true")
		}
	})

	t.Run("unresolved is not auto-resolved", func(t *testing.T) {
		t.Parallel()
		h := &Hail{ID: "h1"}
		if h.IsAutoResolved() {
			t.Error("IsAutoResolved() = true for unresolved hail, want false")
		}
	})
}

func TestNewMemoryHailQueueWithTimeout(t *testing.T) {
	t.Parallel()

	t.Run("creates queue with timeout", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueueWithTimeout(5 * time.Minute)
		if q.timeout != 5*time.Minute {
			t.Errorf("timeout = %v, want 5m", q.timeout)
		}
	})

	t.Run("zero timeout disables expiry", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueueWithTimeout(0)
		if q.timeout != 0 {
			t.Errorf("timeout = %v, want 0", q.timeout)
		}
	})
}

func TestMemoryHailQueue_SweepExpired(t *testing.T) {
	t.Parallel()

	t.Run("auto-resolves expired hails", func(t *testing.T) {
		t.Parallel()
		timeout := 5 * time.Minute
		q := NewMemoryHailQueueWithTimeout(timeout)
		// Inject a fixed clock for deterministic tests.
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		q.now = func() time.Time { return now }

		// Post a hail created 10 minutes ago (past the 5-minute timeout).
		_ = q.Post(Hail{
			ID:        "h-expired",
			Kind:      HailBlocker,
			Summary:   "need help",
			CreatedAt: now.Add(-10 * time.Minute),
		})

		swept := q.SweepExpired()
		if len(swept) != 1 {
			t.Fatalf("SweepExpired() returned %d hails, want 1", len(swept))
		}
		if swept[0].ID != "h-expired" {
			t.Errorf("swept ID = %q, want %q", swept[0].ID, "h-expired")
		}
		if !swept[0].AutoResolved {
			t.Error("swept hail AutoResolved = false, want true")
		}
		if swept[0].Resolution != autoResolveMessage {
			t.Errorf("swept Resolution = %q, want %q", swept[0].Resolution, autoResolveMessage)
		}
		if swept[0].ResolvedAt != now {
			t.Errorf("swept ResolvedAt = %v, want %v", swept[0].ResolvedAt, now)
		}

		// Verify the hail is now resolved in the queue.
		unresolved := q.Unresolved()
		if len(unresolved) != 0 {
			t.Errorf("Unresolved() = %d after sweep, want 0", len(unresolved))
		}
	})

	t.Run("skips non-expired hails", func(t *testing.T) {
		t.Parallel()
		timeout := 5 * time.Minute
		q := NewMemoryHailQueueWithTimeout(timeout)
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		q.now = func() time.Time { return now }

		// Post a hail created 2 minutes ago (within the 5-minute timeout).
		_ = q.Post(Hail{
			ID:        "h-fresh",
			Kind:      HailBlocker,
			Summary:   "recent question",
			CreatedAt: now.Add(-2 * time.Minute),
		})

		swept := q.SweepExpired()
		if len(swept) != 0 {
			t.Errorf("SweepExpired() returned %d hails, want 0 (hail is not expired)", len(swept))
		}

		unresolved := q.Unresolved()
		if len(unresolved) != 1 {
			t.Errorf("Unresolved() = %d, want 1", len(unresolved))
		}
	})

	t.Run("skips already-resolved hails", func(t *testing.T) {
		t.Parallel()
		timeout := 5 * time.Minute
		q := NewMemoryHailQueueWithTimeout(timeout)
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		q.now = func() time.Time { return now }

		// Post and resolve a hail that is old enough to expire.
		_ = q.Post(Hail{
			ID:        "h-old-resolved",
			Kind:      HailBlocker,
			Summary:   "already answered",
			CreatedAt: now.Add(-10 * time.Minute),
		})
		_ = q.Resolve("h-old-resolved", "human answered")

		swept := q.SweepExpired()
		if len(swept) != 0 {
			t.Errorf("SweepExpired() returned %d hails, want 0 (already resolved)", len(swept))
		}

		// Confirm it's still human-resolved, not auto-resolved.
		all := q.All()
		if all[0].AutoResolved {
			t.Error("human-resolved hail became AutoResolved after sweep")
		}
	})

	t.Run("disabled when timeout is zero", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueueWithTimeout(0)
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		q.now = func() time.Time { return now }

		// Post a very old hail.
		_ = q.Post(Hail{
			ID:        "h-old",
			Kind:      HailBlocker,
			Summary:   "ancient hail",
			CreatedAt: now.Add(-24 * time.Hour),
		})

		swept := q.SweepExpired()
		if len(swept) != 0 {
			t.Errorf("SweepExpired() returned %d hails with timeout=0, want 0", len(swept))
		}

		unresolved := q.Unresolved()
		if len(unresolved) != 1 {
			t.Errorf("Unresolved() = %d with timeout=0, want 1", len(unresolved))
		}
	})

	t.Run("disabled for default queue with no timeout", func(t *testing.T) {
		t.Parallel()
		q := NewMemoryHailQueue()

		_ = q.Post(Hail{
			ID:        "h-old",
			Kind:      HailBlocker,
			Summary:   "old hail",
			CreatedAt: time.Now().Add(-24 * time.Hour),
		})

		swept := q.SweepExpired()
		if len(swept) != 0 {
			t.Errorf("SweepExpired() returned %d hails for default queue, want 0", len(swept))
		}
	})

	t.Run("mixed expired and fresh hails", func(t *testing.T) {
		t.Parallel()
		timeout := 5 * time.Minute
		q := NewMemoryHailQueueWithTimeout(timeout)
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		q.now = func() time.Time { return now }

		// One expired, one fresh, one already resolved.
		_ = q.Post(Hail{
			ID:        "h-expired",
			Kind:      HailBlocker,
			Summary:   "old",
			CreatedAt: now.Add(-10 * time.Minute),
		})
		_ = q.Post(Hail{
			ID:        "h-fresh",
			Kind:      HailAmbiguity,
			Summary:   "new",
			CreatedAt: now.Add(-1 * time.Minute),
		})
		_ = q.Post(Hail{
			ID:        "h-resolved",
			Kind:      HailDecisionNeeded,
			Summary:   "answered",
			CreatedAt: now.Add(-10 * time.Minute),
		})
		_ = q.Resolve("h-resolved", "human said yes")

		swept := q.SweepExpired()
		if len(swept) != 1 {
			t.Fatalf("SweepExpired() returned %d hails, want 1", len(swept))
		}
		if swept[0].ID != "h-expired" {
			t.Errorf("swept[0].ID = %q, want %q", swept[0].ID, "h-expired")
		}

		unresolved := q.Unresolved()
		if len(unresolved) != 1 {
			t.Fatalf("Unresolved() = %d after sweep, want 1", len(unresolved))
		}
		if unresolved[0].ID != "h-fresh" {
			t.Errorf("remaining unresolved ID = %q, want %q", unresolved[0].ID, "h-fresh")
		}
	})

	t.Run("idempotent sweep", func(t *testing.T) {
		t.Parallel()
		timeout := 5 * time.Minute
		q := NewMemoryHailQueueWithTimeout(timeout)
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		q.now = func() time.Time { return now }

		_ = q.Post(Hail{
			ID:        "h-expired",
			Kind:      HailBlocker,
			Summary:   "old",
			CreatedAt: now.Add(-10 * time.Minute),
		})

		// First sweep auto-resolves.
		swept1 := q.SweepExpired()
		if len(swept1) != 1 {
			t.Fatalf("first SweepExpired() returned %d, want 1", len(swept1))
		}

		// Second sweep should return nothing (already resolved).
		swept2 := q.SweepExpired()
		if len(swept2) != 0 {
			t.Errorf("second SweepExpired() returned %d, want 0", len(swept2))
		}
	})

	t.Run("returns deep copy", func(t *testing.T) {
		t.Parallel()
		timeout := 5 * time.Minute
		q := NewMemoryHailQueueWithTimeout(timeout)
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		q.now = func() time.Time { return now }

		_ = q.Post(Hail{
			ID:        "h-expired",
			Kind:      HailBlocker,
			Summary:   "old",
			Options:   []string{"a", "b"},
			CreatedAt: now.Add(-10 * time.Minute),
		})

		swept := q.SweepExpired()
		swept[0].Summary = "mutated"
		swept[0].Options[0] = "mutated"

		all := q.All()
		if all[0].Summary == "mutated" {
			t.Error("SweepExpired() returned reference to internal state (Summary)")
		}
		if all[0].Options[0] == "mutated" {
			t.Error("SweepExpired() returned reference to internal Options slice")
		}
	})

	t.Run("hail at exact timeout boundary is expired", func(t *testing.T) {
		t.Parallel()
		timeout := 5 * time.Minute
		q := NewMemoryHailQueueWithTimeout(timeout)
		now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		q.now = func() time.Time { return now }

		// Created exactly at the cutoff (now - timeout).
		_ = q.Post(Hail{
			ID:        "h-boundary",
			Kind:      HailBlocker,
			Summary:   "boundary",
			CreatedAt: now.Add(-timeout),
		})

		swept := q.SweepExpired()
		if len(swept) != 1 {
			t.Errorf("SweepExpired() at exact boundary returned %d, want 1", len(swept))
		}
	})
}
