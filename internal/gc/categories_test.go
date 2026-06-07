package gc

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// baseTime is a fixed reference instant used across GC tests so the injected
// clock is fully deterministic.
var baseTime = time.Unix(1_700_000_000, 0).UTC()

// newGCDB returns a migrated SQLite database (the real fabric schema) backed by
// a temp file, suitable for exercising the row sweepers against actual columns
// and foreign keys.
func newGCDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(dir, "gc.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.Close() })
	return fab.DB()
}

// insertRepo registers an active repo row so the worktree reaper and run sweep
// can discover it.
func insertRepo(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO repos (path, name, status, added_at, updated_at, last_seen_at) VALUES (?, ?, 'active', 0, 0, 0)",
		path, filepath.Base(path))
	if err != nil {
		t.Fatalf("insert repo: %v", err)
	}
}

// insertNebula inserts a nebula with the given status and updated_at (unix).
func insertNebula(t *testing.T, db *sql.DB, id, status string, updatedAt int64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO nebulas (id, repo_path, name, status, created_at, updated_at) VALUES (?, '', ?, ?, 0, ?)",
		id, id, status, updatedAt)
	if err != nil {
		t.Fatalf("insert nebula %s: %v", id, err)
	}
}

// markNebulaDeleted stamps a nebula's deleted_at directly (simulating a prior
// mark phase) so the sweep phase can be exercised in isolation.
func markNebulaDeleted(t *testing.T, db *sql.DB, id string, deletedAt int64) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		"UPDATE nebulas SET deleted_at = ? WHERE id = ?", deletedAt, id); err != nil {
		t.Fatalf("mark nebula deleted: %v", err)
	}
}

// insertPhase adds a phase belonging to a nebula so cascade counts are non-zero.
func insertPhase(t *testing.T, db *sql.DB, nebulaID, phaseID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO phases (nebula_id, id, seq, title, body_blob_hash, frontmatter_toml) VALUES (?, ?, 0, ?, '', '')",
		nebulaID, phaseID, phaseID)
	if err != nil {
		t.Fatalf("insert phase %s: %v", phaseID, err)
	}
}

// insertRun inserts a constellation run with the given state, repo, and
// completed_at.
func insertRun(t *testing.T, db *sql.DB, id, nebulaID, state, repoPath string, completedAt int64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO constellation_runs (id, nebula_id, state, repo_path, created_at, updated_at, completed_at) VALUES (?, ?, ?, ?, 0, 0, ?)",
		id, nebulaID, state, repoPath, completedAt)
	if err != nil {
		t.Fatalf("insert run %s: %v", id, err)
	}
}

// markRunDeleted stamps a run's deleted_at directly.
func markRunDeleted(t *testing.T, db *sql.DB, id string, deletedAt int64) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		"UPDATE constellation_runs SET deleted_at = ? WHERE id = ?", deletedAt, id); err != nil {
		t.Fatalf("mark run deleted: %v", err)
	}
}

// insertInvocation adds a star_invocation child of a run.
func insertInvocation(t *testing.T, db *sql.DB, runID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		"INSERT INTO star_invocations (run_id, seq, node, star_name, state) VALUES (?, 0, 'coder', 'coder', 'done')",
		runID); err != nil {
		t.Fatalf("insert invocation: %v", err)
	}
}

// count runs a single-int aggregate query for assertions.
func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

