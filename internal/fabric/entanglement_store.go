package fabric

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// EntanglementStore is the typed lifecycle API over the entanglements table.
// It drives a symbol through declared → claimed → in_flight → fulfilled, with
// withdrawn (terminal failure) and deprecated (producer removed the symbol) as
// the two branches off that spine. Each transition stamps a timestamp column so
// the TUI can render the history and the pre-flight coordination check can rank
// active intents by recency.
//
// The store shares the entanglements table with the legacy publisher path
// (PublishEntanglement, status='pending'); the two coexist. Migration 009 adds
// the lifecycle columns and migrates prior 'pending' rows to fulfilled/withdrawn
// based on their producing run's terminal state.
type EntanglementStore struct {
	db *sql.DB
}

// NewEntanglementStore constructs a store over an open database.
func NewEntanglementStore(db *sql.DB) *EntanglementStore {
	return &EntanglementStore{db: db}
}

// Declare records a producer's intent for a symbol with status 'declared'.
// Called by the architect operator at spec-parse time. Idempotent on
// (producer, kind, name): a second Declare for the same symbol is a no-op, not
// an error, so re-parsing a spec never disturbs an already-advancing lifecycle.
func (s *EntanglementStore) Declare(ctx context.Context, e Entanglement) error {
	if e.Producer == "" || e.Kind == "" || e.Name == "" {
		return fmt.Errorf("fabric: declare entanglement requires producer, kind, and name")
	}
	status := StatusDeclared
	if e.Status != "" {
		status = e.Status
	}
	const q = `
		INSERT INTO entanglements
			(producer, consumer, kind, name, signature, package, status,
			 run_id, phase_id, current_signature, declared_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(producer, kind, name) DO NOTHING`
	var consumer *string
	if e.Consumer != "" {
		consumer = &e.Consumer
	}
	_, err := s.db.ExecContext(ctx, q,
		e.Producer, consumer, e.Kind, e.Name, e.Signature, e.Package, status,
		nullString(e.RunID), e.PhaseID, e.Signature, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("fabric: declare entanglement %q/%q/%q: %w", e.Producer, e.Kind, e.Name, err)
	}
	return nil
}

// Claim transitions a declaration to 'claimed' when a coder picks up the phase,
// stamping run_id, phase_id, and claimed_at. It is a no-op when no row for the
// symbol is in 'declared' (the lifecycle has already advanced or never started).
func (s *EntanglementStore) Claim(ctx context.Context, runID, phaseID, name string) error {
	const q = `
		UPDATE entanglements
		   SET status = ?, run_id = ?, phase_id = ?, claimed_at = ?
		 WHERE name = ? AND status = ?`
	_, err := s.db.ExecContext(ctx, q,
		StatusClaimed, nullString(runID), phaseID, time.Now().Unix(), name, StatusDeclared)
	if err != nil {
		return fmt.Errorf("fabric: claim entanglement %q: %w", name, err)
	}
	return nil
}

// MarkInFlight records that a green build has touched the symbol, updating
// current_signature and in_flight_at atomically in a single statement. The
// signature is what siblings read in their pre-flight coordination notes. It
// affects only non-terminal rows (declared|claimed|in_flight) so a later build
// refreshes the signature without resurrecting a withdrawn row.
//
// A row the architect declared carries a NULL run_id (the run that will act on
// it is not yet known); MarkInFlight matches such an unbound row by name and
// binds it to runID, the same way Claim does. That lets the runtime's
// green-build hook advance an architect declaration even when no Claim ran
// first. A row already bound to a different run is left untouched.
func (s *EntanglementStore) MarkInFlight(ctx context.Context, runID, name, signature string) error {
	const q = `
		UPDATE entanglements
		   SET status = ?, current_signature = ?, in_flight_at = ?, run_id = ?
		 WHERE name = ?
		   AND (run_id = ? OR run_id IS NULL)
		   AND status IN (?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q,
		StatusInFlight, signature, time.Now().Unix(), runID,
		name, runID,
		StatusDeclared, StatusClaimed, StatusInFlight)
	if err != nil {
		return fmt.Errorf("fabric: mark in_flight entanglement %q: %w", name, err)
	}
	return nil
}

// Deprecate transitions any of the run's non-terminal entanglements for the
// symbol to 'deprecated'. Called by neutron when a diff deletes the symbol's
// declaration so downstream consumers learn not to reintroduce a use of it.
func (s *EntanglementStore) Deprecate(ctx context.Context, runID, name string) error {
	const q = `
		UPDATE entanglements
		   SET status = ?
		 WHERE run_id = ? AND name = ?
		   AND status IN (?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q,
		StatusDeprecated, runID, name,
		StatusDeclared, StatusClaimed, StatusInFlight)
	if err != nil {
		return fmt.Errorf("fabric: deprecate entanglement %q: %w", name, err)
	}
	return nil
}

