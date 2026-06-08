package config

import (
	"fmt"
	"os"
	"sort"
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

// MergeGateConfig holds the [merge_gate] section of a repo's .quasar.yaml. It
// configures the cross-phase merge gate that re-verifies a merged tree before a
// phase is declared fulfilled. A repo without a [merge_gate] block — or with an
// empty field — falls back to the built-in default applied downstream
// (gitops.DefaultVerifyCommand for the command), so the Go-centric default
// works out of the box and a non-Go repo overrides it here.
//
// This block is the per-repo landing surface. The merge_attempt operator and
// gitops.MergeAttempt already honor both knobs; the remaining link — reading
// cfg.MergeGate and injecting it as the merge-gate constellation's inputs — is
// owned by the merge-gate-firing supervisor and tracked in
// docs/constellation-runtime-followup.md.
type MergeGateConfig struct {
	// VerifyCommand runs in the merge worktree after a clean merge to confirm
	// the merged tree still builds and tests pass. Empty uses the built-in
	// default.
	VerifyCommand string `mapstructure:"verify_command"`
	// VerifyTimeout caps the verify command so a runaway build cannot block the
	// gate forever. Parsed as a Go duration (e.g. "5m"); the merge_attempt
	// operator converts it to a deadline gitops.MergeAttempt enforces. Empty
	// disables the cap.
	VerifyTimeout string `mapstructure:"verify_timeout"`
}

// GitHubConfig holds the [github] section of a repo's .quasar.yaml. It carries
// top-level repo settings that are not adapter-specific (the GitHub ticket and
// forge adapters keep their own [integrations.github]/[forge.github] blocks).
type GitHubConfig struct {
	// BaseBranch is the branch PRs target and worktrees branch from (e.g. "main").
	BaseBranch string `mapstructure:"base_branch"`
}

// CockpitConfig holds the [cockpit] section of a repo's .quasar.yaml. It
// configures the optional React cockpit served from the supervisor process.
// The cockpit is off by default in v1: with Enabled false, cmd/serve.go does
// not register the HTTP routes and the embedded bundle is excluded from the
// build (the //go:embed directive is guarded by the `cockpit` build tag).
type CockpitConfig struct {
	// Enabled turns the cockpit HTTP server on. Default: false.
	Enabled bool `mapstructure:"enabled"`
	// Addr is the listen address (host:port). Default: "127.0.0.1:7330".
	Addr string `mapstructure:"addr"`
	// TrustedProxies lists CIDRs/hosts trusted for X-Forwarded-For when the
	// cockpit runs behind a reverse proxy. Empty disables proxy trust.
	TrustedProxies []string `mapstructure:"trusted_proxies"`
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
	// adapter name (e.g. "github"). This block is a legacy surface from the
	// on-demand ticket-source model that the sensor-driven model replaced: no
	// new code path acts on it, and sensors are now configured as TOML files
	// under <repo>/sensors/. Load() retains the field so legacy files still
	// parse and emits a deprecation warning when a block is present (see
	// warnDeprecatedIntegrations); `quasar doctor` still surfaces it as a
	// diagnostic. Empty when no integrations are configured.
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

	// GitHub holds the [github] section: top-level repo settings such as the
	// base branch. Empty fields fall back to the consumer's own defaults.
	GitHub GitHubConfig `mapstructure:"github"`

	// GC holds the [gc] section: garbage-collection TTLs and grace window for
	// the global SQLite store. Defaults are conservative (see DefaultGCConfig).
	GC GCConfig `mapstructure:"gc"`

	// MergeGate holds the [merge_gate] section: the cross-phase merge gate's
	// verify command and timeout override. Empty fields use built-in defaults.
	MergeGate MergeGateConfig `mapstructure:"merge_gate"`

	// Cockpit holds the [cockpit] section: the optional React cockpit server.
	// Disabled by default; see CockpitConfig.
	Cockpit CockpitConfig `mapstructure:"cockpit"`
}

// ErrInlineToken indicates an integration or forge section stored a secret
// inline. Tokens must be supplied via token_env or token_file, never written
// to .quasar.yaml.
var ErrInlineToken = fmt.Errorf("inline token is not allowed; use token_env or token_file")

// Load reads configuration from the global viper instance, applying built-in
// defaults for any values not set by config file, environment, or flags. It is
// the single-repo entry point wired through the root command's initConfig.
func Load() (Config, error) {
	return loadFrom(viper.GetViper())
}

// Default returns the configuration with only built-in defaults applied — the
// same values LoadFromPath produces for an empty .quasar.yaml. Callers that may
// not have a config file (e.g. the repos resolver for a repo without a
// .quasar.yaml) use it so "no file" and "empty file" converge on identical
// built-in defaults rather than a zero-valued Config.
func Default() (Config, error) {
	return loadFrom(viper.New())
}

// LoadFromPath reads configuration from an explicit .quasar.yaml file, isolated
// from the global viper instance. The resolver uses it to load a registered
// repo's config without disturbing the process-wide single-repo config that
// Load() depends on. The same defaults and inline-token guardrail apply.
func LoadFromPath(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	return loadFrom(v)
}

// setDefaults registers the built-in defaults on v. Centralized so Load() and
// LoadFromPath() stay in sync.
func setDefaults(v *viper.Viper) {
	v.SetDefault("claude_path", "claude")
	v.SetDefault("beads_path", "beads")
	v.SetDefault("work_dir", ".")
	v.SetDefault("max_review_cycles", 3)
	v.SetDefault("max_budget_usd", 5.0)
	v.SetDefault("model", "")
	v.SetDefault("coder_system_prompt", "")
	v.SetDefault("reviewer_system_prompt", "")
	v.SetDefault("verbose", false)
	v.SetDefault("lint_commands", DefaultLintCommands)
	v.SetDefault("cache_optimization", true)
	v.SetDefault("cache_verbose", false)
	v.SetDefault("project_context_path", "")
	v.SetDefault("max_context_tokens", 10000)
	v.SetDefault("fix_effort", "low")
	v.SetDefault("fallback_model", "")
	v.SetDefault("pre_commit.fail_on_error", true)
	v.SetDefault("cockpit.enabled", false)
	v.SetDefault("cockpit.addr", "127.0.0.1:7330")
	setGCDefaults(v)
}

// loadFrom applies defaults, unmarshals, and enforces the inline-token
// guardrail against the supplied viper instance.
func loadFrom(v *viper.Viper) (Config, error) {
	setDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Tokens must never live in .quasar.yaml. Reject any key named `token`
	// (case-insensitive) anywhere in the document — top-level or nested under
	// any section.
	if err := checkInlineTokens("", v.AllSettings()); err != nil {
		return Config{}, err
	}

	if err := cfg.GC.Validate(); err != nil {
		return Config{}, err
	}

	warnDeprecatedIntegrations(cfg)

	return cfg, nil
}

// warnDeprecatedIntegrations emits a deprecation notice to stderr when a legacy
// [integrations.*] block is present. The sensor-driven model configures sensors
// via per-repo TOML files under <repo>/sensors/; the old block is ignored by
// all new code paths but tolerated (not a load error) so pre-existing
// .quasar.yaml files keep loading.
func warnDeprecatedIntegrations(cfg Config) {
	if len(cfg.IntegrationSections) == 0 {
		return
	}
	names := make([]string, 0, len(cfg.IntegrationSections))
	for name := range cfg.IntegrationSections {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic warning text
	fmt.Fprintf(os.Stderr,
		"warning: deprecated [integrations.*] block(s) found (%s); they are ignored. Configure sensors as TOML files under <repo>/sensors/ instead.\n",
		strings.Join(names, ", "))
}

// checkInlineTokens recursively walks the decoded settings and returns
// ErrInlineToken (wrapped with the offending dotted path) if any key is named
// `token`. The check is case-insensitive so `Token`, `TOKEN`, etc. are all
// rejected. prefix is the dotted path to settings (empty at the top level).
func checkInlineTokens(prefix string, settings map[string]any) error {
	for key, val := range settings {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if strings.EqualFold(key, "token") {
			return fmt.Errorf("config: [%s] contains an inline token: %w", path, ErrInlineToken)
		}
		if nested, ok := val.(map[string]any); ok {
			if err := checkInlineTokens(path, nested); err != nil {
				return err
			}
		}
	}
	return nil
}
