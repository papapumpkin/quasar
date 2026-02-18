package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDefaultSplashConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultSplashConfig()

	t.Run("dimensions are 62x19", func(t *testing.T) {
		t.Parallel()
		if cfg.Width != 62 || cfg.Height != 19 {
			t.Errorf("DefaultSplashConfig() = %dx%d, want 62x19", cfg.Width, cfg.Height)
		}
	})

	t.Run("not looping", func(t *testing.T) {
		t.Parallel()
		if cfg.Loop {
			t.Error("DefaultSplashConfig().Loop should be false")
		}
	})

	t.Run("FPS is positive", func(t *testing.T) {
		t.Parallel()
		if cfg.FPS <= 0 {
			t.Errorf("DefaultSplashConfig().FPS = %d, want > 0", cfg.FPS)
		}
	})

	t.Run("spins are positive", func(t *testing.T) {
		t.Parallel()
		if cfg.Spins <= 0 {
			t.Errorf("DefaultSplashConfig().Spins = %f, want > 0", cfg.Spins)
		}
	})
}

func TestSpinnerConfig(t *testing.T) {
	t.Parallel()

	cfg := SpinnerConfig()

	t.Run("dimensions are 36x11", func(t *testing.T) {
		t.Parallel()
		if cfg.Width != 36 || cfg.Height != 11 {
			t.Errorf("SpinnerConfig() = %dx%d, want 36x11", cfg.Width, cfg.Height)
		}
	})

	t.Run("is looping", func(t *testing.T) {
		t.Parallel()
		if !cfg.Loop {
			t.Error("SpinnerConfig().Loop should be true")
		}
	})

	t.Run("is more compact than default", func(t *testing.T) {
		t.Parallel()
		def := DefaultSplashConfig()
		if cfg.Width >= def.Width || cfg.Height >= def.Height {
			t.Errorf("SpinnerConfig() should be smaller than DefaultSplashConfig()")
		}
	})
}

func TestNewSplash(t *testing.T) {
	t.Parallel()

	t.Run("starts not done", func(t *testing.T) {
		t.Parallel()
		s := NewSplash(DefaultSplashConfig())
		if s.Done() {
			t.Error("NewSplash should not be done initially")
		}
	})

	t.Run("computes total frames from spins", func(t *testing.T) {
		t.Parallel()
		cfg := DefaultSplashConfig()
		s := NewSplash(cfg)
		want := int(cfg.Spins * 120)
		if s.totalFrames != want {
			t.Errorf("totalFrames = %d, want %d", s.totalFrames, want)
		}
	})
}

func TestSplashModelInit(t *testing.T) {
	t.Parallel()

	s := NewSplash(DefaultSplashConfig())
	cmd := s.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil tick command")
	}
}

func TestSplashModelView(t *testing.T) {
	t.Parallel()

	t.Run("default config produces non-empty output", func(t *testing.T) {
		t.Parallel()
		s := NewSplash(DefaultSplashConfig())
		view := s.View()
		if len(view) == 0 {
			t.Error("View() should produce non-empty output")
		}
	})

	t.Run("spinner config produces non-empty output", func(t *testing.T) {
		t.Parallel()
		s := NewSplash(SpinnerConfig())
		view := s.View()
		if len(view) == 0 {
			t.Error("View() should produce non-empty output")
		}
	})

	t.Run("shows title when configured", func(t *testing.T) {
		t.Parallel()
		cfg := DefaultSplashConfig()
		cfg.ShowTitle = true
		s := NewSplash(cfg)
		view := s.View()
		if !strings.Contains(view, "Q") || !strings.Contains(view, "R") {
			t.Error("View() should contain title characters when ShowTitle is true")
		}
	})

	t.Run("omits title when not configured", func(t *testing.T) {
		t.Parallel()
		cfg := DefaultSplashConfig()
		cfg.ShowTitle = false
		s := NewSplash(cfg)
		view := s.View()
		if strings.Contains(view, "Q    U    A    S    A    R") {
			t.Error("View() should not contain spaced title when ShowTitle is false")
		}
	})

	t.Run("contains star core characters", func(t *testing.T) {
		t.Parallel()
		s := NewSplash(DefaultSplashConfig())
		view := s.View()
		if !strings.Contains(view, "@") {
			t.Error("View() should contain star core character '@'")
		}
	})
}

func TestSplashModelDone(t *testing.T) {
	t.Parallel()

	t.Run("keypress ends splash mode", func(t *testing.T) {
		t.Parallel()
		s := NewSplash(DefaultSplashConfig())
		updated, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		sm := updated.(SplashModel)
		if !sm.Done() {
			t.Error("keypress should set Done() to true in splash mode")
		}
	})

	t.Run("keypress does not end spinner mode", func(t *testing.T) {
		t.Parallel()
		s := NewSplash(SpinnerConfig())
		updated, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		sm := updated.(SplashModel)
		if sm.Done() {
			t.Error("keypress should not set Done() in spinner/loop mode")
		}
	})

	t.Run("completes after enough ticks in splash mode", func(t *testing.T) {
		t.Parallel()
		cfg := DefaultSplashConfig()
		cfg.Spins = 0.1 // very short animation
		s := NewSplash(cfg)

		// Advance past totalFrames + 10 hold frames.
		for i := 0; i < s.totalFrames+20; i++ {
			var model tea.Model
			model, _ = s.Update(splashTickMsg{})
			s = model.(SplashModel)
			if s.Done() {
				return // success
			}
		}
		t.Error("splash should be done after enough ticks")
	})

	t.Run("spinner never self-completes", func(t *testing.T) {
		t.Parallel()
		s := NewSplash(SpinnerConfig())
		// Advance many frames.
		for i := 0; i < 500; i++ {
			var model tea.Model
			model, _ = s.Update(splashTickMsg{})
			s = model.(SplashModel)
		}
		if s.Done() {
			t.Error("spinner mode should never self-complete")
		}
	})
}

func TestSplashModelTeaModelInterface(t *testing.T) {
	t.Parallel()

	// Verify SplashModel satisfies tea.Model at compile time.
	var _ tea.Model = SplashModel{}
}

func TestSplashDopplerBucket(t *testing.T) {
	t.Parallel()

	t.Run("max blueshift at v=-1", func(t *testing.T) {
		t.Parallel()
		// Should return ramp index 0 (max blueshift).
		_ = splashDopplerBucket(-1.0)
	})

	t.Run("neutral at v=0", func(t *testing.T) {
		t.Parallel()
		// Should return ramp index 4 (neutral).
		_ = splashDopplerBucket(0.0)
	})

	t.Run("max redshift at v=1", func(t *testing.T) {
		t.Parallel()
		// Should return ramp index 8 (max redshift).
		_ = splashDopplerBucket(1.0)
	})

	t.Run("clamps out of range", func(t *testing.T) {
		t.Parallel()
		// Should not panic with extreme values.
		_ = splashDopplerBucket(-5.0)
		_ = splashDopplerBucket(5.0)
	})
}

func TestSplashDensityChar(t *testing.T) {
	t.Parallel()

	t.Run("close distance returns star", func(t *testing.T) {
		t.Parallel()
		ch := splashDensityChar(1.0)
		if ch != '*' {
			t.Errorf("splashDensityChar(1.0) = %c, want *", ch)
		}
	})

	t.Run("far distance returns space", func(t *testing.T) {
		t.Parallel()
		ch := splashDensityChar(100.0)
		if ch != ' ' {
			t.Errorf("splashDensityChar(100.0) = %c, want space", ch)
		}
	})
}
