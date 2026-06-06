package fabric

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/blobstore"
)

// newRunStoreTest spins up an on-disk SQLite fabric (running all migrations,
// including 004) and returns a run store plus a seeded nebula ID to satisfy the
// nebula_id reference.
func newRunStoreTest(t *testing.T) (*ConstellationRunStore, string) {
	t.Helper()
	dir := t.TempDir()
	fab, err := NewSQLiteFabric(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.db.Close() })

	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	neb := NewNebulaStore(fab.DB(), blobs)
	id, err := neb.Insert(context.Background(), NebulaRow{Name: "demo", Status: "running"})
	if err != nil {
		t.Fatalf("seed nebula: %v", err)
	}
	return NewConstellationRunStore(fab.DB()), id
}

func TestConstellationRunStore(t *testing.T) {
	ctx := context.Background()

	t.Run("insert and get round-trips", func(t *testing.T) {
		store, nebID := newRunStoreTest(t)
		id, err := store.InsertRun(ctx, RunRow{
			RepoPath:          "/repo",
			NebulaID:          nebID,
			ConstellationName: "ship",
			Snapshot:          []byte("name = 'ship'"),
			CurrentNode:       "start",
			DAGStateTOML:      "cycle = 0",
		})
		if err != nil {
			t.Fatalf("InsertRun: %v", err)
		}
		got, err := store.GetRun(ctx, id)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if got.NebulaID != nebID || got.ConstellationName != "ship" {
			t.Fatalf("unexpected run: %+v", got)
		}
		if string(got.Snapshot) != "name = 'ship'" {
			t.Errorf("snapshot not persisted: %q", got.Snapshot)
		}
		if got.State != "running" {
			t.Errorf("default state = %q, want running", got.State)
		}
	})

	t.Run("missing run returns ErrRunNotFound", func(t *testing.T) {
		store, _ := newRunStoreTest(t)
		if _, err := store.GetRun(ctx, "nope"); err != ErrRunNotFound {
			t.Fatalf("err = %v, want ErrRunNotFound", err)
		}
	})

	t.Run("save progress and complete", func(t *testing.T) {
		store, nebID := newRunStoreTest(t)
		id, _ := store.InsertRun(ctx, RunRow{NebulaID: nebID, ConstellationName: "x", CurrentNode: "a"})
		run, _ := store.GetRun(ctx, id)
		run.CurrentNode = "b"
		run.StepIndex = 1
		run.DAGStateTOML = "cycle = 1"
		if err := store.SaveProgress(ctx, run); err != nil {
			t.Fatalf("SaveProgress: %v", err)
		}
		if err := store.Complete(ctx, id, "done"); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		got, _ := store.GetRun(ctx, id)
		if got.CurrentNode != "b" || got.State != "done" || got.CompletedAt == 0 {
			t.Fatalf("post-complete state wrong: %+v", got)
		}
	})

	t.Run("reaper marks stale running runs crashed", func(t *testing.T) {
		store, nebID := newRunStoreTest(t)
		id, _ := store.InsertRun(ctx, RunRow{
			NebulaID: nebID, ConstellationName: "x", CurrentNode: "a", HeartbeatAt: 1,
		})
		n, err := store.ReapStale(ctx, 1000)
		if err != nil {
			t.Fatalf("ReapStale: %v", err)
		}
		if n != 1 {
			t.Fatalf("reaped %d, want 1", n)
		}
		got, _ := store.GetRun(ctx, id)
		if got.State != "crashed" {
			t.Errorf("state = %q, want crashed", got.State)
		}
	})

	t.Run("fresh heartbeat survives reaper", func(t *testing.T) {
		store, nebID := newRunStoreTest(t)
		id, _ := store.InsertRun(ctx, RunRow{NebulaID: nebID, ConstellationName: "x", CurrentNode: "a"})
		if _, err := store.ReapStale(ctx, 1); err != nil {
			t.Fatalf("ReapStale: %v", err)
		}
		got, _ := store.GetRun(ctx, id)
		if got.State != "running" {
			t.Errorf("state = %q, want running", got.State)
		}
	})

	t.Run("star invocation insert", func(t *testing.T) {
		store, nebID := newRunStoreTest(t)
		id, _ := store.InsertRun(ctx, RunRow{NebulaID: nebID, ConstellationName: "x", CurrentNode: "a"})
		rowID, err := store.InsertStarInvocation(ctx, StarInvocationRow{
			RunID: id, Node: "coder", StarName: "coder", State: "done", CostUSD: 0.5, StartedAt: 1,
		})
		if err != nil {
			t.Fatalf("InsertStarInvocation: %v", err)
		}
		if rowID == 0 {
			t.Errorf("expected non-zero invocation row id")
		}
	})
}
