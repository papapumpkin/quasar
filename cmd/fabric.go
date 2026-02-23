// Package cmd provides CLI commands for quasar.
//
// fabric.go defines the root `quasar fabric` command group and registers all
// fabric subcommands. Archive and purge are implemented in fabric_archive.go.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/papapumpkin/quasar/internal/fabric"
)

var fabricCmd = &cobra.Command{
	Use:   "fabric",
	Short: "Interact with the fabric coordination substrate",
	Long: `The fabric command group provides CLI access to the shared coordination
database used by quasar agents. Agents use these subcommands to read state,
post entanglements, manage file claims, and view changes — never touching
SQLite directly.`,
}

func init() {
	fabricCmd.PersistentFlags().String("db", "", "fabric database path (default .quasar/fabric.db)")
	fabricCmd.PersistentFlags().String("task", "", "current task ID (or QUASAR_TASK_ID env)")

	_ = viper.BindPFlag("fabric_db", fabricCmd.PersistentFlags().Lookup("db"))
	_ = viper.BindPFlag("task_id", fabricCmd.PersistentFlags().Lookup("task"))
	_ = viper.BindEnv("fabric_db", "QUASAR_FABRIC_DB")
	_ = viper.BindEnv("task_id", "QUASAR_TASK_ID")

	rootCmd.AddCommand(fabricCmd)
}

// fabricDBPath returns the resolved fabric database path from flags, env, or default.
func fabricDBPath() string {
	if p := viper.GetString("fabric_db"); p != "" {
		return p
	}
	return ".quasar/fabric.db"
}

// fabricTaskID returns the resolved task ID from flags, env, or empty string.
func fabricTaskID() string {
	return viper.GetString("task_id")
}

// openFabric opens the fabric database at the resolved path.
func openFabric(cmd *cobra.Command) (*fabric.SQLiteFabric, error) {
	dbPath := fabricDBPath()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("fabric database not found: %s", dbPath)
	}
	f, err := fabric.NewSQLiteFabric(cmd.Context(), dbPath)
	if err != nil {
		return nil, fmt.Errorf("open fabric: %w", err)
	}
	return f, nil
}

// requireTaskID returns the task ID or an error if none is configured.
func requireTaskID() (string, error) {
	id := fabricTaskID()
	if id == "" {
		return "", fmt.Errorf("task ID required: use --task flag or set QUASAR_TASK_ID")
	}
	return id, nil
}
