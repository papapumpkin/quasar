package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// loadFromYAML resets viper, loads the given YAML document into it, and calls
// Load(). It centralizes the global-viper setup the config layer relies on.
func loadFromYAML(t *testing.T, yaml string) (Config, error) {
	t.Helper()
	resetViper()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	return Load()
}

func TestLoad_IntegrationSectionsRoundTrip(t *testing.T) {
	const yaml = `
integrations:
  github:
    repo: "papapumpkin/quasar"
    token_env: "GITHUB_TOKEN"
    token_file: ""
forge:
  github:
    repo: "papapumpkin/quasar"
    base_branch: "main"
    token_env: "GITHUB_TOKEN"
pre_commit:
  commands:
    - "gofmt -l ."
    - "go vet ./..."
`
	cfg, err := loadFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	gh, ok := cfg.IntegrationSections["github"]
	if !ok {
		t.Fatalf("IntegrationSections missing github key: %+v", cfg.IntegrationSections)
	}
	if gh["repo"] != "papapumpkin/quasar" {
		t.Errorf("github.repo = %v, want papapumpkin/quasar", gh["repo"])
	}
	if gh["token_env"] != "GITHUB_TOKEN" {
		t.Errorf("github.token_env = %v, want GITHUB_TOKEN", gh["token_env"])
	}

	if _, ok := cfg.ForgeSections["github"]; !ok {
		t.Errorf("ForgeSections missing github key: %+v", cfg.ForgeSections)
	}

	if len(cfg.PreCommit.Commands) != 2 {
		t.Fatalf("PreCommit.Commands = %v, want 2 entries", cfg.PreCommit.Commands)
	}
	if cfg.PreCommit.Commands[0] != "gofmt -l ." {
		t.Errorf("PreCommit.Commands[0] = %q, want %q", cfg.PreCommit.Commands[0], "gofmt -l .")
	}
}

func TestLoad_EmptySectionsDefaultEmpty(t *testing.T) {
	cfg, err := loadFromYAML(t, "max_review_cycles: 4\n")
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if len(cfg.IntegrationSections) != 0 {
		t.Errorf("IntegrationSections = %v, want empty", cfg.IntegrationSections)
	}
	if len(cfg.ForgeSections) != 0 {
		t.Errorf("ForgeSections = %v, want empty", cfg.ForgeSections)
	}
	if len(cfg.PreCommit.Commands) != 0 {
		t.Errorf("PreCommit.Commands = %v, want empty", cfg.PreCommit.Commands)
	}
	if !cfg.PreCommit.FailOnError {
		t.Error("PreCommit.FailOnError = false, want true (default)")
	}
}

func TestLoad_PreCommitFailOnErrorOverride(t *testing.T) {
	cfg, err := loadFromYAML(t, "pre_commit:\n  fail_on_error: false\n")
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.PreCommit.FailOnError {
		t.Error("PreCommit.FailOnError = true, want false when explicitly disabled")
	}
}

func TestLoad_InlineTokenRejected(t *testing.T) {
	cases := map[string]string{
		"integrations": "integrations:\n  github:\n    token: \"ghp_xxx\"\n",
		"forge":        "forge:\n  github:\n    token: \"ghp_xxx\"\n",
		"uppercase":    "integrations:\n  github:\n    TOKEN: \"ghp_xxx\"\n",
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadFromYAML(t, yaml)
			if err == nil {
				t.Fatal("expected inline-token error, got nil")
			}
			if !errors.Is(err, ErrInlineToken) {
				t.Errorf("error = %v, want wrap of ErrInlineToken", err)
			}
			if !strings.Contains(err.Error(), "token_env") {
				t.Errorf("error %q should mention token_env/token_file", err.Error())
			}
		})
	}
}

