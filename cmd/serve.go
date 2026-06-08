package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/cockpit"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/repos"
)

// serveCmd implements `quasar serve`: run the supervisor process, optionally
// serving the cockpit HTTP/WebSocket API from the same binary. The cockpit is
// gated by [cockpit].enabled (default off) and can be forced on with --cockpit.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the supervisor, optionally serving the cockpit API",
	Long: `Serve runs the Quasar supervisor. When the cockpit is enabled (via
[cockpit].enabled in .quasar.yaml or the --cockpit flag), it also serves the
read-mostly JSON + WebSocket API the cockpit UI consumes, from this same
process. The bearer token is read from <data-dir>/cockpit-token; generate one
with 'quasar cockpit token'.`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().Bool("cockpit", false, "force the cockpit API on regardless of config")
	serveCmd.Flags().Bool("cockpit-only", false, "serve only the cockpit API (no TUI), for tests")
	serveCmd.Flags().String("addr", "", "cockpit listen address (overrides [cockpit].addr)")
	rootCmd.AddCommand(serveCmd)
}

// runServe resolves config, decides whether the cockpit is enabled, and (when
// it is) builds and runs the cockpit server until interrupted.
func runServe(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	force, _ := cmd.Flags().GetBool("cockpit")
	enabled := cfg.Cockpit.Enabled || force
	if !enabled {
		fmt.Fprintln(os.Stderr, "cockpit disabled; nothing to serve (enable [cockpit].enabled or pass --cockpit)")
		return nil
	}

	addr := cfg.Cockpit.Addr
	if a, _ := cmd.Flags().GetString("addr"); a != "" {
		addr = a
	}

	srv, cleanup, err := buildCockpitServer(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "cockpit listening on %s\n", addr)
	return srv.Run(ctx, addr)
}

// buildCockpitServer opens the fabric database, constructs the data stores, and
// assembles a cockpit.Server. The returned cleanup closes the database.
func buildCockpitServer(ctx context.Context, cfg config.Config) (*cockpit.Server, func(), error) {
	dbPath := fabricDBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create db dir: %w", err)
	}
	fab, err := fabric.NewSQLiteFabric(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open fabric: %w", err)
	}
	cleanup := func() { _ = fab.Close() }

	root, err := blobRoot()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	blobs, err := blobstore.New(root, fab.DB())
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("open blobstore: %w", err)
	}

	token, err := readCockpitToken()
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	srv, err := cockpit.New(cockpit.Opts{
		Enabled: true,
		Token:   token,
		Repos:   repos.New(fab.DB()),
		Nebulas: fabric.NewNebulaStore(fab.DB(), blobs),
		Runs:    fabric.NewConstellationRunStore(fab.DB()),
		Logger:  os.Stderr,
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("build cockpit server: %w", err)
	}
	return srv, cleanup, nil
}
