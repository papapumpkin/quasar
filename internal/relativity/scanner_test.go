package relativity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockGitQuerier implements GitQuerier for testing.
type mockGitQuerier struct {
	branches    map[string]bool
	firstCommit map[string]time.Time // branch -> time
	firstTouch  map[string]time.Time // path -> time
	mergeTime   map[string]time.Time // branch -> time
	diffAdded   map[string][]string  // "base|head" -> packages
	diffMod     map[string][]string  // "base|head" -> packages
}

func (m *mockGitQuerier) BranchExists(_ context.Context, name string) (bool, error) {
	return m.branches[name], nil
}

func (m *mockGitQuerier) FirstCommitOnBranch(_ context.Context, branch string) (time.Time, error) {
	if t, ok := m.firstCommit[branch]; ok {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("no commits on %s", branch)
}

func (m *mockGitQuerier) FirstCommitTouching(_ context.Context, path string) (time.Time, error) {
	if t, ok := m.firstTouch[path]; ok {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("no commits touching %s", path)
}

func (m *mockGitQuerier) MergeCommitToMain(_ context.Context, branch string) (time.Time, error) {
	if t, ok := m.mergeTime[branch]; ok {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("branch %s not merged", branch)
}

func (m *mockGitQuerier) DiffPackages(_ context.Context, base, head string) (added, modified []string, err error) {
	key := base + "|" + head
	return m.diffAdded[key], m.diffMod[key], nil
}

// writeNebulaDir creates a minimal nebula directory with a manifest and
// optional phase files.
func writeNebulaDir(t *testing.T, base, name, defaultType string, phases []string, requiresNebulae []string) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	// Write nebula.toml.
	requires := "[]"
	if len(requiresNebulae) > 0 {
		requires = "["
		for i, r := range requiresNebulae {
			if i > 0 {
				requires += ", "
			}
			requires += fmt.Sprintf("%q", r)
		}
		requires += "]"
	}

	manifest := fmt.Sprintf(`[nebula]
name = %q
description = "Test nebula"

[defaults]
type = %q

[dependencies]
requires_nebulae = %s
`, name, defaultType, requires)
	if err := os.WriteFile(filepath.Join(dir, "nebula.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write nebula.toml: %v", err)
	}

	// Write phase files.
	for _, phase := range phases {
		content := fmt.Sprintf("+++\nid = %q\ntitle = %q\ntype = %q\n+++\n\n## Problem\n\nTest.\n", phase, phase, defaultType)
		if err := os.WriteFile(filepath.Join(dir, phase+".md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write phase %s: %v", phase, err)
		}
	}
}

func TestScanDiscovery(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "alpha", "feature", []string{"phase-1", "phase-2"}, nil)
	writeNebulaDir(t, base, "beta", "bug", []string{"fix-1"}, nil)

	scanner := &Scanner{
		Git:        &mockGitQuerier{branches: map[string]bool{}},
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(st.Nebulas) != 2 {
		t.Fatalf("nebulas count = %d, want 2", len(st.Nebulas))
	}

	// Verify both were discovered (sorted by name since no dates).
	names := make(map[string]bool)
	for _, e := range st.Nebulas {
		names[e.Name] = true
	}
	if !names["alpha"] {
		t.Error("missing nebula: alpha")
	}
	if !names["beta"] {
		t.Error("missing nebula: beta")
	}
}

func TestScanPhaseCount(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "multi-phase", "feature", []string{"p1", "p2", "p3"}, nil)

	scanner := &Scanner{
		Git:        &mockGitQuerier{branches: map[string]bool{}},
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(st.Nebulas) != 1 {
		t.Fatalf("nebulas count = %d, want 1", len(st.Nebulas))
	}
	if st.Nebulas[0].TotalPhases != 3 {
		t.Errorf("total_phases = %d, want 3", st.Nebulas[0].TotalPhases)
	}
}

func TestScanGitDerivedDates(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "dated", "feature", []string{"p1"}, nil)

	created := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	completed := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)

	git := &mockGitQuerier{
		branches:    map[string]bool{"nebula/dated": true},
		firstCommit: map[string]time.Time{"nebula/dated": created},
		mergeTime:   map[string]time.Time{"nebula/dated": completed},
	}

	scanner := &Scanner{
		Git:        git,
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	e := st.Nebulas[0]
	if !e.Created.Equal(created) {
		t.Errorf("created = %v, want %v", e.Created, created)
	}
	if !e.Completed.Equal(completed) {
		t.Errorf("completed = %v, want %v", e.Completed, completed)
	}
}

func TestScanStatusInference(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "planned-neb", "feature", []string{"p1"}, nil)
	writeNebulaDir(t, base, "active-neb", "feature", []string{"p1"}, nil)
	writeNebulaDir(t, base, "done-neb", "feature", []string{"p1"}, nil)

	created := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	completed := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	git := &mockGitQuerier{
		branches: map[string]bool{
			"nebula/active-neb": true,
			"nebula/done-neb":   true,
		},
		firstCommit: map[string]time.Time{
			"nebula/active-neb": created,
			"nebula/done-neb":   created,
		},
		mergeTime: map[string]time.Time{
			"nebula/done-neb": completed,
		},
	}

	scanner := &Scanner{
		Git:        git,
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	statuses := make(map[string]string)
	for _, e := range st.Nebulas {
		statuses[e.Name] = e.Status
	}

	tests := []struct {
		name string
		want string
	}{
		{"planned-neb", StatusPlanned},
		{"active-neb", StatusInProgress},
		{"done-neb", StatusCompleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statuses[tt.name]; got != tt.want {
				t.Errorf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScanSequenceStability(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "first", "feature", nil, nil)
	writeNebulaDir(t, base, "second", "feature", nil, nil)
	writeNebulaDir(t, base, "third", "feature", nil, nil)

	git := &mockGitQuerier{
		branches: map[string]bool{},
		firstTouch: map[string]time.Time{
			filepath.Join(".nebulas", "first"):  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			filepath.Join(".nebulas", "second"): time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			filepath.Join(".nebulas", "third"):  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	scanner := &Scanner{
		Git:        git,
		NebulasDir: base,
		Repo:       "test/repo",
	}

	// Scan twice and verify sequence stability.
	for run := 0; run < 2; run++ {
		st, err := scanner.Scan(context.Background())
		if err != nil {
			t.Fatalf("scan %d: %v", run, err)
		}

		seqs := make(map[string]int)
		for _, e := range st.Nebulas {
			seqs[e.Name] = e.Sequence
		}

		if seqs["first"] != 1 {
			t.Errorf("run %d: first sequence = %d, want 1", run, seqs["first"])
		}
		if seqs["second"] != 2 {
			t.Errorf("run %d: second sequence = %d, want 2", run, seqs["second"])
		}
		if seqs["third"] != 3 {
			t.Errorf("run %d: third sequence = %d, want 3", run, seqs["third"])
		}
	}
}

func TestScanRelationships(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "core", "feature", nil, nil)
	writeNebulaDir(t, base, "extension", "feature", nil, []string{"core"})
	writeNebulaDir(t, base, "advanced", "feature", nil, []string{"core", "extension"})

	scanner := &Scanner{
		Git:        &mockGitQuerier{branches: map[string]bool{}},
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	byName := make(map[string]*Entry)
	for i := range st.Nebulas {
		byName[st.Nebulas[i].Name] = &st.Nebulas[i]
	}

	t.Run("core enables extension and advanced", func(t *testing.T) {
		enables := byName["core"].Enables
		if len(enables) != 2 {
			t.Fatalf("core enables count = %d, want 2", len(enables))
		}
		if enables[0] != "advanced" || enables[1] != "extension" {
			t.Errorf("core enables = %v, want [advanced extension]", enables)
		}
	})

	t.Run("extension builds_on core", func(t *testing.T) {
		buildsOn := byName["extension"].BuildsOn
		if len(buildsOn) != 1 || buildsOn[0] != "core" {
			t.Errorf("extension builds_on = %v, want [core]", buildsOn)
		}
	})

	t.Run("advanced builds_on both", func(t *testing.T) {
		buildsOn := byName["advanced"].BuildsOn
		if len(buildsOn) != 2 {
			t.Fatalf("advanced builds_on count = %d, want 2", len(buildsOn))
		}
		if buildsOn[0] != "core" || buildsOn[1] != "extension" {
			t.Errorf("advanced builds_on = %v, want [core extension]", buildsOn)
		}
	})
}

func TestScanCategory(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "feature-neb", "feature", []string{"f1", "f2"}, nil)
	writeNebulaDir(t, base, "bug-neb", "bug", []string{"b1"}, nil)
	writeNebulaDir(t, base, "task-neb", "task", []string{"t1"}, nil)

	scanner := &Scanner{
		Git:        &mockGitQuerier{branches: map[string]bool{}},
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	categories := make(map[string]string)
	for _, e := range st.Nebulas {
		categories[e.Name] = e.Category
	}

	tests := []struct {
		name string
		want string
	}{
		{"feature-neb", CategoryFeature},
		{"bug-neb", CategoryBugfix},
		{"task-neb", CategoryEnhancement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := categories[tt.name]; got != tt.want {
				t.Errorf("category = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScanFallbackCreatedDate(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "no-branch", "feature", nil, nil)

	touched := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)

	git := &mockGitQuerier{
		branches: map[string]bool{},
		firstTouch: map[string]time.Time{
			filepath.Join(".nebulas", "no-branch"): touched,
		},
	}

	scanner := &Scanner{
		Git:        git,
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !st.Nebulas[0].Created.Equal(touched) {
		t.Errorf("created = %v, want %v", st.Nebulas[0].Created, touched)
	}
}

func TestScanEmptyDirectory(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	scanner := &Scanner{
		Git:        &mockGitQuerier{branches: map[string]bool{}},
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(st.Nebulas) != 0 {
		t.Errorf("expected empty nebulas, got %d", len(st.Nebulas))
	}
}

func TestScanNonExistentDirectory(t *testing.T) {
	t.Parallel()

	scanner := &Scanner{
		Git:        &mockGitQuerier{branches: map[string]bool{}},
		NebulasDir: filepath.Join(t.TempDir(), "nonexistent"),
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(st.Nebulas) != 0 {
		t.Errorf("expected empty nebulas, got %d", len(st.Nebulas))
	}
}

func TestScanDiffPackages(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "with-diff", "feature", nil, nil)

	git := &mockGitQuerier{
		branches:    map[string]bool{"nebula/with-diff": true},
		firstCommit: map[string]time.Time{"nebula/with-diff": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		diffAdded:   map[string][]string{"main|nebula/with-diff": {"internal/new"}},
		diffMod:     map[string][]string{"main|nebula/with-diff": {"internal/existing", "cmd"}},
	}

	scanner := &Scanner{
		Git:        git,
		NebulasDir: base,
		Repo:       "test/repo",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	e := st.Nebulas[0]
	if len(e.PackagesAdded) != 1 || e.PackagesAdded[0] != "internal/new" {
		t.Errorf("packages_added = %v, want [internal/new]", e.PackagesAdded)
	}
	if len(e.PackagesModified) != 2 {
		t.Errorf("packages_modified = %v, want [internal/existing cmd]", e.PackagesModified)
	}
	if len(e.Areas) != 3 {
		t.Errorf("areas = %v, want 3 entries", e.Areas)
	}
}

func TestScanHeader(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), ".nebulas")
	writeNebulaDir(t, base, "alpha", "feature", nil, nil)

	scanner := &Scanner{
		Git:        &mockGitQuerier{branches: map[string]bool{}},
		NebulasDir: base,
		Repo:       "github.com/papapumpkin/quasar",
	}

	st, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if st.Relativity.Version != 1 {
		t.Errorf("version = %d, want 1", st.Relativity.Version)
	}
	if st.Relativity.Repo != "github.com/papapumpkin/quasar" {
		t.Errorf("repo = %q, want github.com/papapumpkin/quasar", st.Relativity.Repo)
	}
	if st.Relativity.LastScan.IsZero() {
		t.Error("last_scan should not be zero")
	}
}

func TestMergeAbandonedEntries(t *testing.T) {
	t.Parallel()

	existing := &Spacetime{
		Nebulas: []Entry{
			{Name: "alive", Status: StatusCompleted, Summary: "Still here"},
			{Name: "removed", Status: StatusInProgress, Summary: "Going away"},
		},
	}
	scanned := &Spacetime{
		Nebulas: []Entry{
			{Name: "alive", Status: StatusCompleted},
		},
	}

	merged := Merge(existing, scanned)

	if len(merged.Nebulas) != 2 {
		t.Fatalf("nebulas count = %d, want 2", len(merged.Nebulas))
	}

	// Verify the alive entry preserved its summary.
	if merged.Nebulas[0].Summary != "Still here" {
		t.Errorf("alive summary = %q, want %q", merged.Nebulas[0].Summary, "Still here")
	}

	// Verify the removed entry is marked abandoned.
	if merged.Nebulas[1].Name != "removed" {
		t.Errorf("second entry name = %q, want %q", merged.Nebulas[1].Name, "removed")
	}
	if merged.Nebulas[1].Status != StatusAbandoned {
		t.Errorf("removed status = %q, want %q", merged.Nebulas[1].Status, StatusAbandoned)
	}
	if merged.Nebulas[1].Summary != "Going away" {
		t.Errorf("removed summary = %q, want preserved", merged.Nebulas[1].Summary)
	}
}
