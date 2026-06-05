package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/integrations"
	// Blank import for its side effect only: the github adapter registers
	// itself with the integration registry from package init(). The command
	// dispatches through integrations.Default() and never references github
	// types directly, keeping cmd/ free of tracking-system-specific logic.
	_ "github.com/papapumpkin/quasar/internal/integrations/github"
	"github.com/papapumpkin/quasar/internal/nebula"
)

// defaultNebulaDir is the default parent directory for generated nebulas.
const defaultNebulaDir = ".nebulas"

// ticketArchitect generates a nebula directory from a ticket. It is the seam
// the cmd layer depends on so tests substitute a fake architect without an LLM.
// The production implementation wraps nebula.FromTicketInto.
type ticketArchitect interface {
	FromTicket(ctx context.Context, t *integrations.Ticket, targetDir string) (*nebula.NebulaInfo, error)
}

// nebulaRecorder persists a draft nebula's provenance row. *fabric.SQLiteFabric
// satisfies it; tests inject a fake.
type nebulaRecorder interface {
	InsertNebula(ctx context.Context, rec fabric.NebulaRecord) error
}

// sourceBuilder resolves a registered integration name plus its parsed config
// section into a concrete TicketSource. It is a seam so tests can return a fake
// source without a configured adapter.
type sourceBuilder func(name string, section map[string]any) (integrations.TicketSource, error)

// nebulaNewDeps bundles the collaborators of the nebula-new flow so the core
// orchestration (runNebulaNewWith) is unit-testable with fakes.
type nebulaNewDeps struct {
	cfg         config.Config
	buildSource sourceBuilder
	architect   ticketArchitect
	recorder    nebulaRecorder
	dirExists   func(path string) bool
	out         io.Writer
}

// addNebulaNewFlags registers flags for `quasar nebula new` and silences
// cobra's default usage/error printing so runtime errors (which carry their own
// actionable messages) are surfaced verbatim by Execute.
func addNebulaNewFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Override the auto-derived nebula directory name")
	cmd.Flags().String("dir", defaultNebulaDir, "Parent directory for the generated nebula")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
}

// runNebulaNew is the cobra adapter for `quasar nebula new <source>:<id>`. It
// wires production collaborators (config, claude invoker, fabric DB) and
// delegates the orchestration to runNebulaNewWith.
func runNebulaNew(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	name, _ := cmd.Flags().GetString("name")
	dir, _ := cmd.Flags().GetString("dir")
	if dir == "" {
		dir = defaultNebulaDir
	}

	// Open (creating if needed) the fabric DB for the draft-provenance row.
	dbPath := fabricDBPath()
	if mkErr := os.MkdirAll(filepath.Dir(dbPath), 0o755); mkErr != nil {
		return fmt.Errorf("create fabric directory: %w", mkErr)
	}
	fc, err := fabric.NewSQLiteFabric(cmd.Context(), dbPath)
	if err != nil {
		return fmt.Errorf("open fabric database: %w", err)
	}
	defer fc.Close()

	invoker := claude.NewInvoker(cfg.ClaudePath, cfg.Verbose)

	deps := nebulaNewDeps{
		cfg: cfg,
		buildSource: func(srcName string, section map[string]any) (integrations.TicketSource, error) {
			return integrations.Default().BuildTicketSource(srcName, section, integrations.OSSecretResolver{})
		},
		architect: claudeArchitect{invoker: invoker},
		recorder:  fc,
		dirExists: func(p string) bool { _, statErr := os.Stat(p); return statErr == nil },
		out:       os.Stderr,
	}

	return runNebulaNewWith(cmd.Context(), args[0], name, dir, deps)
}

// claudeArchitect is the production ticketArchitect backed by the architect LLM.
type claudeArchitect struct {
	invoker agent.Invoker
}

// FromTicket generates the nebula into targetDir via the architect agent.
func (a claudeArchitect) FromTicket(ctx context.Context, t *integrations.Ticket, targetDir string) (*nebula.NebulaInfo, error) {
	return nebula.FromTicketInto(ctx, a.invoker, t, targetDir)
}

// runNebulaNewWith orchestrates the ticket -> draft-nebula flow against injected
// collaborators. ref is the raw "<source>:<id>" argument. It returns an error
// wrapped via newExitError(2, ...) when the ticket does not exist so scripts can
// distinguish that case; all other failures exit 1.
func runNebulaNewWith(ctx context.Context, ref, nameOverride, parentDir string, deps nebulaNewDeps) error {
	sourceName, sourceID, err := splitTicketRef(ref)
	if err != nil {
		return err
	}

	section, ok := deps.cfg.IntegrationSections[sourceName]
	if !ok {
		return fmt.Errorf("no integration named %q configured; add an [integrations.%s] block to .quasar.yaml", sourceName, sourceName)
	}

	src, err := deps.buildSource(sourceName, section)
	if err != nil {
		return err
	}

	t, err := src.Fetch(ctx, sourceID)
	if err != nil {
		if errors.Is(err, integrations.ErrTicketNotFound) {
			return newExitError(2, err)
		}
		return fmt.Errorf("fetch %s:%s: %w", sourceName, sourceID, err)
	}

	name := nameOverride
	if name == "" {
		name = nebula.SlugifyTicket(t)
	}
	target := resolveCollision(parentDir, name, deps.dirExists)

	info, err := deps.architect.FromTicket(ctx, t, target)
	if err != nil {
		return err
	}

	rec := fabric.NebulaRecord{
		ID:         info.Name,
		SourceType: "ticket",
		SourceName: t.SourceName,
		SourceID:   t.SourceID,
		Path:       info.Path,
		Status:     "draft",
	}
	if err := deps.recorder.InsertNebula(ctx, rec); err != nil {
		return fmt.Errorf("record draft nebula: %w", err)
	}

	fmt.Fprintf(deps.out, "created draft nebula at %s (source: %s, ref: %s)\n", info.Path, t.SourceName, t.SourceID)
	return nil
}

// splitTicketRef parses a "<source>:<id>" reference on the first colon, so ids
// may themselves contain colons. Empty source or id is rejected with a usage
// message.
func splitTicketRef(ref string) (source, id string, err error) {
	source, id, found := strings.Cut(ref, ":")
	if !found || source == "" || id == "" {
		return "", "", fmt.Errorf("invalid ticket reference %q: expected <source>:<id> (e.g. github:42)", ref)
	}
	return source, id, nil
}

// resolveCollision returns the first non-existent directory among
// <parent>/<name>, <parent>/<name>-2, <parent>/<name>-3, … so a re-pulled
// ticket lands beside the existing draft rather than overwriting it.
func resolveCollision(parent, name string, exists func(path string) bool) string {
	target := filepath.Join(parent, name)
	for i := 2; exists(target); i++ {
		target = filepath.Join(parent, fmt.Sprintf("%s-%d", name, i))
	}
	return target
}
