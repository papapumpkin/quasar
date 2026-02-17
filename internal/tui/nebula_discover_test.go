package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDiscoverNebulae(t *testing.T) {
	t.Parallel()

	// Create a temp directory with sibling nebula directories.
	root := t.TempDir()

	// Create current nebula (should be excluded).
	currentDir := filepath.Join(root, "current-nebula")
	createTestNebula(t, currentDir, "Current Nebula", 3)

	// Create a sibling nebula that is ready (no state file).
	readyDir := filepath.Join(root, "ready-nebula")
	createTestNebula(t, readyDir, "Ready Nebula", 2)

	// Create a sibling nebula with a done state.
	doneDir := filepath.Join(root, "done-nebula")
	createTestNebula(t, doneDir, "Done Nebula", 1)
	writeTestState(t, doneDir, `version = 1
nebula_name = "Done Nebula"
[phases.phase-1]
bead_id = "beads-abc"
status = "done"
created_at = 2024-01-01T00:00:00Z
updated_at = 2024-01-01T00:00:00Z
`)

	// Create an in_progress nebula.
	progressDir := filepath.Join(root, "progress-nebula")
	createTestNebula(t, progressDir, "Progress Nebula", 2)
	writeTestState(t, progressDir, `version = 1
nebula_name = "Progress Nebula"
[phases.phase-1]
bead_id = "beads-abc"
status = "done"
created_at = 2024-01-01T00:00:00Z
updated_at = 2024-01-01T00:00:00Z
`)

	// Create a non-nebula directory (no nebula.toml — should be skipped).
	nonNebula := filepath.Join(root, "not-a-nebula")
	if err := os.MkdirAll(nonNebula, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a regular file in root (should be skipped).
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	choices, err := DiscoverNebulae(currentDir)
	if err != nil {
		t.Fatalf("DiscoverNebulae returned error: %v", err)
	}

	// Should find 3 nebulae (ready, done, progress) — not current, not-a-nebula, not README.
	if len(choices) != 3 {
		t.Fatalf("expected 3 choices, got %d: %+v", len(choices), choices)
	}

	// Build a map for easier assertions.
	byName := make(map[string]NebulaChoice)
	for _, c := range choices {
		byName[c.Name] = c
	}

	t.Run("excludes current nebula", func(t *testing.T) {
		t.Parallel()
		if _, ok := byName["Current Nebula"]; ok {
			t.Error("current nebula should be excluded from results")
		}
	})

	t.Run("ready nebula", func(t *testing.T) {
		t.Parallel()
		c, ok := byName["Ready Nebula"]
		if !ok {
			t.Fatal("expected Ready Nebula in results")
		}
		if c.Status != "ready" {
			t.Errorf("expected status 'ready', got %q", c.Status)
		}
		if c.Phases != 2 {
			t.Errorf("expected 2 phases, got %d", c.Phases)
		}
		if c.Done != 0 {
			t.Errorf("expected 0 done, got %d", c.Done)
		}
	})

	t.Run("done nebula", func(t *testing.T) {
		t.Parallel()
		c, ok := byName["Done Nebula"]
		if !ok {
			t.Fatal("expected Done Nebula in results")
		}
		if c.Status != "done" {
			t.Errorf("expected status 'done', got %q", c.Status)
		}
		if c.Done != 1 {
			t.Errorf("expected 1 done, got %d", c.Done)
		}
	})

	t.Run("in_progress nebula", func(t *testing.T) {
		t.Parallel()
		c, ok := byName["Progress Nebula"]
		if !ok {
			t.Fatal("expected Progress Nebula in results")
		}
		if c.Status != "in_progress" {
			t.Errorf("expected status 'in_progress', got %q", c.Status)
		}
		if c.Done != 1 {
			t.Errorf("expected 1 done, got %d", c.Done)
		}
		if c.Phases != 2 {
			t.Errorf("expected 2 phases, got %d", c.Phases)
		}
	})
}

func TestDiscoverNebulae_EmptyParent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	currentDir := filepath.Join(root, "only-nebula")
	createTestNebula(t, currentDir, "Only", 1)

	choices, err := DiscoverNebulae(currentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(choices) != 0 {
		t.Errorf("expected 0 choices, got %d", len(choices))
	}
}

func TestClassifyNebulaStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		phases     int
		stateToml  string
		wantStatus string
		wantDone   int
	}{
		{
			name:       "no state phases means ready",
			phases:     3,
			stateToml:  "",
			wantStatus: "ready",
			wantDone:   0,
		},
		{
			name:   "all done means done",
			phases: 1,
			stateToml: `[phases.p1]
bead_id = "b1"
status = "done"
created_at = 2024-01-01T00:00:00Z
updated_at = 2024-01-01T00:00:00Z
`,
			wantStatus: "done",
			wantDone:   1,
		},
		{
			name:   "some done some pending means in_progress",
			phases: 2,
			stateToml: `[phases.p1]
bead_id = "b1"
status = "done"
created_at = 2024-01-01T00:00:00Z
updated_at = 2024-01-01T00:00:00Z
`,
			wantStatus: "in_progress",
			wantDone:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			dir := filepath.Join(root, "test-nebula")
			createTestNebula(t, dir, "Test", tt.phases)

			if tt.stateToml != "" {
				writeTestState(t, dir, "version = 1\n"+tt.stateToml)
			}

			// Re-load to get the actual nebula and state.
			// We use DiscoverNebulae with a dummy current to exercise the full path.
			dummyDir := filepath.Join(root, "dummy")
			createTestNebula(t, dummyDir, "Dummy", 1)

			choices, err := DiscoverNebulae(dummyDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var found *NebulaChoice
			for i := range choices {
				if choices[i].Name == "Test" {
					found = &choices[i]
					break
				}
			}
			if found == nil {
				t.Fatal("expected to find 'Test' nebula")
			}
			if found.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", found.Status, tt.wantStatus)
			}
			if found.Done != tt.wantDone {
				t.Errorf("done = %d, want %d", found.Done, tt.wantDone)
			}
		})
	}
}

// createTestNebula creates a minimal nebula directory with a manifest and phase files.
func createTestNebula(t *testing.T, dir, name string, phaseCount int) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := "[nebula]\nname = " + `"` + name + `"` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "nebula.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= phaseCount; i++ {
		phaseContent := "+++\nid = \"phase-" + itoa(i) + "\"\ntitle = \"Phase " + itoa(i) + "\"\n+++\nDo stuff.\n"
		fileName := "phase-" + itoa(i) + ".md"
		if err := os.WriteFile(filepath.Join(dir, fileName), []byte(phaseContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// writeTestState writes a nebula.state.toml file.
func writeTestState(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "nebula.state.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// itoa converts an int to a string.
func itoa(i int) string {
	return strconv.Itoa(i)
}
