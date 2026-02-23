package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
)

var pulseCmd = &cobra.Command{
	Use:   "pulse",
	Short: "Manage shared execution context pulses",
	Long: `The pulse command group lets quasars emit and query shared execution
context. Pulses propagate through the fabric so concurrent and downstream
quasars share decisions, notes, failures, and reviewer feedback without
direct communication.`,
}

func init() {
	// emit subcommand
	emitCmd := &cobra.Command{
		Use:   "emit [content]",
		Short: "Emit a pulse to the fabric",
		Long: `Emits a pulse (shared execution context) to the fabric database.

  --kind     Required: note, decision, failure, reviewer_feedback
  --task     Source task ID (or QUASAR_TASK_ID env)
  --db       Fabric database path (or QUASAR_FABRIC_DB env)

Prints the pulse ID to stdout on success.`,
		Args: cobra.ExactArgs(1),
		RunE: runPulseEmit,
	}
	emitCmd.Flags().String("kind", "", "pulse kind: note, decision, failure, reviewer_feedback (required)")
	emitCmd.Flags().String("task", os.Getenv("QUASAR_TASK_ID"), "source task ID (or QUASAR_TASK_ID env)")
	emitCmd.Flags().String("db", os.Getenv("QUASAR_FABRIC_DB"), "fabric database path (or QUASAR_FABRIC_DB env)")
	_ = emitCmd.MarkFlagRequired("kind")

	// list subcommand
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List pulses from the fabric",
		Long: `Lists pulses stored in the fabric, optionally filtered by task ID.

  --task     Filter by task ID (optional)
  --db       Fabric database path (or QUASAR_FABRIC_DB env)`,
		RunE: runPulseList,
	}
	listCmd.Flags().String("task", "", "filter by task ID (optional)")
	listCmd.Flags().String("db", os.Getenv("QUASAR_FABRIC_DB"), "fabric database path (or QUASAR_FABRIC_DB env)")

	pulseCmd.AddCommand(emitCmd)
	pulseCmd.AddCommand(listCmd)
	rootCmd.AddCommand(pulseCmd)
}

func runPulseEmit(cmd *cobra.Command, args []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	task, _ := cmd.Flags().GetString("task")
	dbPath, _ := cmd.Flags().GetString("db")
	content := args[0]

	if err := fabric.ValidatePulseKind(kind); err != nil {
		return err
	}
	if task == "" {
		return fmt.Errorf("task ID required: use --task or set QUASAR_TASK_ID")
	}
	if dbPath == "" {
		return fmt.Errorf("fabric database path required: use --db or set QUASAR_FABRIC_DB")
	}

	ctx := cmd.Context()
	f, err := fabric.NewSQLiteFabric(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open fabric: %w", err)
	}
	defer f.Close()

	p := fabric.Pulse{
		TaskID:  task,
		Content: content,
		Kind:    kind,
	}

	id, err := f.EmitPulseReturningID(ctx, p)
	if err != nil {
		return fmt.Errorf("emit pulse: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), id)
	return nil
}

func runPulseList(cmd *cobra.Command, _ []string) error {
	task, _ := cmd.Flags().GetString("task")
	dbPath, _ := cmd.Flags().GetString("db")

	if dbPath == "" {
		return fmt.Errorf("fabric database path required: use --db or set QUASAR_FABRIC_DB")
	}

	ctx := cmd.Context()
	f, err := fabric.NewSQLiteFabric(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open fabric: %w", err)
	}
	defer f.Close()

	var pulses []fabric.Pulse
	if task != "" {
		pulses, err = f.PulsesFor(ctx, task)
	} else {
		pulses, err = f.AllPulses(ctx)
	}
	if err != nil {
		return fmt.Errorf("query pulses: %w", err)
	}

	out := cmd.OutOrStdout()
	for i, p := range pulses {
		if i > 0 {
			fmt.Fprintln(out)
		}
		ts := p.CreatedAt.Format("15:04:05")
		fmt.Fprintf(out, "[%s] %s (%s)\n", ts, p.Kind, p.TaskID)
		fmt.Fprintln(out, p.Content)
	}

	return nil
}
