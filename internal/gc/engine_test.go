package gc

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/gitops"
)

// testConfig returns a valid GCConfig with short, test-friendly durations.
func testConfig() config.GCConfig {
	return config.GCConfig{
		Enabled:      true,
		TickInterval: time.Hour,
		GraceWindow:  24 * time.Hour,
		TTLs: config.GCTTLConfig{
			CompletedNebulas:     time.Hour,
			FailedNebulas:        time.Hour,
			ConstellationRuns:    time.Hour,
			SensorEvents:         time.Hour,
			TriggerQueueConsumed: time.Hour,
			AuditLog:             time.Hour,
		},
		Blobs: config.GCBlobConfig{
			SweepInterval:     24 * time.Hour,
			MinAgeBeforeSweep: time.Hour,
		},
	}
}

// fakeReaper records Reap calls and returns a canned report. It lets the engine
// be tested without a real git repo; the reaper itself is covered separately.
type fakeReaper struct {
	repos      []string
	protected  map[string]bool
	dryRunSeen bool
}

func (f *fakeReaper) Reap(_ context.Context, repoPath string, _ time.Duration, _ time.Time, isProtected func(string) bool, dryRun bool) (*gitops.ReapReport, error) {
	f.repos = append(f.repos, repoPath)
	f.dryRunSeen = dryRun
	// Record what the engine considers protected so tests can assert the
	// predicate was wired from non-terminal run ids.
	if isProtected != nil && isProtected("run-live") {
		if f.protected == nil {
			f.protected = map[string]bool{}
		}
		f.protected["run-live"] = true
	}
	return &gitops.ReapReport{Removed: []string{filepath.Join(repoPath, "wt")}, ReclaimedBytes: 10}, nil
}

func newBlobStore(t *testing.T, db *sql.DB) *blobstore.Store {
	t.Helper()
	store, err := blobstore.New(filepath.Join(t.TempDir(), "blobs"), db)
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	return store
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("rejects a nil database", func(t *testing.T) {
		t.Parallel()
		if _, err := New(Opts{Config: testConfig()}); err == nil {
			t.Error("expected error for nil DB")
		}
	})

	t.Run("rejects an invalid config", func(t *testing.T) {
		t.Parallel()
		db := newGCDB(t)
		bad := testConfig()
		bad.TickInterval = 0
		if _, err := New(Opts{DB: db, Config: bad}); err == nil {
			t.Error("expected error for zero tick interval")
		}
	})

	t.Run("defaults the clock and worktree max age", func(t *testing.T) {
		t.Parallel()
		db := newGCDB(t)
		e, err := New(Opts{DB: db, Config: testConfig()})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if e.clock == nil {
			t.Error("clock not defaulted")
		}
		if e.worktreeMaxAge <= 0 {
			t.Error("worktreeMaxAge not defaulted")
		}
	})
}

func TestRunDisabled(t *testing.T) {
	t.Parallel()
	db := newGCDB(t)
	cfg := testConfig()
	cfg.Enabled = false
	e, err := New(Opts{DB: db, Config: cfg})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Run must return immediately (not block on a ticker) when disabled.
	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run(disabled) = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run(disabled) did not return promptly")
	}
}

