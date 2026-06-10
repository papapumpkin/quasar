package gc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/gitops"
)

// WorktreeReaper removes stale Quasar worktrees for a repo. The interface is
// declared here, where it is consumed; gitops.WorktreeReaper satisfies it.
type WorktreeReaper interface {
	Reap(ctx context.Context, repoPath string, maxAge time.Duration, now time.Time, isProtected func(name string) bool, dryRun bool) (*gitops.ReapReport, error)
}

// Opts configures a new Engine. DB and Config are required; the rest default to
// no-op behavior (nil blobs disables blob sweep, nil reaper disables worktree
// reaping, nil audit drops audit lines, nil clock uses time.Now).
type Opts struct {
	DB             *sql.DB
	Config         config.GCConfig
	Blobs          *blobstore.Store
	Reaper         WorktreeReaper
	Audit          *AuditLog
	Clock          func() time.Time
	Logger         io.Writer
	WorktreeMaxAge time.Duration
}

// Engine is the garbage collector. It owns the only code path that hard-deletes
// lifecycle rows and unreferenced blobs. Sweeps run sequentially per tick; the
// blob mark-and-sweep runs on its own slower schedule inside Run.
type Engine struct {
	db             *sql.DB
	cfg            config.GCConfig
	blobs          *blobstore.Store
	reaper         WorktreeReaper
	audit          *AuditLog
	clock          func() time.Time
	logger         io.Writer
	worktreeMaxAge time.Duration
}

// New constructs an Engine.
func New(opts Opts) (*Engine, error) {
	if opts.DB == nil {
		return nil, errors.New("gc: nil database")
	}
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	maxAge := opts.WorktreeMaxAge
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	e := &Engine{
		db:             opts.DB,
		cfg:            opts.Config,
		blobs:          opts.Blobs,
		reaper:         opts.Reaper,
		audit:          opts.Audit,
		clock:          clock,
		logger:         opts.Logger,
		worktreeMaxAge: maxAge,
	}
	// Route dropped-audit-append diagnostics through the engine's logger so a
	// silently-failing audit trail becomes visible. No-op when audit is nil.
	e.audit.SetLogf(e.logf)
	return e, nil
}

// RunOnceOpts parameterizes a single GC pass.
type RunOnceOpts struct {
	// DryRun reports what would be deleted without mutating anything.
	DryRun bool
	// Category, when non-empty, restricts the pass to a single category
	// (one of the Category* constants, including "blobs").
	Category string
	// ReapWorktrees runs the worktree reaper across registered repos.
	ReapWorktrees bool
}

// Report is the outcome of a RunOnce pass.
type Report struct {
	Categories []CategoryResult
	Blobs      *blobstore.SweepReport
	Worktrees  []WorktreeResult
	DryRun     bool
}

// WorktreeResult records one repo's worktree reap.
type WorktreeResult struct {
	RepoPath       string
	Removed        []string
	ReclaimedBytes int64
}

// Run starts the GC loop, blocking until ctx is canceled. It ticks the row
// sweep at cfg.TickInterval and the blob sweep at cfg.Blobs.SweepInterval. When
// GC is disabled it returns nil immediately.
func (e *Engine) Run(ctx context.Context) error {
	if !e.cfg.Enabled {
		e.logf("gc: disabled; sweeper not started")
		return nil
	}
	rowTick := time.NewTicker(e.cfg.TickInterval)
	defer rowTick.Stop()
	blobTick := time.NewTicker(e.cfg.Blobs.SweepInterval)
	defer blobTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-rowTick.C:
			if _, err := e.sweepCategories(ctx, RunOnceOpts{ReapWorktrees: true}); err != nil {
				e.logf("gc: row sweep error: %v", err)
			}
		case <-blobTick.C:
			if _, err := e.sweepBlobs(ctx, false); err != nil {
				e.logf("gc: blob sweep error: %v", err)
			}
		}
	}
}

