package fabric

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/blobstore"
)

// newCheckpointStoreTest spins up an on-disk SQLite fabric (running all
// migrations, including 008) and returns a checkpoint store plus a seeded run ID
// to satisfy the run_id reference.
func newCheckpointStoreTest(t *testing.T) (*CheckpointStore, string) {
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
	nebID, err := neb.Insert(context.Background(), NebulaRow{Name: "demo", Status: "running"})
	if err != nil {
		t.Fatalf("seed nebula: %v", err)
	}
	runs := NewConstellationRunStore(fab.DB())
	runID, err := runs.InsertRun(context.Background(), RunRow{NebulaID: nebID, ConstellationName: "coder-reviewer", CurrentNode: "coder"})
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return NewCheckpointStore(fab.DB()), runID
}

func TestCheckpointStore(t *testing.T) {
	ctx := context.Background()

	t.Run("insert and latest round-trips with files", func(t *testing.T) {
		store, runID := newCheckpointStoreTest(t)
		id, err := store.Insert(ctx, CheckpointRow{
			RunID:        runID,
			Cycle:        2,
			Trigger:      "go build ./...",
			ManifestHash: "manifest-aaa",
			Files: []CheckpointFile{
				{Path: "internal/foo.go", BlobHash: "blob-foo"},
				{Path: "internal/bar.go", BlobHash: "blob-bar"},
			},
		})
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
		if id == 0 {
			t.Fatal("Insert returned zero id")
		}

		got, err := store.Latest(ctx, runID)
		if err != nil {
			t.Fatalf("Latest: %v", err)
		}
		if got.Cycle != 2 || got.Trigger != "go build ./..." || got.ManifestHash != "manifest-aaa" {
			t.Fatalf("unexpected checkpoint: %+v", got)
		}
		if got.CreatedAt == 0 {
			t.Error("CreatedAt not defaulted")
		}
		if len(got.Files) != 2 {
			t.Fatalf("got %d files, want 2", len(got.Files))
		}
		// Files are ordered by path: bar before foo.
		if got.Files[0].Path != "internal/bar.go" || got.Files[1].Path != "internal/foo.go" {
			t.Errorf("files not path-ordered: %+v", got.Files)
		}
	})

	t.Run("latest picks the most recent of several", func(t *testing.T) {
		store, runID := newCheckpointStoreTest(t)
		for i, h := range []string{"m1", "m2", "m3"} {
			if _, err := store.Insert(ctx, CheckpointRow{RunID: runID, Cycle: i + 1, Trigger: "go vet ./...", ManifestHash: h}); err != nil {
				t.Fatalf("Insert %s: %v", h, err)
			}
		}
		got, err := store.Latest(ctx, runID)
		if err != nil {
			t.Fatalf("Latest: %v", err)
		}
		if got.ManifestHash != "m3" {
			t.Errorf("latest manifest = %q, want m3", got.ManifestHash)
		}

		all, err := store.ListForRun(ctx, runID)
		if err != nil {
			t.Fatalf("ListForRun: %v", err)
		}
		if len(all) != 3 || all[0].ManifestHash != "m1" || all[2].ManifestHash != "m3" {
			t.Errorf("ListForRun ordering wrong: %+v", all)
		}
	})

	t.Run("missing run returns ErrCheckpointNotFound", func(t *testing.T) {
		store, _ := newCheckpointStoreTest(t)
		if _, err := store.Latest(ctx, "no-such-run"); err != ErrCheckpointNotFound {
			t.Fatalf("err = %v, want ErrCheckpointNotFound", err)
		}
	})

	t.Run("insert rejects missing run_id and manifest", func(t *testing.T) {
		store, runID := newCheckpointStoreTest(t)
		if _, err := store.Insert(ctx, CheckpointRow{ManifestHash: "m"}); err == nil {
			t.Error("expected error for empty run_id")
		}
		if _, err := store.Insert(ctx, CheckpointRow{RunID: runID}); err == nil {
			t.Error("expected error for empty manifest hash")
		}
	})
}

// TestCheckpointsForeignKey verifies the migration declares the run_id foreign
// key to constellation_runs with ON DELETE CASCADE. The fabric does not enable
// PRAGMA foreign_keys globally, so this asserts the declaration structurally via
// PRAGMA foreign_key_list rather than exercising runtime cascade.
func TestCheckpointsForeignKey(t *testing.T) {
	store, _ := newCheckpointStoreTest(t)
	ctx := context.Background()

	rows, err := store.db.QueryContext(ctx, "PRAGMA foreign_key_list(checkpoints)")
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_list: %v", err)
	}
	defer rows.Close() //nolint:errcheck // iteration cleanup

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}

	found := false
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		row := map[string]string{}
		for i, name := range cols {
			row[name] = asString(cells[i])
		}
		if row["table"] == "constellation_runs" && row["from"] == "run_id" {
			found = true
			if row["to"] != "id" {
				t.Errorf("FK references %q, want id", row["to"])
			}
			if row["on_delete"] != "CASCADE" {
				t.Errorf("FK on_delete = %q, want CASCADE", row["on_delete"])
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !found {
		t.Error("checkpoints.run_id FK to constellation_runs not declared")
	}
}

// asString renders a PRAGMA cell (which may arrive as []byte, string, or int64)
// as a string for comparison.
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return ""
	}
}