func TestSweepNebulas(t *testing.T) {
	ctx := context.Background()
	ttl := time.Hour
	grace := 24 * time.Hour

	t.Run("marks a terminal nebula whose updated_at is past the TTL", func(t *testing.T) {
		db := newGCDB(t)
		old := baseTime.Add(-2 * time.Hour).Unix()      // older than ttl
		fresh := baseTime.Add(-30 * time.Minute).Unix() // within ttl
		insertNebula(t, db, "old", "completed", old)
		insertNebula(t, db, "fresh", "completed", fresh)
		insertNebula(t, db, "active", "running", old) // wrong status: never marked

		res := sweepNebulas(ctx, db, nil, baseTime, CategoryCompletedNebulas, completedNebulaStatuses, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepNebulas: %v", res.Err)
		}
		if res.Marked != 1 {
			t.Errorf("marked = %d, want 1", res.Marked)
		}
		if count(t, db, "SELECT COUNT(*) FROM nebulas WHERE deleted_at IS NOT NULL") != 1 {
			t.Error("expected exactly one nebula soft-deleted")
		}
		if count(t, db, "SELECT COUNT(*) FROM nebulas WHERE id = 'old' AND deleted_at IS NOT NULL") != 1 {
			t.Error("the old terminal nebula should be marked")
		}
	})

	t.Run("never marks a nebula with a non-terminal run", func(t *testing.T) {
		db := newGCDB(t)
		old := baseTime.Add(-2 * time.Hour).Unix()
		insertNebula(t, db, "busy", "completed", old)
		insertRun(t, db, "run-1", "busy", "running", "/repo", 0)

		res := sweepNebulas(ctx, db, nil, baseTime, CategoryCompletedNebulas, completedNebulaStatuses, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepNebulas: %v", res.Err)
		}
		if res.Marked != 0 {
			t.Errorf("marked = %d, want 0 (run still in flight)", res.Marked)
		}
	})

	t.Run("hard-deletes past grace and cascades phases", func(t *testing.T) {
		db := newGCDB(t)
		insertNebula(t, db, "doomed", "completed", baseTime.Unix())
		insertPhase(t, db, "doomed", "p1")
		insertPhase(t, db, "doomed", "p2")
		// Soft-deleted longer ago than the grace window.
		markNebulaDeleted(t, db, "doomed", baseTime.Add(-2*grace).Unix())

		res := sweepNebulas(ctx, db, nil, baseTime, CategoryCompletedNebulas, completedNebulaStatuses, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepNebulas: %v", res.Err)
		}
		if res.Swept != 1 || res.CascadedChildren != 2 {
			t.Errorf("swept=%d cascaded=%d, want 1 and 2", res.Swept, res.CascadedChildren)
		}
		if count(t, db, "SELECT COUNT(*) FROM nebulas WHERE id = 'doomed'") != 0 {
			t.Error("nebula not hard-deleted")
		}
		if count(t, db, "SELECT COUNT(*) FROM phases WHERE nebula_id = 'doomed'") != 0 {
			t.Error("phases not cascaded")
		}
	})

	t.Run("does not sweep while still within grace", func(t *testing.T) {
		db := newGCDB(t)
		insertNebula(t, db, "recent", "completed", baseTime.Unix())
		markNebulaDeleted(t, db, "recent", baseTime.Add(-grace/2).Unix())

		res := sweepNebulas(ctx, db, nil, baseTime, CategoryCompletedNebulas, completedNebulaStatuses, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepNebulas: %v", res.Err)
		}
		if res.Swept != 0 {
			t.Errorf("swept = %d, want 0 (still in grace)", res.Swept)
		}
		if count(t, db, "SELECT COUNT(*) FROM nebulas WHERE id = 'recent'") != 1 {
			t.Error("nebula deleted before grace elapsed")
		}
	})

	t.Run("dry run mutates nothing", func(t *testing.T) {
		db := newGCDB(t)
		insertNebula(t, db, "old", "completed", baseTime.Add(-2*time.Hour).Unix())
		insertNebula(t, db, "doomed", "completed", baseTime.Unix())
		markNebulaDeleted(t, db, "doomed", baseTime.Add(-2*grace).Unix())

		res := sweepNebulas(ctx, db, nil, baseTime, CategoryCompletedNebulas, completedNebulaStatuses, ttl, grace, true)
		if res.Err != nil {
			t.Fatalf("sweepNebulas: %v", res.Err)
		}
		if res.Marked != 1 || res.Swept != 1 {
			t.Errorf("dry run report marked=%d swept=%d, want 1 and 1", res.Marked, res.Swept)
		}
		// Nothing actually changed: 'old' still un-marked, 'doomed' still present.
		if count(t, db, "SELECT COUNT(*) FROM nebulas WHERE id = 'old' AND deleted_at IS NULL") != 1 {
			t.Error("dry run marked a row")
		}
		if count(t, db, "SELECT COUNT(*) FROM nebulas WHERE id = 'doomed'") != 1 {
			t.Error("dry run hard-deleted a row")
		}
	})
}

