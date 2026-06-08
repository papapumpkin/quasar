package artifacts

import "testing"

func TestLoadStarCoordinationAware(t *testing.T) {
	t.Parallel()

	t.Run("absent key defaults to true", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "stars/plain.md", `+++
name = "plain"
model = "claude-sonnet-4-6"
+++

A coder with no coordination block.
`)
		star, err := l.LoadStar("plain")
		if err != nil {
			t.Fatalf("LoadStar: %v", err)
		}
		if !star.CoordinationAware {
			t.Error("absent coordination_aware should default to true")
		}
	})

	t.Run("can be explicitly disabled", func(t *testing.T) {
		t.Parallel()
		l, repo := newTestLoader(t)
		writeFile(t, repo, "stars/architect.md", `+++
name = "architect"
model = "claude-sonnet-4-6"
coordination_aware = false
+++

An architect that skips the pre-flight coordination check.
`)
		star, err := l.LoadStar("architect")
		if err != nil {
			t.Fatalf("LoadStar: %v", err)
		}
		if star.CoordinationAware {
			t.Error("coordination_aware = false should disable the check")
		}
	})
}
