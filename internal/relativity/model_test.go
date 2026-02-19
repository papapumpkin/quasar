package relativity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestSpacetimeRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	created := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	completed := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)

	original := &Spacetime{
		Relativity: Header{
			Version:  1,
			LastScan: now,
			Repo:     "github.com/papapumpkin/quasar",
		},
		Nebulas: []Entry{
			{
				Name:             "tui-landing-page",
				Sequence:         1,
				Status:           StatusCompleted,
				Category:         CategoryFeature,
				Created:          created,
				Completed:        completed,
				Branch:           "nebula/tui-landing-page",
				Areas:            []string{"internal/tui", "cmd"},
				PackagesAdded:    []string{"internal/tui"},
				PackagesModified: []string{"cmd"},
				TotalPhases:      5,
				CompletedPhases:  5,
				Enables:          []string{"dag-engine"},
				BuildsOn:         []string{},
				Summary:          "Built the TUI home screen with bubbletea.",
				Lessons:          []string{"bubbletea model/update/view pattern works well"},
			},
			{
				Name:     "dag-engine",
				Sequence: 2,
				Status:   StatusPlanned,
				Category: CategoryFeature,
			},
		},
	}

	data, err := toml.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Spacetime
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify header.
	if decoded.Relativity.Version != 1 {
		t.Errorf("version = %d, want 1", decoded.Relativity.Version)
	}
	if decoded.Relativity.Repo != original.Relativity.Repo {
		t.Errorf("repo = %q, want %q", decoded.Relativity.Repo, original.Relativity.Repo)
	}
	if !decoded.Relativity.LastScan.Equal(now) {
		t.Errorf("last_scan = %v, want %v", decoded.Relativity.LastScan, now)
	}

	// Verify nebulas count.
	if len(decoded.Nebulas) != 2 {
		t.Fatalf("nebulas count = %d, want 2", len(decoded.Nebulas))
	}

	// Verify first entry fields.
	e := decoded.Nebulas[0]
	if e.Name != "tui-landing-page" {
		t.Errorf("name = %q, want %q", e.Name, "tui-landing-page")
	}
	if e.Sequence != 1 {
		t.Errorf("sequence = %d, want 1", e.Sequence)
	}
	if e.Status != StatusCompleted {
		t.Errorf("status = %q, want %q", e.Status, StatusCompleted)
	}
	if e.Category != CategoryFeature {
		t.Errorf("category = %q, want %q", e.Category, CategoryFeature)
	}
	if !e.Created.Equal(created) {
		t.Errorf("created = %v, want %v", e.Created, created)
	}
	if !e.Completed.Equal(completed) {
		t.Errorf("completed = %v, want %v", e.Completed, completed)
	}
	if e.Branch != "nebula/tui-landing-page" {
		t.Errorf("branch = %q, want %q", e.Branch, "nebula/tui-landing-page")
	}
	if len(e.Areas) != 2 || e.Areas[0] != "internal/tui" {
		t.Errorf("areas = %v, want [internal/tui cmd]", e.Areas)
	}
	if len(e.PackagesAdded) != 1 || e.PackagesAdded[0] != "internal/tui" {
		t.Errorf("packages_added = %v, want [internal/tui]", e.PackagesAdded)
	}
	if e.TotalPhases != 5 || e.CompletedPhases != 5 {
		t.Errorf("phases = %d/%d, want 5/5", e.CompletedPhases, e.TotalPhases)
	}
	if len(e.Enables) != 1 || e.Enables[0] != "dag-engine" {
		t.Errorf("enables = %v, want [dag-engine]", e.Enables)
	}
	if e.Summary != "Built the TUI home screen with bubbletea." {
		t.Errorf("summary = %q, want non-empty", e.Summary)
	}
	if len(e.Lessons) != 1 {
		t.Errorf("lessons count = %d, want 1", len(e.Lessons))
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "spacetime.toml")

	now := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	original := &Spacetime{
		Relativity: Header{
			Version:  1,
			LastScan: now,
			Repo:     "github.com/papapumpkin/quasar",
		},
		Nebulas: []Entry{
			{
				Name:     "test-nebula",
				Sequence: 1,
				Status:   StatusInProgress,
				Category: CategoryRefactor,
				Summary:  "Refactoring the core loop.",
			},
		},
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Relativity.Version != 1 {
		t.Errorf("version = %d, want 1", loaded.Relativity.Version)
	}
	if len(loaded.Nebulas) != 1 {
		t.Fatalf("nebulas count = %d, want 1", len(loaded.Nebulas))
	}
	if loaded.Nebulas[0].Name != "test-nebula" {
		t.Errorf("name = %q, want %q", loaded.Nebulas[0].Name, "test-nebula")
	}
	if loaded.Nebulas[0].Summary != "Refactoring the core loop." {
		t.Errorf("summary = %q, want %q", loaded.Nebulas[0].Summary, "Refactoring the core loop.")
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	t.Parallel()

	st, err := Load("/nonexistent/path/spacetime.toml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(st.Nebulas) != 0 {
		t.Errorf("expected empty nebulas, got %d", len(st.Nebulas))
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "spacetime.toml")

	if err := os.WriteFile(path, []byte("not valid toml {{{}}}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
	if !strings.Contains(err.Error(), "parsing spacetime.toml") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "parsing spacetime.toml")
	}
}

func TestSaveCreatesDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "spacetime.toml")

	st := &Spacetime{
		Relativity: Header{Version: 1},
	}

	if err := Save(path, st); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMergePreservesManualAnnotations(t *testing.T) {
	t.Parallel()

	existing := &Spacetime{
		Relativity: Header{Version: 1, Repo: "old-repo"},
		Nebulas: []Entry{
			{
				Name:     "alpha",
				Sequence: 1,
				Status:   StatusCompleted,
				Summary:  "Hand-written summary.",
				Lessons:  []string{"lesson one", "lesson two"},
				Enables:  []string{"beta"},
				BuildsOn: []string{"gamma"},
			},
			{
				Name:    "beta",
				Summary: "Beta summary.",
			},
		},
	}

	scanned := &Spacetime{
		Relativity: Header{Version: 1, Repo: "new-repo"},
		Nebulas: []Entry{
			{
				Name:            "alpha",
				Sequence:        1,
				Status:          StatusCompleted,
				Category:        CategoryFeature,
				TotalPhases:     3,
				CompletedPhases: 3,
				// Summary intentionally empty — should be filled from existing.
			},
			{
				Name:     "alpha-v2",
				Sequence: 2,
				Status:   StatusPlanned,
			},
		},
	}

	merged := Merge(existing, scanned)

	t.Run("header uses scanned values", func(t *testing.T) {
		if merged.Relativity.Repo != "new-repo" {
			t.Errorf("repo = %q, want %q", merged.Relativity.Repo, "new-repo")
		}
	})

	t.Run("manual summary preserved", func(t *testing.T) {
		if merged.Nebulas[0].Summary != "Hand-written summary." {
			t.Errorf("summary = %q, want %q", merged.Nebulas[0].Summary, "Hand-written summary.")
		}
	})

	t.Run("manual lessons preserved", func(t *testing.T) {
		if len(merged.Nebulas[0].Lessons) != 2 {
			t.Errorf("lessons count = %d, want 2", len(merged.Nebulas[0].Lessons))
		}
	})

	t.Run("manual enables preserved", func(t *testing.T) {
		if len(merged.Nebulas[0].Enables) != 1 || merged.Nebulas[0].Enables[0] != "beta" {
			t.Errorf("enables = %v, want [beta]", merged.Nebulas[0].Enables)
		}
	})

	t.Run("manual builds_on preserved", func(t *testing.T) {
		if len(merged.Nebulas[0].BuildsOn) != 1 || merged.Nebulas[0].BuildsOn[0] != "gamma" {
			t.Errorf("builds_on = %v, want [gamma]", merged.Nebulas[0].BuildsOn)
		}
	})

	t.Run("auto-derived fields take precedence", func(t *testing.T) {
		if merged.Nebulas[0].TotalPhases != 3 {
			t.Errorf("total_phases = %d, want 3", merged.Nebulas[0].TotalPhases)
		}
		if merged.Nebulas[0].Category != CategoryFeature {
			t.Errorf("category = %q, want %q", merged.Nebulas[0].Category, CategoryFeature)
		}
	})

	t.Run("new entries pass through", func(t *testing.T) {
		if len(merged.Nebulas) != 2 {
			t.Fatalf("nebulas count = %d, want 2", len(merged.Nebulas))
		}
		if merged.Nebulas[1].Name != "alpha-v2" {
			t.Errorf("name = %q, want %q", merged.Nebulas[1].Name, "alpha-v2")
		}
	})

	t.Run("entries not in scan are dropped", func(t *testing.T) {
		for _, e := range merged.Nebulas {
			if e.Name == "beta" {
				t.Errorf("beta should not be in merged result (not in scan)")
			}
		}
	})
}

func TestMergeScannedSummaryWins(t *testing.T) {
	t.Parallel()

	existing := &Spacetime{
		Nebulas: []Entry{
			{Name: "x", Summary: "old summary"},
		},
	}
	scanned := &Spacetime{
		Nebulas: []Entry{
			{Name: "x", Summary: "new summary"},
		},
	}

	merged := Merge(existing, scanned)
	if merged.Nebulas[0].Summary != "new summary" {
		t.Errorf("summary = %q, want %q", merged.Nebulas[0].Summary, "new summary")
	}
}
