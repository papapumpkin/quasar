package fabric

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrRunNotFound is returned when a constellation run row is absent.
var ErrRunNotFound = errors.New("fabric: constellation run not found")

// ConstellationRunStore is the typed API over the constellation_runs and
// star_invocations tables. The runtime owns the write side: it inserts a run on
// Fire, advances it on Step (persisting dag_state_toml for resume), and a
// supervisor reaps stale rows whose heartbeat has gone cold.
type ConstellationRunStore struct {
	db *sql.DB
}

// NewConstellationRunStore constructs a store over an open database.
func NewConstellationRunStore(db *sql.DB) *ConstellationRunStore {
	return &ConstellationRunStore{db: db}
}

// RunRow is the typed projection of a constellation_runs row.
type RunRow struct {
	ID                string
	RepoPath          string
	NebulaID          string
	ConstellationName string
	Snapshot          []byte // snapshotted constellation TOML at Fire time
	ParentRunID       string // "" when this is a root run
	State             string // running|paused|blocked_on_review|crashed|killed|done|failed
	CurrentNode       string
	StepIndex         int
	Cycle             int
	DAGStateTOML      string
	CreatedAt         int64
	UpdatedAt         int64
	CompletedAt       int64
	HeartbeatAt       int64
}

// StarInvocationRow records one star (or builtin) node firing within a run, so
// the TUI can render a step trace by polling, robust to runtime restarts.
type StarInvocationRow struct {
	RunID            string
	Seq              int
	Node             string
	StarName         string
	State            string // running|done|failed
	Cycle            int
	CostUSD          float64
	DurationMs       int64
	RationaleBlob    string
	RationalePreview string
	ParsedResultTOML string
	StartedAt        int64
	EndedAt          int64
}

// InsertRun persists a new run. A blank ID is generated. CreatedAt/UpdatedAt/
// HeartbeatAt default to now when zero. Returns the run ID.
func (s *ConstellationRunStore) InsertRun(ctx context.Context, r RunRow) (string, error) {
	now := time.Now()
	if r.ID == "" {
		r.ID = fmt.Sprintf("crun-%d-%s", now.UnixNano(), slugify(r.ConstellationName))
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = now.Unix()
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = r.CreatedAt
	}
	if r.HeartbeatAt == 0 {
		r.HeartbeatAt = r.CreatedAt
	}
	if r.State == "" {
		r.State = "running"
	}

	const q = `
		INSERT INTO constellation_runs
			(id, repo_path, nebula_id, constellation_name, constellation_snapshot,
			 parent_run_id, state, current_node, step_index, cycle, dag_state_toml,
			 created_at, updated_at, heartbeat_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, q,
		r.ID, r.RepoPath, r.NebulaID, r.ConstellationName, r.Snapshot,
		nullIfEmpty(r.ParentRunID), r.State, r.CurrentNode, r.StepIndex, r.Cycle, r.DAGStateTOML,
		r.CreatedAt, r.UpdatedAt, r.HeartbeatAt,
	); err != nil {
		return "", fmt.Errorf("fabric: insert constellation run %q: %w", r.ID, err)
	}
	return r.ID, nil
}

// GetRun loads a run by ID. Returns ErrRunNotFound when absent.
func (s *ConstellationRunStore) GetRun(ctx context.Context, id string) (*RunRow, error) {
	const q = `
		SELECT id, repo_path, nebula_id, constellation_name,
		       COALESCE(constellation_snapshot, x''),
		       COALESCE(parent_run_id, ''), state, current_node, step_index, cycle,
		       COALESCE(dag_state_toml, ''),
		       created_at, updated_at, COALESCE(completed_at, 0), heartbeat_at
		FROM constellation_runs WHERE id = ?`
	var r RunRow
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&r.ID, &r.RepoPath, &r.NebulaID, &r.ConstellationName, &r.Snapshot,
		&r.ParentRunID, &r.State, &r.CurrentNode, &r.StepIndex, &r.Cycle, &r.DAGStateTOML,
		&r.CreatedAt, &r.UpdatedAt, &r.CompletedAt, &r.HeartbeatAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fabric: get constellation run %q: %w", id, err)
	}
	return &r, nil
}

// SaveProgress persists a transition: the new state, current node, step index,
// cycle, and serialized DAG state. It bumps updated_at and the heartbeat so the
// reaper sees the run as alive.
func (s *ConstellationRunStore) SaveProgress(ctx context.Context, r *RunRow) error {
	now := time.Now().Unix()
	const q = `
		UPDATE constellation_runs
		SET state = ?, current_node = ?, step_index = ?, cycle = ?, dag_state_toml = ?,
		    updated_at = ?, heartbeat_at = ?
		WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q,
		r.State, r.CurrentNode, r.StepIndex, r.Cycle, r.DAGStateTOML, now, now, r.ID,
	)
	if err != nil {
		return fmt.Errorf("fabric: save run progress %q: %w", r.ID, err)
	}
	return notFoundIfZeroRun(res, r.ID)
}

