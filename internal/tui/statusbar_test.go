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

	t.Run("nebula mode shows active phases", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Name:       "test-nebula",
			Total:      10,
			Completed:  4,
			InProgress: 2,
			Width:      120,
		}
		view := sb.View()
		if !strings.Contains(view, "4/10") {
			t.Errorf("expected 4/10 progress in view, got: %s", view)
		}
		if !strings.Contains(view, "2 active") {
			t.Errorf("expected '2 active' in view, got: %s", view)
		}
	})

	t.Run("nebula mode hides active when zero in-progress", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Name:       "test-nebula",
			Total:      10,
			Completed:  10,
			InProgress: 0,
			Width:      120,
		}
		view := sb.View()
		if strings.Contains(view, "active") {
			t.Errorf("expected no 'active' label when InProgress=0, got: %s", view)
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

func TestFormatTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tokens int
		want   string
	}{
		{"zero", 0, "0"},
		{"small", 42, "42"},
		{"just below 1k", 999, "999"},
		{"exactly 1k", 1000, "1.0k"},
		{"thousands", 284300, "284.3k"},
		{"large", 1500000, "1500.0k"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormatTokens(tt.tokens)
			if got != tt.want {
				t.Errorf("FormatTokens(%d) = %q, want %q", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestFormatElapsedCompact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"seconds only", 45 * time.Second, "45s"},
		{"minutes and seconds", 4*time.Minute + 32*time.Second, "4m 32s"},
		{"hours and minutes", 2*time.Hour + 15*time.Minute, "2h 15m"},
		{"exact minute", 1 * time.Minute, "1m 0s"},
		{"exact hour", 1 * time.Hour, "1h 0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatElapsedCompact(tt.d)
			if got != tt.want {
				t.Errorf("formatElapsedCompact(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestBottomBar(t *testing.T) {
	t.Parallel()

	t.Run("renders all segments with progress", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			TotalTokens:  284300,
			CostUSD:      1.42,
			FinalElapsed: 4*time.Minute + 32*time.Second,
			Total:        8,
			Completed:    5,
			Width:        120,
		}
		view := sb.BottomBar()
		for _, want := range []string{"tokens", "284.3k", "cost", "$1.42", "elapsed", "4m 32s", "progress", "5/8"} {
			if !strings.Contains(view, want) {
				t.Errorf("expected %q in bottom bar, got: %s", want, view)
			}
		}
	})

	t.Run("omits progress when no phases", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			TotalTokens:  1000,
			CostUSD:      0.50,
			FinalElapsed: 10 * time.Second,
			Width:        120,
		}
		view := sb.BottomBar()
		if strings.Contains(view, "progress") {
			t.Errorf("expected no progress segment when Total=0, got: %s", view)
		}
		if !strings.Contains(view, "1.0k") {
			t.Errorf("expected token count 1.0k, got: %s", view)
		}
	})

	t.Run("empty when width is zero", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{Width: 0}
		if got := sb.BottomBar(); got != "" {
			t.Errorf("expected empty bottom bar for zero width, got: %q", got)
		}
	})

	t.Run("progress bar uses block characters", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Total:     4,
			Completed: 2,
			Width:     120,
		}
		view := sb.BottomBar()
		if !strings.Contains(view, "█") {
			t.Errorf("expected filled block character █ in progress bar, got: %s", view)
		}
		if !strings.Contains(view, "░") {
			t.Errorf("expected empty block character ░ in progress bar, got: %s", view)
		}
	})

	t.Run("progress bar full when all completed", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Total:     5,
			Completed: 5,
			Width:     120,
		}
		view := sb.BottomBar()
		if strings.Contains(view, "░") {
			t.Errorf("expected no empty blocks when fully complete, got: %s", view)
		}
	})

	t.Run("never exceeds width", func(t *testing.T) {
		t.Parallel()
		for _, width := range []int{40, 60, 80, 120} {
			sb := StatusBar{
				TotalTokens:  999999,
				CostUSD:      999.99,
				FinalElapsed: 99*time.Hour + 59*time.Minute,
				Total:        100,
				Completed:    50,
				Width:        width,
			}
			view := sb.BottomBar()
			w := lipgloss.Width(view)
			if w > width {
				t.Errorf("width=%d: bottom bar width %d > %d: %q", width, w, width, view)
			}
		}
	})

	t.Run("live elapsed timer when FinalElapsed is zero", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			StartTime: time.Now().Add(-2 * time.Minute),
			Width:     120,
		}
		view := sb.BottomBar()
		if !strings.Contains(view, "2m") {
			t.Errorf("expected live elapsed ~2m in bottom bar, got: %s", view)
		}
	})

	t.Run("zero elapsed shows 0s", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{Width: 120}
		view := sb.BottomBar()
		if !strings.Contains(view, "0s") {
			t.Errorf("expected 0s elapsed in bottom bar, got: %s", view)
		}
	})
}

func TestRenderBottomProgressBar(t *testing.T) {
	t.Parallel()

	t.Run("empty when total is zero", func(t *testing.T) {
		t.Parallel()
		if got := renderBottomProgressBar(0, 0, 10); got != "" {
			t.Errorf("expected empty for zero total, got: %q", got)
		}
	})

	t.Run("empty when width is zero", func(t *testing.T) {
		t.Parallel()
		if got := renderBottomProgressBar(1, 2, 0); got != "" {
			t.Errorf("expected empty for zero width, got: %q", got)
		}
	})

	t.Run("all filled when complete", func(t *testing.T) {
		t.Parallel()
		bar := renderBottomProgressBar(5, 5, 10)
		if strings.Contains(bar, "░") {
			t.Errorf("expected no empty blocks when fully complete, got: %s", bar)
		}
		if !strings.Contains(bar, "█") {
			t.Errorf("expected filled blocks, got: %s", bar)
		}
	})

	t.Run("partial fill", func(t *testing.T) {
		t.Parallel()
		bar := renderBottomProgressBar(1, 2, 10)
		if !strings.Contains(bar, "█") {
			t.Errorf("expected some filled blocks, got: %s", bar)
		}
		if !strings.Contains(bar, "░") {
			t.Errorf("expected some empty blocks, got: %s", bar)
		}
	})
}
