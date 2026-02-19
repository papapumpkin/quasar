package relativity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestLockFileRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)
	original := &LockFile{
		Version:     1,
		GeneratedAt: now,
		SourceHash:  "sha256:abc123def456",
		Graph: Graph{
			Order: []string{"tui-landing-page", "dag-engine", "relativity"},
			Waves: [][]string{
				{"tui-landing-page"},
				{"dag-engine", "relativity"},
			},
		},
		Metrics: []Metric{
			{
				Name:            "tui-landing-page",
				Wave:            1,
				ImpactScore:     0.85,
				Centrality:      0.6,
				DownstreamCount: 2,
				AreaOverlap:     []string{"cmd"},
			},
			{
				Name:            "dag-engine",
				Wave:            2,
				ImpactScore:     0.72,
				Centrality:      0.3,
				DownstreamCount: 0,
				AreaOverlap:     []string{},
			},
		},
		Staleness: Staleness{
			NebulaCount:   3,
			LastGitCommit: "abc1234",
			BranchTips:    map[string]string{"nebula/dag-engine": "def5678"},
		},
	}

	data, err := toml.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded LockFile
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify top-level fields.
	if decoded.Version != 1 {
		t.Errorf("version = %d, want 1", decoded.Version)
	}
	if !decoded.GeneratedAt.Equal(now) {
		t.Errorf("generated_at = %v, want %v", decoded.GeneratedAt, now)
	}
	if decoded.SourceHash != "sha256:abc123def456" {
		t.Errorf("source_hash = %q, want %q", decoded.SourceHash, "sha256:abc123def456")
	}

	// Verify graph.
	if len(decoded.Graph.Order) != 3 {
		t.Fatalf("order count = %d, want 3", len(decoded.Graph.Order))
	}
	if decoded.Graph.Order[0] != "tui-landing-page" {
		t.Errorf("order[0] = %q, want %q", decoded.Graph.Order[0], "tui-landing-page")
	}
	if len(decoded.Graph.Waves) != 2 {
		t.Fatalf("waves count = %d, want 2", len(decoded.Graph.Waves))
	}
	if len(decoded.Graph.Waves[0]) != 1 || decoded.Graph.Waves[0][0] != "tui-landing-page" {
		t.Errorf("waves[0] = %v, want [tui-landing-page]", decoded.Graph.Waves[0])
	}

	// Verify metrics.
	if len(decoded.Metrics) != 2 {
		t.Fatalf("metrics count = %d, want 2", len(decoded.Metrics))
	}
	m := decoded.Metrics[0]
	if m.Name != "tui-landing-page" {
		t.Errorf("metrics[0].name = %q, want %q", m.Name, "tui-landing-page")
	}
	if m.Wave != 1 {
		t.Errorf("metrics[0].wave = %d, want 1", m.Wave)
	}
	if m.ImpactScore != 0.85 {
		t.Errorf("metrics[0].impact_score = %f, want 0.85", m.ImpactScore)
	}
	if m.DownstreamCount != 2 {
		t.Errorf("metrics[0].downstream_count = %d, want 2", m.DownstreamCount)
	}

	// Verify staleness.
	if decoded.Staleness.NebulaCount != 3 {
		t.Errorf("nebula_count = %d, want 3", decoded.Staleness.NebulaCount)
	}
	if decoded.Staleness.LastGitCommit != "abc1234" {
		t.Errorf("last_git_commit = %q, want %q", decoded.Staleness.LastGitCommit, "abc1234")
	}
	if tip, ok := decoded.Staleness.BranchTips["nebula/dag-engine"]; !ok || tip != "def5678" {
		t.Errorf("branch_tips[nebula/dag-engine] = %q, want %q", tip, "def5678")
	}
}

func TestLoadSaveLockRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".relativity", "spacetime.lock")

	now := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)
	original := &LockFile{
		Version:     1,
		GeneratedAt: now,
		SourceHash:  "sha256:test",
		Graph: Graph{
			Order: []string{"alpha"},
			Waves: [][]string{{"alpha"}},
		},
		Metrics: []Metric{
			{Name: "alpha", Wave: 1, ImpactScore: 1.0},
		},
		Staleness: Staleness{
			NebulaCount:   1,
			LastGitCommit: "aaa111",
			BranchTips:    map[string]string{},
		},
	}

	if err := SaveLock(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadLock(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded lock is nil")
	}

	if loaded.Version != 1 {
		t.Errorf("version = %d, want 1", loaded.Version)
	}
	if loaded.SourceHash != "sha256:test" {
		t.Errorf("source_hash = %q, want %q", loaded.SourceHash, "sha256:test")
	}
	if len(loaded.Graph.Order) != 1 || loaded.Graph.Order[0] != "alpha" {
		t.Errorf("order = %v, want [alpha]", loaded.Graph.Order)
	}
	if len(loaded.Metrics) != 1 || loaded.Metrics[0].Name != "alpha" {
		t.Errorf("metrics = %v, want [{alpha ...}]", loaded.Metrics)
	}
}

func TestLoadLockNonExistentFile(t *testing.T) {
	t.Parallel()

	lf, err := LoadLock("/nonexistent/path/spacetime.lock")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if lf != nil {
		t.Error("expected nil for missing file")
	}
}

func TestLoadLockInvalidTOML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "spacetime.lock")

	if err := os.WriteFile(path, []byte("not valid toml {{{}}}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadLock(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
	if !strings.Contains(err.Error(), "parsing spacetime.lock") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "parsing spacetime.lock")
	}
}

func TestTopologicalOrder(t *testing.T) {
	t.Parallel()

	t.Run("linear chain", func(t *testing.T) {
		entries := []Entry{
			{Name: "c", BuildsOn: []string{"b"}},
			{Name: "a"},
			{Name: "b", BuildsOn: []string{"a"}},
		}
		order := topologicalOrder(entries)
		if len(order) != 3 {
			t.Fatalf("order count = %d, want 3", len(order))
		}
		// a must come before b, b before c.
		indexOf := make(map[string]int)
		for i, n := range order {
			indexOf[n] = i
		}
		if indexOf["a"] >= indexOf["b"] {
			t.Errorf("a (%d) should come before b (%d)", indexOf["a"], indexOf["b"])
		}
		if indexOf["b"] >= indexOf["c"] {
			t.Errorf("b (%d) should come before c (%d)", indexOf["b"], indexOf["c"])
		}
	})

	t.Run("diamond dependency", func(t *testing.T) {
		entries := []Entry{
			{Name: "d", BuildsOn: []string{"b", "c"}},
			{Name: "b", BuildsOn: []string{"a"}},
			{Name: "c", BuildsOn: []string{"a"}},
			{Name: "a"},
		}
		order := topologicalOrder(entries)
		indexOf := make(map[string]int)
		for i, n := range order {
			indexOf[n] = i
		}
		if indexOf["a"] >= indexOf["b"] || indexOf["a"] >= indexOf["c"] {
			t.Errorf("a should come before b and c: %v", order)
		}
		if indexOf["b"] >= indexOf["d"] || indexOf["c"] >= indexOf["d"] {
			t.Errorf("b and c should come before d: %v", order)
		}
	})

	t.Run("no dependencies", func(t *testing.T) {
		entries := []Entry{
			{Name: "beta"},
			{Name: "alpha"},
			{Name: "gamma"},
		}
		order := topologicalOrder(entries)
		// With no deps, should be alphabetical.
		if order[0] != "alpha" || order[1] != "beta" || order[2] != "gamma" {
			t.Errorf("order = %v, want [alpha beta gamma]", order)
		}
	})

	t.Run("empty", func(t *testing.T) {
		order := topologicalOrder(nil)
		if order != nil {
			t.Errorf("order = %v, want nil", order)
		}
	})

	t.Run("unknown dependency ignored", func(t *testing.T) {
		entries := []Entry{
			{Name: "a", BuildsOn: []string{"unknown"}},
			{Name: "b"},
		}
		order := topologicalOrder(entries)
		if len(order) != 2 {
			t.Fatalf("order count = %d, want 2", len(order))
		}
	})
}

