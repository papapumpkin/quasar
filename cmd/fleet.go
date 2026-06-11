package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/constellations"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/gitops"
	"github.com/papapumpkin/quasar/internal/repos"
	"github.com/papapumpkin/quasar/internal/sensors"
	"github.com/papapumpkin/quasar/internal/tui/fleet"
)

// triggerSupervisorInterval is how often the fleet's background supervisor
// polls trigger_queue while the dashboard is open. One second balances
// approval latency (operators expect their [a] keystroke to actually launch
// a run promptly) against database churn (the query is cheap on the
// well-indexed pending row count).
const triggerSupervisorInterval = time.Second

// stepDriverInterval is how often the step driver advances running runs.
// Tighter than the supervisor because Step is the hot path — every node
// firing in every constellation, every back-edge cycle, every nested
// dispatch. 250ms is a deliberate compromise: tight enough that a
// well-behaved star invocation's wall-clock latency dominates total run
// time (not driver wakeup), loose enough that DB churn stays bounded.
const stepDriverInterval = 250 * time.Millisecond

// fleetCmd launches the multi-repo fleet dashboard: a three-lane home view
// (awaiting-approval drafts, in-flight runs, recent terminal nebulas) grouped
// by registered repository, reading from the shared fabric database.
var fleetCmd = &cobra.Command{
	Use:     "fleet",
	Aliases: []string{"tui"},
	Short:   "Launch the multi-repo fleet dashboard",
	Long: `Open the fleet dashboard: a three-lane view of every registered repo's
sensor-produced drafts awaiting approval, in-flight constellation runs, and
recently completed nebulas. Approve a draft with [a] to kick off the architect.`,
	Args: cobra.NoArgs,
	RunE: runFleet,
}

func init() {
	fleetCmd.Flags().String("db", "", "fabric database path (default .quasar/fabric.db)")
	rootCmd.AddCommand(fleetCmd)
}

// runFleet opens the fabric database and runs the fleet dashboard program.
func runFleet(cmd *cobra.Command, _ []string) error {
	if !isStderrTTY() {
		return fmt.Errorf("quasar fleet requires a TTY (terminal)")
	}

	dbPath, _ := cmd.Flags().GetString("db")
	if dbPath == "" {
		dbPath = fabricDBPath()
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	fab, err := fabric.NewSQLiteFabric(cmd.Context(), dbPath)
	if err != nil {
		return fmt.Errorf("open fabric: %w", err)
	}
	defer fab.Close() //nolint:errcheck // close error is non-fatal for a read-mostly TUI

	// Start the trigger-queue supervisor in the background so fleet
	// approvals actually fire constellation runs. Construction is
	// best-effort: if claude or the blobstore is unavailable, the fleet
	// dashboard still opens — the operator just won't see approvals
	// propagate. A diagnostic line lands in the supervisor log either way.
	supCtx, stopSupervisor := context.WithCancel(cmd.Context())
	defer stopSupervisor()
	if err := startTriggerSupervisor(supCtx, fab, dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "fleet: trigger supervisor disabled: %v\n", err)
	}
	// Start sensor pollers so awaiting-approval drafts appear without a
	// manual poll. onSeed is nil here; the awaiting-lane 30s tick picks up
	// new rows on its next fire. The fleet TUI has no notifier to push to.
	startSensorSupervisor(supCtx, fab, dbPath, nil)

	statePath := filepath.Join(filepath.Dir(dbPath), "tui-state.json")
	model := fleet.NewModel(cmd.Context(), fleet.NewStore(fab.DB()), statePath)

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("fleet TUI error: %w", err)
	}
	return nil
}

// startTriggerSupervisor constructs a per-repo Runtime cache and runs the
// constellations.Supervisor in a background goroutine. Errors during
// construction return a non-nil error so the caller can disable the
// supervisor cleanly; once Run starts, per-tick errors are logged to the
// supervisor log file and the loop keeps going until ctx is canceled.
//
// The supervisor's stderr would corrupt the Bubble Tea altscreen, so all
// diagnostics route to .quasar/supervisor.log alongside the fabric DB.
// Tail it during TUI sessions to see what the consumer is doing.
func startTriggerSupervisor(ctx context.Context, fab *fabric.SQLiteFabric, dbPath string) error {
	cache, err := buildRuntimeCache(fab, dbPath, nil, "")
	if err != nil {
		return err
	}
	logger := openSupervisorLog(dbPath)
	startSupervisorAndDriver(ctx, fab, cache, logger)
	return nil
}

