// Package cmd provides CLI commands for quasar.
//
// repo.go defines the `quasar repo` command group: explicit registration and
// lifecycle management of the git repositories a long-running Quasar process
// operates on. Registration is opt-in — there is no auto-discovery.
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/repos"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Register and manage the repositories Quasar operates on",
	Long: `The repo command group manages the set of git repositories a persistent
Quasar process is willing to operate on. Repos are registered explicitly
(no auto-discovery); each carries its own .quasar.yaml, sensors, and
pre-commit rules.`,
}

func init() {
	repoCmd.PersistentFlags().String("db", "", "fabric database path (default .quasar/fabric.db)")

	registerCmd := &cobra.Command{
		Use:   "register <path>",
		Short: "Register a git repository",
		Args:  cobra.ExactArgs(1),
		RunE:  runRepoRegister,
	}
	registerCmd.Flags().String("name", "", "display name (defaults to the directory name)")

	unregisterCmd := &cobra.Command{
		Use:   "unregister <path>",
		Short: "Soft-delete a registered repository",
		Args:  cobra.ExactArgs(1),
		RunE:  runRepoUnregister,
	}
	unregisterCmd.Flags().Bool("force", false, "orphan in-flight nebulas and remove anyway")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List registered repositories",
		Args:  cobra.NoArgs,
		RunE:  runRepoList,
	}
	listCmd.Flags().String("status", "", "filter by status (active|paused|removed)")
	listCmd.Flags().Bool("json", false, "output JSON")

	pauseCmd := &cobra.Command{
		Use:   "pause <path>",
		Short: "Pause a repository (sensors stop; in-flight work continues)",
		Args:  cobra.ExactArgs(1),
		RunE:  runRepoPause,
	}

	resumeCmd := &cobra.Command{
		Use:   "resume <path>",
		Short: "Resume a paused repository",
		Args:  cobra.ExactArgs(1),
		RunE:  runRepoResume,
	}

	showCmd := &cobra.Command{
		Use:   "show <path>",
		Short: "Show a repository's details and summary",
		Args:  cobra.ExactArgs(1),
		RunE:  runRepoShow,
	}

	repoCmd.AddCommand(registerCmd, unregisterCmd, listCmd, pauseCmd, resumeCmd, showCmd)
	rootCmd.AddCommand(repoCmd)
}

// openRegistry opens (creating if necessary) the fabric database and returns a
// registry plus a close function. The DB path comes from --db, env, or the
// default .quasar/fabric.db.
func openRegistry(cmd *cobra.Command) (*repos.Registry, func() error, error) {
	dbPath := repoDBPath(cmd)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create db dir: %w", err)
	}
	fab, err := fabric.NewSQLiteFabric(cmd.Context(), dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open fabric: %w", err)
	}
	return repos.New(fab.DB()), fab.Close, nil
}

// repoDBPath resolves the database path from the --db flag, falling back to the
// shared fabric default.
func repoDBPath(cmd *cobra.Command) string {
	if p, _ := cmd.Flags().GetString("db"); p != "" {
		return p
	}
	return fabricDBPath()
}

func runRepoRegister(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	reg, closeFn, err := openRegistry(cmd)
	if err != nil {
		return err
	}
	defer closeFn() //nolint:errcheck // close error is non-fatal for a read/write CLI op

	repo, err := reg.Register(cmd.Context(), args[0], name)
	if err != nil {
		return registerExitError(err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "registered: %s (name: %s)\n", repo.Path, repo.Name)
	return nil
}

// registerExitError maps registration failures to documented exit codes:
// 3 for an invalid path, 2 for an already-registered repo, 1 otherwise.
func registerExitError(err error) error {
	switch {
	case errors.Is(err, repos.ErrRepoPathInvalid):
		return newExitError(3, err)
	case errors.Is(err, repos.ErrRepoAlreadyRegistered):
		return newExitError(2, err)
	default:
		return newExitError(1, err)
	}
}

func runRepoUnregister(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	reg, closeFn, err := openRegistry(cmd)
	if err != nil {
		return err
	}
	defer closeFn() //nolint:errcheck // close error is non-fatal

	if err := reg.Unregister(cmd.Context(), args[0], force); err != nil {
		if errors.Is(err, repos.ErrRepoActiveNebulas) {
			return newExitError(1, fmt.Errorf("%w\nre-run with --force to orphan in-flight nebulas", err))
		}
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "unregistered: %s\n", args[0])
	return nil
}

func runRepoList(cmd *cobra.Command, args []string) error {
	status, _ := cmd.Flags().GetString("status")
	asJSON, _ := cmd.Flags().GetBool("json")
	reg, closeFn, err := openRegistry(cmd)
	if err != nil {
		return err
	}
	defer closeFn() //nolint:errcheck // close error is non-fatal

	list, err := reg.List(cmd.Context(), status)
	if err != nil {
		return err
	}

	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if list == nil {
			list = []*repos.Repo{}
		}
		return enc.Encode(list)
	}

	if len(list) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no repositories registered")
		return nil
	}
	for _, repo := range list {
		fmt.Fprintf(cmd.ErrOrStderr(), "%-8s  %s  %s  added %s\n",
			repo.Status, repo.Path, repo.Name, humanizeSince(repo.AddedAt))
	}
	return nil
}

func runRepoPause(cmd *cobra.Command, args []string) error {
	return setRepoStatus(cmd, args[0], repos.StatusPaused, "paused")
}

func runRepoResume(cmd *cobra.Command, args []string) error {
	return setRepoStatus(cmd, args[0], repos.StatusActive, "resumed")
}

// setRepoStatus flips a repo's status and prints a confirmation.
func setRepoStatus(cmd *cobra.Command, path, status, verb string) error {
	reg, closeFn, err := openRegistry(cmd)
	if err != nil {
		return err
	}
	defer closeFn() //nolint:errcheck // close error is non-fatal

	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}
	if err := reg.SetStatus(cmd.Context(), abs, status); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", verb, abs)
	return nil
}

func runRepoShow(cmd *cobra.Command, args []string) error {
	reg, closeFn, err := openRegistry(cmd)
	if err != nil {
		return err
	}
	defer closeFn() //nolint:errcheck // close error is non-fatal

	abs, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", args[0], err)
	}
	repo, err := reg.Get(cmd.Context(), abs)
	if err != nil {
		return err
	}
	active, err := reg.CountActiveNebulas(cmd.Context(), abs)
	if err != nil {
		return err
	}

	out := cmd.ErrOrStderr()
	fmt.Fprintf(out, "path:           %s\n", repo.Path)
	fmt.Fprintf(out, "name:           %s\n", repo.Name)
	fmt.Fprintf(out, "status:         %s\n", repo.Status)
	fmt.Fprintf(out, "added:          %s\n", repo.AddedAt.Format(time.RFC3339))
	fmt.Fprintf(out, "updated:        %s\n", repo.UpdatedAt.Format(time.RFC3339))
	fmt.Fprintf(out, "last seen:      %s\n", repo.LastSeenAt.Format(time.RFC3339))
	fmt.Fprintf(out, "active nebulas: %d\n", active)

	res, err := repos.NewResolver(repo)
	if err != nil {
		return err
	}
	sensors, err := res.AllSensorPaths()
	if err != nil {
		return err
	}
	if len(sensors) == 0 {
		fmt.Fprintln(out, "sensors:        (none configured)")
	} else {
		fmt.Fprintln(out, "sensors:")
		for _, s := range sensors {
			fmt.Fprintf(out, "  - %s\n", filepath.Base(s))
		}
	}
	return nil
}

// humanizeSince renders a coarse relative-time string for list output.
func humanizeSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
