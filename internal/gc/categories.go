package gc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Category names. These match the keys under gc.ttls in .quasar.yaml and the
// --category flag of `quasar gc run`.
const (
	CategoryCompletedNebulas     = "completed_nebulas"
	CategoryFailedNebulas        = "failed_nebulas"
	CategoryConstellationRuns    = "constellation_runs"
	CategorySensorEvents         = "sensor_events"
	CategoryTriggerQueueConsumed = "trigger_queue_consumed"
	CategoryBlobs                = "blobs"
)

// Terminal status sets. The nebula lifecycle has no dedicated completed_at
// column, so age is measured from updated_at, which is bumped on every status
// transition — including the move into a terminal status.
var (
	completedNebulaStatuses = []string{"completed", "shipped", "merged", "done"}
	failedNebulaStatuses    = []string{"failed", "killed", "crashed", "abandoned"}
	terminalRunStates       = []string{"done", "failed", "killed", "crashed"}
	// nonTerminalRunStates guards a nebula from GC while a run still touches it
	// and scopes the per-repo "skip if running" rule for the run sweep.
	nonTerminalRunStates = []string{"running", "paused", "blocked_on_review"}
)

// CategoryResult summarizes one category's mark+sweep within a single pass.
type CategoryResult struct {
	Category         string
	Marked           int
	Swept            int
	CascadedChildren int
	Err              error
}

// sweepNebulas marks terminal nebulas of the given status set whose updated_at
// is older than ttl, then hard-deletes those soft-deleted longer than the grace
// window (cascading their phases). A nebula with a non-terminal constellation
// run is never marked, so the GC cannot race the runtime.
func sweepNebulas(ctx context.Context, db *sql.DB, audit *AuditLog, now time.Time, category string, statuses []string, ttl, grace time.Duration, dryRun bool) CategoryResult {
	res := CategoryResult{Category: category}
	nowUnix := now.Unix()
	cutoff := now.Add(-ttl).Unix()

	statusIn, statusArgs := inClause(statuses)
	markSelect := fmt.Sprintf(
		`SELECT id FROM nebulas
		   WHERE deleted_at IS NULL AND status IN (%s) AND updated_at < ?
		     AND id NOT IN (SELECT nebula_id FROM constellation_runs WHERE state IN (%s))`,
		statusIn, placeholders(len(nonTerminalRunStates)))
	markArgs := append(append([]any{}, statusArgs...), cutoff)
	markArgs = append(markArgs, toAnySlice(nonTerminalRunStates)...)

	ids, err := selectIDs(ctx, db, markSelect, markArgs...)
	if err != nil {
		res.Err = err
		return res
	}
	for _, id := range ids {
		if !dryRun {
			if _, err := db.ExecContext(ctx, "UPDATE nebulas SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL", nowUnix, id); err != nil {
				res.Err = fmt.Errorf("gc: mark nebula %s: %w", id, err)
				return res
			}
		}
		res.Marked++
		_ = audit.Append(AuditEntry{Category: category, Action: ActionMark, NebulaID: id, Reason: "ttl_expired", DryRun: dryRun})
	}

	// Sweep phase: hard-delete nebulas whose grace window has elapsed.
	graceCutoff := now.Add(-grace).Unix()
	sweepIDs, err := selectIDs(ctx, db,
		"SELECT id FROM nebulas WHERE deleted_at IS NOT NULL AND deleted_at < ?", graceCutoff)
	if err != nil {
		res.Err = err
		return res
	}
	for _, id := range sweepIDs {
		phases, err := childCount(ctx, db, "phases", "nebula_id", id)
		if err != nil {
			res.Err = err
			return res
		}
		if !dryRun {
			if err := deleteWithChildren(ctx, db, "nebulas", "phases", "nebula_id", id); err != nil {
				res.Err = err
				return res
			}
		}
		res.Swept++
		res.CascadedChildren += phases
		_ = audit.Append(AuditEntry{Category: category, Action: ActionSweep, NebulaID: id, CascadedPhases: phases, DryRun: dryRun})
	}
	return res
}

