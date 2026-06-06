package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/tui/fleet"
)

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

	statePath := filepath.Join(filepath.Dir(dbPath), "tui-state.json")
	model := fleet.NewModel(cmd.Context(), fleet.NewStore(fab.DB()), statePath)

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("fleet TUI error: %w", err)
	}
	return nil
}
