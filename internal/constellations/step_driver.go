package constellations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// defaultStepDriverBatchLimit caps how many running runs a single Tick
// advances per pass. Bounded so a long-running Step on one run never starves
// the others, and so the driver yields control to ctx cancellation
// promptly.
const defaultStepDriverBatchLimit = 8

// Stepper is the trigger-driver's seam onto whatever materializes a
// per-repo Step call. It carries repoPath so multi-repo implementations
// route to the correct per-repo Runtime — single-repo or test
// implementations ignore it. Same shape as Firer (supervisor.go).
//
// *Runtime is bound to a single repo at construction and does NOT satisfy
// this interface directly; use SingleRepoStepper to wrap one, or
// RuntimeCacheStepper to route across many.
type Stepper interface {
	Step(ctx context.Context, repoPath, runID string) (string, error)
}

// SingleRepoStepper adapts a single *Runtime to the Stepper interface,
// ignoring repoPath. Useful in tests and single-repo deployments.
type SingleRepoStepper struct {
	Runtime *Runtime
}

// Step ignores repoPath and dispatches to the wrapped Runtime.
func (s *SingleRepoStepper) Step(ctx context.Context, _, runID string) (string, error) {
	return s.Runtime.Step(ctx, runID)
}

// RuntimeCacheStepper satisfies Stepper by routing each call through the
// cache — the production path for fleet's step driver. Mirrors
// RuntimeCacheFirer.
type RuntimeCacheStepper struct {
	Cache *RuntimeCache
}

// Step resolves the per-repo Runtime and dispatches the Step call.
func (s *RuntimeCacheStepper) Step(ctx context.Context, repoPath, runID string) (string, error) {
	rt, err := s.Cache.Get(ctx, repoPath)
	if err != nil {
		return "", fmt.Errorf("stepper: get runtime for %q: %w", repoPath, err)
	}
	return rt.Step(ctx, runID)
}

// StepDriver advances every constellation_run row in state='running' by one
// Step per Tick. Without it, runs fired by the Supervisor stall at their
// entry node — Fire creates the row, Step advances it, and nothing else
// called Step from production code before this driver shipped.
//
// Concurrency model: SQLite's row-level serialization plus the Step
// implementation's atomic transition makes calling Step on the same run
// twice idempotent (the second call sees a terminal state and returns
// ErrTerminal). The driver claims a batch of running rows per Tick and
// dispatches them serially — parallel Step across runs is correct but the
// added supervision surface isn't needed for v1.
type StepDriver struct {
	// DB is the fabric database. Required.
	DB *sql.DB
	// Stepper routes Step calls. Required. Production uses
	// RuntimeCacheStepper; tests use SingleRepoStepper or a fake.
	Stepper Stepper
	// Logger receives per-tick and per-step diagnostics. Nil writes to
	// stderr; the fleet uses .quasar/supervisor.log so stderr does not
	// corrupt the Bubble Tea altscreen.
	Logger io.Writer
	// BatchLimit caps running rows advanced per Tick. Zero defaults to 8.
	BatchLimit int
}

// Tick selects up to BatchLimit running runs (oldest heartbeat first so a
// stalled run doesn't starve others) and calls Step on each. Returns the
// number of runs advanced. A run that reports terminal during Step is
// counted (the transition is the advance). Per-run errors are logged and
// the loop continues — the next Tick retries non-terminal failures, which
// is appropriate because most failure modes are transient (network blip,
// LLM rate limit).
func (d *StepDriver) Tick(ctx context.Context) (int, error) {
	rows, err := d.selectRunning(ctx)
	if err != nil {
		return 0, err
	}
	advanced := 0
	for _, r := range rows {
		if ctx.Err() != nil {
			return advanced, ctx.Err()
		}
		// Step's return value is the NEW state. We don't act on it directly;
		// the run row's state column reflects it on the next select.
		if _, err := d.Stepper.Step(ctx, r.RepoPath, r.ID); err != nil {
			if errors.Is(err, ErrTerminal) {
				// Already terminal — another tick or external action raced
				// us. Not an error, just a no-op.
				continue
			}
			d.logf("run %s (repo %q): step: %v", r.ID, r.RepoPath, err)
			continue
		}
		advanced++
	}
	return advanced, nil
}

// Run drives Tick on every interval until ctx is canceled. Per-tick errors
// are logged and the loop continues.
func (d *StepDriver) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := d.Tick(ctx); err != nil {
				d.logf("tick: %v", err)
			}
		}
	}
}

// runningRunRow is the projection the driver needs per claim: just the ID +
// repo_path for routing. The runtime reads the full row inside Step.
type runningRunRow struct {
	ID       string
	RepoPath string
}

// selectRunning returns up to BatchLimit running rows, oldest heartbeat
// first. Running rows whose heartbeat has gone cold are picked up before
// fresher ones so a stuck run doesn't perpetually defer its Step.
func (d *StepDriver) selectRunning(ctx context.Context) ([]runningRunRow, error) {
	limit := d.BatchLimit
	if limit <= 0 {
		limit = defaultStepDriverBatchLimit
	}
	// parent_run_id IS NULL restricts the driver to top-level runs. A child run
	// is owned and driven to terminal synchronously by its parent's Step
	// (dispatchConstellation); claiming one here would let the driver re-step a
	// node whose side effect already ran — e.g. a child left 'running' after a
	// dispatch error, or any second driver/process racing the parent.
	const q = `
		SELECT id, COALESCE(repo_path, '')
		  FROM constellation_runs
		 WHERE state = 'running'
		   AND deleted_at IS NULL
		   AND parent_run_id IS NULL
		 ORDER BY heartbeat_at ASC
		 LIMIT ?`
	rows, err := d.DB.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("step driver: select running: %w", err)
	}
	defer rows.Close()

	var out []runningRunRow
	for rows.Next() {
		var r runningRunRow
		if err := rows.Scan(&r.ID, &r.RepoPath); err != nil {
			return nil, fmt.Errorf("step driver: scan run row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// logf writes a diagnostic to the configured logger, defaulting to stderr.
func (d *StepDriver) logf(format string, args ...any) {
	w := d.Logger
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "constellations.StepDriver: "+format+"\n", args...)
}
