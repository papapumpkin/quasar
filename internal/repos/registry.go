package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// terminalNebulaStatuses are the nebula lifecycle statuses considered complete.
// A repo with only terminal nebulas can be unregistered without --force; any
// nebula in a non-terminal status blocks unregistration (or is orphaned with
// force). Kept here rather than in fabric to avoid a dependency cycle; the
// authoritative status set lives with the nebula lifecycle.
var terminalNebulaStatuses = []string{"merged", "done", "shipped", "canceled", "orphaned", "failed"}

// Registry manages the set of repos Quasar is willing to operate on. It owns
// CRUD against the repos table and the orphaning of a removed repo's nebulas.
type Registry struct {
	db *sql.DB
}

// New constructs a Registry over an open database. The caller owns the db's
// lifetime; the schema (repos table, nebulas.repo_path) must already exist via
// the fabric migrations.
func New(db *sql.DB) *Registry {
	return &Registry{db: db}
}

// Register adds a repo by path. It validates the path exists, is a directory,
// contains a .git entry, and is readable, then resolves it to an absolute path.
// The name defaults to filepath.Base(path) when empty. Returns
// ErrRepoAlreadyRegistered if the path is already registered, or
// ErrRepoPathInvalid if validation fails.
func (r *Registry) Register(ctx context.Context, path string, name string) (*Repo, error) {
	abs, err := validateRepoPath(path)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = filepath.Base(abs)
	}

	now := time.Now().Unix()
	const q = `INSERT INTO repos (path, name, status, added_at, updated_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO NOTHING`
	res, err := r.db.ExecContext(ctx, q, abs, name, StatusActive, now, now, now)
	if err != nil {
		return nil, fmt.Errorf("repos: register %q: %w", abs, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("repos: register rows affected: %w", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("%w: %s", ErrRepoAlreadyRegistered, abs)
	}

	return r.Get(ctx, abs)
}

// Unregister soft-deletes a repo. Without force, it returns ErrRepoActiveNebulas
// if the repo has any nebula in a non-terminal status. With force, the repo's
// status flips to "removed" and its non-terminal nebulas are flagged
// "orphaned" so the TUI surfaces them for manual resolution. Returns
// ErrRepoNotRegistered if the path is unknown.
func (r *Registry) Unregister(ctx context.Context, path string, force bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrRepoPathInvalid, path, err)
	}
	if _, err := r.Get(ctx, abs); err != nil {
		return err
	}

	active, err := r.activeNebulaCount(ctx, abs)
	if err != nil {
		return err
	}
	if active > 0 && !force {
		return fmt.Errorf("%w: %s has %d active nebula(s)", ErrRepoActiveNebulas, abs, active)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repos: begin unregister tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.ExecContext(ctx,
		"UPDATE repos SET status = ?, updated_at = ? WHERE path = ?",
		StatusRemoved, time.Now().Unix(), abs,
	); err != nil {
		return fmt.Errorf("repos: mark removed %q: %w", abs, err)
	}

	orphan := "UPDATE nebulas SET status = 'orphaned', updated_at = CURRENT_TIMESTAMP " +
		"WHERE repo_path = ? AND status NOT IN (" + statusPlaceholders() + ")"
	if _, err := tx.ExecContext(ctx, orphan, orphanArgs(abs)...); err != nil {
		return fmt.Errorf("repos: orphan nebulas for %q: %w", abs, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repos: commit unregister: %w", err)
	}
	return nil
}

// List returns all registered repos ordered by path. When statusFilter is
// non-empty, only repos with that status are returned.
func (r *Registry) List(ctx context.Context, statusFilter string) ([]*Repo, error) {
	q := "SELECT path, name, status, added_at, updated_at, last_seen_at FROM repos"
	var args []any
	if statusFilter != "" {
		q += " WHERE status = ?"
		args = append(args, statusFilter)
	}
	q += " ORDER BY path"

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("repos: list: %w", err)
	}
	defer rows.Close()

	var result []*Repo
	for rows.Next() {
		repo, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repos: iterate list: %w", err)
	}
	return result, nil
}