// RunOnce performs one full pass (or one category when opts.Category is set) and
// returns a report. Used by `quasar gc run` and `quasar gc blobs`.
func (e *Engine) RunOnce(ctx context.Context, opts RunOnceOpts) (*Report, error) {
	report := &Report{DryRun: opts.DryRun}

	if opts.Category == CategoryBlobs {
		blobReport, err := e.sweepBlobs(ctx, opts.DryRun)
		report.Blobs = blobReport
		return report, err
	}

	cats, err := e.sweepCategories(ctx, opts)
	report.Categories = cats
	if err != nil {
		return report, err
	}

	// A full pass (no specific category) also sweeps blobs and reaps worktrees.
	if opts.Category == "" {
		blobReport, err := e.sweepBlobs(ctx, opts.DryRun)
		report.Blobs = blobReport
		if err != nil {
			return report, err
		}
		if opts.ReapWorktrees {
			wt, err := e.reapWorktrees(ctx, opts.DryRun)
			report.Worktrees = wt
			if err != nil {
				return report, err
			}
		}
	}
	return report, nil
}

// sweepCategories runs the enabled row categories sequentially, recording each
// in gc_runs. When opts.Category is set, only that category runs.
func (e *Engine) sweepCategories(ctx context.Context, opts RunOnceOpts) ([]CategoryResult, error) {
	now := e.clock()
	g := e.cfg.GraceWindow
	t := e.cfg.TTLs

	all := map[string]func() CategoryResult{
		CategoryCompletedNebulas: func() CategoryResult {
			return sweepNebulas(ctx, e.db, e.audit, now, CategoryCompletedNebulas, completedNebulaStatuses, t.CompletedNebulas, g, opts.DryRun)
		},
		CategoryFailedNebulas: func() CategoryResult {
			return sweepNebulas(ctx, e.db, e.audit, now, CategoryFailedNebulas, failedNebulaStatuses, t.FailedNebulas, g, opts.DryRun)
		},
		CategoryConstellationRuns: func() CategoryResult {
			return sweepRuns(ctx, e.db, e.audit, now, t.ConstellationRuns, g, opts.DryRun)
		},
		CategorySensorEvents: func() CategoryResult {
			return sweepSensorEvents(ctx, e.db, e.audit, now, t.SensorEvents, g, opts.DryRun)
		},
		CategoryTriggerQueueConsumed: func() CategoryResult {
			return sweepTriggerQueue(ctx, e.db, e.audit, now, t.TriggerQueueConsumed, opts.DryRun)
		},
	}

	order := []string{
		CategoryCompletedNebulas, CategoryFailedNebulas,
		CategoryConstellationRuns, CategorySensorEvents, CategoryTriggerQueueConsumed,
	}

	var results []CategoryResult
	for _, name := range order {
		if opts.Category != "" && opts.Category != name {
			continue
		}
		started := e.clock()
		res := all[name]()
		results = append(results, res)
		if !opts.DryRun {
			e.recordRun(ctx, started, name, res.Swept, 0, res.Err)
		}
		if res.Err != nil {
			return results, fmt.Errorf("gc: category %s: %w", name, res.Err)
		}
	}
	if opts.Category != "" && len(results) == 0 {
		return nil, fmt.Errorf("gc: unknown category %q", opts.Category)
	}
	return results, nil
}

// sweepBlobs runs the blob mark-and-sweep, but skips entirely when any
// constellation run is in flight anywhere: an in-flight run may be about to
// write a reference whose blob would otherwise look unreferenced.
func (e *Engine) sweepBlobs(ctx context.Context, dryRun bool) (*blobstore.SweepReport, error) {
	if e.blobs == nil {
		return nil, nil
	}
	running, err := anyRunning(ctx, e.db)
	if err != nil {
		return nil, err
	}
	if running {
		e.logf("gc: skipping blob sweep; a constellation run is active")
		return nil, nil
	}

	started := e.clock()
	report, err := e.blobs.Sweep(ctx, e.cfg.Blobs.MinAgeBeforeSweep, started, dryRun)
	if err != nil {
		if !dryRun {
			e.recordRun(ctx, started, CategoryBlobs, 0, 0, err)
		}
		return report, err
	}
	for _, b := range report.Swept {
		_ = e.audit.Append(AuditEntry{Category: CategoryBlobs, Action: ActionSweep, Hash: b.Hash, SizeBytes: b.SizeBytes, DryRun: dryRun})
	}
	if !dryRun {
		e.recordRun(ctx, started, CategoryBlobs, len(report.Swept), report.ReclaimedBytes, nil)
	}
	return report, nil
}

