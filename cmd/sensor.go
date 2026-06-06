package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/repos"
	"github.com/papapumpkin/quasar/internal/sensors"
)

// sensorCmd groups the sensor administration subcommands. It is hidden from the
// help output: these are debugging affordances for operators, not part of the
// supported surface. In normal operation the supervisor (a later phase) drives
// every sensor on its configured poll_interval.
var sensorCmd = &cobra.Command{
	Use:    "sensor",
	Short:  "Sensor administration (debugging)",
	Hidden: true,
}

func init() {
	pollCmd := &cobra.Command{
		Use:   "poll <repo-path> <sensor-name>",
		Short: "Force a single poll cycle for one sensor (debugging)",
		Long: "Forces one immediate poll of the named sensor on a registered repo, " +
			"bypassing the supervisor's poll_interval tick. New events are deduplicated, " +
			"seeded into awaiting_approval nebulas, and the cursor is persisted — exactly " +
			"as a scheduled tick would. Useful for testing a sensor without waiting for the " +
			"next interval.",
		Args: cobra.ExactArgs(2),
		RunE: runSensorPoll,
	}
	sensorCmd.AddCommand(pollCmd)
	rootCmd.AddCommand(sensorCmd)
}

// runSensorPoll wires a one-shot Scheduler over the shared fabric DB and forces
// a single poll cycle for the named sensor on the given registered repo.
func runSensorPoll(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	repoArg, sensorName := args[0], args[1]

	abs, err := filepath.Abs(repoArg)
	if err != nil {
		return fmt.Errorf("sensor poll: resolve %q: %w", repoArg, err)
	}

	dbPath := fabricDBPath()
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		return fmt.Errorf("sensor poll: fabric database not found: %s", dbPath)
	}
	fab, err := fabric.NewSQLiteFabric(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("sensor poll: open fabric: %w", err)
	}
	defer fab.Close() //nolint:errcheck // close error is non-fatal for a CLI op

	// The repo must be registered: sensor_cursors, sensor_events, and nebulas all
	// carry a repo_path foreign key, so polling an unregistered path would fail at
	// the first insert. Resolving the row also yields the canonical stored path.
	repo, err := repos.New(fab.DB()).Get(ctx, abs)
	if err != nil {
		return fmt.Errorf("sensor poll: %w", err)
	}

	sched, err := buildSensorScheduler(ctx, fab, repo, sensorName)
	if err != nil {
		return err
	}

	res, err := sched.PollOnce(ctx)
	if err != nil {
		return fmt.Errorf("sensor poll %s/%s: %w", repo.Path, sensorName, err)
	}

	printSensorPollResult(cmd, repo.Path, sensorName, res)
	return nil
}

// buildSensorScheduler loads the repo's sensor instance, constructs and
// configures the named sensor, and wires a Scheduler over the fabric-backed
// cursor, event, and nebula stores. Trigger firing is intentionally left nil:
// this debug path seeds nebulas but does not launch constellations (the
// supervisor in a later phase owns the production trigger).
func buildSensorScheduler(ctx context.Context, fab *fabric.SQLiteFabric, repo *repos.Repo, sensorName string) (*sensors.Scheduler, error) {
	resolver, err := repos.NewResolver(repo)
	if err != nil {
		return nil, fmt.Errorf("sensor poll: %w", err)
	}
	instance, err := artifacts.New(resolver).LoadSensorInstance(sensorName)
	if err != nil {
		return nil, fmt.Errorf("sensor poll: load sensor %q: %w", sensorName, err)
	}

	sensor, err := sensors.Default().BuildSensor(instance.Type)
	if err != nil {
		return nil, fmt.Errorf("sensor poll: %w", err)
	}
	if err := sensor.Configure(instance.Config, sensors.OSSecretResolver{}); err != nil {
		return nil, fmt.Errorf("sensor poll: configure %q: %w", sensorName, err)
	}

	root, err := blobRoot()
	if err != nil {
		return nil, err
	}
	blobs, err := blobstore.New(root, fab.DB())
	if err != nil {
		return nil, fmt.Errorf("sensor poll: open blobstore: %w", err)
	}

	sched, err := sensors.NewScheduler(sensors.SchedulerOpts{
		RepoPath: repo.Path,
		Instance: instance,
		Sensor:   sensor,
		Cursors:  fabric.NewSensorCursorStore(fab.DB()),
		Events:   fabric.NewSensorEventStore(fab.DB()),
		Nebulas:  &seedNebulaInserter{store: fabric.NewNebulaStore(fab.DB(), blobs)},
		Logger:   os.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("sensor poll: %w", err)
	}
	return sched, nil
}

// seedNebulaInserter adapts *fabric.NebulaStore to the sensors.NebulaInserter
// the scheduler consumes. It lives in cmd because mapping the sensor package's
// SeedNebula onto fabric.NebulaRow requires importing both packages, and the
// layering rules forbid sensors from importing fabric (that is exactly why the
// scheduler declares its own narrow inserter interface).
type seedNebulaInserter struct{ store *fabric.NebulaStore }

// Insert maps a seed nebula onto a NebulaRow and writes it via the store. The
// sensor-derived goals and constraints are rendered into the row's context block
// so the architect that later refines the seed inherits them.
func (a *seedNebulaInserter) Insert(ctx context.Context, n sensors.SeedNebula) (string, error) {
	contextTOML, err := renderSeedContextTOML(n.Goals, n.Constraints)
	if err != nil {
		return "", fmt.Errorf("sensor poll: render seed context: %w", err)
	}
	return a.store.Insert(ctx, fabric.NebulaRow{
		RepoPath:    n.RepoPath,
		Name:        n.Name,
		Description: n.Description,
		SourceName:  n.SourceName,
		SourceID:    n.SourceID,
		SourceURL:   n.SourceURL,
		ContextTOML: contextTOML,
		Status:      n.Status,
	})
}

// seedContext is the subset of a nebula's [context] block a sensor can fill from
// the source item. The TOML keys match the nebula manifest's Context schema so
// the architect reads them back unchanged.
type seedContext struct {
	Goals       []string `toml:"goals,omitempty"`
	Constraints []string `toml:"constraints,omitempty"`
}

// renderSeedContextTOML marshals the derived goals and constraints into a context
// TOML block, or returns "" when both are empty so the row carries no context.
func renderSeedContextTOML(goals, constraints []string) (string, error) {
	if len(goals) == 0 && len(constraints) == 0 {
		return "", nil
	}
	b, err := toml.Marshal(seedContext{Goals: goals, Constraints: constraints})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// printSensorPollResult writes the poll summary to stderr (human-readable
// output never goes to stdout), one line per seeded nebula id so the operator
// can immediately inspect the drafts the cycle produced.
func printSensorPollResult(cmd *cobra.Command, repoPath, sensorName string, res sensors.PollResult) {
	out := cmd.ErrOrStderr()
	fmt.Fprintf(out, "sensor %s/%s: observed=%d seeded=%d fired=%d queued=%d\n",
		repoPath, sensorName, res.Observed, res.Seeded, res.Fired, res.Queued)
	for _, id := range res.NebulaIDs {
		fmt.Fprintf(out, "  seeded nebula %s\n", id)
	}
}
