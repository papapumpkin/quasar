package artifacts

import "testing"

func TestParseStarCheckpoint(t *testing.T) {
	t.Parallel()

	t.Run("strict lint rejects an unsupported triggers key", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		l.Strict = true
		writeFile(t, repo, "stars/pyworker.md", `+++
name = "pyworker"
model = "claude-sonnet-4-6"

[checkpoint]
enabled = true
triggers = ["python -m pytest --collect-only", "ruff check ."]
+++

You are a Python worker.
`)
		// triggers is not a real knob (checkpointing is per-dispatch, not
		// per-build), so strict loading must surface it rather than silently
		// accept a list that has no effect.
		if _, err := l.LoadStar("pyworker"); err == nil {
			t.Fatal("expected strict load to reject the unknown 'triggers' key, got nil")
		}
	})

	t.Run("absent block defaults to enabled", func(t *testing.T) {
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
