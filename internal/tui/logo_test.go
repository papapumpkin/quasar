package tui

import (
	"strings"
	"testing"
)

func TestLogo(t *testing.T) {
	t.Parallel()

	t.Run("contains QUASAR text", func(t *testing.T) {
		t.Parallel()
		got := Logo()
		if !strings.Contains(got, "QUASAR") {
			t.Errorf("Logo() should contain QUASAR, got: %s", got)
		}
	})

	t.Run("contains jet characters", func(t *testing.T) {
		t.Parallel()
		got := Logo()
		if !strings.Contains(got, "╋") {
			t.Errorf("Logo() should contain jet character ╋, got: %s", got)
		}
	})

	t.Run("is single line", func(t *testing.T) {
		t.Parallel()
		got := Logo()
		if strings.Contains(got, "\n") {
			t.Errorf("Logo() should be a single line, got: %s", got)
		}
	})
}

func TestLogoPlain(t *testing.T) {
	t.Parallel()

	t.Run("returns expected text", func(t *testing.T) {
		t.Parallel()
		want := "━━╋━━ QUASAR ━━╋━━"
		got := LogoPlain()
		if got != want {
			t.Errorf("LogoPlain() = %q, want %q", got, want)
		}
	})

	t.Run("contains no ANSI escapes", func(t *testing.T) {
		t.Parallel()
		got := LogoPlain()
		if strings.Contains(got, "\033") {
			t.Errorf("LogoPlain() should not contain ANSI escapes, got: %s", got)
		}
	})
}
