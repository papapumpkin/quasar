package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBannerSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		width int
		want  BannerSize
	}{
		{"very narrow hides art", 50, BannerNone},
		{"narrow shows XS pill", 60, BannerXS},
		{"medium shows S-A wide ellipse", 90, BannerS},
		{"wide shows S-A top banner", 120, BannerS},
		{"extra wide shows S-A top banner", 200, BannerS},
		{"boundary below 60", 59, BannerNone},
		{"boundary below 90", 89, BannerXS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := Banner{Width: tt.width, Height: 40}
			got := b.Size()
			if got != tt.want {
				t.Errorf("Banner{Width: %d}.Size() = %d, want %d", tt.width, got, tt.want)
			}
		})
	}
}

func TestBannerViewReturnsEmptyForNone(t *testing.T) {
	t.Parallel()
	b := Banner{Width: 40, Height: 30}
	got := b.View()
	if got != "" {
		t.Errorf("Banner{Width: 40}.View() should be empty, got %q", got)
	}
}

func TestBannerViewWideTerminalShowsTopBanner(t *testing.T) {
	t.Parallel()
	b := Banner{Width: 130, Height: 30}
	got := b.View()
	if got == "" {
		t.Fatal("Banner{Width: 130}.View() should return top banner for wide terminal")
	}
	if !strings.Contains(got, "Q") {
		t.Error("Wide terminal banner should contain QUASAR text")
	}
}

func TestBannerViewXS(t *testing.T) {
	t.Parallel()
	b := Banner{Width: 70, Height: 30}
	got := b.View()
	if got == "" {
		t.Fatal("Banner{Width: 70}.View() should not be empty")
	}
	if !strings.Contains(got, "Q") {
		t.Error("XS banner should contain QUASAR text")
	}
}

func TestBannerViewS(t *testing.T) {
	t.Parallel()
	b := Banner{Width: 100, Height: 30}
	got := b.View()
	if got == "" {
		t.Fatal("Banner{Width: 100}.View() should not be empty")
	}
	if !strings.Contains(got, "Q") {
		t.Error("S banner should contain QUASAR text")
	}
}

func TestBannerSidePanelWidthAlwaysZero(t *testing.T) {
	t.Parallel()

	// Side panel mode is deprecated; SidePanelWidth always returns 0.
	for _, width := range []int{80, 130, 200} {
		b := Banner{Width: width, Height: 30}
		if got := b.SidePanelWidth(); got != 0 {
			t.Errorf("SidePanelWidth() for width %d = %d, want 0", width, got)
		}
	}
}

func TestBannerSidePanelViewAlwaysEmpty(t *testing.T) {
	t.Parallel()

	// Side panel mode is deprecated; SidePanelView always returns "".
	for _, width := range []int{80, 130, 200} {
		b := Banner{Width: width, Height: 30}
		got := b.SidePanelView(40)
		if got != "" {
			t.Errorf("SidePanelView() for width %d should be empty, got length %d", width, len(got))
		}
	}
}

func TestBannerSplashView(t *testing.T) {
	t.Parallel()
	b := Banner{Width: 120, Height: 40}
	got := b.SplashView()
	if got == "" {
		t.Fatal("SplashView should not be empty")
	}
	if !strings.Contains(got, "Q") {
		t.Error("SplashView should contain QUASAR text")
	}
}

func TestBannerViewIsCached(t *testing.T) {
	t.Parallel()
	b := Banner{Width: 100, Height: 30}
	first := b.View()
	second := b.View()
	if first != second {
		t.Error("View() should return identical cached output on repeated calls")
	}
}

func TestColorDopplerLine(t *testing.T) {
	t.Parallel()

	t.Run("empty line returns empty", func(t *testing.T) {
		t.Parallel()
		if got := colorDopplerLine(""); got != "" {
			t.Errorf("colorDopplerLine(\"\") = %q, want empty", got)
		}
	})

	t.Run("core character gets styled", func(t *testing.T) {
		t.Parallel()
		got := colorDopplerLine("--@--")
		if !strings.Contains(got, "@") {
			t.Error("colorDopplerLine should preserve @ character")
		}
	})
}

func TestIsFadeChar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		r    rune
		want bool
	}{
		{'.', true},
		{'·', true},
		{':', true},
		{'-', false},
		{'@', false},
		{' ', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			t.Parallel()
			if got := isFadeChar(tt.r); got != tt.want {
				t.Errorf("isFadeChar(%q) = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}

func TestArtVariantsNotEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		art  []string
	}{
		{"XS", artXS},
		{"SA", artSA},
		{"SB", artSB},
		{"XL", artXL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if len(tt.art) == 0 {
				t.Errorf("art%s should not be empty", tt.name)
			}
		})
	}
}

// artWidth returns the maximum visual width of an art variant's lines.
// This is a test helper — it exists only in tests to validate art dimensions.
func artWidth(art []string) int {
	max := 0
	for _, line := range art {
		w := utf8.RuneCountInString(line)
		if w > max {
			max = w
		}
	}
	return max
}

func TestArtWidth(t *testing.T) {
	t.Parallel()

	t.Run("XS max width", func(t *testing.T) {
		t.Parallel()
		w := artWidth(artXS)
		if w == 0 {
			t.Error("artWidth(artXS) should be > 0")
		}
	})
}