func TestAssignWaves(t *testing.T) {
	t.Parallel()

	t.Run("independent nebulas share wave", func(t *testing.T) {
		entries := []Entry{
			{Name: "alpha"},
			{Name: "beta"},
			{Name: "gamma"},
		}
		waves := assignWaves(entries)
		if len(waves) != 1 {
			t.Fatalf("waves count = %d, want 1", len(waves))
		}
		if len(waves[0]) != 3 {
			t.Errorf("wave[0] count = %d, want 3", len(waves[0]))
		}
	})

	t.Run("dependency chain creates sequential waves", func(t *testing.T) {
		entries := []Entry{
			{Name: "c", BuildsOn: []string{"b"}},
			{Name: "a"},
			{Name: "b", BuildsOn: []string{"a"}},
		}
		waves := assignWaves(entries)
		if len(waves) != 3 {
			t.Fatalf("waves count = %d, want 3", len(waves))
		}
		if waves[0][0] != "a" {
			t.Errorf("wave[0] = %v, want [a]", waves[0])
		}
		if waves[1][0] != "b" {
			t.Errorf("wave[1] = %v, want [b]", waves[1])
		}
		if waves[2][0] != "c" {
			t.Errorf("wave[2] = %v, want [c]", waves[2])
		}
	})

	t.Run("parallel branches merge at diamond", func(t *testing.T) {
		entries := []Entry{
			{Name: "root"},
			{Name: "left", BuildsOn: []string{"root"}},
			{Name: "right", BuildsOn: []string{"root"}},
			{Name: "join", BuildsOn: []string{"left", "right"}},
		}
		waves := assignWaves(entries)
		if len(waves) != 3 {
			t.Fatalf("waves count = %d, want 3", len(waves))
		}
		// Wave 1: root; Wave 2: left, right; Wave 3: join.
		if waves[0][0] != "root" {
			t.Errorf("wave[0] = %v, want [root]", waves[0])
		}
		if len(waves[1]) != 2 {
			t.Errorf("wave[1] count = %d, want 2", len(waves[1]))
		}
		if waves[2][0] != "join" {
			t.Errorf("wave[2] = %v, want [join]", waves[2])
		}
	})

	t.Run("empty", func(t *testing.T) {
		waves := assignWaves(nil)
		if waves != nil {
			t.Errorf("waves = %v, want nil", waves)
		}
	})
}

