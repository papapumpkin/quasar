package fabric

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
)

// previewLen is the number of body characters mirrored into phases.body_preview
// so fleet/list views can render without touching the blobstore.
const previewLen = 500

// ErrNebulaNotFound is returned by NebulaStore.Get when no nebula has the id.
var ErrNebulaNotFound = errors.New("fabric: nebula not found")

// NebulaRecord is one row in the nebulas table: the provenance and status of a
// nebula known to this fabric database. Sensor-seeded drafts record their
// originating source here so a later review surface (TUI / web UI) can list and
// act on them. Manually authored nebulas are not persisted here.
type NebulaRecord struct {
	ID         string // nebula directory name; primary key
	SourceType string // e.g. "ticket"; distinguishes from future origins
	SourceName string // integration name, e.g. "github"
	SourceID   string // adapter-canonical id, e.g. "papapumpkin/quasar#42"
	Path       string // on-disk path to the nebula directory
	Status     string // lifecycle status, e.g. "draft"
}

// InsertNebula records a new nebula row. created_at and updated_at are set by
// the database. The ID must be unique — re-pulling a ticket produces a new
// directory name (with a numeric suffix) and therefore a distinct row.
func (f *SQLiteFabric) InsertNebula(ctx context.Context, rec NebulaRecord) error {
	const q = `
		INSERT INTO nebulas (id, source_type, source_name, source_id, path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	if _, err := f.db.ExecContext(ctx, q,
		rec.ID, rec.SourceType, rec.SourceName, rec.SourceID, rec.Path, rec.Status,
	); err != nil {
		return fmt.Errorf("fabric: insert nebula %q: %w", rec.ID, err)
	}
	return nil
}

// NebulaStore is the typed Go API over the nebulas/phases tables and the
// blobstore. It replaces the filesystem state.toml load/save pattern as the
// canonical execution-time store. Writes use immediate transactions; blob
// writes precede row inserts so a crash never leaves a dangling hash.
type NebulaStore struct {
	db    *sql.DB
	blobs *blobstore.Store
}

// NewNebulaStore constructs a NebulaStore over the given database and blobstore.
func NewNebulaStore(db *sql.DB, blobs *blobstore.Store) *NebulaStore {
	return &NebulaStore{db: db, blobs: blobs}
}

// Insert creates a new nebula row and returns its generated id
// (nebula-<unix>-<slug>). Status defaults to "draft".
func (s *NebulaStore) Insert(ctx context.Context, n NebulaRow) (string, error) {
	now := time.Now()
	id := generateNebulaID(now, n.Name)
	status := n.Status
	if status == "" {
		status = "draft"
	}

	const q = `
		INSERT INTO nebulas
			(id, repo_path, name, description, source_name, source_id, source_url,
			 defaults_toml, execution_toml, context_toml, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, q,
		id, n.RepoPath, n.Name, n.Description, n.SourceName, n.SourceID, n.SourceURL,
		n.DefaultsTOML, n.ExecutionTOML, n.ContextTOML, status, now.Unix(), now.Unix(),
	); err != nil {
		return "", fmt.Errorf("fabric: insert nebula %q: %w", id, err)
	}
	return id, nil
}

// InsertPhase adds a phase to an existing nebula. The body is written to the
// blobstore first; the row stores the hash plus a preview.
func (s *NebulaStore) InsertPhase(ctx context.Context, nebulaID string, phase PhaseRow) error {
	hash, err := s.blobs.Put(ctx, []byte(phase.Body))
	if err != nil {
		return fmt.Errorf("fabric: store phase body %q/%q: %w", nebulaID, phase.ID, err)
	}

	const q = `
		INSERT INTO phases
			(nebula_id, id, seq, title, body_blob_hash, body_preview, frontmatter_toml, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`
	if _, err := s.db.ExecContext(ctx, q,
		nebulaID, phase.ID, phase.Seq, phase.Title, hash, preview(phase.Body), phase.FrontmatterTOML,
	); err != nil {
		return fmt.Errorf("fabric: insert phase %q/%q: %w", nebulaID, phase.ID, err)
	}
	return nil
}