func TestSweepRuns(t *testing.T) {
	ctx := context.Background()
	ttl := time.Hour
	grace := 24 * time.Hour
	old := baseTime.Add(-2 * time.Hour).Unix()

	t.Run("marks a terminal run past TTL", func(t *testing.T) {
		db := newGCDB(t)
		insertNebula(t, db, "n1", "completed", 0)
		insertRun(t, db, "run-done", "n1", "done", "/repo-a", old)

		res := sweepRuns(ctx, db, nil, baseTime, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepRuns: %v", res.Err)
		}
		if res.Marked != 1 {
			t.Errorf("marked = %d, want 1", res.Marked)
		}
	})

	t.Run("skips an entire repo that has a non-terminal run", func(t *testing.T) {
		db := newGCDB(t)
		insertNebula(t, db, "n1", "completed", 0)
		insertNebula(t, db, "n2", "completed", 0)
		// A terminal run and a still-running run share the busy repo.
		insertRun(t, db, "run-done", "n1", "done", "/busy", old)
		insertRun(t, db, "run-live", "n2", "running", "/busy", 0)

		res := sweepRuns(ctx, db, nil, baseTime, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepRuns: %v", res.Err)
		}
		if res.Marked != 0 {
			t.Errorf("marked = %d, want 0 (repo is busy)", res.Marked)
		}
	})

	t.Run("hard-deletes past grace and cascades star_invocations", func(t *testing.T) {
		db := newGCDB(t)
		insertNebula(t, db, "n1", "completed", 0)
		insertRun(t, db, "run-old", "n1", "done", "/repo-a", old)
		insertInvocation(t, db, "run-old")
		insertInvocation(t, db, "run-old")
		markRunDeleted(t, db, "run-old", baseTime.Add(-2*grace).Unix())

		res := sweepRuns(ctx, db, nil, baseTime, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepRuns: %v", res.Err)
		}
		if res.Swept != 1 || res.CascadedChildren != 2 {
			t.Errorf("swept=%d cascaded=%d, want 1 and 2", res.Swept, res.CascadedChildren)
		}
		if count(t, db, "SELECT COUNT(*) FROM star_invocations WHERE run_id = 'run-old'") != 0 {
			t.Error("invocations not cascaded")
		}
	})

	t.Run("cascades checkpoints and frees their blobs", func(t *testing.T) {
		db := newGCDB(t)
		blobs, err := blobstore.New(filepath.Join(t.TempDir(), "blobs"), db)
		if err != nil {
			t.Fatalf("blobstore.New: %v", err)
		}
		insertNebula(t, db, "n1", "completed", 0)
		insertRun(t, db, "run-cp", "n1", "done", "/repo-a", old)

		// Persist a real checkpoint: two file blobs + a manifest blob, referenced
		// from the checkpoints / checkpoint_files rows.
		fooHash, _ := blobs.Put(ctx, []byte("package foo"))
		barHash, _ := blobs.Put(ctx, []byte("package bar"))
		manHash, _ := blobs.Put(ctx, []byte(`{"foo.go":{"h":"x"},"bar.go":{"h":"y"}}`))
		cpStore := fabric.NewCheckpointStore(db)
		if _, err := cpStore.Insert(ctx, fabric.CheckpointRow{
			RunID: "run-cp", Cycle: 1, Trigger: "go build ./...", ManifestHash: manHash,
			Files: []fabric.CheckpointFile{
				{Path: "foo.go", BlobHash: fooHash, Mode: 0o644},
				{Path: "bar.go", BlobHash: barHash, Mode: 0o644},
			},
		}); err != nil {
			t.Fatalf("insert checkpoint: %v", err)
		}

		markRunDeleted(t, db, "run-cp", baseTime.Add(-2*grace).Unix())
		res := sweepRuns(ctx, db, nil, baseTime, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepRuns: %v", res.Err)
		}
		// One run swept; CascadedChildren counts the single checkpoint row.
		if res.Swept != 1 || res.CascadedChildren != 1 {
			t.Errorf("swept=%d cascaded=%d, want 1 and 1", res.Swept, res.CascadedChildren)
		}
		if count(t, db, "SELECT COUNT(*) FROM checkpoints WHERE run_id = 'run-cp'") != 0 {
			t.Error("checkpoints not cascaded")
		}
		if count(t, db, "SELECT COUNT(*) FROM checkpoint_files") != 0 {
			t.Error("checkpoint_files not cascaded")
		}

		// With the referencing rows gone, the blob sweep reclaims all three blobs.
		// Blobs are stamped with real wall-clock time on Put, so the sweep's "now"
		// must be real time (not baseTime) for the min-age check to pass.
		rep, err := blobs.Sweep(ctx, 0, time.Now().Add(time.Hour), false)
		if err != nil {
			t.Fatalf("blob Sweep: %v", err)
		}
		if len(rep.Swept) != 3 {
			t.Errorf("blob sweep reclaimed %d blobs, want 3 (manifest + 2 files)", len(rep.Swept))
		}
	})
}

