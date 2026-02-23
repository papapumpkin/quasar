// Package neutron provides epoch archival and stale state cleanup for fabrics.
//
// A neutron is a standalone SQLite file containing the archived state of one
// epoch — its entanglements, discoveries, pulses, task records, and aggregate
// metadata. After archival, the active fabric is purged so it stays clean for
// future epochs.
package neutron

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver.

	"github.com/papapumpkin/quasar/internal/fabric"
)

// Sentinel errors for archive validation.
var (
	ErrActiveClaims          = errors.New("neutron: active claims remain in fabric")
	ErrUnresolvedDiscoveries = errors.New("neutron: unresolved discoveries remain in fabric")
)

// neutronSchema is the DDL for the standalone neutron SQLite file.
const neutronSchema = `
CREATE TABLE IF NOT EXISTS metadata (
    epoch_id    TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    total_cost  REAL,
    wall_clock  INTEGER,
    task_count  INTEGER,
    cycle_count INTEGER
);

CREATE TABLE IF NOT EXISTS tasks (
    task_id     TEXT PRIMARY KEY,
    final_state TEXT NOT NULL,
    cycles_used INTEGER,
    cost_usd    REAL
);

CREATE TABLE IF NOT EXISTS entanglements (
    id        INTEGER PRIMARY KEY,
    producer  TEXT NOT NULL,
    consumer  TEXT,
    interface TEXT NOT NULL,
    status    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS discoveries (
    id          INTEGER PRIMARY KEY,
    source_task TEXT NOT NULL,
    kind        TEXT NOT NULL,
    detail      TEXT NOT NULL,
    resolved    BOOLEAN,
    created     TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pulses (
    id        INTEGER PRIMARY KEY,
    task_id   TEXT NOT NULL,
    content   TEXT NOT NULL,
    kind      TEXT NOT NULL,
    created   TIMESTAMP
);
`

// Neutron represents an archived epoch snapshot.
type Neutron struct {
	EpochID   string
	CreatedAt time.Time
	DBPath    string // path to the archived SQLite file
}

// ArchiveOptions controls optional behavior during archival.
type ArchiveOptions struct {
	// Force allows archival even when unresolved discoveries exist.
	Force bool
}

// Archive snapshots the current epoch's state from the fabric into a neutron.
// It validates that no active claims remain (error unless forced for discoveries),
// creates a new SQLite file at outputPath, copies all relevant state, and purges
// the epoch's rows from the active fabric.
func Archive(ctx context.Context, f fabric.Fabric, epochID, outputPath string, opts ArchiveOptions) (*Neutron, error) {
	// Step 1: Validate no active claims remain.
	claims, err := f.AllClaims(ctx)
	if err != nil {
		return nil, fmt.Errorf("neutron: read claims: %w", err)
	}
	if len(claims) > 0 {
		return nil, fmt.Errorf("%w: %d claim(s) still held", ErrActiveClaims, len(claims))
	}

	// Step 2: Check for unresolved discoveries.
	unresolved, err := f.UnresolvedDiscoveries(ctx)
	if err != nil {
		return nil, fmt.Errorf("neutron: read unresolved discoveries: %w", err)
	}
	if len(unresolved) > 0 && !opts.Force {
		return nil, fmt.Errorf("%w: %d unresolved", ErrUnresolvedDiscoveries, len(unresolved))
	}

	// Step 3: Read all fabric state.
	phaseStates, err := f.AllPhaseStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("neutron: read phase states: %w", err)
	}

	entanglements, err := f.AllEntanglements(ctx)
	if err != nil {
		return nil, fmt.Errorf("neutron: read entanglements: %w", err)
	}

	discoveries, err := f.AllDiscoveries(ctx)
	if err != nil {
		return nil, fmt.Errorf("neutron: read discoveries: %w", err)
	}

	pulses, err := f.AllPulses(ctx)
	if err != nil {
		return nil, fmt.Errorf("neutron: read pulses: %w", err)
	}

	// Step 4: Create the neutron SQLite file.
	ndb, err := openNeutronDB(ctx, outputPath)
	if err != nil {
		return nil, err
	}
	defer ndb.Close()

	now := time.Now().UTC()

	// Write all data in a single transaction.
	tx, err := ndb.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("neutron: begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort on error paths

	// Metadata.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO metadata (epoch_id, created_at, task_count) VALUES (?, ?, ?)`,
		epochID, now, len(phaseStates)); err != nil {
		return nil, fmt.Errorf("neutron: insert metadata: %w", err)
	}

	// Tasks (phase states).
	for taskID, state := range phaseStates {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tasks (task_id, final_state) VALUES (?, ?)`,
			taskID, state); err != nil {
			return nil, fmt.Errorf("neutron: insert task %s: %w", taskID, err)
		}
	}

	// Entanglements.
	for _, e := range entanglements {
		iface := e.Kind + ":" + e.Name
		if e.Signature != "" {
			iface = e.Signature
		}
		consumer := e.Consumer
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entanglements (id, producer, consumer, interface, status) VALUES (?, ?, ?, ?, ?)`,
			e.ID, e.Producer, consumer, iface, e.Status); err != nil {
			return nil, fmt.Errorf("neutron: insert entanglement %d: %w", e.ID, err)
		}
	}

	// Discoveries.
	for _, d := range discoveries {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO discoveries (id, source_task, kind, detail, resolved, created) VALUES (?, ?, ?, ?, ?, ?)`,
			d.ID, d.SourceTask, d.Kind, d.Detail, d.Resolved, d.CreatedAt); err != nil {
			return nil, fmt.Errorf("neutron: insert discovery %d: %w", d.ID, err)
		}
	}

	// Pulses.
	for _, p := range pulses {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO pulses (id, task_id, content, kind, created) VALUES (?, ?, ?, ?, ?)`,
			p.ID, p.TaskID, p.Content, p.Kind, p.CreatedAt); err != nil {
			return nil, fmt.Errorf("neutron: insert pulse %d: %w", p.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("neutron: commit archive: %w", err)
	}

	// Step 5: Purge the active fabric.
	if err := f.PurgeAll(ctx); err != nil {
		return nil, fmt.Errorf("neutron: purge active fabric: %w", err)
	}

	return &Neutron{
		EpochID:   epochID,
		CreatedAt: now,
		DBPath:    outputPath,
	}, nil
}

// Purge removes all state from the active fabric without archiving.
// Use for abandoned epochs that do not need preservation.
func Purge(ctx context.Context, f fabric.Fabric) error {
	return f.PurgeAll(ctx)
}

// openNeutronDB creates and initializes a new neutron SQLite file.
func openNeutronDB(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("neutron: open database %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, neutronSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("neutron: create schema: %w", err)
	}
	return db, nil
}

// ReadNeutron opens an existing neutron file and returns its metadata.
func ReadNeutron(ctx context.Context, path string) (*Neutron, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("neutron: open %s: %w", path, err)
	}
	defer db.Close()

	var n Neutron
	n.DBPath = path
	err = db.QueryRowContext(ctx, `SELECT epoch_id, created_at FROM metadata LIMIT 1`).
		Scan(&n.EpochID, &n.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("neutron: read metadata from %s: %w", path, err)
	}
	return &n, nil
}
