package checkpoint

// This file implements worktree *snapshots*: content-addressed captures of the
// worktree taken on green-build signals so a coder killed mid-cycle forfeits at
// most the work since the last known-good build. It is a distinct concern from
// the cycle-state resume Checkpoint (checkpoint.go) that also lives in this
// package — to keep the two greppable apart, every identifier here uses
// "Snapshot" vocabulary, never "Checkpoint", except for the MaybeCheckpoint
// trigger entrypoint named by the design spec.

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
	"github.com/papapumpkin/quasar/internal/fabric"
)

// ErrNoSnapshot indicates a run has no green-build snapshot to restore from.
var ErrNoSnapshot = errors.New("checkpoint: no snapshot for run")

// ToolResult is the minimal view of a completed tool call the Snapshotter needs
// to decide whether to snapshot: the command that ran and its exit code. A
// build-class command (see triggers) that exits 0 fires a snapshot.
type ToolResult struct {
	Command  string // the shell command the coder ran (e.g. "go build ./...")
	ExitCode int    // process exit code; 0 means success
}

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

// DefaultTriggers returns the Go-centric build-class commands that, on exit 0,
// fire a snapshot. A fresh slice is returned each call so callers cannot mutate
// shared state. A non-Go repo overrides these via the star's [checkpoint]
// frontmatter.
func DefaultTriggers() []string {
	return []string{"go build ./...", "go vet ./...", "go test -short ./..."}
}

// Snapshotter captures the worktree on green-build signals. Captures are
// content-addressed via the blobstore so multiple coders (different cycles,
// different phases, different runs) share unchanged file blobs. A single
// Snapshotter serves one worktree across many runs; the run ID is supplied per
// call. It is not safe for concurrent use within a single run.
type Snapshotter struct {
	workdir  string
	blobs    *blobstore.Store
	store    Store
	triggers []string
}

// NewSnapshotter constructs a Snapshotter for one worktree. An empty triggers
// slice falls back to DefaultTriggers, so a caller that omits triggers still
// snapshots on Go green builds.
func NewSnapshotter(workdir string, blobs *blobstore.Store, store Store, triggers []string) *Snapshotter {
	if len(triggers) == 0 {
		triggers = DefaultTriggers()
	}
	return &Snapshotter{workdir: workdir, blobs: blobs, store: store, triggers: triggers}
}

// MaybeCheckpoint is called after a tool result. It snapshots only when the tool
// was a build-class trigger that exited 0, deduping against the run's latest
// snapshot. Returns (nil, nil) for a non-trigger, a non-zero exit, or an
// unchanged tree.
func (s *Snapshotter) MaybeCheckpoint(ctx context.Context, runID string, cycle int, result ToolResult) (*Snapshot, error) {
	if result.ExitCode != 0 || !s.isTrigger(result.Command) {
		return nil, nil
	}
	return s.Checkpoint(ctx, runID, cycle, result.Command)
}

// Checkpoint captures the worktree for runID, deduping against the run's latest
// snapshot: an unchanged tree (same manifest hash) writes no new row and returns
// (nil, nil). This is the green-build entrypoint the runtime calls each cycle.
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
// moment of death) and checkpoint/ (the latest green-build snapshot). It returns
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

// isTrigger reports whether cmd is one of the configured build-class triggers.
// Matching is whitespace-normalized and allows trailing arguments (e.g.
// "go build ./... -v" still matches "go build ./...").
func (s *Snapshotter) isTrigger(cmd string) bool {
	norm := normalizeCmd(cmd)
	for _, t := range s.triggers {
		tn := normalizeCmd(t)
		if tn != "" && (norm == tn || strings.HasPrefix(norm, tn+" ")) {
			return true
		}
	}
	return false
}

// normalizeCmd collapses runs of whitespace to single spaces and trims, so
// trigger matching is insensitive to incidental spacing.
func normalizeCmd(s string) string {
	return strings.Join(strings.Fields(s), " ")
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

// fabricStore adapts *fabric.CheckpointStore to the Store interface, translating
// between the package-local Snapshot and the fabric row types and mapping
// fabric.ErrCheckpointNotFound to ErrNoSnapshot.
type fabricStore struct {
	s *fabric.CheckpointStore
}

// NewFabricStore wraps a fabric checkpoint store as a Store for the Snapshotter.
func NewFabricStore(s *fabric.CheckpointStore) Store {
	return &fabricStore{s: s}
}

// RuntimeCheckpointer adapts a *Snapshotter to the constellation runtime's
// Checkpointer interface (defined in internal/constellations, where it is
// consumed). It collapses Checkpoint's *Snapshot return to a plain error so the
// runtime depends only on this small surface, never on the blob machinery. The
// cmd layer constructs one per repo worktree and injects it via RuntimeOpts.
type RuntimeCheckpointer struct {
	s *Snapshotter
}

// NewRuntimeCheckpointer wraps a Snapshotter for injection into the runtime.
func NewRuntimeCheckpointer(s *Snapshotter) *RuntimeCheckpointer {
	return &RuntimeCheckpointer{s: s}
}

// Checkpoint snapshots the worktree for runID, discarding the stored snapshot
// (the runtime only needs success/failure).
func (r *RuntimeCheckpointer) Checkpoint(ctx context.Context, runID string, cycle int, trigger string) error {
	_, err := r.s.Checkpoint(ctx, runID, cycle, trigger)
	return err
}

// RestoreForReview materializes partial/ and checkpoint/ under baseDir for runID.
func (r *RuntimeCheckpointer) RestoreForReview(ctx context.Context, runID, baseDir string) (partialDir, checkpointDir string, err error) {
	return r.s.RestoreForReview(ctx, runID, baseDir)
}

// Insert maps the Snapshot onto a fabric.CheckpointRow and persists it.
func (f *fabricStore) Insert(ctx context.Context, snap Snapshot) (int64, error) {
	files := make([]fabric.CheckpointFile, len(snap.Files))
	for i, fr := range snap.Files {
		files[i] = fabric.CheckpointFile{Path: fr.Path, BlobHash: fr.BlobHash, Mode: fr.Mode}
	}
	return f.s.Insert(ctx, fabric.CheckpointRow{
		RunID:        snap.RunID,
		Cycle:        snap.Cycle,
		Trigger:      snap.Trigger,
		ManifestHash: snap.ManifestHash,
		CreatedAt:    snap.CreatedAt,
		Files:        files,
	})
}

// Latest loads the newest snapshot for a run, translating the not-found error.
func (f *fabricStore) Latest(ctx context.Context, runID string) (*Snapshot, error) {
	row, err := f.s.Latest(ctx, runID)
	if errors.Is(err, fabric.ErrCheckpointNotFound) {
		return nil, ErrNoSnapshot
	}
	if err != nil {
		return nil, err
	}
	files := make([]FileRef, len(row.Files))
	for i, fr := range row.Files {
		files[i] = FileRef{Path: fr.Path, BlobHash: fr.BlobHash, Mode: fr.Mode}
	}
	return &Snapshot{
		ID:           row.ID,
		RunID:        row.RunID,
		Cycle:        row.Cycle,
		Trigger:      row.Trigger,
		ManifestHash: row.ManifestHash,
		CreatedAt:    row.CreatedAt,
		Files:        files,
	}, nil
}