// sweepRuns marks terminal constellation runs older than ttl and hard-deletes
// those past grace (cascading star_invocations). Runs belonging to a repo that
// currently has any non-terminal run are skipped entirely, honoring the
// per-repo "never GC while that repo's runtime is busy" rule.
func sweepRuns(ctx context.Context, db *sql.DB, audit *AuditLog, now time.Time, ttl, grace time.Duration, dryRun bool) CategoryResult {
	res := CategoryResult{Category: CategoryConstellationRuns}
	nowUnix := now.Unix()
	cutoff := now.Add(-ttl).Unix()

	stateIn, stateArgs := inClause(terminalRunStates)
	busyRepos := fmt.Sprintf("SELECT DISTINCT repo_path FROM constellation_runs WHERE state IN (%s)", placeholders(len(nonTerminalRunStates)))
	markSelect := fmt.Sprintf(
		`SELECT id FROM constellation_runs
		   WHERE deleted_at IS NULL AND state IN (%s)
		     AND COALESCE(completed_at, updated_at) < ?
		     AND repo_path NOT IN (%s)`,
		stateIn, busyRepos)
	markArgs := append(append([]any{}, stateArgs...), cutoff)
	markArgs = append(markArgs, toAnySlice(nonTerminalRunStates)...)

	ids, err := selectIDs(ctx, db, markSelect, markArgs...)
	if err != nil {
		res.Err = err
		return res
	}
	for _, id := range ids {
		if !dryRun {
			if _, err := db.ExecContext(ctx, "UPDATE constellation_runs SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL", nowUnix, id); err != nil {
				res.Err = fmt.Errorf("gc: mark run %s: %w", id, err)
				return res
			}
		}
		res.Marked++
		_ = audit.Append(AuditEntry{Category: CategoryConstellationRuns, Action: ActionMark, RunID: id, Reason: "ttl_expired", DryRun: dryRun})
	}

	graceCutoff := now.Add(-grace).Unix()
	sweepIDs, err := selectIDs(ctx, db,
		"SELECT id FROM constellation_runs WHERE deleted_at IS NOT NULL AND deleted_at < ?", graceCutoff)
	if err != nil {
		res.Err = err
		return res
	}
	for _, id := range sweepIDs {
		invs, err := childCount(ctx, db, "star_invocations", "run_id", id)
		if err != nil {
			res.Err = err
			return res
		}
		if !dryRun {
			if err := deleteWithChildren(ctx, db, "constellation_runs", "star_invocations", "run_id", id); err != nil {
				res.Err = err
				return res
			}
		}
		res.Swept++
		res.CascadedChildren += invs
		_ = audit.Append(AuditEntry{Category: CategoryConstellationRuns, Action: ActionSweep, RunID: id, Count: invs, DryRun: dryRun})
	}
	return res
}

// sweepSensorEvents marks processed sensor events older than ttl and hard-deletes
// those past grace. There are no child rows to cascade.
func sweepSensorEvents(ctx context.Context, db *sql.DB, audit *AuditLog, now time.Time, ttl, grace time.Duration, dryRun bool) CategoryResult {
	res := CategoryResult{Category: CategorySensorEvents}
	nowUnix := now.Unix()
	cutoff := now.Add(-ttl).Unix()

	rows, err := execCount(ctx, db, dryRun,
		`UPDATE sensor_events SET deleted_at = ?
		   WHERE deleted_at IS NULL AND processed_at IS NOT NULL AND received_at < ?`,
		`SELECT COUNT(*) FROM sensor_events
		   WHERE deleted_at IS NULL AND processed_at IS NOT NULL AND received_at < ?`,
		[]any{nowUnix, cutoff}, []any{cutoff})
	if err != nil {
		res.Err = err
		return res
	}
	res.Marked = rows
	if rows > 0 {
		_ = audit.Append(AuditEntry{Category: CategorySensorEvents, Action: ActionMark, Count: rows, Reason: "ttl_expired", DryRun: dryRun})
	}

	graceCutoff := now.Add(-grace).Unix()
	swept, err := execCount(ctx, db, dryRun,
		"DELETE FROM sensor_events WHERE deleted_at IS NOT NULL AND deleted_at < ?",
		"SELECT COUNT(*) FROM sensor_events WHERE deleted_at IS NOT NULL AND deleted_at < ?",
		[]any{graceCutoff}, []any{graceCutoff})
	if err != nil {
		res.Err = err
		return res
	}
	res.Swept = swept
	if swept > 0 {
		_ = audit.Append(AuditEntry{Category: CategorySensorEvents, Action: ActionSweep, Count: swept, DryRun: dryRun})
	}
	return res
}

