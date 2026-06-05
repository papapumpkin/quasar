// Package blobstore implements a content-addressed blob store backed by the
// filesystem with a SQLite registry. Large LLM outputs and diffs are stored
// once by SHA-256 hash, zstd-compressed on disk, and tracked in a blobs table
// so a later mark-and-sweep garbage collector can reclaim unreferenced content.
package blobstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
)

// ErrBlobNotFound is returned by Get when the requested hash is not present.
var ErrBlobNotFound = errors.New("blobstore: blob not found")

// blobsSchema creates the registry table. It mirrors the definition shipped in
// the fabric migration and uses IF NOT EXISTS so the store is usable against
// any database, migrated or not.
const blobsSchema = `
CREATE TABLE IF NOT EXISTS blobs (
    hash         TEXT PRIMARY KEY,
    size_bytes   INTEGER NOT NULL,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);`

// BlobInfo describes a single registry row. SizeBytes is the decompressed
// content length; the on-disk file is smaller due to zstd compression.
type BlobInfo struct {
	Hash       string
	SizeBytes  int64
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Store is a content-addressed blob store. Blobs live at
// <root>/<sha256[:2]>/<sha256[2:]> (git-style fanout). Content is
// zstd-compressed on write and decompressed on read; writes are atomic via
// write-tmp-then-rename.
type Store struct {
	root string
	db   *sql.DB
}

// New creates a Store rooted at the given directory, creating the directory and
// the blobs registry table if they do not already exist.
func New(root string, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("blobstore: nil database")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("blobstore: create root %q: %w", root, err)
	}
	if _, err := db.ExecContext(context.Background(), blobsSchema); err != nil {
		return nil, fmt.Errorf("blobstore: create blobs table: %w", err)
	}
	return &Store{root: root, db: db}, nil
}

// path returns the on-disk path for a hash using git-style two-char fanout.
func (s *Store) path(hash string) (dir, file string) {
	dir = filepath.Join(s.root, hash[:2])
	return dir, filepath.Join(dir, hash[2:])
}

// Put computes the SHA-256 of content, writes it (zstd-compressed) to the store
// if not already present, upserts the registry row, and returns the hash.
// Calls with identical content are idempotent: the same hash, one file, one row.
func (s *Store) Put(ctx context.Context, content []byte) (string, error) {
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])

	dir, file := s.path(hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("blobstore: create dir %q: %w", dir, err)
	}

	// Only write the file if it is not already present — identical content
	// always hashes to the same path, so an existing file is byte-identical.
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		if err := s.writeAtomic(file, content); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", fmt.Errorf("blobstore: stat %q: %w", file, err)
	}

	if err := s.upsertRow(ctx, hash, int64(len(content))); err != nil {
		return "", err
	}
	return hash, nil
}

// writeAtomic compresses content and writes it to file via a temp file + rename
// so a crash mid-write never leaves a partial blob at the canonical path.
func (s *Store) writeAtomic(file string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(file), ".tmp-*")
	if err != nil {
		return fmt.Errorf("blobstore: create temp: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we fail before the rename.
	defer os.Remove(tmpName) //nolint:errcheck // no-op once renamed away

	enc, err := zstd.NewWriter(tmp, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		tmp.Close() //nolint:errcheck // already failing
		return fmt.Errorf("blobstore: new zstd writer: %w", err)
	}
	if _, err := enc.Write(content); err != nil {
		enc.Close() //nolint:errcheck // already failing
		tmp.Close() //nolint:errcheck // already failing
		return fmt.Errorf("blobstore: compress: %w", err)
	}
	if err := enc.Close(); err != nil {
		tmp.Close() //nolint:errcheck // already failing
		return fmt.Errorf("blobstore: flush zstd: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("blobstore: close temp: %w", err)
	}
	if err := os.Rename(tmpName, file); err != nil {
		return fmt.Errorf("blobstore: rename into place: %w", err)
	}
	return nil
}

// upsertRow inserts the blob row or refreshes last_seen_at if it already exists.
func (s *Store) upsertRow(ctx context.Context, hash string, size int64) error {
	now := time.Now().Unix()
	const q = `
		INSERT INTO blobs (hash, size_bytes, created_at, last_seen_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(hash) DO UPDATE SET last_seen_at = excluded.last_seen_at`
	if _, err := s.db.ExecContext(ctx, q, hash, size, now, now); err != nil {
		return fmt.Errorf("blobstore: upsert row %s: %w", hash, err)
	}
	return nil
}

// Get returns the decompressed content for the given hash, or ErrBlobNotFound
// if the blob is absent from disk.
func (s *Store) Get(ctx context.Context, hash string) ([]byte, error) {
	_, file := s.path(hash)
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrBlobNotFound, hash)
	}
	if err != nil {
		return nil, fmt.Errorf("blobstore: open %s: %w", hash, err)
	}
	defer f.Close() //nolint:errcheck // read-only handle

	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("blobstore: new zstd reader: %w", err)
	}
	defer dec.Close()

	content, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("blobstore: decompress %s: %w", hash, err)
	}
	return content, nil
}

// Has reports whether the blob exists on disk without reading its content.
func (s *Store) Has(ctx context.Context, hash string) bool {
	_, file := s.path(hash)
	_, err := os.Stat(file)
	return err == nil
}

// Walk iterates over every row in the blobs registry. The returned sequence
// reads lazily; iteration stops early if the consumer breaks. Used by the GC
// mark-and-sweep.
func (s *Store) Walk(ctx context.Context) (iter.Seq[BlobInfo], error) {
	rows, err := s.db.QueryContext(ctx, "SELECT hash, size_bytes, created_at, last_seen_at FROM blobs ORDER BY hash")
	if err != nil {
		return nil, fmt.Errorf("blobstore: query blobs: %w", err)
	}
	return func(yield func(BlobInfo) bool) {
		defer rows.Close() //nolint:errcheck // iteration cleanup
		for rows.Next() {
			var (
				info               BlobInfo
				created, lastSeen  int64
			)
			if err := rows.Scan(&info.Hash, &info.SizeBytes, &created, &lastSeen); err != nil {
				return
			}
			info.CreatedAt = time.Unix(created, 0)
			info.LastSeenAt = time.Unix(lastSeen, 0)
			if !yield(info) {
				return
			}
		}
	}, nil
}

// Delete removes a blob from disk and from the registry. Missing files are not
// an error (the registry row is still removed); this keeps GC idempotent.
func (s *Store) Delete(ctx context.Context, hash string) error {
	_, file := s.path(hash)
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("blobstore: remove %s: %w", hash, err)
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM blobs WHERE hash = ?", hash); err != nil {
		return fmt.Errorf("blobstore: delete row %s: %w", hash, err)
	}
	return nil
}