// Complete marks a run terminal with the given state and stamps completed_at.
func (s *ConstellationRunStore) Complete(ctx context.Context, id, state string) error {
	now := time.Now().Unix()
	const q = `UPDATE constellation_runs SET state = ?, completed_at = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, state, now, now, id)
	if err != nil {
		return fmt.Errorf("fabric: complete run %q: %w", id, err)
	}
	return notFoundIfZeroRun(res, id)
}

// Heartbeat refreshes a running run's liveness timestamp.
func (s *ConstellationRunStore) Heartbeat(ctx context.Context, id string) error {
	const q = `UPDATE constellation_runs SET heartbeat_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("fabric: heartbeat run %q: %w", id, err)
	}
	return notFoundIfZeroRun(res, id)
}

// ListByState returns runs in the given state, oldest first.
func (s *ConstellationRunStore) ListByState(ctx context.Context, state string) ([]*RunRow, error) {
	const q = `
		SELECT id, repo_path, nebula_id, constellation_name,
		       COALESCE(parent_run_id, ''), state, current_node, step_index, cycle,
		       COALESCE(dag_state_toml, ''),
		       created_at, updated_at, COALESCE(completed_at, 0), heartbeat_at
		FROM constellation_runs WHERE state = ? ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, state)
	if err != nil {
		return nil, fmt.Errorf("fabric: list runs by state %q: %w", state, err)
	}
	defer rows.Close()

	var out []*RunRow
	for rows.Next() {
		var r RunRow
		if err := rows.Scan(
			&r.ID, &r.RepoPath, &r.NebulaID, &r.ConstellationName,
			&r.ParentRunID, &r.State, &r.CurrentNode, &r.StepIndex, &r.Cycle, &r.DAGStateTOML,
			&r.CreatedAt, &r.UpdatedAt, &r.CompletedAt, &r.HeartbeatAt,
		); err != nil {
			return nil, fmt.Errorf("fabric: scan run: %w", err)
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// ReapStale marks running runs whose heartbeat is older than cutoff (unix
// seconds) as 'crashed'. Returns the number reaped. The supervisor calls this
// at boot to recover from a hard crash.
func (s *ConstellationRunStore) ReapStale(ctx context.Context, cutoff int64) (int, error) {
	const q = `UPDATE constellation_runs SET state = 'crashed' WHERE state = 'running' AND heartbeat_at < ?`
	res, err := s.db.ExecContext(ctx, q, cutoff)
	if err != nil {
		return 0, fmt.Errorf("fabric: reap stale runs: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// insertStarInvocationSQL inserts one star_invocation row. Shared by the plain
// insert and the budget-charging transaction so the column list stays in sync.
const insertStarInvocationSQL = `
	INSERT INTO star_invocations
		(run_id, seq, node, star_name, state, cycle, cost_usd, duration_ms,
		 rationale_blob_hash, rationale_preview, parsed_result_toml, started_at, ended_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// execer is the subset of *sql.DB and *sql.Tx used to write a star invocation,
// so the insert runs either standalone or inside a budget-charging transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// insertStarInvocation executes the shared insert against ex and returns the
// new row ID.
func insertStarInvocation(ctx context.Context, ex execer, inv StarInvocationRow) (int64, error) {
	res, err := ex.ExecContext(ctx, insertStarInvocationSQL,
		inv.RunID, inv.Seq, inv.Node, inv.StarName, inv.State, inv.Cycle, inv.CostUSD, inv.DurationMs,
		nullIfEmpty(inv.RationaleBlob), inv.RationalePreview, inv.ParsedResultTOML,
		inv.StartedAt, nullIfZero(inv.EndedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("fabric: insert star invocation (run %q): %w", inv.RunID, err)
	}
	return res.LastInsertId()
}

// InsertStarInvocation records a star/builtin node firing. Returns the row ID.
// It does not touch budget; use RecordInvocationCost to charge a run's budget
// atomically with the insert.
func (s *ConstellationRunStore) InsertStarInvocation(ctx context.Context, inv StarInvocationRow) (int64, error) {
	return insertStarInvocation(ctx, s.db, inv)
}

// InitializeBudget seeds a run's budget cap. Both the initial and remaining
// columns are set to amount so remaining decrements from the cap as invocations
// charge it. Callers pass a positive amount; an uncapped run leaves these
// columns NULL (the InsertRun default) and is never initialized.
func (s *ConstellationRunStore) InitializeBudget(ctx context.Context, runID string, amount float64) error {
	const q = `UPDATE constellation_runs SET budget_usd_initial = ?, budget_usd_remaining = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, amount, amount, runID)
	if err != nil {
		return fmt.Errorf("fabric: initialize budget %q: %w", runID, err)
	}
	return notFoundIfZeroRun(res, runID)
}

// RunBudget reports a run's budget cap. capSet is false when budget_usd_initial
// IS NULL (no-cap mode); initial and remaining are zero in that case.
func (s *ConstellationRunStore) RunBudget(ctx context.Context, runID string) (capSet bool, initial, remaining float64, err error) {
	const q = `SELECT budget_usd_initial, budget_usd_remaining FROM constellation_runs WHERE id = ?`
	var ini, rem sql.NullFloat64
	err = s.db.QueryRowContext(ctx, q, runID).Scan(&ini, &rem)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, 0, ErrRunNotFound
	}
	if err != nil {
		return false, 0, 0, fmt.Errorf("fabric: read budget %q: %w", runID, err)
	}
	return ini.Valid, ini.Float64, rem.Float64, nil
}

// RecordInvocationCost inserts a star_invocation row and, when the run carries a
// budget cap, decrements budget_usd_remaining by the row's cost in the same
// transaction — so a crash between the two can neither double-charge nor skip.
// The first decrement that drives remaining to <= 0 stamps budget_exhausted_at.
// An uncapped run (budget_usd_initial IS NULL) only gets the invocation row; the
// cost is still recorded on that row for telemetry. Returns the new row ID.
func (s *ConstellationRunStore) RecordInvocationCost(ctx context.Context, inv StarInvocationRow) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("fabric: begin invocation tx (run %q): %w", inv.RunID, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	id, err := insertStarInvocation(ctx, tx, inv)
	if err != nil {
		return 0, err
	}

	const decrement = `
		UPDATE constellation_runs
		SET budget_usd_remaining = budget_usd_remaining - ?,
		    budget_exhausted_at = CASE
		        WHEN budget_exhausted_at IS NULL AND (budget_usd_remaining - ?) <= 0
		        THEN ? ELSE budget_exhausted_at END
		WHERE id = ? AND budget_usd_initial IS NOT NULL`
	if _, err := tx.ExecContext(ctx, decrement, inv.CostUSD, inv.CostUSD, time.Now().Unix(), inv.RunID); err != nil {
		return 0, fmt.Errorf("fabric: charge budget (run %q): %w", inv.RunID, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("fabric: commit invocation tx (run %q): %w", inv.RunID, err)
	}
	return id, nil
}

// NodeCost is one node's aggregated star-invocation spend within a run, used to
// build a budget-exhaustion failure's top-costs breakdown.
type NodeCost struct {
	Node        string
	Invocations int
	CostUSD     float64
}

// CostBreakdown returns per-node invocation counts and summed cost for a run,
// most expensive first.
func (s *ConstellationRunStore) CostBreakdown(ctx context.Context, runID string) ([]NodeCost, error) {
	const q = `
		SELECT node, COUNT(*), COALESCE(SUM(cost_usd), 0)
		FROM star_invocations WHERE run_id = ?
		GROUP BY node ORDER BY SUM(cost_usd) DESC, node`
	rows, err := s.db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, fmt.Errorf("fabric: cost breakdown %q: %w", runID, err)
	}
	defer rows.Close()

	var out []NodeCost
	for rows.Next() {
		var nc NodeCost
		if err := rows.Scan(&nc.Node, &nc.Invocations, &nc.CostUSD); err != nil {
			return nil, fmt.Errorf("fabric: scan node cost: %w", err)
		}
		out = append(out, nc)
	}
	return out, rows.Err()
}

// InvocationsForRun returns every star/builtin invocation recorded for a run,
// ordered by seq, so the cockpit run-detail view can render the step trace.
func (s *ConstellationRunStore) InvocationsForRun(ctx context.Context, runID string) ([]*StarInvocationRow, error) {
	const q = `
		SELECT run_id, seq, node, star_name, state, cycle, cost_usd, duration_ms,
		       COALESCE(rationale_blob_hash, ''), rationale_preview, parsed_result_toml,
		       started_at, COALESCE(ended_at, 0)
		FROM star_invocations WHERE run_id = ? ORDER BY seq`
	rows, err := s.db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, fmt.Errorf("fabric: invocations for run %q: %w", runID, err)
	}
	defer rows.Close()

	var out []*StarInvocationRow
	for rows.Next() {
		var inv StarInvocationRow
		if err := rows.Scan(
			&inv.RunID, &inv.Seq, &inv.Node, &inv.StarName, &inv.State, &inv.Cycle,
			&inv.CostUSD, &inv.DurationMs, &inv.RationaleBlob, &inv.RationalePreview,
			&inv.ParsedResultTOML, &inv.StartedAt, &inv.EndedAt,
		); err != nil {
			return nil, fmt.Errorf("fabric: scan star invocation: %w", err)
		}
		out = append(out, &inv)
	}
	return out, rows.Err()
}

// nullIfEmpty maps "" to a SQL NULL so REFERENCES/foreign keys stay nullable.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullIfZero maps 0 to a SQL NULL for optional timestamp columns.
func nullIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// notFoundIfZeroRun returns ErrRunNotFound when an update affected no rows.
func notFoundIfZeroRun(res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("fabric: rows affected for run %q: %w", id, err)
	}
	if n == 0 {
		return ErrRunNotFound
	}
	return nil
}
