package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/gc"
	"github.com/papapumpkin/quasar/internal/gitops"
)

// gcCmd groups the garbage-collection subcommands. The GC is the only path that
// hard-deletes lifecycle state, blobs, and stale worktrees from the shared
// SQLite store; these subcommands drive it as one-shot operations.
var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Garbage-collect completed nebulas, runs, blobs, and stale worktrees",
}

func init() {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run one GC pass over the row categories (and blobs + worktrees)",
		Args:  cobra.NoArgs,
		RunE:  runGCRun,
	}
	runCmd.Flags().Bool("dry-run", false, "print what would be deleted without deleting")
	runCmd.Flags().String("category", "", "restrict to one category (e.g. completed_nebulas)")

	blobsCmd := &cobra.Command{
		Use:   "blobs",
		Short: "Run the blob mark-and-sweep only",
		Args:  cobra.NoArgs,
		RunE:  runGCBlobs,
	}
	blobsCmd.Flags().Bool("dry-run", false, "print what would be deleted without deleting")

	auditCmd := &cobra.Command{
		Use:   "audit",
		Short: "Tail the GC JSONL audit log",
		Args:  cobra.NoArgs,
		RunE:  runGCAudit,
	}
	auditCmd.Flags().Duration("since", 24*time.Hour, "only show entries newer than this")

	gcCmd.AddCommand(runCmd, blobsCmd, auditCmd)
	rootCmd.AddCommand(gcCmd)
}

// openGCEngine wires an Engine over the shared fabric DB, blobstore, and a
// real-git worktree reaper, plus a JSONL audit log under the data dir.
func openGCEngine(cmd *cobra.Command) (*gc.Engine, func() error, error) {
	dbPath := fabricDBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create db dir: %w", err)
	}
	fab, err := fabric.NewSQLiteFabric(cmd.Context(), dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open fabric: %w", err)
	}

	root, err := blobRoot()
	if err != nil {
		fab.Close() //nolint:errcheck // already failing
		return nil, nil, err
	}
	blobs, err := blobstore.New(root, fab.DB())
	if err != nil {
		fab.Close() //nolint:errcheck // already failing
		return nil, nil, fmt.Errorf("open blobstore: %w", err)
	}

	auditFile, err := os.OpenFile(gcAuditPath(dbPath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fab.Close() //nolint:errcheck // already failing
		return nil, nil, fmt.Errorf("open gc audit log: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		auditFile.Close() //nolint:errcheck // already failing
		fab.Close()       //nolint:errcheck // already failing
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	engine, err := gc.New(gc.Opts{
		DB:     fab.DB(),
		Config: cfg.GC,
		Blobs:  blobs,
		Reaper: gitops.NewWorktreeReaper(),
		Audit:  gc.NewAuditLog(auditFile, time.Now),
		Logger: os.Stderr,
	})
	if err != nil {
		auditFile.Close() //nolint:errcheck // already failing
		fab.Close()       //nolint:errcheck // already failing
		return nil, nil, err
	}
	closeFn := func() error {
		auditFile.Close() //nolint:errcheck // best-effort
		return fab.Close()
	}
	return engine, closeFn, nil
}

// gcAuditPath returns the JSONL audit log path alongside the fabric database.
func gcAuditPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "gc-audit.log")
}

func runGCRun(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	category, _ := cmd.Flags().GetString("category")

	engine, closeFn, err := openGCEngine(cmd)
	if err != nil {
		return err
	}
	defer closeFn() //nolint:errcheck // close error is non-fatal for a CLI op

	report, err := engine.RunOnce(cmd.Context(), gc.RunOnceOpts{
		DryRun:        dryRun,
		Category:      category,
		ReapWorktrees: true,
	})
	if err != nil {
		return err
	}
	printGCReport(report)
	return nil
}

func runGCBlobs(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	engine, closeFn, err := openGCEngine(cmd)
	if err != nil {
		return err
	}
	defer closeFn() //nolint:errcheck // close error is non-fatal for a CLI op

	report, err := engine.RunOnce(cmd.Context(), gc.RunOnceOpts{DryRun: dryRun, Category: gc.CategoryBlobs})
	if err != nil {
		return err
	}
	printGCReport(report)
	return nil
}

func runGCAudit(cmd *cobra.Command, _ []string) error {
	since, _ := cmd.Flags().GetDuration("since")
	path := gcAuditPath(fabricDBPath())
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "gc: no audit log yet")
			return nil
		}
		return fmt.Errorf("open gc audit log: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only handle

	entries, err := gc.ReadAuditSince(f, time.Now().Add(-since))
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Fprintf(os.Stderr, "%s  %-18s %-6s %s\n", e.TS, e.Category, e.Action, auditDetail(e))
	}
	fmt.Fprintf(os.Stderr, "gc: %d audit entries in the last %s\n", len(entries), since)
	return nil
}

// auditDetail renders the salient identifier of an audit entry for the tail view.
func auditDetail(e gc.AuditEntry) string {
	switch {
	case e.NebulaID != "":
		return e.NebulaID
	case e.RunID != "":
		return e.RunID
	case e.Hash != "":
		return e.Hash
	case e.RepoPath != "":
		return e.RepoPath
	default:
		return fmt.Sprintf("count=%d", e.Count)
	}
}

// printGCReport writes a human-readable summary of a pass to stderr. The dry-run
// banner makes it unmistakable that nothing was deleted.
func printGCReport(report *gc.Report) {
	if report.DryRun {
		fmt.Fprintln(os.Stderr, "gc: DRY RUN — no changes made")
	}
	for _, c := range report.Categories {
		fmt.Fprintf(os.Stderr, "gc: %-22s marked=%d swept=%d cascaded=%d\n", c.Category, c.Marked, c.Swept, c.CascadedChildren)
	}
	if report.Blobs != nil {
		fmt.Fprintf(os.Stderr, "gc: blobs                  swept=%d reclaimed=%dB kept_young=%d\n",
			len(report.Blobs.Swept), report.Blobs.ReclaimedBytes, report.Blobs.SkippedYoung)
	}
	for _, w := range report.Worktrees {
		fmt.Fprintf(os.Stderr, "gc: worktrees %s removed=%d reclaimed=%dB\n", w.RepoPath, len(w.Removed), w.ReclaimedBytes)
	}
}