// reapWorktrees reaps stale worktrees for every active registered repo. A
// worktree is protected from reaping while its constellation_run is non-terminal.
func (e *Engine) reapWorktrees(ctx context.Context, dryRun bool) ([]WorktreeResult, error) {
	if e.reaper == nil {
		return nil, nil
	}
	protected, err := nonTerminalRunIDs(ctx, e.db)
	if err != nil {
		return nil, err
	}
	isProtected := func(name string) bool { _, ok := protected[name]; return ok }

	repos, err := activeRepoPaths(ctx, e.db)
	if err != nil {
		return nil, err
	}
	now := e.clock()
	var out []WorktreeResult
	for _, repo := range repos {
		rep, err := e.reaper.Reap(ctx, repo, e.worktreeMaxAge, now, isProtected, dryRun)
		if err != nil {
			e.logf("gc: worktree reap %s: %v", repo, err)
			continue
		}
		if len(rep.Removed) == 0 {
			continue
		}
		out = append(out, WorktreeResult{RepoPath: repo, Removed: rep.Removed, ReclaimedBytes: rep.ReclaimedBytes})
		_ = e.audit.Append(AuditEntry{Category: "worktrees", Action: ActionReap, RepoPath: repo, Count: len(rep.Removed), ReclaimedBytes: rep.ReclaimedBytes, DryRun: dryRun})
	}
	return out, nil
}

// recordRun inserts a gc_runs ledger row. Failures are logged, not fatal: the
// sweep already happened, and a missing ledger row must not abort the pass.
func (e *Engine) recordRun(ctx context.Context, started time.Time, category string, swept int, reclaimed int64, runErr error) {
	var errStr any
	if runErr != nil {
		errStr = runErr.Error()
	}
	_, err := e.db.ExecContext(ctx,
		`INSERT INTO gc_runs (started_at, completed_at, category, swept_count, reclaimed_bytes, error)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		started.Unix(), e.clock().Unix(), category, swept, reclaimed, errStr)
	if err != nil {
		e.logf("gc: record gc_run for %s: %v", category, err)
	}
}

func (e *Engine) logf(format string, args ...any) {
	if e.logger == nil {
		return
	}
	fmt.Fprintf(e.logger, format+"\n", args...)
}

// anyRunning reports whether any constellation run is currently in 'running'.
func anyRunning(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM constellation_runs WHERE state = 'running'").Scan(&n); err != nil {
		return false, fmt.Errorf("gc: check running runs: %w", err)
	}
	return n > 0, nil
}

// nonTerminalRunIDs returns the set of run ids that are still in flight.
func nonTerminalRunIDs(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	q := fmt.Sprintf("SELECT id FROM constellation_runs WHERE state IN (%s)", placeholders(len(nonTerminalRunStates)))
	rows, err := db.QueryContext(ctx, q, toAnySlice(nonTerminalRunStates)...)
	if err != nil {
		return nil, fmt.Errorf("gc: list non-terminal runs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // iteration cleanup
	out := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

// activeRepoPaths returns the paths of all active registered repos.
func activeRepoPaths(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT path FROM repos WHERE status = 'active'")
	if err != nil {
		return nil, fmt.Errorf("gc: list repos: %w", err)
	}
	defer rows.Close() //nolint:errcheck // iteration cleanup
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}
