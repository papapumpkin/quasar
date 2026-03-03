package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/config"
)

func TestResolvePrompts_Defaults(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	coder, reviewer := resolvePrompts(cfg)

	if coder != agent.DefaultCoderSystemPrompt {
		t.Errorf("coder prompt = %q, want default", coder)
	}
	if reviewer != agent.DefaultReviewerSystemPrompt {
		t.Errorf("reviewer prompt = %q, want default", reviewer)
	}
}

func TestResolvePrompts_CustomOverrides(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		CoderSystemPrompt:    "custom coder",
		ReviewerSystemPrompt: "custom reviewer",
	}
	coder, reviewer := resolvePrompts(cfg)

	if coder != "custom coder" {
		t.Errorf("coder prompt = %q, want %q", coder, "custom coder")
	}
	if reviewer != "custom reviewer" {
		t.Errorf("reviewer prompt = %q, want %q", reviewer, "custom reviewer")
	}
}

func TestEngineConfigFromSettings(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		ClaudePath:           "/usr/local/bin/claude",
		BeadsPath:            "/usr/local/bin/bd",
		WorkDir:              "/tmp/work",
		MaxReviewCycles:      5,
		MaxBudgetUSD:         10.0,
		Model:                "opus",
		CoderSystemPrompt:    "custom coder",
		ReviewerSystemPrompt: "custom reviewer",
		Verbose:              true,
		LintCommands:         []string{"go vet ./..."},
		CacheOptimization:    true,
		CacheVerbose:         false,
	}

	ecfg := engineConfigFromSettings(cfg, "/path/to/nebula", true, 4, true)

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"NebulaDir", ecfg.NebulaDir, "/path/to/nebula"},
		{"WorkDir", ecfg.WorkDir, "/tmp/work"},
		{"MaxWorkers", ecfg.MaxWorkers, 4},
		{"MaxWorkersExplicit", ecfg.MaxWorkersExplicit, true},
		{"MaxReviewCycles", ecfg.MaxReviewCycles, 5},
		{"MaxBudgetUSD", ecfg.MaxBudgetUSD, 10.0},
		{"Model", ecfg.Model, "opus"},
		{"CoderPrompt", ecfg.CoderPrompt, "custom coder"},
		{"ReviewerPrompt", ecfg.ReviewerPrompt, "custom reviewer"},
		{"Verbose", ecfg.Verbose, true},
		{"Auto", ecfg.Auto, true},
		{"UseTUI", ecfg.UseTUI, true},
		{"NoSplash", ecfg.NoSplash, true},
		{"ClaudePath", ecfg.ClaudePath, "/usr/local/bin/claude"},
		{"BeadsPath", ecfg.BeadsPath, "/usr/local/bin/bd"},
		{"CacheOptimization", ecfg.CacheOptimization, true},
		{"CacheVerbose", ecfg.CacheVerbose, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}

	// Verify lint commands are passed through.
	if len(ecfg.LintCommands) != 1 || ecfg.LintCommands[0] != "go vet ./..." {
		t.Errorf("LintCommands = %v, want [go vet ./...]", ecfg.LintCommands)
	}

	// Verify Resume is false (cockpit doesn't support resume).
	if ecfg.Resume {
		t.Error("Resume should be false for cockpit path")
	}
}

func TestEngineConfigFromSettings_DefaultPrompts(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	ecfg := engineConfigFromSettings(cfg, "/path", false, 1, false)

	if ecfg.CoderPrompt != agent.DefaultCoderSystemPrompt {
		t.Errorf("coder prompt should be default when config is empty")
	}
	if ecfg.ReviewerPrompt != agent.DefaultReviewerSystemPrompt {
		t.Errorf("reviewer prompt should be default when config is empty")
	}
}

// TestResolveEngineConfig exercises resolveEngineConfig with various flag
// combinations. Tests run sequentially (not parallel) because
// resolveEngineConfig calls config.Load() which uses viper's global state.
func TestResolveEngineConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cmd := newTestNebulaApplyCmd()
		if err := cmd.ParseFlags([]string{}); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		ecfg, err := resolveEngineConfig(cmd, []string{"/path/to/nebula"})
		if err != nil {
			t.Fatalf("resolveEngineConfig: %v", err)
		}

		if ecfg.NebulaDir != "/path/to/nebula" {
			t.Errorf("NebulaDir = %q, want %q", ecfg.NebulaDir, "/path/to/nebula")
		}
		if ecfg.Auto {
			t.Error("Auto should be false by default")
		}
		if ecfg.Resume {
			t.Error("Resume should be false by default")
		}
		if ecfg.UseTUI {
			t.Error("UseTUI should be false when Auto is false")
		}
		if ecfg.CoderPrompt == "" {
			t.Error("CoderPrompt should not be empty")
		}
		if ecfg.ReviewerPrompt == "" {
			t.Error("ReviewerPrompt should not be empty")
		}
	})

	t.Run("auto flag", func(t *testing.T) {
		cmd := newTestNebulaApplyCmd()
		if err := cmd.ParseFlags([]string{"--auto"}); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		ecfg, err := resolveEngineConfig(cmd, []string{"/path/to/nebula"})
		if err != nil {
			t.Fatalf("resolveEngineConfig: %v", err)
		}

		if !ecfg.Auto {
			t.Error("Auto should be true when --auto is set")
		}
	})

	t.Run("resume without auto is ignored", func(t *testing.T) {
		cmd := newTestNebulaApplyCmd()
		if err := cmd.ParseFlags([]string{"--resume"}); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		ecfg, err := resolveEngineConfig(cmd, []string{"/path/to/nebula"})
		if err != nil {
			t.Fatalf("resolveEngineConfig: %v", err)
		}

		if ecfg.Resume {
			t.Error("Resume should be false when --auto is not set")
		}
	})

	t.Run("max workers explicit", func(t *testing.T) {
		cmd := newTestNebulaApplyCmd()
		if err := cmd.ParseFlags([]string{"--max-workers=4"}); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		ecfg, err := resolveEngineConfig(cmd, []string{"/path/to/nebula"})
		if err != nil {
			t.Fatalf("resolveEngineConfig: %v", err)
		}

		if ecfg.MaxWorkers != 4 {
			t.Errorf("MaxWorkers = %d, want 4", ecfg.MaxWorkers)
		}
		if !ecfg.MaxWorkersExplicit {
			t.Error("MaxWorkersExplicit should be true when --max-workers is set")
		}
	})

	t.Run("no dependency on bus or engine", func(t *testing.T) {
		cmd := newTestNebulaApplyCmd()
		if err := cmd.ParseFlags([]string{}); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		ecfg, err := resolveEngineConfig(cmd, []string{"/path/to/nebula"})
		if err != nil {
			t.Fatalf("resolveEngineConfig: %v", err)
		}

		// resolveEngineConfig should produce a fully-formed EngineConfig
		// without needing any bus, engine, or TUI dependencies.
		if ecfg.CoderPrompt == "" {
			t.Error("CoderPrompt should be populated")
		}
		if ecfg.ReviewerPrompt == "" {
			t.Error("ReviewerPrompt should be populated")
		}
		// The function signature takes only (*cobra.Command, []string) — if it
		// compiled, it doesn't depend on tea.Program, bus.Bus, or Engine.
	})
}

// newTestNebulaApplyCmd creates a minimal cobra.Command with the apply flags
// registered, suitable for testing resolveEngineConfig in isolation.
func newTestNebulaApplyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "apply"}
	addNebulaApplyFlags(cmd)
	cmd.Flags().Bool("verbose", false, "verbose output")
	return cmd
}
