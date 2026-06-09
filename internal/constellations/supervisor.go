package constellations

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"
)

// defaultSupervisorBatchLimit caps how many pending trigger_queue rows a
// single Tick claims. Bounded so a backlog cannot starve other Tick-time
// work and so per-fire failures don't compound into a long tail.
const defaultSupervisorBatchLimit = 8

// Firer is the subset of Runtime the trigger consumer needs. Defined here
// (where consumed) per project convention so the Supervisor is testable
// without a fully-constructed Runtime; *Runtime satisfies it.
type Firer interface {
	Fire(ctx context.Context, constellationName, nebulaID, parentRunID string, budgetOverride float64) (string, error)
}

// Supervisor drains trigger_queue rows by firing a constellation run for each
// pending entry. Without this consumer the fleet view's Approve action — and
// every sensor that enqueues a trigger — is a silent no-op: rows pile up in
// 'pending' until the GC sweeps them, but no architect run is ever started.
//
// Supervisor is intentionally minimal: it claims pending rows, fires the
// named constellation against the named nebula, and marks rows consumed.
// Higher-level concerns (per-repo Runtime caches, multi-repo routing,
// concurrent supervisor coordination) live in callers.
type Supervisor struct {
	// DB is the fabric database the trigger_queue lives in. Required.
	DB *sql.DB
	// Firer is the constellation engine to invoke for each pending trigger.
	// Required. In production this is a *Runtime; tests pass a fake.
	Firer Firer
	// Logger receives per-trigger and per-tick diagnostics. Nil writes to
	// stderr (matching the rest of the project's non-fatal-error
	// convention).
	Logger io.Writer
	// BatchLimit caps pending rows claimed per Tick. Zero defaults to 8.
	BatchLimit int
}

// Tick claims pending trigger_queue rows (up to BatchLimit) and fires the
// referenced constellation for each. Returns the number of rows successfully
// fired. Rows whose Fire fails are still marked consumed — the alternative
// (leaving them pending) would retry indefinitely against the same failure;
// the operator instead sees a log line and can re-approve once the cause is
// fixed.
//
// Concurrency: a row is claimed via an UPDATE...WHERE state='pending'
// guarded by the row's primary key, so a concurrent Tick (across processes,
// or across goroutines on a single DB connection pool) cannot double-fire a
// single trigger. The claim must precede Fire so a crash mid-Fire is
// detectable (consumed-without-run) rather than leaking double runs on
// restart.
func (s *Supervisor) Tick(ctx context.Context) (int, error) {
	pending, err := s.selectPending(ctx)
	if err != nil {
		return 0, err
	}
	fired := 0
	for _, t := range pending {
		claimed, err := s.claim(ctx, t.ID)
		if err != nil {
			s.logf("trigger %d: claim failed: %v", t.ID, err)
			continue
		}
		if !claimed {
			// Another supervisor (or the GC) raced us.
			continue
		}
		// Fire after the claim so a crash between claim and Fire is
		// auditable (the row is consumed but the run isn't there). The
		// operator can re-approve from the fleet view.
		if _, err := s.Firer.Fire(ctx, t.ConstellationName, t.NebulaID, "", 0); err != nil {
			s.logf("trigger %d: fire %q on nebula %q: %v",
				t.ID, t.ConstellationName, t.NebulaID, err)
			continue
		}
		fired++
	}
	return fired, nil
}

// Run drives Tick on every interval until ctx is canceled. Per-tick errors
// are logged and the loop continues; only ctx cancellation exits. Suitable
// to run in a goroutine for the lifetime of a supervisor process.
func (s *Supervisor) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.Tick(ctx); err != nil {
				s.logf("tick: %v", err)
			}
		}
	}
}

// triggerRow is one pending trigger_queue row.
type triggerRow struct {
	ID                int64
	NebulaID          string
	ConstellationName string
	RepoPath          string
}

// selectPending reads up to BatchLimit pending rows oldest-first so a backlog
// drains in approval order.
func (s *Supervisor) selectPending(ctx context.Context) ([]triggerRow, error) {
	limit := s.BatchLimit
	if limit <= 0 {
		limit = defaultSupervisorBatchLimit
	}
	const q = `
		SELECT id, nebula_id, constellation_name, COALESCE(repo_path, '')
		  FROM trigger_queue
		 WHERE state = 'pending'
		 ORDER BY created_at
		 LIMIT ?`
	rows, err := s.DB.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("supervisor: select pending: %w", err)
	}
	defer rows.Close()

	var out []triggerRow
	for rows.Next() {
		var t triggerRow
		if err := rows.Scan(&t.ID, &t.NebulaID, &t.ConstellationName, &t.RepoPath); err != nil {
			return nil, fmt.Errorf("supervisor: scan trigger row: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// claim transitions a single row from pending to consumed atomically. Returns
// true if this caller won the row, false if a concurrent worker did.
func (s *Supervisor) claim(ctx context.Context, id int64) (bool, error) {
	const q = `UPDATE trigger_queue SET state = 'consumed' WHERE id = ? AND state = 'pending'`
	res, err := s.DB.ExecContext(ctx, q, id)
	if err != nil {
		return false, fmt.Errorf("supervisor: claim %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("supervisor: rows affected for %d: %w", id, err)
	}
	return n > 0, nil
}

// logf writes a diagnostic to the configured logger, defaulting to stderr.
func (s *Supervisor) logf(format string, args ...any) {
	w := s.Logger
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "constellations.Supervisor: "+format+"\n", args...)
}
