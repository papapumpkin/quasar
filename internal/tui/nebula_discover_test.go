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

func TestDiscoverAllNebulae(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Create two valid nebulas.
	alphaDir := filepath.Join(root, "alpha")
	createTestNebulaWithDescription(t, alphaDir, "Alpha Nebula", "First nebula", 3)

	betaDir := filepath.Join(root, "beta")
	createTestNebulaWithDescription(t, betaDir, "Beta Nebula", "Second nebula", 2)

	// Create a non-nebula directory (no nebula.toml — should be skipped).
	notNebula := filepath.Join(root, "random-dir")
	if err := os.MkdirAll(notNebula, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a regular file (should be skipped).
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	choices, err := DiscoverAllNebulae(root)
	if err != nil {
		t.Fatalf("DiscoverAllNebulae returned error: %v", err)
	}

	if len(choices) != 2 {
		t.Fatalf("expected 2 choices, got %d: %+v", len(choices), choices)
	}

	byName := make(map[string]NebulaChoice)
	for _, c := range choices {
		byName[c.Name] = c
	}

	t.Run("alpha nebula found with description", func(t *testing.T) {
		t.Parallel()
		c, ok := byName["Alpha Nebula"]
		if !ok {
			t.Fatal("expected Alpha Nebula in results")
		}
		if c.Description != "First nebula" {
			t.Errorf("expected description 'First nebula', got %q", c.Description)
		}
		if c.Phases != 3 {
			t.Errorf("expected 3 phases, got %d", c.Phases)
		}
		if c.Status != "ready" {
			t.Errorf("expected status 'ready', got %q", c.Status)
		}
	})

	t.Run("beta nebula found with description", func(t *testing.T) {
		t.Parallel()
		c, ok := byName["Beta Nebula"]
		if !ok {
			t.Fatal("expected Beta Nebula in results")
		}
		if c.Description != "Second nebula" {
			t.Errorf("expected description 'Second nebula', got %q", c.Description)
		}
		if c.Phases != 2 {
			t.Errorf("expected 2 phases, got %d", c.Phases)
		}
	})
}

func TestDiscoverAllNebulae_EmptyDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	choices, err := DiscoverAllNebulae(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(choices) != 0 {
		t.Errorf("expected 0 choices, got %d", len(choices))
	}
}

func TestDiscoverAllNebulae_InvalidDir(t *testing.T) {
	t.Parallel()

	_, err := DiscoverAllNebulae("/nonexistent/path/to/nebulae")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestDiscoverAllNebulae_WithState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	doneDir := filepath.Join(root, "done-neb")
	createTestNebulaWithDescription(t, doneDir, "Done Neb", "All done", 1)
	writeTestState(t, doneDir, `version = 1
nebula_name = "Done Neb"
[phases.phase-1]
bead_id = "beads-abc"
status = "done"
created_at = 2024-01-01T00:00:00Z
updated_at = 2024-01-01T00:00:00Z
`)

	choices, err := DiscoverAllNebulae(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}

	if choices[0].Status != "done" {
		t.Errorf("expected status 'done', got %q", choices[0].Status)
	}
	if choices[0].Done != 1 {
		t.Errorf("expected 1 done, got %d", choices[0].Done)
	}
	if choices[0].Description != "All done" {
		t.Errorf("expected description 'All done', got %q", choices[0].Description)
	}
}

func TestDiscoverAllNebulae_FallbackName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Create a nebula with an empty name — should fall back to dir name.
	dir := filepath.Join(root, "my-neb-dir")
	createTestNebulaWithDescription(t, dir, "", "", 1)

	choices, err := DiscoverAllNebulae(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}

	if choices[0].Name != "my-neb-dir" {
		t.Errorf("expected name 'my-neb-dir' (dir fallback), got %q", choices[0].Name)
	}
}

func TestDiscoverNebulae_PopulatesDescription(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	currentDir := filepath.Join(root, "current")
	createTestNebula(t, currentDir, "Current", 1)

	siblingDir := filepath.Join(root, "sibling")
	createTestNebulaWithDescription(t, siblingDir, "Sibling", "A sibling nebula", 2)

	choices, err := DiscoverNebulae(currentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}

	if choices[0].Description != "A sibling nebula" {
		t.Errorf("expected description 'A sibling nebula', got %q", choices[0].Description)
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

// createTestNebulaWithDescription creates a nebula directory with a name, description, and phase files.
func createTestNebulaWithDescription(t *testing.T, dir, name, description string, phaseCount int) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := "[nebula]\nname = " + `"` + name + `"` + "\ndescription = " + `"` + description + `"` + "\n"
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

// itoa converts an int to a string.
func itoa(i int) string {
	return strconv.Itoa(i)
}
