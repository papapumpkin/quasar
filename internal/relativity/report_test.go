package relativity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// testCatalog returns a Spacetime with realistic data for report tests.
func testCatalog() *Spacetime {
	created1 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	completed1 := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	created2 := time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC)
	created3 := time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC)

	return &Spacetime{
		Relativity: Header{
			Version: 1,
			Repo:    "github.com/papapumpkin/quasar",
		},
		Nebulas: []Entry{
			{
				Name:             "tui-landing-page",
				Sequence:         1,
				Status:           StatusCompleted,
				Category:         CategoryFeature,
				Created:          created1,
				Completed:        completed1,
				Branch:           "nebula/tui-landing-page",
				Areas:            []string{"internal/tui", "cmd"},
				PackagesAdded:    []string{"internal/tui"},
				PackagesModified: []string{"cmd"},
				TotalPhases:      5,
				CompletedPhases:  5,
				Enables:          []string{"dag-engine"},
				Summary:          "Built the TUI home screen with bubbletea.",
				Lessons:          []string{"bubbletea model works well"},
			},
			{
				Name:            "dag-engine",
				Sequence:        2,
				Status:          StatusPlanned,
				Category:        CategoryFeature,
				Created:         created2,
				Areas:           []string{"internal/dag"},
				TotalPhases:     5,
				CompletedPhases: 0,
				BuildsOn:        []string{"tui-landing-page"},
				Enables:         []string{"nebula-wiring"},
				Summary:         "DAG-based task dependency engine.",
			},
			{
				Name:            "relativity",
				Sequence:        3,
				Status:          StatusInProgress,
				Category:        CategoryInfra,
				Created:         created3,
				Areas:           []string{"internal/relativity", "cmd"},
				TotalPhases:     3,
				CompletedPhases: 1,
				Summary:         "Catalog system tracking nebula evolution.",
			},
		},
	}
}

// emptyCatalog returns a Spacetime with no nebulas.
func emptyCatalog() *Spacetime {
	return &Spacetime{
		Relativity: Header{Version: 1, Repo: "github.com/papapumpkin/quasar"},
	}
}

// singleCatalog returns a Spacetime with exactly one nebula.
func singleCatalog() *Spacetime {
	return &Spacetime{
		Relativity: Header{Version: 1, Repo: "github.com/papapumpkin/quasar"},
		Nebulas: []Entry{
			{
				Name:            "bootstrap",
				Sequence:        1,
				Status:          StatusCompleted,
				Category:        CategoryInfra,
				Created:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				Completed:       time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
				Areas:           []string{"cmd"},
				TotalPhases:     2,
				CompletedPhases: 2,
				Summary:         "Initial project setup.",
			},
		},
	}
}

func TestFormatByName(t *testing.T) {
	t.Parallel()

	for _, name := range FormatNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := FormatByName(name)
			if err != nil {
				t.Fatalf("FormatByName(%q) error: %v", name, err)
			}
			if f == nil {
				t.Fatalf("FormatByName(%q) returned nil", name)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()
		_, err := FormatByName("nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown format")
		}
		if !strings.Contains(err.Error(), "unknown report format") {
			t.Errorf("error = %q, want mention of unknown format", err.Error())
		}
	})
}

func TestTimelineReport(t *testing.T) {
	t.Parallel()
	r := &TimelineReport{}

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(emptyCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if !strings.Contains(out, "No nebulas recorded") {
			t.Errorf("empty output missing placeholder, got:\n%s", out)
		}
	})

	t.Run("single", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(singleCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if !strings.Contains(out, "bootstrap") {
			t.Errorf("output missing nebula name, got:\n%s", out)
		}
	})

	t.Run("many", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(testCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}

		// Check chronological order: sequence 1 before 2 before 3.
		pos1 := strings.Index(out, "tui-landing-page")
		pos2 := strings.Index(out, "dag-engine")
		pos3 := strings.Index(out, "relativity")
		if pos1 >= pos2 || pos2 >= pos3 {
			t.Errorf("entries not in chronological order: %d, %d, %d", pos1, pos2, pos3)
		}

		// Check header.
		if !strings.Contains(out, "# Evolution Timeline") {
			t.Error("missing timeline header")
		}

		// Check areas are included.
		if !strings.Contains(out, "internal/tui") {
			t.Error("missing area in timeline output")
		}

		// Check enables.
		if !strings.Contains(out, "Enables: dag-engine") {
			t.Error("missing enables in timeline output")
		}

		// Check phases.
		if !strings.Contains(out, "5/5 completed") {
			t.Error("missing phase count in timeline output")
		}
	})

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		_, err := r.Render(nil)
		if err == nil {
			t.Fatal("expected error for nil catalog")
		}
	})
}

func TestHeatmapReport(t *testing.T) {
	t.Parallel()
	r := &HeatmapReport{}

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(emptyCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if !strings.Contains(out, "No nebulas recorded") {
			t.Errorf("empty output missing placeholder, got:\n%s", out)
		}
	})

	t.Run("many", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(testCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}

		// cmd is touched by tui-landing-page and relativity — should appear with count 2.
		if !strings.Contains(out, "cmd") {
			t.Error("missing shared area 'cmd'")
		}

		// Check table header.
		if !strings.Contains(out, "Package") {
			t.Error("missing table header")
		}

		// internal/tui should appear.
		if !strings.Contains(out, "internal/tui") {
			t.Error("missing area internal/tui")
		}

		// Category mix for cmd should be "mixed" since it's touched by feature and infra.
		if !strings.Contains(out, "mixed") {
			t.Error("expected 'mixed' category for cmd area")
		}
	})

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		_, err := r.Render(nil)
		if err == nil {
			t.Fatal("expected error for nil catalog")
		}
	})
}