// Get returns a fully populated nebula including its phases, with phase bodies
// loaded from the blobstore. Returns ErrNebulaNotFound if no such nebula.
func (s *NebulaStore) Get(ctx context.Context, id string) (*Nebula, error) {
	const q = `
		SELECT id, repo_path, name, description, status, source_name, source_id, source_url,
		       defaults_toml, execution_toml, context_toml, created_at, updated_at
		FROM nebulas WHERE id = ?`
	var (
		n                     Nebula
		desc, srcName, srcID  sql.NullString
		srcURL, def, exe, ctk sql.NullString
		created, updated      int64
	)
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&n.ID, &n.RepoPath, &n.Name, &desc, &n.Status, &srcName, &srcID, &srcURL,
		&def, &exe, &ctk, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNebulaNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("fabric: get nebula %q: %w", id, err)
	}
	n.Description = desc.String
	n.SourceName = srcName.String
	n.SourceID = srcID.String
	n.SourceURL = srcURL.String
	n.DefaultsTOML = def.String
	n.ExecutionTOML = exe.String
	n.ContextTOML = ctk.String
	n.CreatedAt = time.Unix(created, 0)
	n.UpdatedAt = time.Unix(updated, 0)

	phases, err := s.getPhases(ctx, id)
	if err != nil {
		return nil, err
	}
	n.Phases = phases
	return &n, nil
}

// getPhases loads a nebula's phases ordered by seq, resolving bodies from blobs.
func (s *NebulaStore) getPhases(ctx context.Context, nebulaID string) ([]Phase, error) {
	const q = `
		SELECT id, seq, title, body_blob_hash, frontmatter_toml, status
		FROM phases WHERE nebula_id = ? ORDER BY seq`
	rows, err := s.db.QueryContext(ctx, q, nebulaID)
	if err != nil {
		return nil, fmt.Errorf("fabric: query phases %q: %w", nebulaID, err)
	}
	defer rows.Close()

	var phases []Phase
	for rows.Next() {
		var (
			p    Phase
			hash string
		)
		if err := rows.Scan(&p.ID, &p.Seq, &p.Title, &hash, &p.FrontmatterTOML, &p.Status); err != nil {
			return nil, fmt.Errorf("fabric: scan phase: %w", err)
		}
		body, err := s.blobs.Get(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("fabric: load phase body %q/%q: %w", nebulaID, p.ID, err)
		}
		p.Body = string(body)
		phases = append(phases, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate phases %q: %w", nebulaID, err)
	}
	return phases, nil
}

// List returns nebula summaries matching the filter. Phase bodies are NOT
// loaded; previews exist for that purpose.
func (s *NebulaStore) List(ctx context.Context, filter ListFilter) ([]*NebulaSummary, error) {
	var (
		conds []string
		args  []any
	)
	if filter.RepoPath != "" {
		conds = append(conds, "repo_path = ?")
		args = append(args, filter.RepoPath)
	}
	if filter.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, filter.Status)
	}
	q := `SELECT id, repo_path, name, description, status, source_name, source_id, created_at, updated_at FROM nebulas`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY created_at DESC, id"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("fabric: list nebulas: %w", err)
	}
	defer rows.Close()

	var out []*NebulaSummary
	for rows.Next() {
		var (
			sum                  NebulaSummary
			desc, srcName, srcID sql.NullString
			created, updated     int64
		)
		if err := rows.Scan(&sum.ID, &sum.RepoPath, &sum.Name, &desc, &sum.Status, &srcName, &srcID, &created, &updated); err != nil {
			return nil, fmt.Errorf("fabric: scan nebula summary: %w", err)
		}
		sum.Description = desc.String
		sum.SourceName = srcName.String
		sum.SourceID = srcID.String
		sum.CreatedAt = time.Unix(created, 0)
		sum.UpdatedAt = time.Unix(updated, 0)
		out = append(out, &sum)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate nebulas: %w", err)
	}
	return out, nil
}

// SetStatus transitions a nebula's status and bumps updated_at.
func (s *NebulaStore) SetStatus(ctx context.Context, id, newStatus string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE nebulas SET status = ?, updated_at = ? WHERE id = ?",
		newStatus, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("fabric: set nebula status %q=%q: %w", id, newStatus, err)
	}
	return notFoundIfZero(res, id)
}

// ErrNotDeleted is returned by Undelete when the target nebula is not currently
// soft-deleted (deleted_at IS NULL) — it was never marked, or it was already
// hard-deleted by the GC and is unrecoverable.
var ErrNotDeleted = errors.New("fabric: nebula is not soft-deleted")