// writeConfig writes body to a .quasar.yaml inside a fresh temp dir and returns
// the file path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".quasar.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestDefault_MatchesEmptyConfigFile(t *testing.T) {
	// Default() must produce exactly the built-in defaults a caller would get
	// from LoadFromPath against an empty .quasar.yaml — the resolver relies on
	// this so a config-less repo and an empty-config repo converge.
	def, err := Default()
	if err != nil {
		t.Fatalf("Default returned unexpected error: %v", err)
	}

	empty := writeConfig(t, "")
	fromFile, err := LoadFromPath(empty)
	if err != nil {
		t.Fatalf("LoadFromPath(empty) returned unexpected error: %v", err)
	}

	if def.MaxReviewCycles != 3 {
		t.Errorf("Default().MaxReviewCycles = %d, want 3", def.MaxReviewCycles)
	}
	if def.MaxBudgetUSD != 5.0 {
		t.Errorf("Default().MaxBudgetUSD = %v, want 5.0", def.MaxBudgetUSD)
	}
	if !def.PreCommit.FailOnError {
		t.Error("Default().PreCommit.FailOnError = false, want true")
	}
	if def.MaxReviewCycles != fromFile.MaxReviewCycles ||
		def.MaxBudgetUSD != fromFile.MaxBudgetUSD ||
		def.PreCommit.FailOnError != fromFile.PreCommit.FailOnError {
		t.Errorf("Default() = %+v diverges from empty-file config %+v", def, fromFile)
	}
}

func TestLoadFromPath_RoundTrip(t *testing.T) {
	const yaml = `
github:
  base_branch: "develop"
pre_commit:
  commands:
    - "gofmt -w ."
    - "go vet ./..."
  fail_on_error: true
verify:
  test: "go test ./..."
  lint: "go vet ./..."
  build: "go build ./..."
`
	path := writeConfig(t, yaml)

	cfg, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath returned unexpected error: %v", err)
	}

	if cfg.GitHub.BaseBranch != "develop" {
		t.Errorf("GitHub.BaseBranch = %q, want develop", cfg.GitHub.BaseBranch)
	}
	if len(cfg.PreCommit.Commands) != 2 {
		t.Fatalf("PreCommit.Commands = %v, want 2 entries", cfg.PreCommit.Commands)
	}
	if cfg.PreCommit.Commands[0] != "gofmt -w ." {
		t.Errorf("PreCommit.Commands[0] = %q, want %q", cfg.PreCommit.Commands[0], "gofmt -w .")
	}
	if !cfg.PreCommit.FailOnError {
		t.Error("PreCommit.FailOnError = false, want true")
	}
	if cfg.Verify.Test != "go test ./..." {
		t.Errorf("Verify.Test = %q, want %q", cfg.Verify.Test, "go test ./...")
	}
	if cfg.Verify.Build != "go build ./..." {
		t.Errorf("Verify.Build = %q, want %q", cfg.Verify.Build, "go build ./...")
	}
	// Defaults still apply for unset keys.
	if cfg.MaxReviewCycles != 3 {
		t.Errorf("MaxReviewCycles = %d, want default 3", cfg.MaxReviewCycles)
	}
}

func TestLoadFromPath_InlineTokenRejected(t *testing.T) {
	cases := map[string]string{
		"top_level":   "token: \"ghp_xxx\"\n",
		"nested":      "integrations:\n  github:\n    token: \"ghp_xxx\"\n",
		"uppercase":   "forge:\n  github:\n    TOKEN: \"ghp_xxx\"\n",
		"deep_nested": "github:\n  auth:\n    Token: \"ghp_xxx\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, body)
			_, err := LoadFromPath(path)
			if err == nil {
				t.Fatal("expected inline-token error, got nil")
			}
			if !errors.Is(err, ErrInlineToken) {
				t.Errorf("error = %v, want wrap of ErrInlineToken", err)
			}
			if !strings.Contains(err.Error(), "token_env") {
				t.Errorf("error %q should mention token_env/token_file", err.Error())
			}
		})
	}
}

