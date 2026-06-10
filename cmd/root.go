package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// exitCodeError carries a process exit code alongside an underlying error so a
// command can request a non-default exit status (e.g. exit 2 for "ticket not
// found") while still surfacing its message. Execute inspects it via
// errors.As; any error that is not an *exitCodeError exits 1.
type exitCodeError struct {
	code int
	err  error
}

// Error returns the underlying error's message unchanged.
func (e *exitCodeError) Error() string { return e.err.Error() }

// Unwrap exposes the underlying error for errors.Is/As traversal.
func (e *exitCodeError) Unwrap() error { return e.err }

// newExitError wraps err so Execute terminates with the given exit code.
func newExitError(code int, err error) error {
	return &exitCodeError{code: code, err: err}
}

var rootCmd = &cobra.Command{
	Use:   "quasar",
	Short: "Dual-agent AI coding coordinator",
	Long:  "Quasar coordinates a coder and reviewer agent that cycle on a task until the reviewer approves.",
	RunE:  runRootDefault,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code := 1
		var ec *exitCodeError
		if errors.As(err, &ec) {
			code = ec.code
		}
		os.Exit(code)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().String("config", "", "config file (default .quasar.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
}

func initConfig() {
	if cfgFile, _ := rootCmd.Flags().GetString("config"); cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName(".quasar")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(home)
		}
	}

	viper.SetEnvPrefix("QUASAR")
	viper.AutomaticEnv()

	// A missing config file is fine — we fall back to defaults. A config file
	// that exists but fails to parse is NOT: silently using defaults would mask
	// a real misconfiguration (wrong budget cap, missing sensor/pre-commit
	// config). This init runs before the Bubble Tea altscreen, so stderr is
	// safe here.
	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			fmt.Fprintf(os.Stderr, "quasar: failed to read config %q: %v\n", viper.ConfigFileUsed(), err)
		}
	}
}

// runRootDefault auto-launches the TUI when .nebulas/ exists in the cwd.
// If .nebulas/ is not found, it falls back to showing help.
func runRootDefault(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return cmd.Help()
	}
	nebulaeDir := filepath.Join(wd, ".nebulas")
	if _, err := os.Stat(nebulaeDir); os.IsNotExist(err) {
		return cmd.Help()
	} else if err != nil {
		return fmt.Errorf("failed to access %s: %w", nebulaeDir, err)
	}
	// Delegate to the cockpit subcommand.
	return runTUI(cockpitCmd, nil)
}