func TestGraphReport(t *testing.T) {
	t.Parallel()
	r := &GraphReport{}

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(emptyCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if !strings.Contains(out, "No nebulas recorded") {
			t.Errorf("empty output missing placeholder, got:\n%s", out)
		}
	})

	t.Run("many", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(testCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}

		// tui-landing-page enables dag-engine.
		if !strings.Contains(out, "tui-landing-page → dag-engine") {
			t.Errorf("missing edge tui-landing-page → dag-engine, got:\n%s", out)
		}

		// dag-engine enables nebula-wiring.
		if !strings.Contains(out, "dag-engine → nebula-wiring") {
			t.Errorf("missing edge dag-engine → nebula-wiring, got:\n%s", out)
		}

		// relativity is standalone (no enables, no builds_on).
		if !strings.Contains(out, "relativity (standalone)") {
			t.Errorf("missing standalone marker for relativity, got:\n%s", out)
		}
	})

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		_, err := r.Render(nil)
		if err == nil {
			t.Fatal("expected error for nil catalog")
		}
	})
}

func TestJSONReport(t *testing.T) {
	t.Parallel()
	r := &JSONReport{}

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(emptyCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}

		var result jsonOutput
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}
		if result.Version != 1 {
			t.Errorf("version = %d, want 1", result.Version)
		}
		if len(result.Nebulas) != 0 {
			t.Errorf("nebulas count = %d, want 0", len(result.Nebulas))
		}
	})

	t.Run("many", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(testCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}

		var result jsonOutput
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}

		if result.Version != 1 {
			t.Errorf("version = %d, want 1", result.Version)
		}
		if len(result.Nebulas) != 3 {
			t.Fatalf("nebulas count = %d, want 3", len(result.Nebulas))
		}

		// Verify first entry.
		n := result.Nebulas[0]
		if n.Name != "tui-landing-page" {
			t.Errorf("name = %q, want %q", n.Name, "tui-landing-page")
		}
		if n.Sequence != 1 {
			t.Errorf("sequence = %d, want 1", n.Sequence)
		}
		if n.Status != StatusCompleted {
			t.Errorf("status = %q, want %q", n.Status, StatusCompleted)
		}
	})

	t.Run("nil_slices_are_empty_arrays", func(t *testing.T) {
		t.Parallel()
		catalog := &Spacetime{
			Relativity: Header{Version: 1},
			Nebulas: []Entry{
				{Name: "test", Sequence: 1, Status: StatusPlanned},
			},
		}
		out, err := r.Render(catalog)
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}

		// Ensure nil slices render as [] not null.
		if strings.Contains(out, "null") {
			t.Errorf("JSON contains null for slice fields:\n%s", out)
		}
	})

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		_, err := r.Render(nil)
		if err == nil {
			t.Fatal("expected error for nil catalog")
		}
	})
}

func TestOnboardingReport(t *testing.T) {
	t.Parallel()
	r := &OnboardingReport{}

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(emptyCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if !strings.Contains(out, "no recorded nebulas") {
			t.Errorf("empty output missing placeholder, got:\n%s", out)
		}
		// Should still have the header.
		if !strings.Contains(out, "# Project Onboarding") {
			t.Error("missing onboarding header")
		}
	})

	t.Run("single", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(singleCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		// Should use singular "nebula" not "nebulas".
		if !strings.Contains(out, "1 nebula") {
			t.Errorf("expected singular 'nebula', got:\n%s", out)
		}
		// Should mention the bootstrap nebula.
		if !strings.Contains(out, "bootstrap") {
			t.Error("missing nebula name in onboarding")
		}
		// Should mention completion since all work is done.
		if !strings.Contains(out, "completed") {
			t.Error("missing completed status mention")
		}
	})

	t.Run("many_prose_quality", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(testCatalog())
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}

		// Should read as coherent prose.
		if !strings.Contains(out, "The codebase started with") {
			t.Error("missing narrative opening")
		}

		// Should mention all nebulas.
		for _, name := range []string{"tui-landing-page", "dag-engine", "relativity"} {
			if !strings.Contains(out, name) {
				t.Errorf("missing nebula %q in onboarding", name)
			}
		}

		// Should have area history section.
		if !strings.Contains(out, "Key areas") {
			t.Error("missing area history section")
		}

		// Should have active work section.
		if !strings.Contains(out, "Active work") {
			t.Error("missing active work section")
		}

		// Should mention status counts.
		if !strings.Contains(out, "3 nebulas") {
			t.Errorf("expected '3 nebulas', got:\n%s", out)
		}
	})

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		_, err := r.Render(nil)
		if err == nil {
			t.Fatal("expected error for nil catalog")
		}
	})
}

func TestAllFormatsRenderWithoutError(t *testing.T) {
	t.Parallel()

	catalogs := map[string]*Spacetime{
		"empty":  emptyCatalog(),
		"single": singleCatalog(),
		"many":   testCatalog(),
	}

	for _, name := range FormatNames() {
		for catalogName, catalog := range catalogs {
			t.Run(name+"/"+catalogName, func(t *testing.T) {
				t.Parallel()
				f, err := FormatByName(name)
				if err != nil {
					t.Fatalf("FormatByName(%q): %v", name, err)
				}
				out, err := f.Render(catalog)
				if err != nil {
					t.Fatalf("Render(%q, %q): %v", name, catalogName, err)
				}
				if out == "" {
					t.Errorf("Render(%q, %q) returned empty string", name, catalogName)
				}
			})
		}
	}
}