func TestRunOnceLifecycle(t *testing.T) {
	ctx := context.Background()
	db := newGCDB(t)
	cfg := testConfig()

	// A completed nebula whose updated_at predates the TTL.
	insertNebula(t, db, "n1", "completed", baseTime.Add(-2*time.Hour).Unix())
	insertPhase(t, db, "n1", "p1")

	now := baseTime
	e, err := New(Opts{
		DB:     db,
		Config: cfg,
		Clock:  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Pass 1: the nebula is marked (soft-deleted) but not yet swept.
	rep, err := e.RunOnce(ctx, RunOnceOpts{Category: CategoryCompletedNebulas})
	if err != nil {
		t.Fatalf("RunOnce pass 1: %v", err)
	}
	if got := categoryResult(rep, CategoryCompletedNebulas); got.Marked != 1 || got.Swept != 0 {
		t.Fatalf("pass 1 marked=%d swept=%d, want 1 and 0", got.Marked, got.Swept)
	}
	if count(t, db, "SELECT COUNT(*) FROM nebulas WHERE deleted_at IS NOT NULL") != 1 {
		t.Fatal("nebula not soft-deleted after pass 1")
	}

	// Pass 2: advance past the grace window — the nebula is hard-deleted and its
	// phase cascades away.
	now = baseTime.Add(cfg.GraceWindow + time.Hour)
	rep, err = e.RunOnce(ctx, RunOnceOpts{Category: CategoryCompletedNebulas})
	if err != nil {
		t.Fatalf("RunOnce pass 2: %v", err)
	}
	if got := categoryResult(rep, CategoryCompletedNebulas); got.Swept != 1 || got.CascadedChildren != 1 {
		t.Fatalf("pass 2 swept=%d cascaded=%d, want 1 and 1", got.Swept, got.CascadedChildren)
	}
	if count(t, db, "SELECT COUNT(*) FROM nebulas") != 0 {
		t.Error("nebula not hard-deleted after grace")
	}
	if count(t, db, "SELECT COUNT(*) FROM phases") != 0 {
		t.Error("phase not cascaded")
	}
}

func TestRunOnceRecordsLedger(t *testing.T) {
	ctx := context.Background()
	db := newGCDB(t)
	e, err := New(Opts{DB: db, Config: testConfig(), Clock: fixedClock(baseTime)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := e.RunOnce(ctx, RunOnceOpts{Category: CategorySensorEvents}); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM gc_runs WHERE category = ?", CategorySensorEvents) != 1 {
		t.Error("expected a gc_runs ledger row for the swept category")
	}
}

func TestRunOnceUnknownCategory(t *testing.T) {
	ctx := context.Background()
	db := newGCDB(t)
	e, err := New(Opts{DB: db, Config: testConfig(), Clock: fixedClock(baseTime)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := e.RunOnce(ctx, RunOnceOpts{Category: "bogus"}); err == nil {
		t.Error("expected error for unknown category")
	}
}

func TestBlobSweepSkippedWhileRunning(t *testing.T) {
	ctx := context.Background()
	db := newGCDB(t)
	blobs := newBlobStore(t, db)

	// An unreferenced, old blob that would normally be reaped.
	hash, err := blobs.Put(ctx, []byte("orphan content"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE blobs SET created_at = ?", baseTime.Add(-48*time.Hour).Unix()); err != nil {
		t.Fatalf("age blob: %v", err)
	}

	// A run in flight makes the engine skip the blob sweep entirely.
	insertNebula(t, db, "n1", "running", 0)
	insertRun(t, db, "run-live", "n1", "running", "/repo", 0)

	e, err := New(Opts{DB: db, Config: testConfig(), Blobs: blobs, Clock: fixedClock(baseTime)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rep, err := e.RunOnce(ctx, RunOnceOpts{Category: CategoryBlobs})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Blobs != nil {
		t.Errorf("expected blob sweep to be skipped, got report %+v", rep.Blobs)
	}
	if !blobs.Has(ctx, hash) {
		t.Error("blob was reaped despite an active run")
	}
}

func TestBlobSweepReapsOrphans(t *testing.T) {
	ctx := context.Background()
	db := newGCDB(t)
	blobs := newBlobStore(t, db)

	orphan, err := blobs.Put(ctx, []byte("orphan content"))
	if err != nil {
		t.Fatalf("Put orphan: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE blobs SET created_at = ?", baseTime.Add(-48*time.Hour).Unix()); err != nil {
		t.Fatalf("age blob: %v", err)
	}

	e, err := New(Opts{DB: db, Config: testConfig(), Blobs: blobs, Clock: fixedClock(baseTime)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rep, err := e.RunOnce(ctx, RunOnceOpts{Category: CategoryBlobs})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Blobs == nil || len(rep.Blobs.Swept) != 1 {
		t.Fatalf("expected 1 swept blob, got %+v", rep.Blobs)
	}
	if blobs.Has(ctx, orphan) {
		t.Error("orphan blob not reaped")
	}
}

func TestReapWorktreesWiresProtection(t *testing.T) {
	ctx := context.Background()
	db := newGCDB(t)
	insertRepo(t, db, "/repo-a")
	insertNebula(t, db, "n1", "running", 0)
	insertRun(t, db, "run-live", "n1", "running", "/repo-a", 0) // non-terminal => protected

	reaper := &fakeReaper{}
	e, err := New(Opts{DB: db, Config: testConfig(), Reaper: reaper, Clock: fixedClock(baseTime)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rep, err := e.RunOnce(ctx, RunOnceOpts{ReapWorktrees: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(reaper.repos) != 1 || reaper.repos[0] != "/repo-a" {
		t.Errorf("reaper invoked for repos %v, want [/repo-a]", reaper.repos)
	}
	if !reaper.protected["run-live"] {
		t.Error("isProtected predicate did not flag the in-flight run id")
	}
	if len(rep.Worktrees) != 1 {
		t.Errorf("got %d worktree results, want 1", len(rep.Worktrees))
	}
}

func TestRunOnceDryRunLeavesLedgerEmpty(t *testing.T) {
	ctx := context.Background()
	db := newGCDB(t)
	insertNebula(t, db, "n1", "completed", baseTime.Add(-2*time.Hour).Unix())

	var logBuf bytes.Buffer
	e, err := New(Opts{DB: db, Config: testConfig(), Clock: fixedClock(baseTime), Logger: &logBuf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rep, err := e.RunOnce(ctx, RunOnceOpts{DryRun: true, Category: CategoryCompletedNebulas})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !rep.DryRun {
		t.Error("report should be flagged dry run")
	}
	if got := categoryResult(rep, CategoryCompletedNebulas); got.Marked != 1 {
		t.Errorf("dry run marked = %d, want 1 (reported)", got.Marked)
	}
	// Dry run must not soft-delete or write a ledger row.
	if count(t, db, "SELECT COUNT(*) FROM nebulas WHERE deleted_at IS NOT NULL") != 0 {
		t.Error("dry run mutated the nebulas table")
	}
	if count(t, db, "SELECT COUNT(*) FROM gc_runs") != 0 {
		t.Error("dry run wrote a gc_runs ledger row")
	}
}

// categoryResult finds the CategoryResult for name in a report, failing the test
// if absent.
func categoryResult(rep *Report, name string) CategoryResult {
	for _, c := range rep.Categories {
		if c.Category == name {
			return c
		}
	}
	return CategoryResult{}
}
