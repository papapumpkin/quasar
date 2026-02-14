package config

import "github.com/spf13/viper"

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

func Load() Config {
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
	_ = viper.Unmarshal(&cfg)
	return cfg
}
