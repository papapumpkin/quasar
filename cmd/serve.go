package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/cockpit"
	"github.com/papapumpkin/quasar/internal/cockpit/views"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/constellations"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/tui/fleet"
)

// serveCmd runs Quasar as a long-lived process: the trigger-queue supervisor
// and step driver that progress constellation runs, plus (when enabled) the
// browser-based cockpit dashboard. Unlike `quasar fleet`, it does not require a
// TTY and is the entry point for headless / server deployments.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the supervisor and (optionally) the cockpit web dashboard",
	Long: `Run Quasar headless: start the trigger-queue supervisor and step driver so
approved nebulas progress to completion, and optionally serve the cockpit web
dashboard (a real-time browser mirror of the fleet view).

The cockpit is served when [cockpit].enabled is true in .quasar.yaml or when
--cockpit is passed. It requires a bearer token; generate one with
'quasar cockpit token' first.

Use --cockpit-only to serve just the cockpit (no supervisor) for isolated
testing against an existing fabric database.`,
	Args: cobra.NoArgs,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().String("db", "", "fabric database path (default .quasar/fabric.db)")
	serveCmd.Flags().Bool("cockpit", false, "force-enable the cockpit web dashboard (overrides config)")
	serveCmd.Flags().Bool("cockpit-only", false, "serve only the cockpit, without the supervisor (for testing)")
	serveCmd.Flags().String("addr", "", "cockpit listen address (overrides config)")
	rootCmd.AddCommand(serveCmd)
}

// runServe wires the fabric DB, runtime cache, supervisor/step driver, and the
// cockpit server into one process, then blocks until ctx is canceled or a
// SIGINT/SIGTERM arrives.
func runServe(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPath, _ := cmd.Flags().GetString("db")
	if dbPath == "" {
		dbPath = fabricDBPath()
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	fab, err := fabric.NewSQLiteFabric(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open fabric: %w", err)
	}
	defer fab.Close() //nolint:errcheck // close error is non-fatal at shutdown

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cockpitCfg := cfg.Cockpit
	forceCockpit, _ := cmd.Flags().GetBool("cockpit")
	cockpitOnly, _ := cmd.Flags().GetBool("cockpit-only")
	if forceCockpit || cockpitOnly {
		cockpitCfg.Enabled = true
	}
	if addr, _ := cmd.Flags().GetString("addr"); addr != "" {
		cockpitCfg.Addr = addr
	}

	// One Notifier shared by the runtime event sink (producer) and the cockpit
	// SSE handlers (consumers). Built unconditionally so the sink can be wired
	// into the runtime cache even when the cockpit HTTP server is not served.
	notifier := cockpit.NewNotifier(128)

	// The cockpit server is started first so a bad token or bind fails fast,
	// before we spin up the supervisor.
	var server *cockpit.Server
	if cockpitCfg.Enabled {
		server, err = buildCockpitServer(fab, notifier)
		if err != nil {
			return err
		}
	}

	if !cockpitOnly {
		cache, err := buildRuntimeCache(fab, dbPath, &notifierSink{n: notifier})
		if err != nil {
			return fmt.Errorf("start supervisor: %w", err)
		}
		startSupervisorAndDriver(ctx, fab, cache, openSupervisorLog(dbPath))
		fmt.Fprintln(os.Stderr, "serve: supervisor and step driver running")
	}

	if server != nil {
		errCh := make(chan error, 1)
		go func() { errCh <- server.Run(ctx, cockpitCfg.Addr) }()
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "serve: shutting down")
			return nil
		case err := <-errCh:
			if err != nil {
				return fmt.Errorf("cockpit server: %w", err)
			}
			return nil
		}
	}

	// Supervisor-only mode (cockpit disabled): block until shutdown. This is
	// unreachable under --cockpit-only, which force-enables the cockpit above.
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "serve: shutting down")
	return nil
}

// buildCockpitServer constructs the cockpit HTTP server: reads the bearer token,
// wires the RuntimeActions / render adapters, and leaves GitHub badges disabled
// (the github sensor exposes issue reads only, not PR status). The Notifier is
// shared with the runtime event sink so live events reach connected browsers.
func buildCockpitServer(fab *fabric.SQLiteFabric, notifier *cockpit.Notifier) (*cockpit.Server, error) {
	token, err := readCockpitToken()
	if err != nil {
		return nil, err
	}
	server, err := cockpit.New(cockpit.Opts{
		DB:         fab.DB(),
		Runtime:    fleet.NewStore(fab.DB()),
		Notifier:   notifier,
		GitHub:     nil, // see comment above: no clean PR-status surface today
		Token:      token,
		Assets:     cockpit.Assets(),
		RenderPage: renderCockpitPage,
		RenderRun:  renderCockpitRun,
		Logf:       func(f string, a ...any) { fmt.Fprintf(os.Stderr, "cockpit: "+f+"\n", a...) },
	})
	if err != nil {
		return nil, fmt.Errorf("build cockpit server: %w", err)
	}
	return server, nil
}

// readCockpitToken loads the bearer token written by `quasar cockpit token`.
// A missing file is a clear, actionable error rather than an empty token.
func readCockpitToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	path := filepath.Join(home, ".quasar", "cockpit-token")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("cockpit token not found at %s; run 'quasar cockpit token' first", path)
		}
		return "", fmt.Errorf("read cockpit token %s: %w", path, err)
	}
	tok := string(raw)
	if tok == "" {
		return "", fmt.Errorf("cockpit token at %s is empty; run 'quasar cockpit token' to regenerate", path)
	}
	return tok, nil
}

// notifierSink adapts a cockpit.Notifier to the constellations.EventSink
// interface so per-repo Runtimes publish live state-change events into the
// cockpit's SSE fan-out.
type notifierSink struct {
	n *cockpit.Notifier
}

// notifierSink satisfies the runtime's EventSink contract.
var _ constellations.EventSink = (*notifierSink)(nil)

// Emit publishes the runtime event onto the shared Notifier.
func (s *notifierSink) Emit(topic, typ string, data map[string]any) {
	s.n.Publish(cockpit.Event{Topic: topic, Type: typ, Data: data})
}

// renderCockpitPage is the cockpit.PageRenderer: it renders the fleet board via
// the templ-generated views.Page component. Defined in the cmd layer to keep
// the views→cockpit import one-directional.
func renderCockpitPage(ctx context.Context, w http.ResponseWriter, f cockpit.Fleet) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return views.Page(f).Render(ctx, w)
}

// renderCockpitRun is the cockpit.RunRenderer: it renders a single run card via
// the templ-generated views.RunCardView component.
func renderCockpitRun(ctx context.Context, w io.Writer, rc cockpit.RunCard) error {
	return views.RunCardView(rc).Render(ctx, w)
}
