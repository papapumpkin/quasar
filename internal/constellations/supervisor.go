package constellations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// defaultSupervisorBatchLimit caps how many pending trigger_queue rows a
// single Tick claims. Bounded so a backlog cannot starve other Tick-time
// work and so per-fire failures don't compound into a long tail.
const defaultSupervisorBatchLimit = 8

// Firer is the trigger-consumer's seam onto whatever materializes a
// constellation run for the supervisor. It carries the trigger row's
// repoPath so multi-repo implementations can route to the correct per-repo
// Runtime — single-repo or test implementations ignore it.
//
// *Runtime is bound to a single repo at construction and does NOT satisfy
// this interface directly; use SingleRepoFirer to wrap one, or
// RuntimeCacheFirer (constellations/runtime_cache.go) to route across many.
type Firer interface {
	Fire(ctx context.Context, repoPath, constellationName, nebulaID string) (string, error)
}

// SingleRepoFirer adapts a single *Runtime to the Firer interface, ignoring
// the supervisor's repoPath argument. Useful in tests and in single-repo
// deployments where every trigger row targets the same Runtime.
type SingleRepoFirer struct {
	Runtime *Runtime
}

// Fire ignores the supervisor's repoPath and dispatches to the wrapped
// Runtime. The empty parent_run_id and zero budgetOverride are correct: a
// trigger row launches a top-level run, and budget resolution falls back to
// the nebula manifest (or the runtime's DefaultBudgetUSD).
func (s *SingleRepoFirer) Fire(ctx context.Context, _, constellationName, nebulaID string) (string, error) {
	return s.Runtime.Fire(ctx, constellationName, nebulaID, "", 0)
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
		// operator can re-approve from the fleet view. RepoPath is
		// forwarded so multi-repo implementations route to the per-repo
		// Runtime; single-repo implementations ignore it.
		if _, err := s.Firer.Fire(ctx, t.RepoPath, t.ConstellationName, t.NebulaID); err != nil {
			s.logf("trigger %d: fire %q on nebula %q (repo %q): %v",
				t.ID, t.ConstellationName, t.NebulaID, t.RepoPath, err)
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

// heartbeatInterval is how often an in-flight Step refreshes its run's liveness
// timestamp. It must stay comfortably below any crash reaper's stale cutoff so
// a healthy long step is never mistaken for a dead process.
const heartbeatInterval = 30 * time.Second

// startHeartbeat refreshes runID's liveness timestamp every `interval` until
// the returned stop func is called. A star invocation can block for minutes and
// a nested constellation for an entire child walk; without a periodic refresh,
// a heartbeat-based crash reaper would mistake a healthy long-running step for a
// dead process. Heartbeat write failures are non-fatal and logged. Call stop
// exactly once (Step does so right after dispatch returns). interval is a
// parameter so tests can drive it without mutating shared state.
func (r *Runtime) startHeartbeat(ctx context.Context, runID string, interval time.Duration) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if err := r.runStore.Heartbeat(ctx, runID); err != nil {
					fmt.Fprintf(os.Stderr, "constellations: heartbeat run %s: %v\n", runID, err)
				}
			}
		}
	}()
	return func() { close(done) }
}

// maxStepAttempts bounds how many times the runtime will (re)enter a single
// node without it completing before failing the run. A completed node resets
// the counter (SaveProgress), so this only trips for a node that repeatedly
// crashes the process mid-dispatch and is auto-resumed on restart — a poison
// node. Five tolerates a few transient hard restarts before giving up.
const maxStepAttempts = 5

var (
	// ErrMaxStepAttempts is the failure cause when a node is re-entered more
	// than maxStepAttempts times without completing (a crash-looping node).
	ErrMaxStepAttempts = errors.New("constellations: node exceeded max step attempts")
	// ErrNodePanic wraps a panic recovered from a node dispatch so it fails the
	// run instead of crashing the fleet process.
	ErrNodePanic = errors.New("constellations: node panicked")
)

// guardAttempt bumps the run's poison-node counter before dispatch and fails
// the run when it exceeds maxStepAttempts. The bool reports whether the guard
// handled the run: it returns (StateFailed, true, cause) when it failed the run
// — the caller returns that pair, mirroring the normal fail path — or
// ("", false, nil) to proceed, or ("", false, err) on a store error.
func (r *Runtime) guardAttempt(ctx context.Context, run *fabric.RunRow, st *State, node *artifacts.ConstellationNode) (string, bool, error) {
	attempts, err := r.runStore.BumpStepAttempt(ctx, run.ID)
	if err != nil {
		return "", false, err
	}
	if attempts > maxStepAttempts {
		state, cause := r.fail(ctx, run, st,
			fmt.Errorf("%w: node %q reached %d attempts", ErrMaxStepAttempts, node.ID, attempts))
		return state, true, cause
	}
	return "", false, nil
}

// dispatchSafely runs dispatch with panic recovery so a panicking node (a bug
// in an operator/builtin, or malformed state) fails its run instead of crashing
// the fleet process — which would re-step and re-panic on every restart. The
// recovered panic becomes an ordinary node error routed to the run's failure
// path.
func (r *Runtime) dispatchSafely(ctx context.Context, run *fabric.RunRow, st *State, node *artifacts.ConstellationNode) (out map[string]any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%w: node %q: %v", ErrNodePanic, node.ID, rec)
		}
	}()
	return r.dispatch(ctx, run, st, node)
}

// Resume restores a run interrupted mid-flight. The DAG state and current node
// already live in the row, so resume is a heartbeat refresh that re-asserts the
// run as live; the supervisor then drives Step from the persisted node.
func (r *Runtime) Resume(ctx context.Context, runID string) error {
	run, err := r.runStore.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if isTerminalState(run.State) {
		return ErrTerminal
	}
	if _, err := UnmarshalState(run.DAGStateTOML); err != nil {
		return fmt.Errorf("constellations: resume %q: corrupt dag state: %w", runID, err)
	}
	return r.runStore.Heartbeat(ctx, runID)
}