func TestComputeMetrics(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{
			Name:    "core",
			Areas:   []string{"internal/core", "cmd", "internal/util"},
			Enables: []string{"ext", "app"},
		},
		{
			Name:     "ext",
			Areas:    []string{"internal/ext", "cmd"},
			Enables:  []string{"app"},
			BuildsOn: []string{"core"},
		},
		{
			Name:     "app",
			Areas:    []string{"cmd"},
			BuildsOn: []string{"core", "ext"},
		},
	}

	waveMap := map[string]int{
		"core": 1,
		"ext":  2,
		"app":  3,
	}
	metrics := computeMetrics(entries, waveMap)

	if len(metrics) != 3 {
		t.Fatalf("metrics count = %d, want 3", len(metrics))
	}

	byName := make(map[string]Metric)
	for _, m := range metrics {
		byName[m.Name] = m
	}

	t.Run("impact scores normalized", func(t *testing.T) {
		// core has 3 areas (max), ext has 2, app has 1.
		if byName["core"].ImpactScore != 1.0 {
			t.Errorf("core impact = %f, want 1.0", byName["core"].ImpactScore)
		}
		expected := 2.0 / 3.0
		if diff := byName["ext"].ImpactScore - expected; diff > 0.001 || diff < -0.001 {
			t.Errorf("ext impact = %f, want ~%f", byName["ext"].ImpactScore, expected)
		}
	})

	t.Run("centrality normalized", func(t *testing.T) {
		// core enables 2 (max), ext enables 1, app enables 0.
		if byName["core"].Centrality != 1.0 {
			t.Errorf("core centrality = %f, want 1.0", byName["core"].Centrality)
		}
		if byName["ext"].Centrality != 0.5 {
			t.Errorf("ext centrality = %f, want 0.5", byName["ext"].Centrality)
		}
		if byName["app"].Centrality != 0.0 {
			t.Errorf("app centrality = %f, want 0.0", byName["app"].Centrality)
		}
	})

	t.Run("downstream counts transitive", func(t *testing.T) {
		// core -> ext -> app and core -> app, so core has 2 downstream.
		if byName["core"].DownstreamCount != 2 {
			t.Errorf("core downstream = %d, want 2", byName["core"].DownstreamCount)
		}
		// ext -> app, so ext has 1 downstream.
		if byName["ext"].DownstreamCount != 1 {
			t.Errorf("ext downstream = %d, want 1", byName["ext"].DownstreamCount)
		}
		if byName["app"].DownstreamCount != 0 {
			t.Errorf("app downstream = %d, want 0", byName["app"].DownstreamCount)
		}
	})

	t.Run("area overlap computed", func(t *testing.T) {
		// "cmd" is shared by all three.
		if len(byName["core"].AreaOverlap) == 0 {
			t.Error("core should have area overlap")
		}
		found := false
		for _, a := range byName["core"].AreaOverlap {
			if a == "cmd" {
				found = true
			}
		}
		if !found {
			t.Errorf("core area_overlap = %v, want to contain 'cmd'", byName["core"].AreaOverlap)
		}
	})

	t.Run("wave numbers assigned", func(t *testing.T) {
		if byName["core"].Wave != 1 {
			t.Errorf("core wave = %d, want 1", byName["core"].Wave)
		}
		if byName["ext"].Wave != 2 {
			t.Errorf("ext wave = %d, want 2", byName["ext"].Wave)
		}
		if byName["app"].Wave != 3 {
			t.Errorf("app wave = %d, want 3", byName["app"].Wave)
		}
	})
}

func TestComputeMetricsEmpty(t *testing.T) {
	t.Parallel()

	metrics := computeMetrics(nil, nil)
	if metrics != nil {
		t.Errorf("metrics = %v, want nil", metrics)
	}
}

func TestComputeMetricsNoAreas(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Name: "bare"},
	}
	waveMap := map[string]int{"bare": 1}
	metrics := computeMetrics(entries, waveMap)

	if len(metrics) != 1 {
		t.Fatalf("count = %d, want 1", len(metrics))
	}
	if metrics[0].ImpactScore != 0 {
		t.Errorf("impact = %f, want 0", metrics[0].ImpactScore)
	}
	if metrics[0].Centrality != 0 {
		t.Errorf("centrality = %f, want 0", metrics[0].Centrality)
	}
}

func TestGenerateLock(t *testing.T) {
	t.Parallel()

	st := &Spacetime{
		Relativity: Header{Version: 1},
		Nebulas: []Entry{
			{
				Name:    "core",
				Areas:   []string{"internal/core"},
				Enables: []string{"ext"},
			},
			{
				Name:     "ext",
				Areas:    []string{"internal/ext"},
				BuildsOn: []string{"core"},
			},
		},
	}

	tips := map[string]string{"nebula/core": "aaa", "nebula/ext": "bbb"}
	lf := GenerateLock(st, "sha256:test", "HEAD123", tips)

	if lf.Version != 1 {
		t.Errorf("version = %d, want 1", lf.Version)
	}
	if lf.SourceHash != "sha256:test" {
		t.Errorf("source_hash = %q, want %q", lf.SourceHash, "sha256:test")
	}
	if lf.GeneratedAt.IsZero() {
		t.Error("generated_at should not be zero")
	}

	// Graph: core must come before ext.
	if len(lf.Graph.Order) != 2 {
		t.Fatalf("order count = %d, want 2", len(lf.Graph.Order))
	}
	if lf.Graph.Order[0] != "core" {
		t.Errorf("order[0] = %q, want %q", lf.Graph.Order[0], "core")
	}

	// Waves: core in wave 1, ext in wave 2.
	if len(lf.Graph.Waves) != 2 {
		t.Fatalf("waves count = %d, want 2", len(lf.Graph.Waves))
	}

	// Metrics should have entries.
	if len(lf.Metrics) != 2 {
		t.Fatalf("metrics count = %d, want 2", len(lf.Metrics))
	}

	// Staleness.
	if lf.Staleness.NebulaCount != 2 {
		t.Errorf("nebula_count = %d, want 2", lf.Staleness.NebulaCount)
	}
	if lf.Staleness.LastGitCommit != "HEAD123" {
		t.Errorf("last_git_commit = %q, want %q", lf.Staleness.LastGitCommit, "HEAD123")
	}
	if lf.Staleness.BranchTips["nebula/core"] != "aaa" {
		t.Errorf("branch_tips[nebula/core] = %q, want %q", lf.Staleness.BranchTips["nebula/core"], "aaa")
	}
}

