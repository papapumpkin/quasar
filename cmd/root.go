package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "quasar",
	Short: "Dual-agent AI coding coordinator",
	Long:  "Quasar coordinates a coder and reviewer agent that cycle on a task until the reviewer approves.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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

	// It's fine if no config file is found; we use defaults.
	_ = viper.ReadInConfig()
}
