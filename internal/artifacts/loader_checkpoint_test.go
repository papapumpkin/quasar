package artifacts

import "testing"

func TestParseStarCheckpoint(t *testing.T) {
	t.Parallel()

	t.Run("override triggers take effect", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "stars/pyworker.md", `+++
name = "pyworker"
model = "claude-sonnet-4-6"

[checkpoint]
enabled = true
triggers = ["python -m pytest --collect-only", "ruff check ."]
+++

You are a Python worker.
`)
		star, err := l.LoadStar("pyworker")
		if err != nil {
			t.Fatalf("LoadStar: %v", err)
		}
		if !star.Checkpoint.Enabled {
			t.Error("Enabled = false, want true")
		}
		want := []string{"python -m pytest --collect-only", "ruff check ."}
		if len(star.Checkpoint.Triggers) != len(want) {
			t.Fatalf("triggers = %v, want %v", star.Checkpoint.Triggers, want)
		}
		for i := range want {
			if star.Checkpoint.Triggers[i] != want[i] {
				t.Errorf("triggers[%d] = %q, want %q", i, star.Checkpoint.Triggers[i], want[i])
			}
		}
	})

	t.Run("absent block defaults to enabled with no overrides", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "stars/plain.md", `+++
name = "plain"
model = "claude-sonnet-4-6"
+++

A star with no checkpoint block.
`)
		star, err := l.LoadStar("plain")
		if err != nil {
			t.Fatalf("LoadStar: %v", err)
		}
		if !star.Checkpoint.Enabled {
			t.Error("absent [checkpoint] should default Enabled to true")
		}
		if len(star.Checkpoint.Triggers) != 0 {
			t.Errorf("expected no triggers, got %v", star.Checkpoint.Triggers)
		}
	})

	t.Run("enabled can be explicitly disabled", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "stars/off.md", `+++
name = "off"
model = "claude-sonnet-4-6"

[checkpoint]
enabled = false
+++

Checkpointing disabled.
`)
		star, err := l.LoadStar("off")
		if err != nil {
			t.Fatalf("LoadStar: %v", err)
		}
		if star.Checkpoint.Enabled {
			t.Error("Enabled = true, want false")
		}
	})
}