// buildRuntimeCache constructs the shared RuntimeCache used by both the fleet
// dashboard's background supervisor and `quasar serve`. The optional events
// sink, when non-nil, is threaded into every per-repo Runtime so live
// state-change events reach the cockpit's SSE fan-out; a nil sink disables
// emission. Construction is best-effort: a config-load, blobstore, or invoker
// failure returns a non-nil error so the caller can degrade cleanly.
func buildRuntimeCache(fab *fabric.SQLiteFabric, dbPath string, events constellations.EventSink, tailDir string) (*constellations.RuntimeCache, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	blobs, err := blobstore.New(filepath.Dir(dbPath), fab.DB())
	if err != nil {
		return nil, fmt.Errorf("open blobstore: %w", err)
	}

	invoker := claude.NewInvoker(cfg.ClaudePath, cfg.Verbose)
	if err := invoker.Validate(); err != nil {
		return nil, fmt.Errorf("validate claude invoker: %w", err)
	}

	cache, err := constellations.NewRuntimeCache(constellations.RuntimeCacheOpts{
		DB:               fab.DB(),
		Blobs:            blobs,
		Invoker:          invoker,
		DefaultBudgetUSD: cfg.MaxBudgetUSD,
		PreCommitFor:     repoPreCommitFor,
		Events:           events,
		TailDir:          tailDir,
	})
	if err != nil {
		return nil, fmt.Errorf("build runtime cache: %w", err)
	}
	return cache, nil
}

// startSupervisorAndDriver runs the trigger-queue Supervisor and the StepDriver
// in a single background goroutine bound to ctx. Both share the supplied logger
// handle, which is closed once after both return (ctx cancellation triggers
// both). This is the shared launch path for `quasar fleet` and `quasar serve`.
func startSupervisorAndDriver(ctx context.Context, fab *fabric.SQLiteFabric, cache *constellations.RuntimeCache, logger io.Writer) {
	sup := &constellations.Supervisor{
		DB:     fab.DB(),
		Firer:  &constellations.RuntimeCacheFirer{Cache: cache},
		Logger: logger,
	}
	// StepDriver is the other half of the trigger pipeline: the Supervisor
	// fires runs at their entry node, the StepDriver walks them through to
	// terminal. Without it, fired runs stall at the entry node forever.
	// Faster interval than the Supervisor because Step is the hot path —
	// every node firing in a constellation, every cycle of an inner loop.
	driver := &constellations.StepDriver{
		DB:      fab.DB(),
		Stepper: &constellations.RuntimeCacheStepper{Cache: cache},
		Logger:  logger,
	}
	go func() {
		// Both share the same logger handle. Close once after both have
		// returned (ctx cancellation triggers both).
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			sup.Run(ctx, triggerSupervisorInterval)
		}()
		go func() {
			defer wg.Done()
			driver.Run(ctx, stepDriverInterval)
		}()
		wg.Wait()
		closeIfCloser(logger)
	}()
}

// closeIfCloser closes w only when it implements io.Closer. The supervisor log
// writer is an *os.File on success but io.Discard on open failure; io.Discard
// is non-nil yet has no Close method, so a bare logger.(io.Closer) assertion
// would panic on exactly the misconfiguration the fallback exists to tolerate.
func closeIfCloser(w io.Writer) {
	if c, ok := w.(io.Closer); ok {
		_ = c.Close()
	}
}

