package tui

import (
	"strings"
	"testing"
	"time"
)

func TestStatusBarView(t *testing.T) {
	t.Parallel()

	t.Run("FinalElapsed freezes timer", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			StartTime:    time.Now().Add(-10 * time.Minute),
			FinalElapsed: 5 * time.Second,
			Width:        80,
		}
		view := sb.View()
		if !strings.Contains(view, "5s") {
			t.Errorf("expected frozen elapsed 5s in view, got: %s", view)
		}
		// Should NOT contain a 10m elapsed time.
		if strings.Contains(view, "10m") {
			t.Errorf("expected timer to be frozen at 5s, but found 10m in view: %s", view)
		}
	})

	t.Run("live timer when FinalElapsed is zero", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			StartTime: time.Now().Add(-3 * time.Second),
			Width:     80,
		}
		view := sb.View()
		if !strings.Contains(view, "3s") {
			t.Errorf("expected live elapsed ~3s in view, got: %s", view)
		}
	})

	t.Run("no elapsed when StartTime is zero and FinalElapsed is zero", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Width: 80,
		}
		view := sb.View()
		// Should not contain any time-like pattern.
		if strings.Contains(view, "0s") {
			t.Errorf("expected no elapsed time rendered, got: %s", view)
		}
	})

	t.Run("nebula mode shows progress", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Name:      "test-nebula",
			Total:     5,
			Completed: 2,
			Width:     120,
		}
		view := sb.View()
		if !strings.Contains(view, "nebula") {
			t.Errorf("expected nebula label in view, got: %s", view)
		}
		if !strings.Contains(view, "2/5") {
			t.Errorf("expected 2/5 progress in view, got: %s", view)
		}
	})

	t.Run("loop mode shows cycle", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			BeadID:    "quasar-abc",
			Cycle:     2,
			MaxCycles: 5,
			Width:     120,
		}
		view := sb.View()
		if !strings.Contains(view, "quasar-abc") {
			t.Errorf("expected bead ID in view, got: %s", view)
		}
		if !strings.Contains(view, "2/5") {
			t.Errorf("expected cycle 2/5 in view, got: %s", view)
		}
	})
}