// Undelete clears a nebula's deleted_at, rescuing it from the GC while it is
// still within its grace window. It returns ErrNebulaNotFound when no such row
// exists and ErrNotDeleted when the row exists but was not soft-deleted (so the
// caller can distinguish "already swept" from "never marked").
func (s *NebulaStore) Undelete(ctx context.Context, id string) error {
	var deletedAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT deleted_at FROM nebulas WHERE id = ?", id).Scan(&deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNebulaNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("fabric: look up nebula %q: %w", id, err)
	}
	if !deletedAt.Valid {
		return fmt.Errorf("%w: %s", ErrNotDeleted, id)
	}
	if _, err := s.db.ExecContext(ctx,
		"UPDATE nebulas SET deleted_at = NULL, updated_at = ? WHERE id = ?",
		time.Now().Unix(), id); err != nil {
		return fmt.Errorf("fabric: undelete nebula %q: %w", id, err)
	}
	return nil
}

// UpdatePhaseResult records a phase's completion outcome. A non-empty Diff is
// written to the blobstore and its hash stored on the row.
func (s *NebulaStore) UpdatePhaseResult(ctx context.Context, nebulaID, phaseID string, result PhaseResult) error {
	var diffHash sql.NullString
	if len(result.Diff) > 0 {
		hash, err := s.blobs.Put(ctx, result.Diff)
		if err != nil {
			return fmt.Errorf("fabric: store phase diff %q/%q: %w", nebulaID, phaseID, err)
		}
		diffHash = sql.NullString{String: hash, Valid: true}
	}

	const q = `
		UPDATE phases
		SET status = ?, result_toml = ?, diff_blob_hash = ?, completed_at = ?
		WHERE nebula_id = ? AND id = ?`
	res, err := s.db.ExecContext(ctx, q,
		result.Status, result.ResultTOML, diffHash, time.Now().Unix(), nebulaID, phaseID)
	if err != nil {
		return fmt.Errorf("fabric: update phase result %q/%q: %w", nebulaID, phaseID, err)
	}
	return notFoundIfZero(res, nebulaID+"/"+phaseID)
}

// AppendMasterReview persists a master-review cycle's outcome onto the nebula
// row, optionally transitioning status. The single master_review_toml column
// holds the latest review; multi-cycle history lands with the dedicated table
// in a later phase.
func (s *NebulaStore) AppendMasterReview(ctx context.Context, nebulaID string, review MasterReviewRow) error {
	now := time.Now().Unix()
	if review.Status != "" {
		res, err := s.db.ExecContext(ctx,
			"UPDATE nebulas SET master_review_toml = ?, status = ?, updated_at = ? WHERE id = ?",
			review.ReviewTOML, review.Status, now, nebulaID)
		if err != nil {
			return fmt.Errorf("fabric: append master review %q: %w", nebulaID, err)
		}
		return notFoundIfZero(res, nebulaID)
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE nebulas SET master_review_toml = ?, updated_at = ? WHERE id = ?",
		review.ReviewTOML, now, nebulaID)
	if err != nil {
		return fmt.Errorf("fabric: append master review %q: %w", nebulaID, err)
	}
	return notFoundIfZero(res, nebulaID)
}

// MarkForGC sets gc_at to now+grace; the GC sweep cascades the delete later.
func (s *NebulaStore) MarkForGC(ctx context.Context, id string, graceDur time.Duration) error {
	gcAt := time.Now().Add(graceDur).Unix()
	res, err := s.db.ExecContext(ctx,
		"UPDATE nebulas SET gc_at = ?, updated_at = ? WHERE id = ?",
		gcAt, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("fabric: mark nebula for gc %q: %w", id, err)
	}
	return notFoundIfZero(res, id)
}

// notFoundIfZero returns ErrNebulaNotFound when the statement matched no rows.
func notFoundIfZero(res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("fabric: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNebulaNotFound, id)
	}
	return nil
}

// preview returns the first previewLen characters of body for list views,
// counting by rune so multi-byte characters are not split.
func preview(body string) string {
	r := []rune(body)
	if len(r) <= previewLen {
		return body
	}
	return string(r[:previewLen])
}

// generateNebulaID builds a stable, sortable id of the form
// nebula-<unix>-<slug> from the creation time and name.
func generateNebulaID(now time.Time, name string) string {
	slug := slugify(name)
	if slug == "" {
		return fmt.Sprintf("nebula-%d", now.Unix())
	}
	return fmt.Sprintf("nebula-%d-%s", now.Unix(), slug)
}

// slugify lowercases name and replaces runs of non-alphanumeric characters with
// single hyphens, trimming leading/trailing hyphens.
func slugify(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevHyphen = false
		case !prevHyphen && b.Len() > 0:
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}
