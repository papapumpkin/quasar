package checkpoint

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// fakeStore is an in-memory Store for exercising the Snapshotter without a
// database. It records every Insert so tests can assert how many rows were
// written.
type fakeStore struct {
	inserted []Snapshot
	latest   *Snapshot
}

func (f *fakeStore) Insert(_ context.Context, snap Snapshot) (int64, error) {
	snap.ID = int64(len(f.inserted) + 1)
	f.inserted = append(f.inserted, snap)
	f.latest = &snap
	return snap.ID, nil
}

func (f *fakeStore) Latest(_ context.Context, _ string) (*Snapshot, error) {
	if f.latest == nil {
		return nil, ErrNoSnapshot
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

func TestCheckpoint(t *testing.T) {
	ctx := context.Background()

	t.Run("writes a deduped snapshot", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "package main"})
		store := &fakeStore{}
		s := NewSnapshotter(work, newTestBlobs(t), store)

		snap, err := s.Checkpoint(ctx, "run-1", 1, "post-dispatch:coder")
		if err != nil {
			t.Fatalf("Checkpoint: %v", err)
		}
		if snap == nil {
			t.Fatal("expected a snapshot, got nil")
		}
		if len(store.inserted) != 1 {
			t.Fatalf("got %d rows, want 1", len(store.inserted))
		}
		if snap.Trigger != "post-dispatch:coder" || snap.RunID != "run-1" {
			t.Errorf("unexpected snapshot: %+v", snap)
		}
		if len(snap.Files) != 1 || snap.Files[0].Path != "main.go" {
			t.Errorf("unexpected files: %+v", snap.Files)
		}
	})

	t.Run("dedups an unchanged tree: no second row", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "package main"})
		store := &fakeStore{}
		s := NewSnapshotter(work, newTestBlobs(t), store)

		first, err := s.Checkpoint(ctx, "run-1", 1, "post-dispatch:coder")
		if err != nil || first == nil {
			t.Fatalf("first Checkpoint: snap=%v err=%v", first, err)
		}
		second, err := s.Checkpoint(ctx, "run-1", 1, "post-dispatch:coder")
		if err != nil {
			t.Fatalf("second Checkpoint: %v", err)
		}
		if second != nil {
			t.Errorf("expected dedup (nil), got %+v", second)
		}
		if len(store.inserted) != 1 {
			t.Errorf("got %d rows, want 1 (dedup)", len(store.inserted))
		}
	})

	t.Run("snapshots again after a file changes", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "package main"})
		store := &fakeStore{}
		s := NewSnapshotter(work, newTestBlobs(t), store)

		if _, err := s.Checkpoint(ctx, "run-1", 1, "post-dispatch:coder"); err != nil {
			t.Fatalf("first: %v", err)
		}
		if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package main // edited"), 0o644); err != nil {
			t.Fatalf("edit: %v", err)
		}
		snap, err := s.Checkpoint(ctx, "run-1", 2, "post-dispatch:coder")
		if err != nil || snap == nil {
			t.Fatalf("second: snap=%v err=%v", snap, err)
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
	s := NewSnapshotter(work, newTestBlobs(t), &fakeStore{})

	a, err := s.Capture(ctx, "run-1", 1, "go build ./...")
	if err != nil {
		t.Fatalf("capture a: %v", err)
	}
	b, err := s.Capture(ctx, "run-1", 1, "go build ./...")
	if err != nil {
		t.Fatalf("capture b: %v", err)
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
	s := NewSnapshotter(work, newTestBlobs(t), &fakeStore{})

	snap, err := s.Capture(ctx, "run-1", 1, "go build ./...")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	dest := t.TempDir()
	if err := s.Restore(ctx, snap, dest); err != nil {
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

// TestRestorePreservesMode verifies the executable bit survives a snapshot +
// restore round-trip. Skipped on Windows, which has no unix permission bits.
func TestRestorePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits not meaningful on Windows")
	}
	ctx := context.Background()
	work := t.TempDir()
	script := filepath.Join(work, "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := NewSnapshotter(work, newTestBlobs(t), &fakeStore{})
	snap, err := s.Capture(ctx, "run-1", 1, "go build ./...")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	dest := t.TempDir()
	if err := s.Restore(ctx, snap, dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("stat restored: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("restored script lost its executable bit: mode %v", info.Mode().Perm())
	}
}

func TestRestoreForReview(t *testing.T) {
	ctx := context.Background()

	t.Run("produces partial and checkpoint trees", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "v1"})
		store := &fakeStore{}
		s := NewSnapshotter(work, newTestBlobs(t), store)

		if _, err := s.Capture(ctx, "run-1", 1, "go build ./..."); err != nil {
			t.Fatalf("Capture: %v", err)
		}
		// Simulate post-snapshot in-flight work that the coder died mid-edit.
		if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("v2-broken"), 0o644); err != nil {
			t.Fatalf("edit: %v", err)
		}

		base := t.TempDir()
		partial, checkpoint, err := s.RestoreForReview(ctx, "run-1", base)
		if err != nil {
			t.Fatalf("RestoreForReview: %v", err)
		}
		gotPartial, _ := os.ReadFile(filepath.Join(partial, "main.go"))
		gotCheckpoint, _ := os.ReadFile(filepath.Join(checkpoint, "main.go"))
		if string(gotPartial) != "v2-broken" {
			t.Errorf("partial = %q, want v2-broken", gotPartial)
		}
		if string(gotCheckpoint) != "v1" {
			t.Errorf("checkpoint = %q, want v1 (last captured snapshot)", gotCheckpoint)
		}
	})

	t.Run("returns ErrNoSnapshot when none exist", func(t *testing.T) {
		work := writeWorktree(t, map[string]string{"main.go": "v1"})
		s := NewSnapshotter(work, newTestBlobs(t), &fakeStore{})
		if _, _, err := s.RestoreForReview(ctx, "run-1", t.TempDir()); err != ErrNoSnapshot {
			t.Fatalf("err = %v, want ErrNoSnapshot", err)
		}
	})
}

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
	s := NewSnapshotter(work, blobs, store)

	snap, err := s.Capture(ctx, "run-e2e", 3, "go test -short ./...")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if snap.Cycle != 3 {
		t.Errorf("cycle = %d, want 3", snap.Cycle)
	}

	// Latest (through the adapter) returns the persisted snapshot with files.
	got, err := store.Latest(ctx, "run-e2e")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got.ManifestHash != snap.ManifestHash || len(got.Files) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	dest := t.TempDir()
	if err := s.Restore(ctx, got, dest); err != nil {
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

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
