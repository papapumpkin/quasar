package checkpoint

// This file holds the adapters that connect the worktree-Snapshotter to its two
// collaborators without leaking either across the seam: fabricStore translates
// between the package-local Snapshot and the fabric DB row types, and
// RuntimeCheckpointer collapses the Snapshotter onto the small Checkpointer
// surface the constellation runtime consumes. Keeping them out of snapshot.go
// leaves that file to the pure capture/restore logic.

import (
	"context"
	"errors"

	"github.com/papapumpkin/quasar/internal/fabric"
)

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
