package fabric

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrCheckpointNotFound is returned when a run has no checkpoints.
var ErrCheckpointNotFound = errors.New("fabric: checkpoint not found")

// CheckpointStore is the typed API over the checkpoints and checkpoint_files
// tables. The runtime inserts one checkpoint per green build during a cycle; on
// a dead-coder termination the supervisor loads the latest to offer the reviewer
// a known-good fallback state alongside the partial worktree.
type CheckpointStore struct {
	db *sql.DB
}

// NewCheckpointStore constructs a store over an open database.
func NewCheckpointStore(db *sql.DB) *CheckpointStore {
	return &CheckpointStore{db: db}
}

// CheckpointFile is one file captured in a checkpoint: its repo-relative path
// and the blob hash of its exact bytes.
type CheckpointFile struct {
	Path     string
	BlobHash string
}

// CheckpointRow is the typed projection of a checkpoints row plus its files.
type CheckpointRow struct {
	ID           int64
	RunID        string
	Cycle        int
	Trigger      string // the build-class command that fired this checkpoint
	ManifestHash string // blob hash of the canonical {path: blob_hash} manifest
	CreatedAt    int64
	Files        []CheckpointFile
}

// Insert persists a checkpoint and its file rows in a single transaction so a
// crash never leaves a checkpoints row without its checkpoint_files (which would
// hide live file blobs from the GC). CreatedAt defaults to now when zero.
// Returns the new checkpoint ID.
func (s *CheckpointStore) Insert(ctx context.Context, cp CheckpointRow) (int64, error) {
	if cp.RunID == "" {
		return 0, errors.New("fabric: checkpoint run_id is required")
	}
	if cp.ManifestHash == "" {
		return 0, errors.New("fabric: checkpoint manifest hash is required")
	}
	if cp.CreatedAt == 0 {
		cp.CreatedAt = time.Now().Unix()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("fabric: begin checkpoint tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	const insCheckpoint = `
		INSERT INTO checkpoints (run_id, cycle, trigger_cmd, manifest_blob_hash, created_at)
		VALUES (?, ?, ?, ?, ?)`
	res, err := tx.ExecContext(ctx, insCheckpoint, cp.RunID, cp.Cycle, cp.Trigger, cp.ManifestHash, cp.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("fabric: insert checkpoint for run %q: %w", cp.RunID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("fabric: checkpoint last insert id: %w", err)
	}

	const insFile = `
		INSERT INTO checkpoint_files (checkpoint_id, path, file_blob_hash)
		VALUES (?, ?, ?)`
	for _, f := range cp.Files {
		if _, err := tx.ExecContext(ctx, insFile, id, f.Path, f.BlobHash); err != nil {
			return 0, fmt.Errorf("fabric: insert checkpoint file %q: %w", f.Path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("fabric: commit checkpoint: %w", err)
	}
	return id, nil
}

// Latest returns the most recent checkpoint for a run (highest id), with its
// files loaded. Returns ErrCheckpointNotFound when the run has no checkpoints.
func (s *CheckpointStore) Latest(ctx context.Context, runID string) (*CheckpointRow, error) {
	const q = `
		SELECT id, run_id, cycle, trigger_cmd, manifest_blob_hash, created_at
		FROM checkpoints WHERE run_id = ?
		ORDER BY id DESC LIMIT 1`
	var cp CheckpointRow
	err := s.db.QueryRowContext(ctx, q, runID).Scan(
		&cp.ID, &cp.RunID, &cp.Cycle, &cp.Trigger, &cp.ManifestHash, &cp.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCheckpointNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fabric: latest checkpoint for run %q: %w", runID, err)
	}

	files, err := s.filesFor(ctx, cp.ID)
	if err != nil {
		return nil, err
	}
	cp.Files = files
	return &cp, nil
}

// ListForRun returns every checkpoint for a run, oldest first, without loading
// each one's files. Used by surfaces that render the checkpoint timeline.
func (s *CheckpointStore) ListForRun(ctx context.Context, runID string) ([]CheckpointRow, error) {
	const q = `
		SELECT id, run_id, cycle, trigger_cmd, manifest_blob_hash, created_at
		FROM checkpoints WHERE run_id = ?
		ORDER BY id ASC`
	rows, err := s.db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, fmt.Errorf("fabric: list checkpoints for run %q: %w", runID, err)
	}
	defer rows.Close() //nolint:errcheck // iteration cleanup

	var out []CheckpointRow
	for rows.Next() {
		var cp CheckpointRow
		if err := rows.Scan(&cp.ID, &cp.RunID, &cp.Cycle, &cp.Trigger, &cp.ManifestHash, &cp.CreatedAt); err != nil {
			return nil, fmt.Errorf("fabric: scan checkpoint: %w", err)
		}
		out = append(out, cp)
	}
	return out, rows.Err()
}

// filesFor loads the checkpoint_files rows for a checkpoint, ordered by path so
// restores are deterministic.
func (s *CheckpointStore) filesFor(ctx context.Context, checkpointID int64) ([]CheckpointFile, error) {
	const q = `
		SELECT path, file_blob_hash FROM checkpoint_files
		WHERE checkpoint_id = ? ORDER BY path ASC`
	rows, err := s.db.QueryContext(ctx, q, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("fabric: load checkpoint files for %d: %w", checkpointID, err)
	}
	defer rows.Close() //nolint:errcheck // iteration cleanup

	var out []CheckpointFile
	for rows.Next() {
		var f CheckpointFile
		if err := rows.Scan(&f.Path, &f.BlobHash); err != nil {
			return nil, fmt.Errorf("fabric: scan checkpoint file: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
