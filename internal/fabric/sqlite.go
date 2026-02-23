package fabric

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
CREATE TABLE IF NOT EXISTS fabric (
    task_id    TEXT PRIMARY KEY,
    state      TEXT NOT NULL,
    worker     TEXT NOT NULL DEFAULT '',
    posted_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS entanglements (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    producer   TEXT NOT NULL,
    consumer   TEXT,
    kind       TEXT NOT NULL,
    name       TEXT NOT NULL,
    signature  TEXT NOT NULL,
    package    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(producer, kind, name)
);

CREATE TABLE IF NOT EXISTS file_claims (
    filepath   TEXT PRIMARY KEY,
    owner_task TEXT NOT NULL,
    claimed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS discoveries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_task TEXT NOT NULL,
    kind        TEXT NOT NULL,
    detail      TEXT NOT NULL,
    affects     TEXT,
    resolved    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS beads (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    content    TEXT NOT NULL,
    kind       TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// SQLiteFabric implements Fabric using a local SQLite database in WAL mode.
type SQLiteFabric struct {
	db *sql.DB
}

// NewSQLiteFabric opens (or creates) a SQLite database at dbPath, enables WAL
// mode and busy timeout, and creates the schema tables if they do not exist.
func NewSQLiteFabric(ctx context.Context, dbPath string) (*SQLiteFabric, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("fabric: open database: %w", err)
	}

	// Limit to one connection. SQLite only supports a single writer; using
	// one connection avoids SQLITE_BUSY contention between pooled connections
	// that each need their own PRAGMA setup. WAL mode still benefits external
	// readers and provides crash-safe writes.
	db.SetMaxOpenConns(1)

	// Enable WAL mode — readers never block writers, writers never block readers.
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("fabric: enable WAL mode: %w", err)
	}

	// Busy timeout avoids SQLITE_BUSY under concurrent access from external processes.
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("fabric: set busy timeout: %w", err)
	}

	// Create tables idempotently.
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("fabric: create schema: %w", err)
	}

	return &SQLiteFabric{db: db}, nil
}

// SetPhaseState upserts the phase's state in the fabric table.
func (f *SQLiteFabric) SetPhaseState(ctx context.Context, phaseID, state string) error {
	const q = `
		INSERT INTO fabric (task_id, state, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(task_id) DO UPDATE SET state = excluded.state, updated_at = CURRENT_TIMESTAMP`
	if _, err := f.db.ExecContext(ctx, q, phaseID, state); err != nil {
		return fmt.Errorf("fabric: set phase state %q=%q: %w", phaseID, state, err)
	}
	return nil
}

// GetPhaseState returns the current state for a phase, or empty string if no
// entry exists.
func (f *SQLiteFabric) GetPhaseState(ctx context.Context, phaseID string) (string, error) {
	var state string
	err := f.db.QueryRowContext(ctx, "SELECT state FROM fabric WHERE task_id = ?", phaseID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("fabric: get phase state %q: %w", phaseID, err)
	}
	return state, nil
}