func TestIsStale(t *testing.T) {
	t.Parallel()

	baseLock := &LockFile{
		Version:    1,
		SourceHash: "sha256:abc",
		Staleness: Staleness{
			NebulaCount:   2,
			LastGitCommit: "HEAD1",
			BranchTips:    map[string]string{"nebula/a": "tip1"},
		},
	}

	t.Run("nil lock is stale", func(t *testing.T) {
		if !IsStale(nil, "sha256:abc", 2, "HEAD1", map[string]string{"nebula/a": "tip1"}) {
			t.Error("nil lock should be stale")
		}
	})

	t.Run("matching state is not stale", func(t *testing.T) {
		if IsStale(baseLock, "sha256:abc", 2, "HEAD1", map[string]string{"nebula/a": "tip1"}) {
			t.Error("matching state should not be stale")
		}
	})

	t.Run("different hash is stale", func(t *testing.T) {
		if !IsStale(baseLock, "sha256:different", 2, "HEAD1", map[string]string{"nebula/a": "tip1"}) {
			t.Error("different hash should be stale")
		}
	})

	t.Run("different nebula count is stale", func(t *testing.T) {
		if !IsStale(baseLock, "sha256:abc", 5, "HEAD1", map[string]string{"nebula/a": "tip1"}) {
			t.Error("different count should be stale")
		}
	})

	t.Run("different HEAD is stale", func(t *testing.T) {
		if !IsStale(baseLock, "sha256:abc", 2, "HEAD2", map[string]string{"nebula/a": "tip1"}) {
			t.Error("different HEAD should be stale")
		}
	})

	t.Run("advanced branch tip is stale", func(t *testing.T) {
		if !IsStale(baseLock, "sha256:abc", 2, "HEAD1", map[string]string{"nebula/a": "tip2"}) {
			t.Error("advanced branch tip should be stale")
		}
	})

	t.Run("new branch is stale", func(t *testing.T) {
		tips := map[string]string{"nebula/a": "tip1", "nebula/b": "tip2"}
		if !IsStale(baseLock, "sha256:abc", 2, "HEAD1", tips) {
			t.Error("new branch should be stale")
		}
	})

	t.Run("removed branch is stale", func(t *testing.T) {
		if !IsStale(baseLock, "sha256:abc", 2, "HEAD1", map[string]string{}) {
			t.Error("removed branch should be stale")
		}
	})
}

func TestHashFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")

	content := []byte("version = 1\nrepo = \"test\"\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	hash, err := HashFile(path)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("hash = %q, want sha256: prefix", hash)
	}
	if len(hash) < 71 { // "sha256:" (7) + 64 hex chars
		t.Errorf("hash too short: %q", hash)
	}

	// Same content should produce the same hash.
	hash2, err := HashFile(path)
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}
	if hash != hash2 {
		t.Errorf("hash mismatch: %q != %q", hash, hash2)
	}

	// Different content should produce a different hash.
	path2 := filepath.Join(dir, "test2.toml")
	if err := os.WriteFile(path2, []byte("different"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	hash3, err := HashFile(path2)
	if err != nil {
		t.Fatalf("hash3: %v", err)
	}
	if hash == hash3 {
		t.Error("different content should produce different hash")
	}
}

func TestHashFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := HashFile("/nonexistent/file.toml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
