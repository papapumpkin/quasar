package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
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

	t.Run("single line output never exceeds width", func(t *testing.T) {
		t.Parallel()
		for _, width := range []int{40, 60, 80, 120, 200} {
			sb := StatusBar{
				Name:      "very-long-nebula-task-name-that-should-be-truncated",
				Total:     10,
				Completed: 3,
				CostUSD:   1.24,
				BudgetUSD: 10.00,
				StartTime: time.Now().Add(-5 * time.Minute),
				Width:     width,
			}
			view := sb.View()
			lines := strings.Split(view, "\n")
			for i, line := range lines {
				w := lipgloss.Width(line)
				if w > width {
					t.Errorf("width=%d: line %d has width %d > %d: %q", width, i, w, width, line)
				}
			}
		}
	})

	t.Run("narrow width truncates name with ellipsis", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Name:      "extremely-long-name-that-exceeds-any-reasonable-width",
			Total:     3,
			Completed: 1,
			Width:     50,
		}
		view := sb.View()
		if !strings.Contains(view, "...") {
			t.Errorf("expected truncation ellipsis in narrow view, got: %s", view)
		}
	})

	t.Run("stopping indicator shown", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Width:    120,
			Stopping: true,
		}
		view := sb.View()
		if !strings.Contains(view, "STOPPING") {
			t.Errorf("expected STOPPING indicator in view, got: %s", view)
		}
	})

	t.Run("paused indicator shown", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Width:  120,
			Paused: true,
		}
		view := sb.View()
		if !strings.Contains(view, "PAUSED") {
			t.Errorf("expected PAUSED indicator in view, got: %s", view)
		}
	})

	t.Run("budget bar shown when budget set", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			CostUSD:   2.50,
			BudgetUSD: 10.00,
			Width:     120,
		}
		view := sb.View()
		if !strings.Contains(view, "$2.50") {
			t.Errorf("expected cost $2.50 in view, got: %s", view)
		}
		if !strings.Contains(view, "$10.00") {
			t.Errorf("expected budget $10.00 in view, got: %s", view)
		}
	})
}

func TestDropSegments(t *testing.T) {
	t.Parallel()

	t.Run("drops lowest priority first", func(t *testing.T) {
		t.Parallel()
		segments := []statusSegment{
			{text: "cost", priority: 2},
			{text: "elapsed", priority: 1},
		}
		// totalWidth = len("cost") + len("elapsed") + 1 = 12; allow only 6 → must drop one.
		result := dropSegments(segments, 6)
		if len(result) != 1 {
			t.Fatalf("expected 1 segment after drop, got %d", len(result))
		}
		if result[0].text != "cost" {
			t.Errorf("expected cost to survive (higher priority), got: %s", result[0].text)
		}
	})

	t.Run("keeps all when within budget", func(t *testing.T) {
		t.Parallel()
		segments := []statusSegment{
			{text: "a", priority: 2},
			{text: "b", priority: 1},
		}
		result := dropSegments(segments, 100)
		if len(result) != 2 {
			t.Errorf("expected 2 segments when plenty of space, got %d", len(result))
		}
	})

	t.Run("empty segments", func(t *testing.T) {
		t.Parallel()
		result := dropSegments(nil, 10)
		if len(result) != 0 {
			t.Errorf("expected 0 segments from nil input, got %d", len(result))
		}
	})
}

func TestTotalWidth(t *testing.T) {
	t.Parallel()

	t.Run("empty segments", func(t *testing.T) {
		t.Parallel()
		w := totalWidth(nil)
		if w != 1 { // trailing space
			t.Errorf("expected width 1 for empty segments, got %d", w)
		}
	})

	t.Run("plain text segments", func(t *testing.T) {
		t.Parallel()
		segments := []statusSegment{
			{text: "abc"},
			{text: "de"},
		}
		w := totalWidth(segments)
		// "abc" + "de" + trailing space = 6
		if w != 6 {
			t.Errorf("expected width 6, got %d", w)
		}
	})
}

func TestJoinSegments(t *testing.T) {
	t.Parallel()

	t.Run("joins with trailing space", func(t *testing.T) {
		t.Parallel()
		segments := []statusSegment{
			{text: "hello"},
			{text: " world"},
		}
		result := joinSegments(segments)
		if result != "hello world " {
			t.Errorf("expected 'hello world ', got: %q", result)
		}
	})

	t.Run("empty segments returns space", func(t *testing.T) {
		t.Parallel()
		result := joinSegments(nil)
		if result != " " {
			t.Errorf("expected single space, got: %q", result)
		}
	})
}

