package repos

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates parent dirs and writes a file under root.
func writeFile(t *testing.T, root, rel string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return path
}

func TestResolver_OverridePaths(t *testing.T) {
	root := t.TempDir()
	constOverride := writeFile(t, root, "constellations/coder-reviewer.toml")
	starOverride := writeFile(t, root, "stars/architect.md")
	skillOverride := writeFile(t, root, "skills/go-style.md")

	res, err := NewResolver(&Repo{Path: root})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"constellation override", res.ConstellationPath("coder-reviewer"), constOverride},
		{"constellation embedded", res.ConstellationPath("missing"), EmbeddedPath},
		{"star override", res.StarPath("architect"), starOverride},
		{"star embedded", res.StarPath("missing"), EmbeddedPath},
		{"skill override", res.SkillPath("go-style"), skillOverride},
		{"skill embedded", res.SkillPath("missing"), EmbeddedPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestResolver_SensorPaths(t *testing.T) {
	root := t.TempDir()
	gh := writeFile(t, root, "sensors/github.toml")
	jira := writeFile(t, root, "sensors/jira.toml")
	// A non-toml file must be ignored by AllSensorPaths.
	writeFile(t, root, "sensors/README.md")

	res, err := NewResolver(&Repo{Path: root})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	t.Run("configured sensor", func(t *testing.T) {
		got, err := res.SensorPath("github")
		if err != nil {
			t.Fatalf("SensorPath: %v", err)
		}
		if got != gh {
			t.Errorf("got %q, want %q", got, gh)
		}
	})

	t.Run("unconfigured sensor", func(t *testing.T) {
		_, err := res.SensorPath("nonexistent")
		if !errors.Is(err, ErrSensorNotConfigured) {
			t.Errorf("err = %v, want ErrSensorNotConfigured", err)
		}
	})

	t.Run("all sensor paths", func(t *testing.T) {
		got, err := res.AllSensorPaths()
		if err != nil {
			t.Fatalf("AllSensorPaths: %v", err)
		}
		want := []string{gh, jira} // sorted lexically
		if len(got) != len(want) {
			t.Fatalf("got %d paths %v, want %d", len(got), got, len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("path[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
}

func TestResolver_NoSensorsDir(t *testing.T) {
	res, err := NewResolver(&Repo{Path: t.TempDir()})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	paths, err := res.AllSensorPaths()
	if err != nil {
		t.Fatalf("AllSensorPaths: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("got %v, want empty", paths)
	}
}

func TestResolver_Config(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".quasar.yaml"),
		[]byte("github:\n  base_branch: \"trunk\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := NewResolver(&Repo{Path: root})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if got := res.Config().GitHub.BaseBranch; got != "trunk" {
		t.Errorf("BaseBranch = %q, want trunk", got)
	}
}

func TestResolver_MissingConfigAppliesDefaults(t *testing.T) {
	res, err := NewResolver(&Repo{Path: t.TempDir()})
	if err != nil {
		t.Fatalf("NewResolver should tolerate missing .quasar.yaml: %v", err)
	}
	// A repo without a .quasar.yaml must fall back to the same built-in
	// defaults an empty config file would produce — not a zero-valued Config.
	cfg := res.Config()
	if cfg.MaxReviewCycles != 3 {
		t.Errorf("MaxReviewCycles = %d, want default 3", cfg.MaxReviewCycles)
	}
	if cfg.MaxBudgetUSD != 5.0 {
		t.Errorf("MaxBudgetUSD = %v, want default 5.0", cfg.MaxBudgetUSD)
	}
	if !cfg.PreCommit.FailOnError {
		t.Error("PreCommit.FailOnError = false, want default true")
	}
}

func TestNewResolver_NilRepo(t *testing.T) {
	if _, err := NewResolver(nil); err == nil {
		t.Error("NewResolver(nil) = nil error, want error")
	}
}
