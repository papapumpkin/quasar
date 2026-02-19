package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTUICmd_Registered(t *testing.T) {
	t.Parallel()

	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "tui" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'tui' subcommand to be registered on rootCmd")
	}
}

func TestTUICmd_Flags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		flag string
	}{
		{"dir", "dir"},
		{"no-splash", "no-splash"},
		{"max-workers", "max-workers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := tuiCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Errorf("expected flag %q to be registered on tui command", tt.flag)
			}
		})
	}
}

func TestTUICmd_NoNebulasDir(t *testing.T) {
	// Not parallel: modifies shared tuiCmd flag state.
	dir := t.TempDir()

	if err := tuiCmd.Flags().Set("dir", dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tuiCmd.Flags().Set("dir", "") }()

	runErr := runTUI(tuiCmd, nil)
	if runErr == nil {
		t.Fatal("expected error when .nebulas/ doesn't exist")
	}
	expected := "no .nebulas/ directory found in " + dir
	if got := runErr.Error(); got != expected {
		t.Errorf("unexpected error: %q, want %q", got, expected)
	}
}

func TestTUICmd_RequiresTTY(t *testing.T) {
	// Not parallel: modifies shared tuiCmd flag state.
	dir := t.TempDir()
	nebulaeDir := filepath.Join(dir, ".nebulas")
	if err := os.MkdirAll(nebulaeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := tuiCmd.Flags().Set("dir", dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tuiCmd.Flags().Set("dir", "") }()

	runErr := runTUI(tuiCmd, nil)
	if runErr == nil {
		t.Fatal("expected error when not on a TTY")
	}
	if got := runErr.Error(); got != "quasar tui requires a TTY (terminal)" {
		t.Errorf("unexpected error: %q", got)
	}
}

func TestRootDefault_NoNebulasShowsHelp(t *testing.T) {
	// Not parallel: uses os.Chdir.
	dir := t.TempDir()
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	err := runRootDefault(rootCmd, nil)
	if err != nil {
		t.Errorf("expected no error from runRootDefault without .nebulas/, got: %v", err)
	}
}
