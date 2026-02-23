package neutron

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
)

func TestReaper_Run(t *testing.T) {
	t.Parallel()

	t.Run("releases stale claims for non-running tasks", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reaper-test.db")
		f, err := fabric.NewSQLiteFabric(ctx, dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteFabric: %v", err)
		}
		defer f.Close()

		// Create a task in done state with a claim.
		if err := f.SetPhaseState(ctx, "old-task", fabric.StateDone); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}
		if err := f.ClaimFile(ctx, "stale.go", "old-task"); err != nil {
			t.Fatalf("ClaimFile: %v", err)
		}

		// Use a very short stale threshold and a "now" far in the future.
		reaper := &Reaper{
			Fabric:     f,
			StaleClaim: 1 * time.Millisecond,
			StaleEpoch: DefaultStaleEpoch,
			Now: func() time.Time {
				return time.Now().Add(1 * time.Hour)
			},
		}

		actions, err := reaper.Run(ctx)
		if err != nil {
			t.Fatalf("Reaper.Run: %v", err)
		}

		var foundRelease bool
		for _, a := range actions {
			if a.Kind == "released_claim" {
				foundRelease = true
				if !strings.Contains(a.Details, "old-task") {
					t.Errorf("expected claim release details to mention old-task, got: %s", a.Details)
				}
			}
		}
		if !foundRelease {
			t.Error("expected a released_claim action, got none")
		}

		// Verify the claim is actually released.
		claims, err := f.AllClaims(ctx)
		if err != nil {
			t.Fatalf("AllClaims: %v", err)
		}
		if len(claims) != 0 {
			t.Errorf("expected 0 claims after reap, got %d", len(claims))
		}
	})

	t.Run("does not release claims for running tasks", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reaper-running.db")
		f, err := fabric.NewSQLiteFabric(ctx, dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteFabric: %v", err)
		}
		defer f.Close()

		// Create a running task with a claim.
		if err := f.SetPhaseState(ctx, "active-task", fabric.StateRunning); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}
		if err := f.ClaimFile(ctx, "active.go", "active-task"); err != nil {
			t.Fatalf("ClaimFile: %v", err)
		}

		reaper := &Reaper{
			Fabric:     f,
			StaleClaim: 1 * time.Millisecond,
			StaleEpoch: DefaultStaleEpoch,
			Now: func() time.Time {
				return time.Now().Add(1 * time.Hour)
			},
		}

		actions, err := reaper.Run(ctx)
		if err != nil {
			t.Fatalf("Reaper.Run: %v", err)
		}

		for _, a := range actions {
			if a.Kind == "released_claim" {
				t.Errorf("should not release claim for running task, got: %s", a.Details)
			}
		}

		// Verify the claim is still there.
		claims, err := f.AllClaims(ctx)
		if err != nil {
			t.Fatalf("AllClaims: %v", err)
		}
		if len(claims) != 1 {
			t.Errorf("expected 1 claim, got %d", len(claims))
		}
	})

	t.Run("does not release fresh claims", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reaper-fresh.db")
		f, err := fabric.NewSQLiteFabric(ctx, dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteFabric: %v", err)
		}
		defer f.Close()

		if err := f.SetPhaseState(ctx, "new-task", fabric.StateQueued); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}
		if err := f.ClaimFile(ctx, "fresh.go", "new-task"); err != nil {
			t.Fatalf("ClaimFile: %v", err)
		}

		// Use current time (claim age = ~0).
		reaper := &Reaper{
			Fabric:     f,
			StaleClaim: 30 * time.Minute,
			StaleEpoch: DefaultStaleEpoch,
			Now:        time.Now,
		}

		actions, err := reaper.Run(ctx)
		if err != nil {
			t.Fatalf("Reaper.Run: %v", err)
		}

		for _, a := range actions {
			if a.Kind == "released_claim" {
				t.Errorf("should not release fresh claim, got: %s", a.Details)
			}
		}
	})

	t.Run("flags epoch with terminal tasks and leftover state", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reaper-epoch.db")
		f, err := fabric.NewSQLiteFabric(ctx, dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteFabric: %v", err)
		}
		defer f.Close()

		// All tasks done, but unresolved discovery remains.
		if err := f.SetPhaseState(ctx, "task-a", fabric.StateDone); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}
		if err := f.SetPhaseState(ctx, "task-b", fabric.StateFailed); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}
		if _, err := f.PostDiscovery(ctx, fabric.Discovery{
			SourceTask: "task-a",
			Kind:       fabric.DiscoveryBudgetAlert,
			Detail:     "over budget",
		}); err != nil {
			t.Fatalf("PostDiscovery: %v", err)
		}

		reaper := &Reaper{
			Fabric:     f,
			StaleClaim: DefaultStaleClaim,
			StaleEpoch: DefaultStaleEpoch,
			Now: func() time.Time {
				return time.Now().Add(2 * time.Hour)
			},
		}

		actions, err := reaper.Run(ctx)
		if err != nil {
			t.Fatalf("Reaper.Run: %v", err)
		}

		var foundFlag bool
		for _, a := range actions {
			if a.Kind == "flagged_epoch" {
				foundFlag = true
				if !strings.Contains(a.Details, "1 unresolved") {
					t.Errorf("expected details to mention unresolved discoveries, got: %s", a.Details)
				}
				if !strings.Contains(a.Details, "stale >") {
					t.Errorf("expected details to mention staleness threshold, got: %s", a.Details)
				}
			}
		}
		if !foundFlag {
			t.Error("expected a flagged_epoch action")
		}
	})

	t.Run("no actions when fabric is clean", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reaper-clean.db")
		f, err := fabric.NewSQLiteFabric(ctx, dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteFabric: %v", err)
		}
		defer f.Close()

		reaper := &Reaper{
			Fabric:     f,
			StaleClaim: DefaultStaleClaim,
			StaleEpoch: DefaultStaleEpoch,
		}

		actions, err := reaper.Run(ctx)
		if err != nil {
			t.Fatalf("Reaper.Run: %v", err)
		}
		if len(actions) != 0 {
			t.Errorf("expected 0 actions on clean fabric, got %d", len(actions))
		}
	})

	t.Run("deduplicates release actions for same owner with multiple claims", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reaper-dedup.db")
		f, err := fabric.NewSQLiteFabric(ctx, dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteFabric: %v", err)
		}
		defer f.Close()

		// Create a done task that holds multiple claims.
		if err := f.SetPhaseState(ctx, "multi-task", fabric.StateDone); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}
		if err := f.ClaimFile(ctx, "a.go", "multi-task"); err != nil {
			t.Fatalf("ClaimFile a.go: %v", err)
		}
		if err := f.ClaimFile(ctx, "b.go", "multi-task"); err != nil {
			t.Fatalf("ClaimFile b.go: %v", err)
		}
		if err := f.ClaimFile(ctx, "c.go", "multi-task"); err != nil {
			t.Fatalf("ClaimFile c.go: %v", err)
		}

		reaper := &Reaper{
			Fabric:     f,
			StaleClaim: 1 * time.Millisecond,
			StaleEpoch: DefaultStaleEpoch,
			Now: func() time.Time {
				return time.Now().Add(1 * time.Hour)
			},
		}

		actions, err := reaper.Run(ctx)
		if err != nil {
			t.Fatalf("Reaper.Run: %v", err)
		}

		// Should produce exactly one released_claim action, not three.
		var releaseCount int
		for _, a := range actions {
			if a.Kind == "released_claim" {
				releaseCount++
			}
		}
		if releaseCount != 1 {
			t.Errorf("expected 1 released_claim action for multi-claim owner, got %d", releaseCount)
		}

		// All claims should be gone.
		claims, err := f.AllClaims(ctx)
		if err != nil {
			t.Fatalf("AllClaims: %v", err)
		}
		if len(claims) != 0 {
			t.Errorf("expected 0 claims after reap, got %d", len(claims))
		}
	})

	t.Run("does not flag epoch when claims are fresh", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reaper-fresh-epoch.db")
		f, err := fabric.NewSQLiteFabric(ctx, dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteFabric: %v", err)
		}
		defer f.Close()

		// All tasks done with a leftover claim, but claim is fresh.
		if err := f.SetPhaseState(ctx, "task-x", fabric.StateDone); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}
		if err := f.ClaimFile(ctx, "recent.go", "task-x"); err != nil {
			t.Fatalf("ClaimFile: %v", err)
		}

		// Use current time — claim age is ~0, well under the 1h threshold.
		reaper := &Reaper{
			Fabric:     f,
			StaleClaim: DefaultStaleClaim,
			StaleEpoch: DefaultStaleEpoch,
			Now:        time.Now,
		}

		actions, err := reaper.Run(ctx)
		if err != nil {
			t.Fatalf("Reaper.Run: %v", err)
		}

		for _, a := range actions {
			if a.Kind == "flagged_epoch" {
				t.Errorf("should not flag epoch when claims are fresh, got: %s", a.Details)
			}
		}
	})

	t.Run("uses default thresholds when zero", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "reaper-defaults.db")
		f, err := fabric.NewSQLiteFabric(ctx, dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteFabric: %v", err)
		}
		defer f.Close()

		// Zero thresholds should use defaults (30m, 1h).
		reaper := &Reaper{
			Fabric: f,
		}

		// Should not panic or error on empty fabric.
		actions, err := reaper.Run(ctx)
		if err != nil {
			t.Fatalf("Reaper.Run: %v", err)
		}
		if len(actions) != 0 {
			t.Errorf("expected 0 actions, got %d", len(actions))
		}
	})
}
