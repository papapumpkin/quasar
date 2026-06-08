package checkpoint

// This file implements worktree *snapshots*: content-addressed captures of the
// worktree the runtime takes after a coder dispatch returns successfully, so a
// coder killed mid-cycle can fall back to the latest captured tree instead of
// only the partial worktree at the moment of death.
//
// Granularity is PER-DISPATCH (cross-cycle), not per-build. Because the coder's
// tool calls run inside an opaque `claude` subprocess, the runtime has no
// in-cycle "build just passed" signal — it only sees the dispatch as a whole
// succeed or fail. So the snapshot a dead coder falls back to is the one taken
// after a PRIOR successful dispatch (or none, if this is the first cycle). True
// per-build granularity — "three green builds in one cycle, forfeit only the
// work after the last" — requires the invoker to surface build-class tool
// results from the subprocess (e.g. parsing `claude` stream-json tool events)
// and is tracked as a follow-up; it is deliberately NOT implemented here.
//
// This is a distinct concern from the cycle-state resume Checkpoint
// (checkpoint.go) that also lives in this package — to keep the two greppable
// apart, every identifier here uses "Snapshot" vocabulary, never "Checkpoint",
// except for the Checkpoint entrypoint the runtime calls.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/papapumpkin/quasar/internal/blobstore"
)

// ErrNoSnapshot indicates a run has no snapshot to restore from.
var ErrNoSnapshot = errors.New("checkpoint: no snapshot for run")

// FileRef pairs a repo-relative path with the blob hash of its exact bytes and
// the file's permission bits, so a restore reproduces both content and mode.
type FileRef struct {
	Path     string
	BlobHash string
	Mode     uint32 // os.FileMode permission bits (e.g. 0o644, 0o755)
}

// Snapshot is a persisted worktree capture: the run and cycle it belongs to, the
// command that triggered it, the content-addressed manifest hash, and every
// file's blob reference. It is the unit RestoreForReview reproduces.
type Snapshot struct {
	ID           int64
	RunID        string
	Cycle        int
	Trigger      string
	ManifestHash string
	CreatedAt    int64
	Files        []FileRef
}

// Store is the persistence the Snapshotter depends on. It is defined here, where
// it is consumed, so the Snapshotter can be unit-tested against a fake. The
// production implementation is the fabric-backed adapter from NewFabricStore.
type Store interface {
	// Insert persists a snapshot and its files, returning the new ID.
	Insert(ctx context.Context, snap Snapshot) (int64, error)
	// Latest returns the most recent snapshot for a run, or ErrNoSnapshot.
	Latest(ctx context.Context, runID string) (*Snapshot, error)
}

// Snapshotter captures the worktree after a successful coder dispatch. Captures
// are content-addressed via the blobstore so multiple coders (different cycles,
// different phases, different runs) share unchanged file blobs. A single
// Snapshotter serves one worktree across many runs; the run ID is supplied per
// call. It is not safe for concurrent use within a single run.
type Snapshotter struct {
	workdir string
	blobs   *blobstore.Store
	store   Store
}

// NewSnapshotter constructs a Snapshotter for one worktree.
func NewSnapshotter(workdir string, blobs *blobstore.Store, store Store) *Snapshotter {
	return &Snapshotter{workdir: workdir, blobs: blobs, store: store}
}

// Checkpoint captures the worktree for runID, deduping against the run's latest
// snapshot: an unchanged tree (same manifest hash) writes no new row and returns
// (nil, nil). This is the entrypoint the runtime calls after a coder dispatch
// returns successfully (see the package comment on per-dispatch granularity).
// trigger is a free-form label recording what prompted the capture.
func (s *Snapshotter) Checkpoint(ctx context.Context, runID string, cycle int, trigger string) (*Snapshot, error) {
	files, manifestHash, err := s.snapshotTree(ctx)
	if err != nil {
		return nil, err
	}
	latest, err := s.store.Latest(ctx, runID)
	if err != nil && !errors.Is(err, ErrNoSnapshot) {
		return nil, fmt.Errorf("checkpoint: load latest: %w", err)
	}
	if latest != nil && latest.ManifestHash == manifestHash {
		return nil, nil // tree unchanged since the last snapshot
	}
	return s.persist(ctx, runID, cycle, trigger, files, manifestHash)
}

// Capture unconditionally writes a snapshot, skipping the dedup check. It is the
// low-level primitive Checkpoint builds on, used by tests and any caller that
// wants an unconditional capture.
func (s *Snapshotter) Capture(ctx context.Context, runID string, cycle int, trigger string) (*Snapshot, error) {
	files, manifestHash, err := s.snapshotTree(ctx)
	if err != nil {
		return nil, err
	}
	return s.persist(ctx, runID, cycle, trigger, files, manifestHash)
}

// persist writes the snapshot row and returns the stored snapshot.
func (s *Snapshotter) persist(ctx context.Context, runID string, cycle int, trigger string, files []FileRef, manifestHash string) (*Snapshot, error) {
	snap := Snapshot{
		RunID:        runID,
		Cycle:        cycle,
		Trigger:      trigger,
		ManifestHash: manifestHash,
		Files:        files,
	}
	id, err := s.store.Insert(ctx, snap)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: persist: %w", err)
	}
	snap.ID = id
	return &snap, nil
}

// manifestEntry is the per-file record in the canonical manifest JSON. Including
// the mode makes a chmod-only change produce a new manifest hash, so a snapshot
// captures permission changes as faithfully as content changes.
type manifestEntry struct {
	Hash string `json:"h"`
	Mode uint32 `json:"m"`
}

