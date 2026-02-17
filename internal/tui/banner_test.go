package tui

import (
	"strings"
	"testing"
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
		{"wide shows S-B side panel", 120, BannerSB},
		{"extra wide shows S-B side panel", 200, BannerSB},
		{"boundary below 60", 59, BannerNone},
		{"boundary below 90", 89, BannerXS},
		{"boundary below 120", 119, BannerS},
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

func TestBannerViewReturnsEmptyForSidePanel(t *testing.T) {
	t.Parallel()
	b := Banner{Width: 130, Height: 30}
	got := b.View()
	if got != "" {
		t.Errorf("Banner{Width: 130}.View() should be empty for side panel mode, got length %d", len(got))
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

func TestBannerSidePanelWidth(t *testing.T) {
	t.Parallel()

	t.Run("returns width for wide terminal", func(t *testing.T) {
		t.Parallel()
		b := Banner{Width: 130, Height: 30}
		if got := b.SidePanelWidth(); got != sidePanelWidth {
			t.Errorf("SidePanelWidth() = %d, want %d", got, sidePanelWidth)
		}
	})

	t.Run("returns 0 for narrow terminal", func(t *testing.T) {
		t.Parallel()
		b := Banner{Width: 80, Height: 30}
		if got := b.SidePanelWidth(); got != 0 {
			t.Errorf("SidePanelWidth() = %d, want 0", got)
		}
	})
}

func TestBannerSidePanelView(t *testing.T) {
	t.Parallel()

	t.Run("returns content for wide terminal", func(t *testing.T) {
		t.Parallel()
		b := Banner{Width: 130, Height: 30}
		got := b.SidePanelView(40)
		if got == "" {
			t.Fatal("SidePanelView should not be empty for wide terminal")
		}
	})

	t.Run("returns empty for narrow terminal", func(t *testing.T) {
		t.Parallel()
		b := Banner{Width: 80, Height: 30}
		got := b.SidePanelView(40)
		if got != "" {
			t.Errorf("SidePanelView should be empty for narrow terminal, got length %d", len(got))
		}
	})
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