// Get returns a single repo by absolute path, or ErrRepoNotRegistered.
func (r *Registry) Get(ctx context.Context, path string) (*Repo, error) {
	const q = "SELECT path, name, status, added_at, updated_at, last_seen_at FROM repos WHERE path = ?"
	repo, err := scanRepo(r.db.QueryRowContext(ctx, q, path))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrRepoNotRegistered, path)
	}
	if err != nil {
		return nil, fmt.Errorf("repos: get %q: %w", path, err)
	}
	return repo, nil
}

// SetStatus updates a repo's status (used for pause/resume). Returns
// ErrRepoNotRegistered if the path is unknown.
func (r *Registry) SetStatus(ctx context.Context, path string, status string) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE repos SET status = ?, updated_at = ? WHERE path = ?",
		status, time.Now().Unix(), path,
	)
	if err != nil {
		return fmt.Errorf("repos: set status %q=%q: %w", path, status, err)
	}
	return requireAffected(res, path)
}

// Touch updates last_seen_at to now. The supervisor calls it on startup for
// every repo it boots a scheduler for. Returns ErrRepoNotRegistered if unknown.
func (r *Registry) Touch(ctx context.Context, path string) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE repos SET last_seen_at = ? WHERE path = ?",
		time.Now().Unix(), path,
	)
	if err != nil {
		return fmt.Errorf("repos: touch %q: %w", path, err)
	}
	return requireAffected(res, path)
}

// CountActiveNebulas returns the number of non-terminal nebulas associated with
// a repo. Used by `quasar repo show` to summarize in-flight work.
func (r *Registry) CountActiveNebulas(ctx context.Context, path string) (int, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %v", ErrRepoPathInvalid, path, err)
	}
	return r.activeNebulaCount(ctx, abs)
}

// activeNebulaCount returns the number of non-terminal nebulas for a repo.
func (r *Registry) activeNebulaCount(ctx context.Context, path string) (int, error) {
	q := "SELECT COUNT(*) FROM nebulas WHERE repo_path = ? AND status NOT IN (" + statusPlaceholders() + ")"
	var n int
	if err := r.db.QueryRowContext(ctx, q, orphanArgs(path)...).Scan(&n); err != nil {
		return 0, fmt.Errorf("repos: count active nebulas for %q: %w", path, err)
	}
	return n, nil
}

// requireAffected converts a zero-row UPDATE into ErrRepoNotRegistered.
func requireAffected(res sql.Result, path string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repos: rows affected for %q: %w", path, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s", ErrRepoNotRegistered, path)
	}
	return nil
}

// statusPlaceholders returns a comma-separated list of ? placeholders matching
// terminalNebulaStatuses for use in a NOT IN clause.
func statusPlaceholders() string {
	return strings.TrimSuffix(strings.Repeat("?,", len(terminalNebulaStatuses)), ",")
}

// orphanArgs returns the query args for a repo_path + terminal-status query:
// the path followed by each terminal status.
func orphanArgs(path string) []any {
	args := make([]any, 0, len(terminalNebulaStatuses)+1)
	args = append(args, path)
	for _, s := range terminalNebulaStatuses {
		args = append(args, s)
	}
	return args
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRepo reads a repo row, converting the stored unix timestamps to time.Time.
func scanRepo(s rowScanner) (*Repo, error) {
	var (
		repo                     Repo
		added, updated, lastSeen int64
	)
	if err := s.Scan(&repo.Path, &repo.Name, &repo.Status, &added, &updated, &lastSeen); err != nil {
		return nil, err
	}
	repo.AddedAt = time.Unix(added, 0)
	repo.UpdatedAt = time.Unix(updated, 0)
	repo.LastSeenAt = time.Unix(lastSeen, 0)
	return &repo, nil
}

// validateRepoPath resolves path to an absolute path and verifies it exists, is
// a directory, contains a .git entry, and is readable. Failures wrap
// ErrRepoPathInvalid.
func validateRepoPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRepoPathInvalid, path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrRepoPathInvalid, abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s is not a directory", ErrRepoPathInvalid, abs)
	}
	if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
		return "", fmt.Errorf("%w: %s has no .git subdirectory", ErrRepoPathInvalid, abs)
	}
	if _, err := os.ReadDir(abs); err != nil {
		return "", fmt.Errorf("%w: %s is not readable: %v", ErrRepoPathInvalid, abs, err)
	}
	return abs, nil
}
