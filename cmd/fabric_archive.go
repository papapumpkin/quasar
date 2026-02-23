package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/neutron"
)

// archiveCmd is the top-level alias: `quasar archive`.
var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Snapshot and purge fabric state for an epoch",
	Long: `Archives the current epoch's fabric state into a standalone neutron SQLite file.
The archive contains entanglements, discoveries, pulses, task records, and metadata.
After archival, all state is purged from the active fabric.

Alias for: quasar fabric archive`,
	RunE: runFabricArchive,
}

// fabricArchiveCmd is the subcommand: `quasar fabric archive`.
var fabricArchiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Snapshot and purge fabric state for an epoch",
	Long: `Archives the current epoch's fabric state into a standalone neutron SQLite file.
The archive contains entanglements, discoveries, pulses, task records, and metadata.
After archival, all state is purged from the active fabric.`,
	RunE: runFabricArchive,
}

// fabricPurgeCmd discards fabric state without archiving.
var fabricPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Discard all fabric state for an abandoned epoch",
	Long: `Removes all state from the active fabric without archiving.
Use this for abandoned epochs that do not need preservation.`,
	RunE: runFabricPurge,
}

func init() {
	// Flags for archive commands.
	for _, cmd := range []*cobra.Command{archiveCmd, fabricArchiveCmd} {
		cmd.Flags().String("epoch", "", "epoch ID to archive (required)")
		cmd.Flags().String("output", "", "output path for neutron file (default: .quasar/neutrons/<epoch>.db)")
		cmd.Flags().Bool("force", false, "archive even with unresolved discoveries")
		_ = cmd.MarkFlagRequired("epoch")
	}

	// Flags for purge command.
	fabricPurgeCmd.Flags().Bool("force", false, "skip confirmation prompt")

	// Register under fabric and root.
	fabricCmd.AddCommand(fabricArchiveCmd)
	fabricCmd.AddCommand(fabricPurgeCmd)
	rootCmd.AddCommand(archiveCmd)
}

func runFabricArchive(cmd *cobra.Command, _ []string) error {
	epochID, _ := cmd.Flags().GetString("epoch")
	outputPath, _ := cmd.Flags().GetString("output")
	force, _ := cmd.Flags().GetBool("force")

	if outputPath == "" {
		outputPath = filepath.Join(".quasar", "neutrons", epochID+".db")
	}

	// Ensure the output directory exists.
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", outputDir, err)
	}

	f, err := openFabric(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	opts := neutron.ArchiveOptions{Force: force}
	n, err := neutron.Archive(cmd.Context(), f, epochID, outputPath, opts)
	if err != nil {
		return fmt.Errorf("fabric archive: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Archived epoch %q → %s (created: %s)\n",
		n.EpochID, n.DBPath, n.CreatedAt.Format("2006-01-02 15:04:05"))
	return nil
}

func runFabricPurge(cmd *cobra.Command, _ []string) error {
	force, _ := cmd.Flags().GetBool("force")

	if !force {
		fmt.Fprintf(cmd.ErrOrStderr(), "This will permanently delete all fabric state. Use --force to confirm.\n")
		return fmt.Errorf("fabric purge: requires --force flag")
	}

	f, err := openFabric(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := neutron.Purge(cmd.Context(), f); err != nil {
		return fmt.Errorf("fabric purge: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Purged all state from active fabric.\n")
	return nil
}