// Fulfill transitions the run's in_flight entanglements to 'fulfilled',
// stamping terminated_at. Called by the supervisor on post-merge success — a
// premature Fulfill from inside a cycle would claim a symbol shipped before the
// merge gate confirms it.
func (s *EntanglementStore) Fulfill(ctx context.Context, runID string) error {
	const q = `
		UPDATE entanglements
		   SET status = ?, terminated_at = ?
		 WHERE run_id = ? AND status = ?`
	_, err := s.db.ExecContext(ctx, q, StatusFulfilled, time.Now().Unix(), runID, StatusInFlight)
	if err != nil {
		return fmt.Errorf("fabric: fulfill entanglements for run %q: %w", runID, err)
	}
	return nil
}

// Withdraw transitions the run's non-terminal entanglements to 'withdrawn',
// stamping terminated_at. Called by the supervisor on terminal failure (failed
// or awaiting_human) so abandoned intent stops showing up in sibling
// coordination notes. The "non-terminal" set is exactly activeStatuses — the
// same rows Active surfaces — so the two share that definition.
func (s *EntanglementStore) Withdraw(ctx context.Context, runID string) error {
	clause, statusArgs := inClause(activeStatuses)
	q := fmt.Sprintf(`
		UPDATE entanglements
		   SET status = ?, terminated_at = ?
		 WHERE run_id = ? AND status IN (%s)`, clause)
	args := append([]any{StatusWithdrawn, time.Now().Unix(), runID}, statusArgs...)
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("fabric: withdraw entanglements for run %q: %w", runID, err)
	}
	return nil
}

// Active returns the symbol's entanglements whose status signals in-flight
// intent (declared | claimed | in_flight | deprecated), most-recent first. The
// pre-flight coordination check (Phase 01) reads these to warn a sibling coder
// about concurrent work and the latest in-flight signature draft. Terminal rows
// (fulfilled, withdrawn) and legacy 'pending' rows are excluded.
func (s *EntanglementStore) Active(ctx context.Context, name string) ([]Entanglement, error) {
	clause, statusArgs := inClause(activeStatuses)
	q := fmt.Sprintf(`
		SELECT id, producer, consumer, kind, name, signature, package, status,
		       run_id, phase_id, current_signature,
		       declared_at, claimed_at, in_flight_at, terminated_at, created_at
		  FROM entanglements
		 WHERE name = ? AND status IN (%s)
		 ORDER BY COALESCE(in_flight_at, claimed_at, declared_at, 0) DESC, id DESC`, clause)
	rows, err := s.db.QueryContext(ctx, q, append([]any{name}, statusArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("fabric: query active entanglements %q: %w", name, err)
	}
	defer rows.Close() //nolint:errcheck // iteration cleanup

	var out []Entanglement
	for rows.Next() {
		e, err := scanLifecycleRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// scanLifecycleRow scans a row carrying the lifecycle columns, coping with the
// NULL-able run_id / timestamp / consumer columns that legacy rows leave unset.
func scanLifecycleRow(rows *sql.Rows) (Entanglement, error) {
	var e Entanglement
	var (
		consumer, runID, curSig sql.NullString
		declaredAt, claimedAt   sql.NullInt64
		inFlightAt, terminated  sql.NullInt64
		createdAt               string
	)
	if err := rows.Scan(
		&e.ID, &e.Producer, &consumer, &e.Kind, &e.Name, &e.Signature, &e.Package, &e.Status,
		&runID, &e.PhaseID, &curSig,
		&declaredAt, &claimedAt, &inFlightAt, &terminated, &createdAt,
	); err != nil {
		return Entanglement{}, fmt.Errorf("fabric: scan lifecycle entanglement: %w", err)
	}
	e.Consumer = consumer.String
	e.RunID = runID.String
	e.CurrentSignature = curSig.String
	e.DeclaredAt = declaredAt.Int64
	e.ClaimedAt = claimedAt.Int64
	e.InFlightAt = inFlightAt.Int64
	e.TerminatedAt = terminated.Int64
	if ts, err := parseTimestamp(createdAt); err == nil {
		e.CreatedAt = ts
	}
	return e, nil
}

// inClause builds the placeholder list ("?, ?, ?") and the matching argument
// slice for a SQL `IN (...)` over the given statuses. It lets activeStatuses be
// the single source of truth for which states count as active, so Active and
// Withdraw can never drift from the documented set.
func inClause(statuses []string) (string, []any) {
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, s := range statuses {
		placeholders[i] = "?"
		args[i] = s
	}
	return strings.Join(placeholders, ", "), args
}

// nullString maps an empty string to a SQL NULL so the run_id column stays NULL
// (and out of the run index) for rows that have no run yet, rather than "".
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
