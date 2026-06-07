package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/repos"
)

// newTestLoader builds a Loader rooted at an empty temp repo, so every LoadX
// falls through to the embedded defaults unless the test writes an override.
func newTestLoader(t *testing.T) (*Loader, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := repos.NewResolver(&repos.Repo{Path: dir})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return New(r), dir
}

// writeFile writes content to <repo>/<rel>, creating parent dirs.
func writeFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestLoadStar(t *testing.T) {
	t.Parallel()

	t.Run("embedded fallback resolves skills", func(t *testing.T) {
		t.Parallel()
		l, _ := newTestLoader(t)

		star, err := l.LoadStar("coder")
		if err != nil {
			t.Fatalf("LoadStar: %v", err)
		}
		if star.Model != "claude-sonnet-4-6" {
			t.Errorf("model = %q", star.Model)
		}
		// Prompt is skill fragments + star body.
		if !strings.Contains(star.Prompt, "You are the coder") {
			t.Error("prompt missing star body")
		}
		if !strings.Contains(star.Prompt, "You have git access") {
			t.Error("prompt missing git-aware skill fragment")
		}
		if !strings.Contains(star.Prompt, "stable cache prefix") {
			t.Error("prompt missing prompt-cache-aware skill fragment")
		}
		// Tools.Allowed is the union of base + each skill's tools_add.
		if !containsStr(star.Tools.Allowed, "Bash(git log *)") {
			t.Errorf("allowed missing skill-added tool: %v", star.Tools.Allowed)
		}
		if countStr(star.Tools.Allowed, "Bash(git diff *)") != 1 {
			t.Errorf("duplicate tool in union: %v", star.Tools.Allowed)
		}
	})

	t.Run("per-repo override wins", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "stars/coder.md", "+++\nname = \"coder\"\nmodel = \"custom-model\"\n+++\n\nOverride body.\n")

		star, err := l.LoadStar("coder")
		if err != nil {
			t.Fatalf("LoadStar: %v", err)
		}
		if star.Model != "custom-model" {
			t.Errorf("override not used: model = %q", star.Model)
		}
		if !strings.HasSuffix(star.SourcePath, filepath.Join("stars", "coder.md")) {
			t.Errorf("source path not the override: %q", star.SourcePath)
		}
	})

	t.Run("unknown skill reference is an error", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "stars/bad.md", "+++\nname = \"bad\"\nskills = [\"does-not-exist\"]\n+++\n\nBody.\n")

		if _, err := l.LoadStar("bad"); err == nil {
			t.Fatal("expected error for unknown skill")
		}
	})
}

func TestLoadConstellation(t *testing.T) {
	t.Parallel()
	l, _ := newTestLoader(t)

	c, err := l.LoadConstellation("coder-reviewer")
	if err != nil {
		t.Fatalf("LoadConstellation: %v", err)
	}
	if len(c.Nodes) != 5 {
		t.Fatalf("nodes = %d, want 5", len(c.Nodes))
	}
	if len(c.Edges) != 7 {
		t.Fatalf("edges = %d, want 7", len(c.Edges))
	}

	// Safety wiring: the commit is a runtime-owned builtin node, not the coder
	// star, so the [pre_commit] gate runs and every git write stays inside the
	// gitops perimeter. Guard against a regression that lets a star self-commit.
	commit := findNodeByID(t, c, "commit")
	if commit.Type != NodeBuiltin || commit.Op != "commit" {
		t.Errorf("commit node = %+v, want builtin op=commit", commit)
	}

	// Expressions are pre-compiled: the unconditional check is that When is a
	// usable Expression, not a deferred string. The approve edge runs from the
	// reviewer-decision node (decide), which exposes the typed `approved` flag.
	approvedEdge := findEdge(t, c, "decide", "_done")
	got, err := approvedEdge.When.Eval(State{"nodes": map[string]any{"decide": map[string]any{"approved": true}}})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got != true {
		t.Errorf("decide.approved guard = %v, want true", got)
	}

	// Node inputs are compiled interpolation templates.
	impl := c.Nodes[0]
	if impl.Type != NodeStar || impl.Star != "coder" {
		t.Errorf("unexpected first node: %+v", impl)
	}
	if impl.Inputs["phase_title"] == nil {
		t.Error("node input not compiled")
	}
}

func TestLoadSensorInstance(t *testing.T) {
	t.Parallel()

	t.Run("opaque config and parsed interval", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "sensors/gh.toml", `name = "gh"
type = "github_issues"
poll_interval = "5m"

[config]
repo = "papapumpkin/quasar"
token_env = "GITHUB_TOKEN"

[[triggers]]
constellation = "architect"
when = "new_item"
`)
		s, err := l.LoadSensorInstance("gh")
		if err != nil {
			t.Fatalf("LoadSensorInstance: %v", err)
		}
		if s.PollInterval.Minutes() != 5 {
			t.Errorf("interval = %v", s.PollInterval)
		}
		if s.Config["repo"] != "papapumpkin/quasar" {
			t.Errorf("config not opaque: %#v", s.Config)
		}
		if len(s.Triggers) != 1 || s.Triggers[0].Constellation != "architect" {
			t.Errorf("triggers = %+v", s.Triggers)
		}
	})

	t.Run("inline token is rejected", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "sensors/bad.toml", "name=\"bad\"\ntype=\"github_issues\"\n[config]\ntoken = \"secret\"\n")
		if _, err := l.LoadSensorInstance("bad"); err == nil {
			t.Fatal("expected inline-token error")
		}
	})
}

func findEdge(t *testing.T, c *Constellation, from, to string) ConstellationEdge {
	t.Helper()
	for _, e := range c.Edges {
		if e.From == from && e.To == to {
			return e
		}
	}
	t.Fatalf("edge %s->%s not found", from, to)
	return ConstellationEdge{}
}

func findNodeByID(t *testing.T, c *Constellation, id string) ConstellationNode {
	t.Helper()
	for _, n := range c.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found", id)
	return ConstellationNode{}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func countStr(ss []string, want string) int {
	n := 0
	for _, s := range ss {
		if s == want {
			n++
		}
	}
	return n
}
