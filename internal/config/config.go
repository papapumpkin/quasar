package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// DefaultLintCommands are the lint commands executed after each coder pass.
var DefaultLintCommands = []string{"go vet ./...", "go fmt ./..."}

// PreCommitConfig holds per-repo commands run before every Quasar commit
// (formatters/linters such as gofmt, prettier, ruff). The schema is reserved
// here; consumption lands in a later phase.
type PreCommitConfig struct {
	// Commands run with the worktree as CWD, in order, before each commit.
	Commands []string `mapstructure:"commands"`

	// FailOnError aborts the commit when any pre-commit command exits non-zero.
	// Defaults to true; reserved here for the later consumption phase.
	FailOnError bool `mapstructure:"fail_on_error"`
}

// VerifyConfig holds the project's verification gate commands. Each field is a
// single shell command (e.g. "go test ./..."). Empty fields disable that gate.
// The schema is reserved here; enforcement of the gates lands in a later nebula
// (the master review/PR loop). `quasar init` populates it from the detected
// language and `quasar doctor` reports which gates are configured.
type VerifyConfig struct {
	Test  string `mapstructure:"test"`
	Lint  string `mapstructure:"lint"`
	Build string `mapstructure:"build"`
}

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

	// IntegrationSections holds the parsed [integrations.*] blocks keyed by
	// adapter name (e.g. "github"). Each section is stored opaquely as a
	// map so adding a new adapter requires no parser change — strong-typing
	// happens inside each adapter's constructor. Empty when no integrations
	// are configured.
	IntegrationSections map[string]map[string]any `mapstructure:"integrations"`

	// ForgeSections holds the parsed [forge.*] blocks keyed by forge name.
	// Stored opaquely for the same reason as IntegrationSections. The forge
	// surface is reserved in this phase; full PR-creation methods land later.
	ForgeSections map[string]map[string]any `mapstructure:"forge"`

	// PreCommit holds the [pre_commit] section. Reserved here; consumption
	// (running the commands before each commit) lands in a later phase.
	PreCommit PreCommitConfig `mapstructure:"pre_commit"`

	// Verify holds the [verify] section: per-project test/lint/build gate
	// commands. Reserved here; enforcement lands in a later nebula. Empty
	// fields (the default) mean the corresponding gate is not configured.
	Verify VerifyConfig `mapstructure:"verify"`
}

// ErrInlineToken indicates an integration or forge section stored a secret
// inline. Tokens must be supplied via token_env or token_file, never written
// to .quasar.yaml.
var ErrInlineToken = fmt.Errorf("inline token is not allowed; use token_env or token_file")

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

	// Tokens must never live in .quasar.yaml. Reject any integration or forge
	// section that contains an inline `token` key (case-insensitive).
	if err := checkInlineTokens("integrations", cfg.IntegrationSections); err != nil {
		return Config{}, err
	}
	if err := checkInlineTokens("forge", cfg.ForgeSections); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// checkInlineTokens returns ErrInlineToken (wrapped with the offending section
// name) if any section under kind defines a `token` key. The check is
// case-insensitive so `Token`, `TOKEN`, etc. are all rejected.
func checkInlineTokens(kind string, sections map[string]map[string]any) error {
	for name, section := range sections {
		for key := range section {
			if strings.EqualFold(key, "token") {
				return fmt.Errorf("config: [%s.%s] contains an inline token: %w", kind, name, ErrInlineToken)
			}
		}
	}
	return nil
}
