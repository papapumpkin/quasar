package checkpoint

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

// ErrNoCheckpoint indicates a run has no green-build checkpoint to restore from.
var ErrNoCheckpoint = errors.New("checkpoint: no checkpoint for run")

// ToolResult is the minimal view of a completed tool call the Checkpointer needs
// to decide whether to snapshot: the command that ran and its exit code. A
// build-class command (see triggers) that exits 0 fires a checkpoint.
type ToolResult struct {
	Command  string // the shell command the coder ran (e.g. "go build ./...")
	ExitCode int    // process exit code; 0 means success
}

// FileRef pairs a repo-relative path with the blob hash of its exact bytes.
type FileRef struct {
	Path     string
	BlobHash string
}

// StoredCheckpoint is a persisted worktree snapshot: the run and cycle it
// belongs to, the command that triggered it, the content-addressed manifest
// hash, and every file's blob reference. It is the unit the supervisor restores.
type StoredCheckpoint struct {
	ID           int64
	RunID        string
	Cycle        int
	Trigger      string
	ManifestHash string
	CreatedAt    int64
	Files        []FileRef
}

// Store is the persistence the Checkpointer depends on. It is defined here, where
// it is consumed, so the Checkpointer can be unit-tested against a fake. The
// production implementation is the fabric-backed adapter from NewFabricStore.
type Store interface {
	// Insert persists a checkpoint and its files, returning the new ID.
	Insert(ctx context.Context, cp StoredCheckpoint) (int64, error)
	// Latest returns the most recent checkpoint for a run, or ErrNoCheckpoint.
	Latest(ctx context.Context, runID string) (*StoredCheckpoint, error)
}

// DefaultTriggers returns the Go-centric build-class commands that, on exit 0,
// fire a checkpoint. A fresh slice is returned each call so callers cannot
// mutate shared state. A non-Go repo overrides these via the star's [checkpoint]
// frontmatter.
func DefaultTriggers() []string {
	return []string{"go build ./...", "go vet ./...", "go test -short ./..."}
}

// Checkpointer snapshots the worktree on green-build signals. Snapshots are
// content-addressed via the blobstore so multiple coders (different cycles,
// different phases) share unchanged file blobs. It is not safe for concurrent
// use; a single coder's runtime drives it sequentially.
type Checkpointer struct {
	workdir  string
	blobs    *blobstore.Store
	store    Store
	triggers []string
	runID    string
	cycle    int
	lastHash string // manifest hash of the last checkpoint, for dedup
}

// New constructs a Checkpointer for one run's worktree. An empty triggers slice
// falls back to DefaultTriggers, so a star that omits the [checkpoint] block
// still checkpoints on Go green builds.
func New(workdir string, blobs *blobstore.Store, store Store, runID string, triggers []string) *Checkpointer {
	if len(triggers) == 0 {
		triggers = DefaultTriggers()
	}
	return &Checkpointer{
		workdir:  workdir,
		blobs:    blobs,
		store:    store,
		triggers: triggers,
		runID:    runID,
	}
}

// SetCycle records the cycle subsequent checkpoints belong to. The runtime calls
// it as the coder-reviewer loop advances.
func (c *Checkpointer) SetCycle(cycle int) { c.cycle = cycle }

// MaybeCheckpoint is called by the runtime after a tool result. It snapshots
// only when the tool was a build-class trigger that exited 0. A snapshot whose
// manifest matches the previous checkpoint is deduped: no new row is written and
// (nil, nil) is returned. Returns (nil, nil) for non-trigger or non-zero exits.
func (c *Checkpointer) MaybeCheckpoint(ctx context.Context, result ToolResult) (*StoredCheckpoint, error) {
	if result.ExitCode != 0 || !c.isTrigger(result.Command) {
		return nil, nil
	}
	files, manifestHash, err := c.scan(ctx)
	if err != nil {
		return nil, err
	}
	if manifestHash == c.lastHash {
		// No files changed since the last checkpoint — skip the redundant row.
		return nil, nil
	}
	return c.persist(ctx, result.Command, files, manifestHash)
}

// Snapshot forces a checkpoint with the given trigger label regardless of dedup
// state. It is the low-level primitive MaybeCheckpoint builds on, and is used by
// tests and any caller that wants an unconditional snapshot.
func (c *Checkpointer) Snapshot(ctx context.Context, trigger string) (*StoredCheckpoint, error) {
	files, manifestHash, err := c.scan(ctx)
	if err != nil {
		return nil, err
	}
	return c.persist(ctx, trigger, files, manifestHash)
}

// persist writes the checkpoint row and records the manifest hash for dedup.
func (c *Checkpointer) persist(ctx context.Context, trigger string, files []FileRef, manifestHash string) (*StoredCheckpoint, error) {
	cp := StoredCheckpoint{
		RunID:        c.runID,
		Cycle:        c.cycle,
		Trigger:      trigger,
		ManifestHash: manifestHash,
		Files:        files,
	}
	id, err := c.store.Insert(ctx, cp)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: persist: %w", err)
	}
	cp.ID = id
	c.lastHash = manifestHash
	return &cp, nil
}

