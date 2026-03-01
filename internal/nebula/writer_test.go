package nebula

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNebula(t *testing.T) {
	t.Parallel()

	makeResult := func() *GenerateResult {
		return &GenerateResult{
			Manifest: Manifest{
				Nebula: Info{
					Name:        "test-nebula",
					Description: "A test nebula for writing",
				},
				Defaults: Defaults{
					Type:     "task",
					Priority: 2,
					Labels:   []string{"test"},
				},
				Execution: Execution{
					MaxWorkers:      2,
					MaxReviewCycles: 5,
					MaxBudgetUSD:    50.0,
				},
				Context: Context{
					Repo:       "github.com/example/repo",
					WorkingDir: ".",
					Goals:      []string{"Goal 1"},
				},
			},
			Phases: []PhaseSpec{
				{
					ID:       "setup-types",
					Title:    "Define Types",
					Type:     "task",
					Priority: 1,
					Body:     "## Problem\n\nNeed types.\n\n## Solution\n\nDefine them.",
				},
				{
					ID:        "add-handlers",
					Title:     "Add Handlers",
					Type:      "task",
					Priority:  2,
					DependsOn: []string{"setup-types"},
					Body:      "## Problem\n\nNeed handlers.\n\n## Solution\n\nAdd them.",
				},
				{
					ID:        "add-tests",
					Title:     "Add Tests",
					Type:      "task",
					Priority:  2,
					DependsOn: []string{"add-handlers"},
					Body:      "## Problem\n\nNeed tests.\n\n## Solution\n\nWrite them.",
				},
			},
		}
	}

	t.Run("happy path creates directory with numbered files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "my-nebula")

		result := makeResult()
		if err := WriteNebula(result, outputDir, WriteOptions{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify nebula.toml exists.
		if _, err := os.Stat(filepath.Join(outputDir, "nebula.toml")); err != nil {
			t.Errorf("nebula.toml not found: %v", err)
		}

		// Verify phase files exist with correct names.
		wantFiles := []string{
			"01-setup-types.md",
			"02-add-handlers.md",
			"03-add-tests.md",
		}
		for _, name := range wantFiles {
			if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
				t.Errorf("expected file %s: %v", name, err)
			}
		}
	})

	t.Run("topological ordering respects dependencies", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "topo-test")

		// Provide phases in reverse dependency order.
		result := &GenerateResult{
			Manifest: makeResult().Manifest,
			Phases: []PhaseSpec{
				{
					ID:        "phase-c",
					Title:     "Phase C",
					Type:      "task",
					Priority:  2,
					DependsOn: []string{"phase-b"},
					Body:      "## C",
				},
				{
					ID:        "phase-b",
					Title:     "Phase B",
					Type:      "task",
					Priority:  2,
					DependsOn: []string{"phase-a"},
					Body:      "## B",
				},
				{
					ID:       "phase-a",
					Title:    "Phase A",
					Type:     "task",
					Priority: 2,
					Body:     "## A",
				},
			},
		}

		if err := WriteNebula(result, outputDir, WriteOptions{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Phase A should be 01, B should be 02, C should be 03.
		entries, err := os.ReadDir(outputDir)
		if err != nil {
			t.Fatalf("reading dir: %v", err)
		}

		var mdFiles []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".md") {
				mdFiles = append(mdFiles, e.Name())
			}
		}

		wantOrder := []string{
			"01-phase-a.md",
			"02-phase-b.md",
			"03-phase-c.md",
		}
		if len(mdFiles) != len(wantOrder) {
			t.Fatalf("expected %d md files, got %d: %v", len(wantOrder), len(mdFiles), mdFiles)
		}
		for i, want := range wantOrder {
			if mdFiles[i] != want {
				t.Errorf("file %d: got %q, want %q", i, mdFiles[i], want)
			}
		}
	})

	t.Run("overwrite protection", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "existing")

		// Pre-create the directory.
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}

		result := makeResult()
		err := WriteNebula(result, outputDir, WriteOptions{Overwrite: false})
		if err == nil {
			t.Fatal("expected error for existing directory")
		}
		if !errors.Is(err, ErrDirExists) {
			t.Errorf("expected ErrDirExists, got: %v", err)
		}
		if !strings.Contains(err.Error(), "--force") {
			t.Errorf("error should mention --force: %v", err)
		}
	})

	t.Run("overwrite allowed replaces directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "overwrite-me")

		// Pre-create with a stale file.
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		staleFile := filepath.Join(outputDir, "stale.txt")
		if err := os.WriteFile(staleFile, []byte("old"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		result := makeResult()
		if err := WriteNebula(result, outputDir, WriteOptions{Overwrite: true}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Stale file should be gone.
		if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
			t.Error("stale file should have been removed")
		}

		// New files should exist.
		if _, err := os.Stat(filepath.Join(outputDir, "nebula.toml")); err != nil {
			t.Error("nebula.toml should exist after overwrite")
		}
	})

	t.Run("temp directory removed after success", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "atomic-test")
		tmpDir := outputDir + ".tmp"

		result := makeResult()
		if err := WriteNebula(result, outputDir, WriteOptions{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The temp directory should not exist after success.
		if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
			t.Error("temp directory should be cleaned up after success")
		}
	})

	t.Run("atomic cleanup on failure", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		// Create a read-only parent so that os.MkdirAll for the temp dir fails.
		parent := filepath.Join(dir, "readonly-parent")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.Chmod(parent, 0o555); err != nil {
			t.Fatalf("setup chmod: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(parent, 0o755) // restore for cleanup
		})

		outputDir := filepath.Join(parent, "my-nebula")
		tmpDir := outputDir + ".tmp"

		result := makeResult()
		err := WriteNebula(result, outputDir, WriteOptions{})
		if err == nil {
			t.Fatal("expected error when parent is read-only")
		}

		// Neither the output dir nor the temp dir should exist.
		if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
			t.Error("output directory should not exist after failure")
		}
		if _, statErr := os.Stat(tmpDir); !os.IsNotExist(statErr) {
			t.Error("temp directory should not exist after failure")
		}
	})

	t.Run("path traversal in phase ID rejected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "traversal-test")

		result := &GenerateResult{
			Manifest: makeResult().Manifest,
			Phases: []PhaseSpec{
				{
					ID:       "../../../evil",
					Title:    "Evil Phase",
					Type:     "task",
					Priority: 1,
					Body:     "## Problem\n\nPath traversal.",
				},
			},
		}

		err := WriteNebula(result, outputDir, WriteOptions{})
		if err == nil {
			t.Fatal("expected error for phase ID with path separator")
		}
		if !strings.Contains(err.Error(), "path separator") {
			t.Errorf("error should mention path separator: %v", err)
		}
	})

	t.Run("overwrite protection for non-directory entity", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "a-file")

		// Create a regular file at the output path.
		if err := os.WriteFile(outputDir, []byte("not a dir"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		result := makeResult()
		err := WriteNebula(result, outputDir, WriteOptions{Overwrite: false})
		if err == nil {
			t.Fatal("expected error for existing file at output path")
		}
		if !errors.Is(err, ErrDirExists) {
			t.Errorf("expected ErrDirExists, got: %v", err)
		}
	})

	t.Run("round-trip fidelity", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "roundtrip")

		result := makeResult()
		if err := WriteNebula(result, outputDir, WriteOptions{}); err != nil {
			t.Fatalf("write error: %v", err)
		}

		// Load the written nebula back.
		loaded, err := Load(outputDir)
		if err != nil {
			t.Fatalf("load error: %v", err)
		}

		// Verify manifest fields.
		if loaded.Manifest.Nebula.Name != result.Manifest.Nebula.Name {
			t.Errorf("name: got %q, want %q", loaded.Manifest.Nebula.Name, result.Manifest.Nebula.Name)
		}
		if loaded.Manifest.Nebula.Description != result.Manifest.Nebula.Description {
			t.Errorf("description: got %q, want %q", loaded.Manifest.Nebula.Description, result.Manifest.Nebula.Description)
		}
		if loaded.Manifest.Execution.MaxWorkers != result.Manifest.Execution.MaxWorkers {
			t.Errorf("max_workers: got %d, want %d", loaded.Manifest.Execution.MaxWorkers, result.Manifest.Execution.MaxWorkers)
		}
		if loaded.Manifest.Execution.MaxBudgetUSD != result.Manifest.Execution.MaxBudgetUSD {
			t.Errorf("max_budget_usd: got %f, want %f", loaded.Manifest.Execution.MaxBudgetUSD, result.Manifest.Execution.MaxBudgetUSD)
		}

		// Verify phases.
		if len(loaded.Phases) != len(result.Phases) {
			t.Fatalf("phases: got %d, want %d", len(loaded.Phases), len(result.Phases))
		}

		// Phases are loaded in file order (which is topological).
		wantIDs := []string{"setup-types", "add-handlers", "add-tests"}
		for i, wantID := range wantIDs {
			if loaded.Phases[i].ID != wantID {
				t.Errorf("phase %d ID: got %q, want %q", i, loaded.Phases[i].ID, wantID)
			}
		}

		// Verify bodies round-trip.
		for _, lp := range loaded.Phases {
			for _, rp := range result.Phases {
				if lp.ID == rp.ID {
					if strings.TrimSpace(lp.Body) != strings.TrimSpace(rp.Body) {
						t.Errorf("phase %q body mismatch:\ngot:  %q\nwant: %q", lp.ID, lp.Body, rp.Body)
					}
				}
			}
		}
	})

	t.Run("empty phases produces manifest only", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		outputDir := filepath.Join(dir, "empty-phases")

		result := &GenerateResult{
			Manifest: makeResult().Manifest,
			Phases:   nil,
		}

		if err := WriteNebula(result, outputDir, WriteOptions{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		entries, err := os.ReadDir(outputDir)
		if err != nil {
			t.Fatalf("reading dir: %v", err)
		}

		if len(entries) != 1 {
			t.Errorf("expected 1 entry (nebula.toml), got %d", len(entries))
		}
		if entries[0].Name() != "nebula.toml" {
			t.Errorf("expected nebula.toml, got %s", entries[0].Name())
		}
	})
}

