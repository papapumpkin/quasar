package blobstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestStore returns a Store backed by a temp-dir SQLite database and blob
// root, suitable for round-trip and registry assertions.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := New(filepath.Join(dir, "blobs"), db)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func TestPut(t *testing.T) {
	ctx := context.Background()

	t.Run("computes hash and writes file at fanout path", func(t *testing.T) {
		s := newTestStore(t)
		content := []byte("hello blobstore")

		hash, err := s.Put(ctx, content)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}

		sum := sha256.Sum256(content)
		want := hex.EncodeToString(sum[:])
		if hash != want {
			t.Errorf("hash = %q, want %q", hash, want)
		}

		_, file := s.path(hash)
		if file != filepath.Join(s.root, hash[:2], hash[2:]) {
			t.Errorf("fanout path = %q, want <root>/%s/%s", file, hash[:2], hash[2:])
		}
		if _, err := os.Stat(file); err != nil {
			t.Errorf("blob file not written: %v", err)
		}
	})

	t.Run("inserts a registry row", func(t *testing.T) {
		s := newTestStore(t)
		content := []byte("registry row content")

		hash, err := s.Put(ctx, content)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}

		var size int64
		err = s.db.QueryRowContext(ctx, "SELECT size_bytes FROM blobs WHERE hash = ?", hash).Scan(&size)
		if err != nil {
			t.Fatalf("query row: %v", err)
		}
		if size != int64(len(content)) {
			t.Errorf("size_bytes = %d, want %d", size, len(content))
		}
	})

	t.Run("is idempotent: one file, one row", func(t *testing.T) {
		s := newTestStore(t)
		content := []byte("dedup me")

		h1, err := s.Put(ctx, content)
		if err != nil {
			t.Fatalf("Put #1: %v", err)
		}
		h2, err := s.Put(ctx, content)
		if err != nil {
			t.Fatalf("Put #2: %v", err)
		}
		if h1 != h2 {
			t.Errorf("hashes differ: %q vs %q", h1, h2)
		}

		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blobs WHERE hash = ?", h1).Scan(&count); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		if count != 1 {
			t.Errorf("row count = %d, want 1", count)
		}
	})
}

func TestGet(t *testing.T) {
	ctx := context.Background()

	t.Run("round-trips content through compression", func(t *testing.T) {
		s := newTestStore(t)
		// Use highly compressible content to exercise zstd.
		content := make([]byte, 4096)
		for i := range content {
			content[i] = byte(i % 7)
		}

		hash, err := s.Put(ctx, content)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}

		got, err := s.Get(ctx, hash)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("round-trip mismatch: got %d bytes, want %d", len(got), len(content))
		}
	})

	t.Run("missing hash returns ErrBlobNotFound", func(t *testing.T) {
		s := newTestStore(t)
		_, err := s.Get(ctx, "deadbeef")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrBlobNotFound) {
			t.Errorf("err = %v, want ErrBlobNotFound", err)
		}
	})
}

func TestHas(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if s.Has(ctx, "deadbeef") {
		t.Error("Has returned true for missing blob")
	}

	hash, err := s.Put(ctx, []byte("present"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !s.Has(ctx, hash) {
		t.Error("Has returned false for present blob")
	}
}

func TestWalk(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	want := map[string]bool{}
	for _, c := range [][]byte{[]byte("a"), []byte("b"), []byte("c")} {
		h, err := s.Put(ctx, c)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		want[h] = true
	}

	seq, err := s.Walk(ctx)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := map[string]bool{}
	for info := range seq {
		got[info.Hash] = true
		if info.SizeBytes != 1 {
			t.Errorf("SizeBytes = %d, want 1", info.SizeBytes)
		}
	}
	if len(got) != len(want) {
		t.Errorf("walked %d blobs, want %d", len(got), len(want))
	}
	for h := range want {
		if !got[h] {
			t.Errorf("blob %s missing from walk", h)
		}
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hash, err := s.Put(ctx, []byte("delete me"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(ctx, hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Has(ctx, hash) {
		t.Error("blob still present after Delete")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blobs WHERE hash = ?", hash).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Errorf("row count = %d, want 0", count)
	}

	// Deleting an absent blob is not an error (GC idempotency).
	if err := s.Delete(ctx, hash); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}
