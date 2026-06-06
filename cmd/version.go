package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Version = "0.1.0"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		// arch-test: stdout-allowed — version is machine-readable output.
		fmt.Printf("quasar %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
