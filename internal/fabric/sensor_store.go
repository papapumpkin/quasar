package fabric

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SensorCursorStore is the typed API over the sensor_cursors table. The
// scheduler persists a sensor's opaque cursor here after each successful poll so
// progress survives a restart; the sensor itself never touches the database.
type SensorCursorStore struct {
	db *sql.DB
}

// NewSensorCursorStore constructs a store over an open database.
func NewSensorCursorStore(db *sql.DB) *SensorCursorStore {
	return &SensorCursorStore{db: db}
}

// Get returns the persisted cursor for (repoPath, sensorName), or a nil
// RawMessage when none has been stored yet (the sensor treats nil as "first
// poll"). A stored empty/NULL cursor is likewise returned as nil.
func (s *SensorCursorStore) Get(ctx context.Context, repoPath, sensorName string) (json.RawMessage, error) {
	const q = `SELECT cursor FROM sensor_cursors WHERE repo_path = ? AND sensor_name = ?`
	var raw []byte
	err := s.db.QueryRowContext(ctx, q, repoPath, sensorName).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fabric: get sensor cursor %q/%q: %w", repoPath, sensorName, err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	return json.RawMessage(raw), nil
}

// Set upserts the cursor for (repoPath, sensorName), stamping updated_at. A nil
// cursor is stored as an empty blob (the column is NOT NULL); Get maps it back
// to nil so the round-trip is lossless.
func (s *SensorCursorStore) Set(ctx context.Context, repoPath, sensorName string, cursor json.RawMessage) error {
	const q = `
		INSERT INTO sensor_cursors (repo_path, sensor_name, cursor, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(repo_path, sensor_name) DO UPDATE SET
			cursor     = excluded.cursor,
			updated_at = excluded.updated_at`
	if cursor == nil {
		cursor = json.RawMessage{}
	}
	if _, err := s.db.ExecContext(ctx, q, repoPath, sensorName, []byte(cursor), time.Now().Unix()); err != nil {
		return fmt.Errorf("fabric: set sensor cursor %q/%q: %w", repoPath, sensorName, err)
	}
	return nil
}

// SensorEventRow is the typed projection of a sensor_events row. ProcessedAt and
// NebulaID are zero/empty until the scheduler has seeded a nebula for the event.
type SensorEventRow struct {
	ID          int64
	RepoPath    string
	SensorName  string
	ExternalID  string
	ReceivedAt  time.Time
	ProcessedAt time.Time // zero until processed
	NebulaID    string    // "" until processed
}

// SensorEventStore is the typed API over the sensor_events table. The UNIQUE
// constraint on (repo_path, sensor_name, external_id) is the deduplication
// mechanism: Insert reports isNew=false for an already-observed event so the
// scheduler skips re-seeding work, even across a crash mid-processing.
type SensorEventStore struct {
	db *sql.DB
}

// NewSensorEventStore constructs a store over an open database.
func NewSensorEventStore(db *sql.DB) *SensorEventStore {
	return &SensorEventStore{db: db}
}

// Insert records an observed event. isNew is false when (repoPath, sensorName,
// externalID) already exists — the caller skips processing duplicates. The id is
// the row's id in both cases, so a caller that loses a race can still locate the
// row. ts is the event's observation time.
func (s *SensorEventStore) Insert(ctx context.Context, repoPath, sensorName, externalID string, ts time.Time) (id int64, isNew bool, err error) {
	const insert = `
		INSERT OR IGNORE INTO sensor_events (repo_path, sensor_name, external_id, received_at)
		VALUES (?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, insert, repoPath, sensorName, externalID, ts.Unix())
	if err != nil {
		return 0, false, fmt.Errorf("fabric: insert sensor event %q/%q/%q: %w", repoPath, sensorName, externalID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("fabric: sensor event rows affected: %w", err)
	}
	if affected == 1 {
		id, err = res.LastInsertId()
		if err != nil {
			return 0, false, fmt.Errorf("fabric: sensor event last insert id: %w", err)
		}
		return id, true, nil
	}

	// Duplicate: the row already exists. Look up its id so the caller can still
	// reference it.
	const find = `SELECT id FROM sensor_events WHERE repo_path = ? AND sensor_name = ? AND external_id = ?`
	if err := s.db.QueryRowContext(ctx, find, repoPath, sensorName, externalID).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("fabric: find sensor event %q/%q/%q: %w", repoPath, sensorName, externalID, err)
	}
	return id, false, nil
}

// MarkProcessed stamps processed_at=now and links the event to the seeded
// nebula. A blank nebulaID is stored as NULL so the ON DELETE SET NULL relation
// stays well-formed. Returns an error if no event has the id.
func (s *SensorEventStore) MarkProcessed(ctx context.Context, id int64, nebulaID string) error {
	const q = `UPDATE sensor_events SET processed_at = ?, nebula_id = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, time.Now().Unix(), nullIfEmpty(nebulaID), id)
	if err != nil {
		return fmt.Errorf("fabric: mark sensor event %d processed: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("fabric: sensor event rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("fabric: sensor event %d not found", id)
	}
	return nil
}

// Unprocessed returns events for (repoPath, sensorName) that have not yet been
// seeded into a nebula, oldest first. The scheduler uses it to recover events an
// earlier tick observed but crashed before processing.
func (s *SensorEventStore) Unprocessed(ctx context.Context, repoPath, sensorName string) ([]SensorEventRow, error) {
	const q = `
		SELECT id, repo_path, sensor_name, external_id, received_at
		FROM sensor_events
		WHERE repo_path = ? AND sensor_name = ? AND processed_at IS NULL
		ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, repoPath, sensorName)
	if err != nil {
		return nil, fmt.Errorf("fabric: list unprocessed sensor events %q/%q: %w", repoPath, sensorName, err)
	}
	defer rows.Close()

	var out []SensorEventRow
	for rows.Next() {
		var (
			r        SensorEventRow
			received int64
		)
		if err := rows.Scan(&r.ID, &r.RepoPath, &r.SensorName, &r.ExternalID, &received); err != nil {
			return nil, fmt.Errorf("fabric: scan sensor event: %w", err)
		}
		r.ReceivedAt = time.Unix(received, 0)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fabric: iterate sensor events: %w", err)
	}
	return out, nil
}