func TestMarshalManifest(t *testing.T) {
	t.Parallel()

	m := Manifest{
		Nebula: Info{
			Name:        "my-nebula",
			Description: "Test description",
		},
		Defaults: Defaults{
			Type:     "task",
			Priority: 2,
		},
		Execution: Execution{
			MaxWorkers:      1,
			MaxReviewCycles: 5,
			MaxBudgetUSD:    50.0,
		},
	}

	data, err := marshalManifest(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must be valid TOML.
	s := string(data)
	if !strings.Contains(s, "my-nebula") {
		t.Error("manifest should contain nebula name")
	}
	if !strings.Contains(s, "Test description") {
		t.Error("manifest should contain description")
	}

	// Must end with newline.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		t.Error("manifest should end with newline")
	}
}

func TestTopoSortPhases(t *testing.T) {
	t.Parallel()

	t.Run("linear chain", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{
			{ID: "c", Priority: 2, DependsOn: []string{"b"}},
			{ID: "a", Priority: 2},
			{ID: "b", Priority: 2, DependsOn: []string{"a"}},
		}

		sorted, err := topoSortPhases(phases)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantOrder := []string{"a", "b", "c"}
		if len(sorted) != len(wantOrder) {
			t.Fatalf("expected %d phases, got %d", len(wantOrder), len(sorted))
		}
		for i, want := range wantOrder {
			if sorted[i].ID != want {
				t.Errorf("position %d: got %q, want %q", i, sorted[i].ID, want)
			}
		}
	})

	t.Run("priority ordering within level", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{
			{ID: "low-priority", Priority: 3},
			{ID: "high-priority", Priority: 1},
			{ID: "mid-priority", Priority: 2},
		}

		sorted, err := topoSortPhases(phases)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantOrder := []string{"high-priority", "mid-priority", "low-priority"}
		for i, want := range wantOrder {
			if sorted[i].ID != want {
				t.Errorf("position %d: got %q, want %q", i, sorted[i].ID, want)
			}
		}
	})

	t.Run("alphabetical tiebreaker", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{
			{ID: "zebra", Priority: 2},
			{ID: "alpha", Priority: 2},
			{ID: "beta", Priority: 2},
		}

		sorted, err := topoSortPhases(phases)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantOrder := []string{"alpha", "beta", "zebra"}
		for i, want := range wantOrder {
			if sorted[i].ID != want {
				t.Errorf("position %d: got %q, want %q", i, sorted[i].ID, want)
			}
		}
	})

	t.Run("cycle detection", func(t *testing.T) {
		t.Parallel()
		phases := []PhaseSpec{
			{ID: "a", Priority: 2, DependsOn: []string{"b"}},
			{ID: "b", Priority: 2, DependsOn: []string{"a"}},
		}

		_, err := topoSortPhases(phases)
		if err == nil {
			t.Fatal("expected error for cycle")
		}
		if !errors.Is(err, ErrDependencyCycle) {
			t.Errorf("expected ErrDependencyCycle, got: %v", err)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		sorted, err := topoSortPhases(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sorted != nil {
			t.Errorf("expected nil, got %v", sorted)
		}
	})

	t.Run("diamond dependency", func(t *testing.T) {
		t.Parallel()
		// A -> B, A -> C, B -> D, C -> D
		phases := []PhaseSpec{
			{ID: "d", Priority: 2, DependsOn: []string{"b", "c"}},
			{ID: "b", Priority: 2, DependsOn: []string{"a"}},
			{ID: "c", Priority: 2, DependsOn: []string{"a"}},
			{ID: "a", Priority: 2},
		}

		sorted, err := topoSortPhases(phases)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// a must be first, d must be last, b and c in between (alphabetical).
		if sorted[0].ID != "a" {
			t.Errorf("expected a first, got %q", sorted[0].ID)
		}
		if sorted[3].ID != "d" {
			t.Errorf("expected d last, got %q", sorted[3].ID)
		}
		if sorted[1].ID != "b" || sorted[2].ID != "c" {
			t.Errorf("expected b, c in middle, got %q, %q", sorted[1].ID, sorted[2].ID)
		}
	})
}