// AllPhaseStates returns a map of phase ID to current state for all phases in the fabric table.
func (f *SQLiteFabric) AllPhaseStates(ctx context.Context) (map[string]string, error) {
	rows, err := f.db.QueryContext(ctx, "SELECT task_id, state FROM fabric ORDER BY task_id")
	if err != nil {
		return nil, fmt.Errorf("fabric: all phase states: %w", err)
	}
	defer rows.Close()

	states := make(map[string]string)
	for rows.Next() {
		var id, state string
		if err := rows.Scan(&id, &state); err != nil {
			return nil, fmt.Errorf("fabric: scan phase state: %w", err)
		}
		states[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate phase states: %w", err)
	}
	return states, nil
}

// PublishEntanglement inserts or updates a single entanglement (upsert on unique constraint).
func (f *SQLiteFabric) PublishEntanglement(ctx context.Context, e Entanglement) error {
	return f.PublishEntanglements(ctx, []Entanglement{e})
}

// PublishEntanglements inserts or updates multiple entanglements in a single transaction.
func (f *SQLiteFabric) PublishEntanglements(ctx context.Context, entanglements []Entanglement) error {
	if len(entanglements) == 0 {
		return nil
	}

	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fabric: begin tx for entanglements: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	const q = `
		INSERT INTO entanglements (producer, consumer, kind, name, signature, package, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(producer, kind, name) DO UPDATE SET
			consumer  = excluded.consumer,
			signature = excluded.signature,
			package   = excluded.package,
			status    = excluded.status`

	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return fmt.Errorf("fabric: prepare entanglement upsert: %w", err)
	}
	defer stmt.Close()

	for _, e := range entanglements {
		status := e.Status
		if status == "" {
			status = StatusPending
		}
		var consumer *string
		if e.Consumer != "" {
			consumer = &e.Consumer
		}
		if _, err := stmt.ExecContext(ctx, e.Producer, consumer, e.Kind, e.Name, e.Signature, e.Package, status); err != nil {
			return fmt.Errorf("fabric: publish entanglement %q/%q/%q: %w", e.Producer, e.Kind, e.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fabric: commit entanglements: %w", err)
	}
	return nil
}

// EntanglementsFor returns all entanglements published by the given phase.
func (f *SQLiteFabric) EntanglementsFor(ctx context.Context, phaseID string) ([]Entanglement, error) {
	const q = `SELECT id, producer, consumer, kind, name, signature, package, status, created_at
		FROM entanglements WHERE producer = ? ORDER BY id`
	return f.queryEntanglements(ctx, q, phaseID)
}

// AllEntanglements returns every entanglement in the fabric.
func (f *SQLiteFabric) AllEntanglements(ctx context.Context) ([]Entanglement, error) {
	const q = `SELECT id, producer, consumer, kind, name, signature, package, status, created_at
		FROM entanglements ORDER BY id`
	return f.queryEntanglements(ctx, q)
}

// queryEntanglements is a shared helper for scanning entanglement rows.
func (f *SQLiteFabric) queryEntanglements(ctx context.Context, query string, args ...any) ([]Entanglement, error) {
	rows, err := f.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fabric: query entanglements: %w", err)
	}
	defer rows.Close()

	var result []Entanglement
	for rows.Next() {
		var e Entanglement
		var ts string
		var consumer sql.NullString
		if err := rows.Scan(&e.ID, &e.Producer, &consumer, &e.Kind, &e.Name, &e.Signature, &e.Package, &e.Status, &ts); err != nil {
			return nil, fmt.Errorf("fabric: scan entanglement: %w", err)
		}
		if consumer.Valid {
			e.Consumer = consumer.String
		}
		createdAt, parseErr := parseTimestamp(ts)
		if parseErr != nil {
			return nil, fmt.Errorf("fabric: parse entanglement timestamp: %w", parseErr)
		}
		e.CreatedAt = createdAt
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate entanglements: %w", err)
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
func (f *SQLiteFabric) ClaimFile(ctx context.Context, filepath, ownerPhaseID string) error {
	const q = `INSERT INTO file_claims (filepath, owner_task) VALUES (?, ?)
		ON CONFLICT(filepath) DO UPDATE SET owner_task = excluded.owner_task
		WHERE file_claims.owner_task = excluded.owner_task`

	res, err := f.db.ExecContext(ctx, q, filepath, ownerPhaseID)
	if err != nil {
		return fmt.Errorf("fabric: claim file %q: %w", filepath, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("fabric: claim file rows affected: %w", err)
	}

	// If no rows were affected, either the insert succeeded (rows=1) or the
	// conflict update matched the same owner (rows=1). If rows==0, the file
	// exists with a different owner and the WHERE clause prevented the update.
	if rows == 0 {
		// Verify: was this an insert that succeeded, or a blocked update?
		var existing string
		if err := f.db.QueryRowContext(ctx, "SELECT owner_task FROM file_claims WHERE filepath = ?", filepath).Scan(&existing); err != nil {
			return fmt.Errorf("fabric: verify file claim %q: %w", filepath, err)
		}
		if existing != ownerPhaseID {
			return fmt.Errorf("%w: %q owned by %q", ErrFileAlreadyClaimed, filepath, existing)
		}
	}
	return nil
}

// ReleaseClaims removes all file claims held by the given phase.
func (f *SQLiteFabric) ReleaseClaims(ctx context.Context, ownerPhaseID string) error {
	if _, err := f.db.ExecContext(ctx, "DELETE FROM file_claims WHERE owner_task = ?", ownerPhaseID); err != nil {
		return fmt.Errorf("fabric: release claims for %q: %w", ownerPhaseID, err)
	}
	return nil
}

// ReleaseFileClaim removes a specific file claim if it is owned by the given phase.
// Returns nil if the file was not claimed.
func (f *SQLiteFabric) ReleaseFileClaim(ctx context.Context, filepath, ownerPhaseID string) error {
	if _, err := f.db.ExecContext(ctx, "DELETE FROM file_claims WHERE filepath = ? AND owner_task = ?", filepath, ownerPhaseID); err != nil {
		return fmt.Errorf("fabric: release file claim %q for %q: %w", filepath, ownerPhaseID, err)
	}
	return nil
}

// FileOwner returns the phase ID that owns the given file path, or empty string
// if unclaimed.
func (f *SQLiteFabric) FileOwner(ctx context.Context, filepath string) (string, error) {
	var owner string
	err := f.db.QueryRowContext(ctx, "SELECT owner_task FROM file_claims WHERE filepath = ?", filepath).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("fabric: file owner %q: %w", filepath, err)
	}
	return owner, nil
}

// ClaimsFor returns all file paths claimed by the given phase.
func (f *SQLiteFabric) ClaimsFor(ctx context.Context, phaseID string) ([]string, error) {
	rows, err := f.db.QueryContext(ctx, "SELECT filepath FROM file_claims WHERE owner_task = ? ORDER BY filepath", phaseID)
	if err != nil {
		return nil, fmt.Errorf("fabric: claims for %q: %w", phaseID, err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("fabric: scan claim: %w", err)
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate claims: %w", err)
	}
	return paths, nil
}

// AllClaims returns all file claims in the fabric.
func (f *SQLiteFabric) AllClaims(ctx context.Context) ([]Claim, error) {
	rows, err := f.db.QueryContext(ctx, "SELECT filepath, owner_task, claimed_at FROM file_claims ORDER BY filepath")
	if err != nil {
		return nil, fmt.Errorf("fabric: all claims: %w", err)
	}
	defer rows.Close()

	var claims []Claim
	for rows.Next() {
		var c Claim
		var ts string
		if err := rows.Scan(&c.Filepath, &c.OwnerTask, &ts); err != nil {
			return nil, fmt.Errorf("fabric: scan claim: %w", err)
		}
		c.ClaimedAt, err = parseTimestamp(ts)
		if err != nil {
			return nil, fmt.Errorf("fabric: parse claim timestamp: %w", err)
		}
		claims = append(claims, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate claims: %w", err)
	}
	return claims, nil
}

// PostDiscovery inserts a new discovery record into the fabric and returns its ID.
func (f *SQLiteFabric) PostDiscovery(ctx context.Context, d Discovery) (int64, error) {
	const q = `INSERT INTO discoveries (source_task, kind, detail, affects, resolved)
		VALUES (?, ?, ?, ?, FALSE)`
	var affects *string
	if d.Affects != "" {
		affects = &d.Affects
	}
	res, err := f.db.ExecContext(ctx, q, d.SourceTask, d.Kind, d.Detail, affects)
	if err != nil {
		return 0, fmt.Errorf("fabric: post discovery from %q: %w", d.SourceTask, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("fabric: get discovery id: %w", err)
	}
	return id, nil
}

// Discoveries returns all discoveries posted by the given task.
func (f *SQLiteFabric) Discoveries(ctx context.Context, taskID string) ([]Discovery, error) {
	const q = `SELECT id, source_task, kind, detail, affects, resolved, created_at
		FROM discoveries WHERE source_task = ? ORDER BY id`
	return f.queryDiscoveries(ctx, q, taskID)
}

// AllDiscoveries returns every discovery in the fabric.
func (f *SQLiteFabric) AllDiscoveries(ctx context.Context) ([]Discovery, error) {
	const q = `SELECT id, source_task, kind, detail, affects, resolved, created_at
		FROM discoveries ORDER BY id`
	return f.queryDiscoveries(ctx, q)
}

// ResolveDiscovery marks a discovery as resolved by its ID.
func (f *SQLiteFabric) ResolveDiscovery(ctx context.Context, id int64) error {
	const q = `UPDATE discoveries SET resolved = TRUE WHERE id = ?`
	res, err := f.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("fabric: resolve discovery %d: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("fabric: resolve discovery rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("fabric: discovery %d not found", id)
	}
	return nil
}

// UnresolvedDiscoveries returns all discoveries that have not been resolved.
func (f *SQLiteFabric) UnresolvedDiscoveries(ctx context.Context) ([]Discovery, error) {
	const q = `SELECT id, source_task, kind, detail, affects, resolved, created_at
		FROM discoveries WHERE resolved = FALSE ORDER BY id`
	return f.queryDiscoveries(ctx, q)
}

// queryDiscoveries is a shared helper for scanning discovery rows.
func (f *SQLiteFabric) queryDiscoveries(ctx context.Context, query string, args ...any) ([]Discovery, error) {
	rows, err := f.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fabric: query discoveries: %w", err)
	}
	defer rows.Close()

	var result []Discovery
	for rows.Next() {
		var d Discovery
		var ts string
		var affects sql.NullString
		if err := rows.Scan(&d.ID, &d.SourceTask, &d.Kind, &d.Detail, &affects, &d.Resolved, &ts); err != nil {
			return nil, fmt.Errorf("fabric: scan discovery: %w", err)
		}
		if affects.Valid {
			d.Affects = affects.String
		}
		createdAt, parseErr := parseTimestamp(ts)
		if parseErr != nil {
			return nil, fmt.Errorf("fabric: parse discovery timestamp: %w", parseErr)
		}
		d.CreatedAt = createdAt
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate discoveries: %w", err)
	}
	return result, nil
}

// AddBead inserts a new bead (working memory entry) for a task.
func (f *SQLiteFabric) AddBead(ctx context.Context, b Bead) error {
	const q = `INSERT INTO beads (task_id, content, kind) VALUES (?, ?, ?)`
	if _, err := f.db.ExecContext(ctx, q, b.TaskID, b.Content, b.Kind); err != nil {
		return fmt.Errorf("fabric: add bead for %q: %w", b.TaskID, err)
	}
	return nil
}

// BeadsFor returns all beads associated with the given task, ordered by ID.
func (f *SQLiteFabric) BeadsFor(ctx context.Context, taskID string) ([]Bead, error) {
	const q = `SELECT id, task_id, content, kind, created_at
		FROM beads WHERE task_id = ? ORDER BY id`
	rows, err := f.db.QueryContext(ctx, q, taskID)
	if err != nil {
		return nil, fmt.Errorf("fabric: query beads for %q: %w", taskID, err)
	}
	defer rows.Close()

	var result []Bead
	for rows.Next() {
		var b Bead
		var ts string
		if err := rows.Scan(&b.ID, &b.TaskID, &b.Content, &b.Kind, &ts); err != nil {
			return nil, fmt.Errorf("fabric: scan bead: %w", err)
		}
		createdAt, parseErr := parseTimestamp(ts)
		if parseErr != nil {
			return nil, fmt.Errorf("fabric: parse bead timestamp: %w", parseErr)
		}
		b.CreatedAt = createdAt
		result = append(result, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate beads: %w", err)
	}
	return result, nil
}

// Close releases the database connection.
func (f *SQLiteFabric) Close() error {
	return f.db.Close()
}
