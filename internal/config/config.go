package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// DefaultLintCommands are the lint commands executed after each coder pass.
var DefaultLintCommands = []string{"go vet ./...", "go fmt ./..."}

// Config holds all runtime configuration for a quasar session.
// Values are populated from .quasar.yaml, QUASAR_* env vars, and CLI flags.
type Config struct {
	ClaudePath           string   `mapstructure:"claude_path"`
	BeadsPath            string   `mapstructure:"beads_path"`
	WorkDir              string   `mapstructure:"work_dir"`
	MaxReviewCycles      int      `mapstructure:"max_review_cycles"`
	MaxBudgetUSD         float64  `mapstructure:"max_budget_usd"`
	Model                string   `mapstructure:"model"`
	CoderSystemPrompt    string   `mapstructure:"coder_system_prompt"`
	ReviewerSystemPrompt string   `mapstructure:"reviewer_system_prompt"`
	Verbose              bool     `mapstructure:"verbose"`
	LintCommands         []string `mapstructure:"lint_commands"`

	// CacheOptimization enables prompt cache optimization. When true, system
	// prompts include a stable project-context prefix that is byte-identical
	// across invocations within a phase for Anthropic prompt cache hits.
	// Default: true.
	CacheOptimization bool `mapstructure:"cache_optimization"`

	// CacheVerbose enables detailed cache-related logging to stderr,
	// independent of the global Verbose flag. Useful for diagnosing
	// cache effectiveness without full verbose output.
	// Default: false.
	CacheVerbose bool `mapstructure:"cache_verbose"`

	// ProjectContextPath overrides automatic Scanner.Scan() with a static
	// file whose contents are used as the project context prefix. When set,
	// the scanner is not invoked and this file is read instead.
	// Default: "" (use Scanner.Scan()).
	ProjectContextPath string `mapstructure:"project_context_path"`

	// MaxContextTokens sets the token budget for context injection.
	// Controls how much project context and fabric state is included.
	// Default: 10000.
	MaxContextTokens int `mapstructure:"max_context_tokens"`

	// FixEffort is the effort level for lint/filter fix invocations.
	// Valid values: "low", "medium", "high", or "" (Claude's default).
	// Default: "low".
	FixEffort string `mapstructure:"fix_effort"`

	// FallbackModel is the automatic fallback model when the primary
	// model is overloaded. Empty means no fallback.
	// Default: "".
	FallbackModel string `mapstructure:"fallback_model"`
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
	viper.SetDefault("lint_commands", DefaultLintCommands)
	viper.SetDefault("cache_optimization", true)
	viper.SetDefault("cache_verbose", false)
	viper.SetDefault("project_context_path", "")
	viper.SetDefault("max_context_tokens", 10000)
	viper.SetDefault("fix_effort", "low")
	viper.SetDefault("fallback_model", "")

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return cfg, nil
}