// scan walks the worktree, stores each file's bytes as a blob, and builds the
// content-addressed manifest. The manifest hash is deterministic: identical tree
// contents always yield the same hash, so two snapshots with no file changes
// share one manifest_blob_hash and reuse every file blob.
func (c *Checkpointer) scan(ctx context.Context) ([]FileRef, string, error) {
	paths, err := scanFiles(c.workdir)
	if err != nil {
		return nil, "", err
	}

	manifest := make(map[string]string, len(paths))
	files := make([]FileRef, 0, len(paths))
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(c.workdir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, "", fmt.Errorf("checkpoint: read %s: %w", rel, err)
		}
		hash, err := c.blobs.Put(ctx, data)
		if err != nil {
			return nil, "", fmt.Errorf("checkpoint: store blob for %s: %w", rel, err)
		}
		manifest[rel] = hash
		files = append(files, FileRef{Path: rel, BlobHash: hash})
	}

	// json.Marshal of a map sorts keys, so the encoding is canonical and the
	// resulting blob hash is stable for identical trees.
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", fmt.Errorf("checkpoint: marshal manifest: %w", err)
	}
	manifestHash, err := c.blobs.Put(ctx, manifestJSON)
	if err != nil {
		return nil, "", fmt.Errorf("checkpoint: store manifest: %w", err)
	}
	return files, manifestHash, nil
}

// Restore writes every file in cp into destDir, reproducing byte-exact content
// from the blobstore. Parent directories are created as needed. Paths that try
// to escape destDir (via "..") are rejected as a path-traversal guard, though
// snapshots only ever hold clean relative paths.
func (c *Checkpointer) Restore(ctx context.Context, cp *StoredCheckpoint, destDir string) error {
	for _, f := range cp.Files {
		target, err := safeJoin(destDir, f.Path)
		if err != nil {
			return err
		}
		data, err := c.blobs.Get(ctx, f.BlobHash)
		if err != nil {
			return fmt.Errorf("checkpoint: fetch blob for %s: %w", f.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("checkpoint: mkdir for %s: %w", f.Path, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("checkpoint: write %s: %w", f.Path, err)
		}
	}
	return nil
}

// RestoreForReview materializes, under baseDir, the two states a reviewer judges
// after a dead-coder termination: partial/ (a copy of the live worktree at the
// moment of death) and checkpoint/ (the latest green-build snapshot). It returns
// ErrNoCheckpoint when the run never produced a checkpoint, leaving the caller to
// fall back to reviewing the partial tree alone. baseDir must be outside the
// worktree so the copy does not snapshot itself.
func (c *Checkpointer) RestoreForReview(ctx context.Context, baseDir string) (partialDir, checkpointDir string, err error) {
	cp, err := c.store.Latest(ctx, c.runID)
	if err != nil {
		return "", "", err
	}
	partialDir = filepath.Join(baseDir, "partial")
	checkpointDir = filepath.Join(baseDir, "checkpoint")

	if err := copyTree(c.workdir, partialDir); err != nil {
		return "", "", fmt.Errorf("checkpoint: copy partial worktree: %w", err)
	}
	if err := c.Restore(ctx, cp, checkpointDir); err != nil {
		return "", "", err
	}
	return partialDir, checkpointDir, nil
}

// isTrigger reports whether cmd is one of the configured build-class triggers.
// Matching is whitespace-normalized and allows trailing arguments (e.g.
// "go build ./... -v" still matches "go build ./...").
func (c *Checkpointer) isTrigger(cmd string) bool {
	norm := normalizeCmd(cmd)
	for _, t := range c.triggers {
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

// scanFiles returns sorted, slash-separated paths of every regular file under
// workdir, excluding the .git directory. Symlinks and other non-regular files
// are skipped.
func scanFiles(workdir string) ([]string, error) {
	var paths []string
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
		rel, err := filepath.Rel(workdir, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("checkpoint: scan worktree %s: %w", workdir, err)
	}
	sort.Strings(paths)
	return paths, nil
}

// copyTree copies every regular file under src into dst, preserving relative
// layout. It reuses scanFiles so the .git exclusion and traversal rules match
// snapshotting exactly.
func copyTree(src, dst string) error {
	paths, err := scanFiles(src)
	if err != nil {
		return err
	}
	for _, rel := range paths {
		target, err := safeJoin(dst, rel)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
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
// between the package-local StoredCheckpoint and the fabric row types and mapping
// fabric.ErrCheckpointNotFound to ErrNoCheckpoint.
type fabricStore struct {
	s *fabric.CheckpointStore
}

// NewFabricStore wraps a fabric checkpoint store as a Store for the Checkpointer.
func NewFabricStore(s *fabric.CheckpointStore) Store {
	return &fabricStore{s: s}
}

// Insert maps the StoredCheckpoint onto a fabric.CheckpointRow and persists it.
func (f *fabricStore) Insert(ctx context.Context, cp StoredCheckpoint) (int64, error) {
	files := make([]fabric.CheckpointFile, len(cp.Files))
	for i, fr := range cp.Files {
		files[i] = fabric.CheckpointFile{Path: fr.Path, BlobHash: fr.BlobHash}
	}
	return f.s.Insert(ctx, fabric.CheckpointRow{
		RunID:        cp.RunID,
		Cycle:        cp.Cycle,
		Trigger:      cp.Trigger,
		ManifestHash: cp.ManifestHash,
		CreatedAt:    cp.CreatedAt,
		Files:        files,
	})
}

// Latest loads the newest checkpoint for a run, translating the not-found error.
func (f *fabricStore) Latest(ctx context.Context, runID string) (*StoredCheckpoint, error) {
	row, err := f.s.Latest(ctx, runID)
	if errors.Is(err, fabric.ErrCheckpointNotFound) {
		return nil, ErrNoCheckpoint
	}
	if err != nil {
		return nil, err
	}
	files := make([]FileRef, len(row.Files))
	for i, fr := range row.Files {
		files[i] = FileRef{Path: fr.Path, BlobHash: fr.BlobHash}
	}
	return &StoredCheckpoint{
		ID:           row.ID,
		RunID:        row.RunID,
		Cycle:        row.Cycle,
		Trigger:      row.Trigger,
		ManifestHash: row.ManifestHash,
		CreatedAt:    row.CreatedAt,
		Files:        files,
	}, nil
}