func TestLoadFromPath_MissingFile(t *testing.T) {
	_, err := LoadFromPath(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// resetViper clears all viper state between tests to avoid cross-contamination.
func resetViper() {
	viper.Reset()
}

func TestLoad_Defaults(t *testing.T) {
	resetViper()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"ClaudePath", cfg.ClaudePath, "claude"},
		{"BeadsPath", cfg.BeadsPath, "beads"},
		{"WorkDir", cfg.WorkDir, "."},
		{"MaxReviewCycles", cfg.MaxReviewCycles, 3},
		{"MaxBudgetUSD", cfg.MaxBudgetUSD, 5.0},
		{"Model", cfg.Model, ""},
		{"CoderSystemPrompt", cfg.CoderSystemPrompt, ""},
		{"ReviewerSystemPrompt", cfg.ReviewerSystemPrompt, ""},
		{"Verbose", cfg.Verbose, false},
		{"CacheOptimization", cfg.CacheOptimization, true},
		{"CacheVerbose", cfg.CacheVerbose, false},
		{"ProjectContextPath", cfg.ProjectContextPath, ""},
		{"MaxContextTokens", cfg.MaxContextTokens, 10000},
		{"FixEffort", cfg.FixEffort, "low"},
		{"FallbackModel", cfg.FallbackModel, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	resetViper()

	tests := []struct {
		name   string
		envKey string
		envVal string
		field  func(Config) any
		want   any
	}{
		{
			name:   "claude_path",
			envKey: "QUASAR_CLAUDE_PATH",
			envVal: "/usr/local/bin/claude",
			field:  func(c Config) any { return c.ClaudePath },
			want:   "/usr/local/bin/claude",
		},
		{
			name:   "beads_path",
			envKey: "QUASAR_BEADS_PATH",
			envVal: "/opt/beads",
			field:  func(c Config) any { return c.BeadsPath },
			want:   "/opt/beads",
		},
		{
			name:   "work_dir",
			envKey: "QUASAR_WORK_DIR",
			envVal: "/tmp/work",
			field:  func(c Config) any { return c.WorkDir },
			want:   "/tmp/work",
		},
		{
			name:   "max_review_cycles",
			envKey: "QUASAR_MAX_REVIEW_CYCLES",
			envVal: "7",
			field:  func(c Config) any { return c.MaxReviewCycles },
			want:   7,
		},
		{
			name:   "max_budget_usd",
			envKey: "QUASAR_MAX_BUDGET_USD",
			envVal: "10.50",
			field:  func(c Config) any { return c.MaxBudgetUSD },
			want:   10.50,
		},
		{
			name:   "model",
			envKey: "QUASAR_MODEL",
			envVal: "opus",
			field:  func(c Config) any { return c.Model },
			want:   "opus",
		},
		{
			name:   "verbose",
			envKey: "QUASAR_VERBOSE",
			envVal: "true",
			field:  func(c Config) any { return c.Verbose },
			want:   true,
		},
		{
			name:   "cache_optimization_disabled",
			envKey: "QUASAR_CACHE_OPTIMIZATION",
			envVal: "false",
			field:  func(c Config) any { return c.CacheOptimization },
			want:   false,
		},
		{
			name:   "cache_verbose",
			envKey: "QUASAR_CACHE_VERBOSE",
			envVal: "true",
			field:  func(c Config) any { return c.CacheVerbose },
			want:   true,
		},
		{
			name:   "project_context_path",
			envKey: "QUASAR_PROJECT_CONTEXT_PATH",
			envVal: "/tmp/my-context.md",
			field:  func(c Config) any { return c.ProjectContextPath },
			want:   "/tmp/my-context.md",
		},
		{
			name:   "max_context_tokens",
			envKey: "QUASAR_MAX_CONTEXT_TOKENS",
			envVal: "20000",
			field:  func(c Config) any { return c.MaxContextTokens },
			want:   20000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetViper()
			// Set env prefix so QUASAR_* env vars map to config keys.
			viper.SetEnvPrefix("QUASAR")
			viper.AutomaticEnv()

			os.Setenv(tt.envKey, tt.envVal)
			defer os.Unsetenv(tt.envKey)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned unexpected error: %v", err)
			}
			got := tt.field(cfg)
			if got != tt.want {
				t.Errorf("%s: got %v (%T), want %v (%T)", tt.name, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestLoad_DefaultsAreNotZero(t *testing.T) {
	resetViper()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.ClaudePath == "" {
		t.Error("ClaudePath should not be empty")
	}
	if cfg.BeadsPath == "" {
		t.Error("BeadsPath should not be empty")
	}
	if cfg.WorkDir == "" {
		t.Error("WorkDir should not be empty")
	}
	if cfg.MaxReviewCycles == 0 {
		t.Error("MaxReviewCycles should not be zero")
	}
	if cfg.MaxBudgetUSD == 0 {
		t.Error("MaxBudgetUSD should not be zero")
	}
	if !cfg.CacheOptimization {
		t.Error("CacheOptimization should default to true")
	}
	if cfg.MaxContextTokens == 0 {
		t.Error("MaxContextTokens should not be zero")
	}
}
