package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds all runtime configuration for a quasar session.
// Values are populated from .quasar.yaml, QUASAR_* env vars, and CLI flags.
type Config struct {
	ClaudePath           string  `mapstructure:"claude_path"`
	BeadsPath            string  `mapstructure:"beads_path"`
	WorkDir              string  `mapstructure:"work_dir"`
	MaxReviewCycles      int     `mapstructure:"max_review_cycles"`
	MaxBudgetUSD         float64 `mapstructure:"max_budget_usd"`
	Model                string  `mapstructure:"model"`
	CoderSystemPrompt    string  `mapstructure:"coder_system_prompt"`
	ReviewerSystemPrompt string  `mapstructure:"reviewer_system_prompt"`
	Verbose              bool    `mapstructure:"verbose"`
}

// Load reads configuration from viper, applying built-in defaults for any
// values not set by config file, environment, or flags.
func Load() (Config, error) {
	viper.SetDefault("claude_path", "claude")
	viper.SetDefault("beads_path", "beads")
	viper.SetDefault("work_dir", ".")
	viper.SetDefault("max_review_cycles", 3)
	viper.SetDefault("max_budget_usd", 5.0)
	viper.SetDefault("model", "")
	viper.SetDefault("coder_system_prompt", "")
	viper.SetDefault("reviewer_system_prompt", "")
	viper.SetDefault("verbose", false)

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return cfg, nil
}
