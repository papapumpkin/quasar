package checkpoint

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// TestFabricStoreEndToEnd exercises the production NewFabricStore adapter against
// a real SQLite-backed fabric: a snapshot persists through the checkpoints /
// checkpoint_files tables and round-trips via Latest, then restores byte-exact.
func TestFabricStoreEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.Close() })

	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	store := NewFabricStore(fabric.NewCheckpointStore(fab.DB()))

	work := writeWorktree(t, map[string]string{"a.go": "package a", "b.go": "package b"})
	c := New(work, blobs, store, "run-e2e", nil)
	c.SetCycle(3)

	cp, err := c.Snapshot(ctx, "go test -short ./...")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if cp.Cycle != 3 {
		t.Errorf("cycle = %d, want 3", cp.Cycle)
	}

	// Latest (through the adapter) returns the persisted checkpoint with files.
	got, err := store.Latest(ctx, "run-e2e")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got.ManifestHash != cp.ManifestHash || len(got.Files) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	dest := t.TempDir()
	if err := c.Restore(ctx, got, dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(dest, "a.go"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if string(restored) != "package a" {
		t.Errorf("restored a.go = %q, want %q", restored, "package a")
	}
}

// fakeStore is an in-memory Store for exercising the Checkpointer without a
// database. It records every Insert so tests can assert how many rows were
// written.
type fakeStore struct {
	inserted []StoredCheckpoint
	latest   *StoredCheckpoint
}

func (f *fakeStore) Insert(_ context.Context, cp StoredCheckpoint) (int64, error) {
	cp.ID = int64(len(f.inserted) + 1)
	f.inserted = append(f.inserted, cp)
	f.latest = &cp
	return cp.ID, nil
}

func (f *fakeStore) Latest(_ context.Context, _ string) (*StoredCheckpoint, error) {
	if f.latest == nil {
		return nil, ErrNoCheckpoint
	}
	return f.latest, nil
}

// newTestBlobs builds a temp-dir blobstore using the same setup as the blobstore
// package's own tests.
func newTestBlobs(t *testing.T) *blobstore.Store {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), db)
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	return blobs
}

// writeWorktree materializes files (path -> content) under a fresh temp dir.
func writeWorktree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func TestMaybeCheckpoint(t *testing.T) {
	ctx := context.Background()

	t.Run("writes a checkpoint on an exit-0 trigger", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "package main"})
		store := &fakeStore{}
		c := New(work, newTestBlobs(t), store, "run-1", nil)

		cp, err := c.MaybeCheckpoint(ctx, ToolResult{Command: "go build ./...", ExitCode: 0})
		if err != nil {
			t.Fatalf("MaybeCheckpoint: %v", err)
		}
		if cp == nil {
			t.Fatal("expected a checkpoint, got nil")
		}
		if len(store.inserted) != 1 {
			t.Fatalf("got %d rows, want 1", len(store.inserted))
		}
		if cp.Trigger != "go build ./..." || cp.RunID != "run-1" {
			t.Errorf("unexpected checkpoint: %+v", cp)
		}
		if len(cp.Files) != 1 || cp.Files[0].Path != "main.go" {
			t.Errorf("unexpected files: %+v", cp.Files)
		}
	})

	t.Run("does not checkpoint on a non-zero exit", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "package main"})
		store := &fakeStore{}
		c := New(work, newTestBlobs(t), store, "run-1", nil)

		cp, err := c.MaybeCheckpoint(ctx, ToolResult{Command: "go build ./...", ExitCode: 1})
		if err != nil {
			t.Fatalf("MaybeCheckpoint: %v", err)
		}
		if cp != nil || len(store.inserted) != 0 {
			t.Fatalf("expected no checkpoint, got %+v (%d rows)", cp, len(store.inserted))
		}
	})

	t.Run("does not checkpoint on a non-trigger command", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "package main"})
		store := &fakeStore{}
		c := New(work, newTestBlobs(t), store, "run-1", nil)

		cp, err := c.MaybeCheckpoint(ctx, ToolResult{Command: "ls -la", ExitCode: 0})
		if err != nil {
			t.Fatalf("MaybeCheckpoint: %v", err)
		}
		if cp != nil || len(store.inserted) != 0 {
			t.Fatalf("expected no checkpoint, got %+v (%d rows)", cp, len(store.inserted))
		}
	})

	t.Run("dedups an unchanged tree: no second row", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "package main"})
		store := &fakeStore{}
		c := New(work, newTestBlobs(t), store, "run-1", nil)

		first, err := c.MaybeCheckpoint(ctx, ToolResult{Command: "go vet ./...", ExitCode: 0})
		if err != nil || first == nil {
			t.Fatalf("first MaybeCheckpoint: cp=%v err=%v", first, err)
		}
		second, err := c.MaybeCheckpoint(ctx, ToolResult{Command: "go vet ./...", ExitCode: 0})
		if err != nil {
			t.Fatalf("second MaybeCheckpoint: %v", err)
		}
		if second != nil {
			t.Errorf("expected dedup (nil), got %+v", second)
		}
		if len(store.inserted) != 1 {
			t.Errorf("got %d rows, want 1 (dedup)", len(store.inserted))
		}
	})

	t.Run("checkpoints again after a file changes", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "package main"})
		store := &fakeStore{}
		c := New(work, newTestBlobs(t), store, "run-1", nil)

		if _, err := c.MaybeCheckpoint(ctx, ToolResult{Command: "go build ./...", ExitCode: 0}); err != nil {
			t.Fatalf("first: %v", err)
		}
		if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package main // edited"), 0o644); err != nil {
			t.Fatalf("edit: %v", err)
		}
		cp, err := c.MaybeCheckpoint(ctx, ToolResult{Command: "go build ./...", ExitCode: 0})
		if err != nil || cp == nil {
			t.Fatalf("second: cp=%v err=%v", cp, err)
		}
		if len(store.inserted) != 2 {
			t.Errorf("got %d rows, want 2", len(store.inserted))
		}
	})
}