// sweepTriggerQueue hard-deletes consumed trigger rows older than ttl. The
// trigger_queue table has no deleted_at column — a consumed trigger has no
// recovery value — so this is a direct delete with no grace window.
func sweepTriggerQueue(ctx context.Context, db *sql.DB, audit *AuditLog, now time.Time, ttl time.Duration, dryRun bool) CategoryResult {
	res := CategoryResult{Category: CategoryTriggerQueueConsumed}
	cutoff := now.Add(-ttl).Unix()
	swept, err := execCount(ctx, db, dryRun,
		"DELETE FROM trigger_queue WHERE state = 'consumed' AND consumed_at IS NOT NULL AND consumed_at < ?",
		"SELECT COUNT(*) FROM trigger_queue WHERE state = 'consumed' AND consumed_at IS NOT NULL AND consumed_at < ?",
		[]any{cutoff}, []any{cutoff})
	if err != nil {
		res.Err = err
		return res
	}
	res.Swept = swept
	if swept > 0 {
		_ = audit.Append(AuditEntry{Category: CategoryTriggerQueueConsumed, Action: ActionSweep, Count: swept, Reason: "ttl_expired", DryRun: dryRun})
	}
	return res
}

// --- shared helpers ---

// selectIDs runs a single-column id query and returns the string ids.
func selectIDs(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("gc: select ids: %w", err)
	}
	defer rows.Close() //nolint:errcheck // iteration cleanup
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("gc: scan id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// childCount returns the number of child rows referencing parentID via fkColumn.
func childCount(ctx context.Context, db *sql.DB, childTable, fkColumn, parentID string) (int, error) {
	var n int
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ?", childTable, fkColumn)
	if err := db.QueryRowContext(ctx, q, parentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("gc: count children %s.%s: %w", childTable, fkColumn, err)
	}
	return n, nil
}

// deleteWithChildren hard-deletes parentID and its children atomically. The
// store runs with foreign-key enforcement off, so children are deleted
// explicitly rather than relying on ON DELETE CASCADE — this also yields an
// exact cascaded-row count for the audit log.
func deleteWithChildren(ctx context.Context, db *sql.DB, parentTable, childTable, fkColumn, parentID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gc: begin delete tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	childDel := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", childTable, fkColumn)
	if _, err := tx.ExecContext(ctx, childDel, parentID); err != nil {
		return fmt.Errorf("gc: delete children of %s: %w", parentID, err)
	}
	parentDel := fmt.Sprintf("DELETE FROM %s WHERE id = ?", parentTable)
	if _, err := tx.ExecContext(ctx, parentDel, parentID); err != nil {
		return fmt.Errorf("gc: delete %s %s: %w", parentTable, parentID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("gc: commit delete tx: %w", err)
	}
	return nil
}

// execCount runs mutate (unless dryRun) and returns the affected row count. When
// dryRun is true it runs countQuery instead so the report reflects what would
// have changed without mutating anything.
func execCount(ctx context.Context, db *sql.DB, dryRun bool, mutate, countQuery string, mutateArgs, countArgs []any) (int, error) {
	if dryRun {
		var n int
		if err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&n); err != nil {
			return 0, fmt.Errorf("gc: count: %w", err)
		}
		return n, nil
	}
	res, err := db.ExecContext(ctx, mutate, mutateArgs...)
	if err != nil {
		return 0, fmt.Errorf("gc: exec: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("gc: rows affected: %w", err)
	}
	return int(n), nil
}

// inClause builds a "?, ?, …" placeholder list and the matching args for an IN
// expression over string values.
func inClause(values []string) (string, []any) {
	return placeholders(len(values)), toAnySlice(values)
}

// placeholders returns "?, ?, …" with n placeholders (at least one).
func placeholders(n int) string {
	if n <= 0 {
		return "''" // an empty IN () is invalid; '' never matches a real value
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// toAnySlice widens a []string to []any for variadic query args.
func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