// snapshotTree walks the worktree, stores each file's bytes as a blob, and builds
// the content-addressed manifest. The manifest hash is deterministic: identical
// tree contents and modes always yield the same hash, so two snapshots with no
// changes share one manifest_blob_hash and reuse every file blob.
func (s *Snapshotter) snapshotTree(ctx context.Context) ([]FileRef, string, error) {
	entries, err := scanFiles(s.workdir)
	if err != nil {
		return nil, "", err
	}

	manifest := make(map[string]manifestEntry, len(entries))
	files := make([]FileRef, 0, len(entries))
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(s.workdir, filepath.FromSlash(e.path)))
		if err != nil {
			return nil, "", fmt.Errorf("checkpoint: read %s: %w", e.path, err)
		}
		hash, err := s.blobs.Put(ctx, data)
		if err != nil {
			return nil, "", fmt.Errorf("checkpoint: store blob for %s: %w", e.path, err)
		}
		manifest[e.path] = manifestEntry{Hash: hash, Mode: e.mode}
		files = append(files, FileRef{Path: e.path, BlobHash: hash, Mode: e.mode})
	}

	// json.Marshal of a map sorts keys, so the encoding is canonical and the
	// resulting blob hash is stable for identical trees.
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", fmt.Errorf("checkpoint: marshal manifest: %w", err)
	}
	manifestHash, err := s.blobs.Put(ctx, manifestJSON)
	if err != nil {
		return nil, "", fmt.Errorf("checkpoint: store manifest: %w", err)
	}
	return files, manifestHash, nil
}

// Restore writes every file in snap into destDir, reproducing byte-exact content
// and permission bits from the blobstore. Parent directories are created as
// needed. Paths that try to escape destDir (via "..") are rejected as a
// path-traversal guard, though snapshots only ever hold clean relative paths.
func (s *Snapshotter) Restore(ctx context.Context, snap *Snapshot, destDir string) error {
	for _, f := range snap.Files {
		target, err := safeJoin(destDir, f.Path)
		if err != nil {
			return err
		}
		data, err := s.blobs.Get(ctx, f.BlobHash)
		if err != nil {
			return fmt.Errorf("checkpoint: fetch blob for %s: %w", f.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("checkpoint: mkdir for %s: %w", f.Path, err)
		}
		mode := os.FileMode(f.Mode).Perm()
		if mode == 0 {
			mode = 0o644 // defensive: a pre-mode snapshot row restores readable
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("checkpoint: write %s: %w", f.Path, err)
		}
		// WriteFile honors the mode only on create; chmod guarantees it on an
		// existing target too (e.g. restoring over a prior materialization).
		if err := os.Chmod(target, mode); err != nil {
			return fmt.Errorf("checkpoint: chmod %s: %w", f.Path, err)
		}
	}
	return nil
}

// RestoreForReview materializes, under baseDir, the two states a reviewer judges
// after a dead-coder termination: partial/ (a copy of the live worktree at the
// moment of death) and checkpoint/ (the latest per-dispatch snapshot). It returns
// ErrNoSnapshot when the run never produced a snapshot, leaving the caller to
// fall back to reviewing the partial tree alone. baseDir must be outside the
// worktree so the copy does not snapshot itself.
func (s *Snapshotter) RestoreForReview(ctx context.Context, runID, baseDir string) (partialDir, checkpointDir string, err error) {
	snap, err := s.store.Latest(ctx, runID)
	if err != nil {
		return "", "", err
	}
	partialDir = filepath.Join(baseDir, "partial")
	checkpointDir = filepath.Join(baseDir, "checkpoint")

	if err := copyTree(s.workdir, partialDir); err != nil {
		return "", "", fmt.Errorf("checkpoint: copy partial worktree: %w", err)
	}
	if err := s.Restore(ctx, snap, checkpointDir); err != nil {
		return "", "", err
	}
	return partialDir, checkpointDir, nil
}

// scannedFile is one regular file discovered by scanFiles: its slash-separated
// path and permission bits.
type scannedFile struct {
	path string
	mode uint32
}

// scanFiles returns path-sorted regular files under workdir, excluding the .git
// directory. Symlinks and other non-regular files are skipped. Each file's
// permission bits are captured so a restore can reproduce them.
func scanFiles(workdir string) ([]scannedFile, error) {
	var out []scannedFile
	err := filepath.WalkDir(workdir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(workdir, path)
		if err != nil {
			return err
		}
		out = append(out, scannedFile{path: filepath.ToSlash(rel), mode: uint32(info.Mode().Perm())})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("checkpoint: scan worktree %s: %w", workdir, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

// copyTree copies every regular file under src into dst, preserving relative
// layout and permission bits. It reuses scanFiles so the .git exclusion and
// traversal rules match snapshotting exactly.
func copyTree(src, dst string) error {
	entries, err := scanFiles(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		target, err := safeJoin(dst, e.path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(e.path)))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(e.mode).Perm()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
	}
	return nil
}

// safeJoin joins base and a slash-separated relative path, rejecting any result
// that escapes base. It guards Restore against a malicious or corrupt manifest.
func safeJoin(base, rel string) (string, error) {
	target := filepath.Join(base, filepath.FromSlash(rel))
	cleanBase := filepath.Clean(base)
	if target != cleanBase && !strings.HasPrefix(target, cleanBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("checkpoint: path %q escapes %q", rel, base)
	}
	return target, nil
}