func TestSnapshotManifestDeterminism(t *testing.T) {
	ctx := context.Background()
	work := writeWorktree(t, map[string]string{
		"internal/foo.go": "package internal",
		"internal/bar.go": "package internal",
	})
	c := New(work, newTestBlobs(t), &fakeStore{}, "run-1", nil)

	a, err := c.Snapshot(ctx, "go build ./...")
	if err != nil {
		t.Fatalf("snapshot a: %v", err)
	}
	b, err := c.Snapshot(ctx, "go build ./...")
	if err != nil {
		t.Fatalf("snapshot b: %v", err)
	}
	if a.ManifestHash == "" {
		t.Fatal("empty manifest hash")
	}
	if a.ManifestHash != b.ManifestHash {
		t.Errorf("manifest hash differs across identical snapshots: %q vs %q", a.ManifestHash, b.ManifestHash)
	}
}

func TestRestoreByteIdentical(t *testing.T) {
	ctx := context.Background()
	original := map[string]string{
		"main.go":         "package main\n\nfunc main() {}\n",
		"internal/x/y.go": "package x\n",
		"docs/readme.txt": "hello\nworld\n",
	}
	work := writeWorktree(t, original)
	c := New(work, newTestBlobs(t), &fakeStore{}, "run-1", nil)

	cp, err := c.Snapshot(ctx, "go build ./...")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	dest := t.TempDir()
	if err := c.Restore(ctx, cp, dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for rel, want := range original {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read restored %s: %v", rel, err)
		}
		if sum(got) != sum([]byte(want)) {
			t.Errorf("restored %s differs from original", rel)
		}
	}
}

func TestRestoreForReview(t *testing.T) {
	ctx := context.Background()

	t.Run("produces partial and checkpoint trees", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "v1"})
		store := &fakeStore{}
		c := New(work, newTestBlobs(t), store, "run-1", nil)

		if _, err := c.Snapshot(ctx, "go build ./..."); err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		// Simulate post-checkpoint in-flight work that the coder died mid-edit.
		if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("v2-broken"), 0o644); err != nil {
			t.Fatalf("edit: %v", err)
		}

		base := t.TempDir()
		partial, checkpoint, err := c.RestoreForReview(ctx, base)
		if err != nil {
			t.Fatalf("RestoreForReview: %v", err)
		}
		gotPartial, _ := os.ReadFile(filepath.Join(partial, "main.go"))
		gotCheckpoint, _ := os.ReadFile(filepath.Join(checkpoint, "main.go"))
		if string(gotPartial) != "v2-broken" {
			t.Errorf("partial = %q, want v2-broken", gotPartial)
		}
		if string(gotCheckpoint) != "v1" {
			t.Errorf("checkpoint = %q, want v1 (last green build)", gotCheckpoint)
		}
	})

	t.Run("returns ErrNoCheckpoint when none exist", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "v1"})
		c := New(work, newTestBlobs(t), &fakeStore{}, "run-1", nil)
		if _, _, err := c.RestoreForReview(ctx, t.TempDir()); err != ErrNoCheckpoint {
			t.Fatalf("err = %v, want ErrNoCheckpoint", err)
		}
	})
}

func TestIsTrigger(t *testing.T) {
	c := New(t.TempDir(), nil, &fakeStore{}, "run-1", []string{"go build ./...", "make test"})
	cases := []struct {
		cmd  string
		want bool
	}{
		{"go build ./...", true},
		{"go  build   ./...", true},    // whitespace-normalized
		{"go build ./... -race", true}, // trailing args
		{"make test", true},
		{"go vet ./...", false}, // not in this star's triggers
		{"go buildxyz", false},  // not a prefix-with-space match
		{"", false},
	}
	for _, tc := range cases {
		if got := c.isTrigger(tc.cmd); got != tc.want {
			t.Errorf("isTrigger(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestDefaultTriggersUsedWhenEmpty(t *testing.T) {
	c := New(t.TempDir(), nil, &fakeStore{}, "run-1", nil)
	if !c.isTrigger("go test -short ./...") {
		t.Error("expected default Go triggers to be active when none provided")
	}
	// DefaultTriggers returns a fresh slice each call (no shared mutable state).
	a := DefaultTriggers()
	a[0] = "mutated"
	if DefaultTriggers()[0] == "mutated" {
		t.Error("DefaultTriggers leaks shared state")
	}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