func TestSweepSensorEvents(t *testing.T) {
	ctx := context.Background()
	ttl := time.Hour
	grace := 24 * time.Hour
	old := baseTime.Add(-2 * time.Hour).Unix()

	insertEvent := func(t *testing.T, db *sql.DB, extID string, receivedAt int64, processed bool) {
		t.Helper()
		var processedAt any
		if processed {
			processedAt = receivedAt
		}
		_, err := db.ExecContext(ctx,
			"INSERT INTO sensor_events (repo_path, sensor_name, external_id, received_at, processed_at) VALUES ('/r', 'github', ?, ?, ?)",
			extID, receivedAt, processedAt)
		if err != nil {
			t.Fatalf("insert event: %v", err)
		}
		insertRepo(t, db, "/r-"+extID) // unrelated; keeps repos table populated
	}

	t.Run("marks processed events past TTL only", func(t *testing.T) {
		db := newGCDB(t)
		insertEvent(t, db, "e-old", old, true)
		insertEvent(t, db, "e-unprocessed", old, false) // never marked
		insertEvent(t, db, "e-fresh", baseTime.Unix(), true)

		res := sweepSensorEvents(ctx, db, nil, baseTime, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepSensorEvents: %v", res.Err)
		}
		if res.Marked != 1 {
			t.Errorf("marked = %d, want 1", res.Marked)
		}
	})

	t.Run("hard-deletes past grace", func(t *testing.T) {
		db := newGCDB(t)
		insertEvent(t, db, "e1", old, true)
		if _, err := db.ExecContext(ctx, "UPDATE sensor_events SET deleted_at = ?", baseTime.Add(-2*grace).Unix()); err != nil {
			t.Fatalf("mark: %v", err)
		}
		res := sweepSensorEvents(ctx, db, nil, baseTime, ttl, grace, false)
		if res.Err != nil {
			t.Fatalf("sweepSensorEvents: %v", res.Err)
		}
		if res.Swept != 1 {
			t.Errorf("swept = %d, want 1", res.Swept)
		}
		if count(t, db, "SELECT COUNT(*) FROM sensor_events") != 0 {
			t.Error("event not hard-deleted")
		}
	})
}

func TestSweepTriggerQueue(t *testing.T) {
	ctx := context.Background()
	ttl := time.Hour
	old := baseTime.Add(-2 * time.Hour).Unix()

	insertTrigger := func(t *testing.T, db *sql.DB, state string, consumedAt any) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			"INSERT INTO trigger_queue (nebula_id, constellation_name, state, created_at, consumed_at) VALUES ('n', 'c', ?, 0, ?)",
			state, consumedAt)
		if err != nil {
			t.Fatalf("insert trigger: %v", err)
		}
	}

	t.Run("deletes consumed triggers past TTL, keeps pending and fresh", func(t *testing.T) {
		db := newGCDB(t)
		insertTrigger(t, db, "consumed", old)
		insertTrigger(t, db, "consumed", baseTime.Unix()) // fresh: kept
		insertTrigger(t, db, "pending", nil)              // never consumed: kept

		res := sweepTriggerQueue(ctx, db, nil, baseTime, ttl, false)
		if res.Err != nil {
			t.Fatalf("sweepTriggerQueue: %v", res.Err)
		}
		if res.Swept != 1 {
			t.Errorf("swept = %d, want 1", res.Swept)
		}
		if count(t, db, "SELECT COUNT(*) FROM trigger_queue") != 2 {
			t.Error("wrong number of triggers retained")
		}
	})

	t.Run("dry run counts but does not delete", func(t *testing.T) {
		db := newGCDB(t)
		insertTrigger(t, db, "consumed", old)
		res := sweepTriggerQueue(ctx, db, nil, baseTime, ttl, true)
		if res.Err != nil {
			t.Fatalf("sweepTriggerQueue: %v", res.Err)
		}
		if res.Swept != 1 {
			t.Errorf("swept = %d, want 1 (counted)", res.Swept)
		}
		if count(t, db, "SELECT COUNT(*) FROM trigger_queue") != 1 {
			t.Error("dry run deleted a trigger")
		}
	})
}

func TestPlaceholders(t *testing.T) {
	t.Parallel()
	cases := map[int]string{0: "''", 1: "?", 2: "?, ?", 3: "?, ?, ?"}
	for n, want := range cases {
		if got := placeholders(n); got != want {
			t.Errorf("placeholders(%d) = %q, want %q", n, got, want)
		}
	}
}
