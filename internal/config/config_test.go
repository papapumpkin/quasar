package config

import (
	"os"
	"testing"

	"github.com/spf13/viper"
)

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
}
