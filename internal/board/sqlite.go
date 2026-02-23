package board

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver.
)

// ErrFileAlreadyClaimed is returned when ClaimFile is called for a file that
// is already owned by a different phase.
var ErrFileAlreadyClaimed = errors.New("file already claimed by another phase")

// schema contains the DDL executed on first open. Using IF NOT EXISTS makes
// it safe to run on every startup.
const schema = `
CREATE TABLE IF NOT EXISTS board (
    task_id    TEXT PRIMARY KEY,
    state      TEXT NOT NULL,
    worker     TEXT NOT NULL DEFAULT '',
    posted_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS contracts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    producer   TEXT NOT NULL,
    kind       TEXT NOT NULL,
    name       TEXT NOT NULL,
    signature  TEXT NOT NULL,
    package    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'fulfilled',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(producer, kind, name)
);

CREATE TABLE IF NOT EXISTS file_claims (
    filepath   TEXT PRIMARY KEY,
    owner_task TEXT NOT NULL,
    claimed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// SQLiteBoard implements Board using a local SQLite database in WAL mode.
type SQLiteBoard struct {
	db *sql.DB
}

// NewSQLiteBoard opens (or creates) a SQLite database at dbPath, enables WAL
// mode and busy timeout, and creates the schema tables if they do not exist.
func NewSQLiteBoard(ctx context.Context, dbPath string) (*SQLiteBoard, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("board: open database: %w", err)
	}

	// Limit to one connection. SQLite only supports a single writer; using
	// one connection avoids SQLITE_BUSY contention between pooled connections
	// that each need their own PRAGMA setup. WAL mode still benefits external
	// readers and provides crash-safe writes.
	db.SetMaxOpenConns(1)

	// Enable WAL mode — readers never block writers, writers never block readers.
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("board: enable WAL mode: %w", err)
	}

	// Busy timeout avoids SQLITE_BUSY under concurrent access from external processes.
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("board: set busy timeout: %w", err)
	}

	// Create tables idempotently.
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("board: create schema: %w", err)
	}

	return &SQLiteBoard{db: db}, nil
}

// SetPhaseState upserts the phase's state in the board table.
func (b *SQLiteBoard) SetPhaseState(ctx context.Context, phaseID, state string) error {
	const q = `
		INSERT INTO board (task_id, state, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(task_id) DO UPDATE SET state = excluded.state, updated_at = CURRENT_TIMESTAMP`
	if _, err := b.db.ExecContext(ctx, q, phaseID, state); err != nil {
		return fmt.Errorf("board: set phase state %q=%q: %w", phaseID, state, err)
	}
	return nil
}

// GetPhaseState returns the current state for a phase, or empty string if no
// entry exists.
func (b *SQLiteBoard) GetPhaseState(ctx context.Context, phaseID string) (string, error) {
	var state string
	err := b.db.QueryRowContext(ctx, "SELECT state FROM board WHERE task_id = ?", phaseID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("board: get phase state %q: %w", phaseID, err)
	}
	return state, nil
}

// PublishContract inserts or updates a single contract (upsert on unique constraint).
func (b *SQLiteBoard) PublishContract(ctx context.Context, c Contract) error {
	return b.PublishContracts(ctx, []Contract{c})
}

// PublishContracts inserts or updates multiple contracts in a single transaction.
func (b *SQLiteBoard) PublishContracts(ctx context.Context, contracts []Contract) error {
	if len(contracts) == 0 {
		return nil
	}

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("board: begin tx for contracts: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	const q = `
		INSERT INTO contracts (producer, kind, name, signature, package, status)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(producer, kind, name) DO UPDATE SET
			signature = excluded.signature,
			package   = excluded.package,
			status    = excluded.status`

	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return fmt.Errorf("board: prepare contract upsert: %w", err)
	}
	defer stmt.Close()

	for _, c := range contracts {
		status := c.Status
		if status == "" {
			status = StatusFulfilled
		}
		if _, err := stmt.ExecContext(ctx, c.Producer, c.Kind, c.Name, c.Signature, c.Package, status); err != nil {
			return fmt.Errorf("board: publish contract %q/%q/%q: %w", c.Producer, c.Kind, c.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("board: commit contracts: %w", err)
	}
	return nil
}

// ContractsFor returns all contracts published by the given phase.
func (b *SQLiteBoard) ContractsFor(ctx context.Context, phaseID string) ([]Contract, error) {
	const q = `SELECT id, producer, kind, name, signature, package, status, created_at
		FROM contracts WHERE producer = ? ORDER BY id`
	return b.queryContracts(ctx, q, phaseID)
}

// AllContracts returns every contract in the board.
func (b *SQLiteBoard) AllContracts(ctx context.Context) ([]Contract, error) {
	const q = `SELECT id, producer, kind, name, signature, package, status, created_at
		FROM contracts ORDER BY id`
	return b.queryContracts(ctx, q)
}

// queryContracts is a shared helper for scanning contract rows.
func (b *SQLiteBoard) queryContracts(ctx context.Context, query string, args ...any) ([]Contract, error) {
	rows, err := b.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("board: query contracts: %w", err)
	}
	defer rows.Close()

	var result []Contract
	for rows.Next() {
		var c Contract
		var ts string
		if err := rows.Scan(&c.ID, &c.Producer, &c.Kind, &c.Name, &c.Signature, &c.Package, &c.Status, &ts); err != nil {
			return nil, fmt.Errorf("board: scan contract: %w", err)
		}
		createdAt, parseErr := parseTimestamp(ts)
		if parseErr != nil {
			return nil, fmt.Errorf("board: parse contract timestamp: %w", parseErr)
		}
		c.CreatedAt = createdAt
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("board: iterate contracts: %w", err)
	}
	return result, nil
}

// timestampFormats lists the formats SQLite drivers may produce for
// CURRENT_TIMESTAMP. modernc.org/sqlite typically returns RFC 3339
// (with "T" separator and "Z" suffix), while canonical SQLite returns
// the space-separated DateTime format.
var timestampFormats = []string{
	time.RFC3339,
	time.DateTime,
}

// parseTimestamp attempts to parse a SQLite timestamp string using known formats.
func parseTimestamp(s string) (time.Time, error) {
	for _, layout := range timestampFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %q", s)
}

// ClaimFile registers file ownership for a phase. Returns ErrFileAlreadyClaimed
// if the file is already owned by a different phase. Re-claiming by the same
// phase is a no-op.
func (b *SQLiteBoard) ClaimFile(ctx context.Context, filepath, ownerPhaseID string) error {
	const q = `INSERT INTO file_claims (filepath, owner_task) VALUES (?, ?)
		ON CONFLICT(filepath) DO UPDATE SET owner_task = excluded.owner_task
		WHERE file_claims.owner_task = excluded.owner_task`

	res, err := b.db.ExecContext(ctx, q, filepath, ownerPhaseID)
	if err != nil {
		return fmt.Errorf("board: claim file %q: %w", filepath, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("board: claim file rows affected: %w", err)
	}

	// If no rows were affected, either the insert succeeded (rows=1) or the
	// conflict update matched the same owner (rows=1). If rows==0, the file
	// exists with a different owner and the WHERE clause prevented the update.
	if rows == 0 {
		// Verify: was this an insert that succeeded, or a blocked update?
		var existing string
		if err := b.db.QueryRowContext(ctx, "SELECT owner_task FROM file_claims WHERE filepath = ?", filepath).Scan(&existing); err != nil {
			return fmt.Errorf("board: verify file claim %q: %w", filepath, err)
		}
		if existing != ownerPhaseID {
			return fmt.Errorf("%w: %q owned by %q", ErrFileAlreadyClaimed, filepath, existing)
		}
	}
	return nil
}

// ReleaseClaims removes all file claims held by the given phase.
func (b *SQLiteBoard) ReleaseClaims(ctx context.Context, ownerPhaseID string) error {
	if _, err := b.db.ExecContext(ctx, "DELETE FROM file_claims WHERE owner_task = ?", ownerPhaseID); err != nil {
		return fmt.Errorf("board: release claims for %q: %w", ownerPhaseID, err)
	}
	return nil
}

// FileOwner returns the phase ID that owns the given file path, or empty string
// if unclaimed.
func (b *SQLiteBoard) FileOwner(ctx context.Context, filepath string) (string, error) {
	var owner string
	err := b.db.QueryRowContext(ctx, "SELECT owner_task FROM file_claims WHERE filepath = ?", filepath).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("board: file owner %q: %w", filepath, err)
	}
	return owner, nil
}

// ClaimsFor returns all file paths claimed by the given phase.
func (b *SQLiteBoard) ClaimsFor(ctx context.Context, phaseID string) ([]string, error) {
	rows, err := b.db.QueryContext(ctx, "SELECT filepath FROM file_claims WHERE owner_task = ? ORDER BY filepath", phaseID)
	if err != nil {
		return nil, fmt.Errorf("board: claims for %q: %w", phaseID, err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("board: scan claim: %w", err)
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("board: iterate claims: %w", err)
	}
	return paths, nil
}

// Close releases the database connection.
func (b *SQLiteBoard) Close() error {
	return b.db.Close()
}