// startSensorSupervisor starts one Scheduler goroutine per (repo, sensor) pair
// found across all active registered repos. Construction errors per repo or
// sensor are logged and skipped rather than aborting the entire supervisor.
// All goroutines stop cleanly when ctx is canceled — no goroutine leaks.
//
// onSeed, when non-nil, is called after each nebula is durably written. The
// serve command wires in a cockpit notifier publish; the fleet command passes
// nil and relies on the awaiting-lane 30s tick to pick up new rows.
func startSensorSupervisor(ctx context.Context, fab *fabric.SQLiteFabric, dbPath string, onSeed func(string)) {
	logger := openSupervisorLog(dbPath)

	repoReg := repos.New(fab.DB())
	activeRepos, err := repoReg.List(ctx, repos.StatusActive)
	if err != nil {
		fmt.Fprintf(logger, "sensor supervisor: list repos: %v\n", err)
		return
	}

	root, err := blobRoot()
	if err != nil {
		fmt.Fprintf(logger, "sensor supervisor: %v\n", err)
		return
	}
	blobs, err := blobstore.New(root, fab.DB())
	if err != nil {
		fmt.Fprintf(logger, "sensor supervisor: open blobstore: %v\n", err)
		return
	}

	db := fab.DB()
	triggerFn := sensors.TriggerFunc(func(ctx context.Context, repoPath, nebulaID, constellationName string) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO trigger_queue (nebula_id, constellation_name, state, created_at, repo_path)
			 VALUES (?, ?, 'pending', ?, ?)`,
			nebulaID, constellationName, time.Now().Unix(), repoPath)
		return err
	})

	var started int
	for _, repo := range activeRepos {
		resolver, err := repos.NewResolver(repo)
		if err != nil {
			fmt.Fprintf(logger, "sensor supervisor: resolver for %q: %v\n", repo.Path, err)
			continue
		}
		instances, err := artifacts.New(resolver).LoadAllSensorInstances()
		if err != nil {
			fmt.Fprintf(logger, "sensor supervisor: load sensors for %q: %v\n", repo.Path, err)
			continue
		}
		if len(instances) == 0 {
			continue
		}
		if err := repoReg.Touch(ctx, repo.Path); err != nil {
			fmt.Fprintf(logger, "sensor supervisor: touch %q: %v\n", repo.Path, err)
		}
		for _, inst := range instances {
			sensor, err := sensors.Default().BuildSensor(inst.Type)
			if err != nil {
				fmt.Fprintf(logger, "sensor supervisor: build %q/%q: %v\n", repo.Path, inst.Name, err)
				continue
			}
			if err := sensor.Configure(inst.Config, sensors.OSSecretResolver{}); err != nil {
				fmt.Fprintf(logger, "sensor supervisor: configure %q/%q: %v\n", repo.Path, inst.Name, err)
				continue
			}
			sched, err := sensors.NewScheduler(sensors.SchedulerOpts{
				RepoPath: repo.Path,
				Instance: inst,
				Sensor:   sensor,
				Cursors:  fabric.NewSensorCursorStore(fab.DB()),
				Events:   fabric.NewSensorEventStore(fab.DB()),
				Nebulas:  &seedNebulaInserter{store: fabric.NewNebulaStore(fab.DB(), blobs)},
				Trigger:  triggerFn,
				OnSeed:   onSeed,
				Logger:   logger,
			})
			if err != nil {
				fmt.Fprintf(logger, "sensor supervisor: scheduler for %q/%q: %v\n", repo.Path, inst.Name, err)
				continue
			}
			repoPath, instName := repo.Path, inst.Name
			started++
			go func(s *sensors.Scheduler) {
				if err := s.Run(ctx); err != nil && ctx.Err() == nil {
					fmt.Fprintf(logger, "sensor supervisor: %q/%q exited: %v\n", repoPath, instName, err)
				}
			}(sched)
		}
	}
	fmt.Fprintf(logger, "sensor supervisor: started %d scheduler(s)\n", started)
}

// openSupervisorLog opens the supervisor's append-mode log file alongside
// the fabric DB. A failure to open it routes diagnostics to io.Discard —
// the alternative (stderr) would corrupt the Bubble Tea altscreen.
func openSupervisorLog(dbPath string) io.Writer {
	logPath := filepath.Join(filepath.Dir(dbPath), "supervisor.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return io.Discard
	}
	return f
}

// repoPreCommitFor reads the per-repo .quasar.yaml and returns its pre-commit
// policy. A missing file is not an error — the repo just runs without a
// pre-commit gate, matching the legacy single-repo behavior.
func repoPreCommitFor(repoPath string) (gitops.PreCommitConfig, error) {
	configPath := filepath.Join(repoPath, ".quasar.yaml")
	if _, err := os.Stat(configPath); err != nil {
		return gitops.PreCommitConfig{}, nil
	}
	cfg, err := config.LoadFromPath(configPath)
	if err != nil {
		return gitops.PreCommitConfig{}, err
	}
	return gitops.PreCommitConfig{
		Commands:    cfg.PreCommit.Commands,
		FailOnError: cfg.PreCommit.FailOnError,
	}, nil
}