func TestStatusBarCompactMode(t *testing.T) {
	t.Parallel()

	t.Run("compact nebula shows percentage", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Name:      "test-nebula-task",
			Total:     10,
			Completed: 5,
			Width:     50, // below CompactWidth=60
		}
		view := sb.View()
		if !strings.Contains(view, "50%") {
			t.Errorf("expected 50%% in compact nebula view, got: %s", view)
		}
	})

	t.Run("compact loop shows cycle fraction", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			BeadID:    "quasar-xyz",
			Cycle:     3,
			MaxCycles: 5,
			Width:     50,
		}
		view := sb.View()
		if !strings.Contains(view, "3/5") {
			t.Errorf("expected 3/5 cycle in compact loop view, got: %s", view)
		}
	})
}

func TestStatusBarMultipleSegments(t *testing.T) {
	t.Parallel()

	t.Run("nebula mode renders all key segments", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Name:      "test-task",
			Total:     5,
			Completed: 2,
			CostUSD:   1.50,
			BudgetUSD: 10.00,
			StartTime: time.Now().Add(-1 * time.Minute),
			Width:     120,
		}
		view := sb.View()
		// Verify key content segments are all present.
		for _, want := range []string{"nebula", "test-task", "2/5", "$1.50", "$10.00"} {
			if !strings.Contains(view, want) {
				t.Errorf("expected %q in status bar, got: %s", want, view)
			}
		}
	})

	t.Run("loop mode renders all key segments", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			BeadID:    "quasar-abc",
			Cycle:     3,
			MaxCycles: 5,
			CostUSD:   0.75,
			StartTime: time.Now().Add(-30 * time.Second),
			Width:     120,
		}
		view := sb.View()
		for _, want := range []string{"task", "quasar-abc", "3/5", "$0.75"} {
			if !strings.Contains(view, want) {
				t.Errorf("expected %q in status bar, got: %s", want, view)
			}
		}
	})
}

func TestStatusBarWidthClamping(t *testing.T) {
	t.Parallel()

	// Configurations that exercise all code paths: nebula, loop, stopping, paused, resources.
	configs := []struct {
		label string
		bar   StatusBar
	}{
		{
			label: "nebula with budget and elapsed",
			bar: StatusBar{
				Name:      "very-long-nebula-task-name-that-should-be-truncated",
				Total:     10,
				Completed: 3,
				CostUSD:   1.24,
				BudgetUSD: 10.00,
				StartTime: time.Now().Add(-5 * time.Minute),
			},
		},
		{
			label: "loop with cycle and cost",
			bar: StatusBar{
				BeadID:    "quasar-extremely-long-bead-identifier",
				Cycle:     4,
				MaxCycles: 7,
				CostUSD:   3.75,
				BudgetUSD: 50.00,
				StartTime: time.Now().Add(-90 * time.Second),
			},
		},
		{
			label: "stopping indicator",
			bar: StatusBar{
				BeadID:    "quasar-stop",
				Cycle:     2,
				MaxCycles: 5,
				CostUSD:   0.50,
				StartTime: time.Now().Add(-10 * time.Second),
				Stopping:  true,
			},
		},
		{
			label: "paused indicator",
			bar: StatusBar{
				Name:      "paused-task",
				Total:     3,
				Completed: 1,
				Paused:    true,
			},
		},
		{
			label: "with resources",
			bar: StatusBar{
				BeadID:    "quasar-res",
				Cycle:     1,
				MaxCycles: 5,
				CostUSD:   0.10,
				StartTime: time.Now().Add(-2 * time.Second),
				Resources: ResourceSnapshot{
					NumProcesses: 4,
					MemoryMB:     512,
					CPUPercent:   45.0,
				},
			},
		},
	}

	widths := []int{MinWidth, 50, CompactWidth - 1, CompactWidth, 80, 100, 120, 200}

	for _, cfg := range configs {
		for _, w := range widths {
			label := cfg.label + "_" + fmt.Sprintf("w%d", w)
			t.Run(label, func(t *testing.T) {
				t.Parallel()
				sb := cfg.bar
				sb.Width = w
				view := sb.View()
				lines := strings.Split(view, "\n")
				for i, line := range lines {
					lineWidth := lipgloss.Width(line)
					if lineWidth > w {
						t.Errorf("line %d: rendered width %d > target %d: %q", i, lineWidth, w, line)
					}
				}
				if len(lines) > 1 {
					t.Errorf("expected single line, got %d lines", len(lines))
				}
			})
		}
	}
}
