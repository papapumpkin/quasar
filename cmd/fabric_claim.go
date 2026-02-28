package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
)

func init() {
	claimCmd := &cobra.Command{
		Use:   "claim",
		Short: "Acquire a file claim in the fabric",
		Long: `Claims exclusive ownership of a file path for the current task.
Exits 0 on success, exits 1 with an error message if already claimed by another task.`,
		RunE: runFabricClaim,
	}
	claimCmd.Flags().String("file", "", "file path to claim (required)")
	_ = claimCmd.MarkFlagRequired("file")
	fabricCmd.AddCommand(claimCmd)

	releaseCmd := &cobra.Command{
		Use:   "release",
		Short: "Release file claims in the fabric",
		Long: `Releases file claims. Use --file to release a specific file claim,
or --all to release all claims for the current task.`,
		RunE: runFabricRelease,
	}
	releaseCmd.Flags().String("file", "", "specific file path to release")
	releaseCmd.Flags().Bool("all", false, "release all claims for the task")
	fabricCmd.AddCommand(releaseCmd)
}

func runFabricClaim(cmd *cobra.Command, _ []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	taskID, err := requireTaskID()
	if err != nil {
		return err
	}

	f, err := openFabric(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx := cmd.Context()
	if err := f.ClaimFile(ctx, filePath, taskID); err != nil {
		if errors.Is(err, fabric.ErrFileAlreadyClaimed) {
			// Show a clear message including the current owner.
			fmt.Fprintf(cmd.OutOrStdout(), "DENIED: %s\n", err)
			return err
		}
		return fmt.Errorf("fabric claim: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Claimed: %s (owner: %s)\n", filePath, taskID)
	return nil
}

func runFabricRelease(cmd *cobra.Command, _ []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	all, _ := cmd.Flags().GetBool("all")

	if filePath == "" && !all {
		return fmt.Errorf("fabric release: either --file or --all is required")
	}

	taskID, err := requireTaskID()
	if err != nil {
		return err
	}

	f, err := openFabric(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx := cmd.Context()

	if all {
		if err := f.ReleaseClaims(ctx, taskID); err != nil {
			return fmt.Errorf("fabric release all: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Released all claims for task: %s\n", taskID)
		return nil
	}

	// Release the specific file claim (only if owned by this task).
	owner, err := f.FileOwner(ctx, filePath)
	if err != nil {
		return fmt.Errorf("fabric release: check owner: %w", err)
	}
	if owner == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "File not claimed: %s\n", filePath)
		return nil
	}
	if owner != taskID {
		return fmt.Errorf("fabric release: %q is owned by %q, not %q", filePath, owner, taskID)
	}

	if err := f.ReleaseFileClaim(ctx, filePath, taskID); err != nil {
		return fmt.Errorf("fabric release: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Released: %s (was owned by: %s)\n", filePath, taskID)
	return nil
}
