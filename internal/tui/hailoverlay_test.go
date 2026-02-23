package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// --- HailOverlay creation tests ---

func TestNewHailOverlay(t *testing.T) {
	t.Parallel()

	t.Run("creates overlay with phase and discovery", func(t *testing.T) {
		t.Parallel()
		msg := MsgHail{
			PhaseID: "db-migrations",
			Discovery: fabric.Discovery{
				Kind:   "requirements_ambiguity",
				Detail: "Which migration strategy?\n- incremental\n- full rebuild\n- hybrid",
			},
		}
		h := NewHailOverlay(msg, nil)

		if h.PhaseID != "db-migrations" {
			t.Errorf("expected PhaseID %q, got %q", "db-migrations", h.PhaseID)
		}
		if h.Discovery.Kind != "requirements_ambiguity" {
			t.Errorf("expected Kind %q, got %q", "requirements_ambiguity", h.Discovery.Kind)
		}
		if len(h.Options) != 3 {
			t.Fatalf("expected 3 options, got %d", len(h.Options))
		}
		if h.Options[0] != "incremental" {
			t.Errorf("expected first option %q, got %q", "incremental", h.Options[0])
		}
		if h.Options[1] != "full rebuild" {
			t.Errorf("expected second option %q, got %q", "full rebuild", h.Options[1])
		}
		if h.Options[2] != "hybrid" {
			t.Errorf("expected third option %q, got %q", "hybrid", h.Options[2])
		}
	})

	t.Run("no options when detail has no bullet lines", func(t *testing.T) {
		t.Parallel()
		msg := MsgHail{
			PhaseID: "api-design",
			Discovery: fabric.Discovery{
				Kind:   "file_conflict",
				Detail: "Two phases editing the same file.",
			},
		}
		h := NewHailOverlay(msg, nil)

		if len(h.Options) != 0 {
			t.Errorf("expected 0 options, got %d", len(h.Options))
		}
	})
}

// --- extractOptions tests ---

func TestExtractOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		detail string
		want   []string
	}{
		{
			name:   "bullet lines extracted",
			detail: "Choose one:\n- alpha\n- beta\n- gamma",
			want:   []string{"alpha", "beta", "gamma"},
		},
		{
			name:   "no bullets returns nil",
			detail: "No options here.",
			want:   nil,
		},
		{
			name:   "mixed content",
			detail: "Header text\n- option A\nMore text\n- option B",
			want:   []string{"option A", "option B"},
		},
		{
			name:   "empty detail",
			detail: "",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractOptions(tt.detail)
			if len(got) != len(tt.want) {
				t.Fatalf("extractOptions() returned %d items, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("option[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- stripOptionLines tests ---

func TestStripOptionLines(t *testing.T) {
	t.Parallel()

	t.Run("removes bullet lines", func(t *testing.T) {
		t.Parallel()
		input := "Header\n- opt1\n- opt2\nFooter"
		got := stripOptionLines(input)
		if strings.Contains(got, "opt1") {
			t.Error("expected option lines to be stripped")
		}
		if !strings.Contains(got, "Header") {
			t.Error("expected non-option lines to be preserved")
		}
		if !strings.Contains(got, "Footer") {
			t.Error("expected non-option lines to be preserved")
		}
	})

	t.Run("preserves text without bullets", func(t *testing.T) {
		t.Parallel()
		input := "Just some text."
		got := stripOptionLines(input)
		if got != "Just some text." {
			t.Errorf("expected %q, got %q", "Just some text.", got)
		}
	})
}

// --- HandleInput tests ---

func TestHailOverlayHandleInput(t *testing.T) {
	t.Parallel()

	t.Run("single letter selects option", func(t *testing.T) {
		t.Parallel()
		h := &HailOverlay{
			Options: []string{"incremental", "full rebuild", "hybrid"},
			Input:   newTestInput("b"),
		}
		got := h.HandleInput()
		if got != "full rebuild" {
			t.Errorf("expected %q, got %q", "full rebuild", got)
		}
	})

	t.Run("single letter a selects first option", func(t *testing.T) {
		t.Parallel()
		h := &HailOverlay{
			Options: []string{"incremental", "full rebuild", "hybrid"},
			Input:   newTestInput("a"),
		}
		got := h.HandleInput()
		if got != "incremental" {
			t.Errorf("expected %q, got %q", "incremental", got)
		}
	})

	t.Run("out of range letter returns literal", func(t *testing.T) {
		t.Parallel()
		h := &HailOverlay{
			Options: []string{"only-one"},
			Input:   newTestInput("z"),
		}
		got := h.HandleInput()
		if got != "z" {
			t.Errorf("expected %q, got %q", "z", got)
		}
	})

	t.Run("free text is returned verbatim", func(t *testing.T) {
		t.Parallel()
		h := &HailOverlay{
			Options: []string{"alpha", "beta"},
			Input:   newTestInput("use a custom strategy"),
		}
		got := h.HandleInput()
		if got != "use a custom strategy" {
			t.Errorf("expected %q, got %q", "use a custom strategy", got)
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		t.Parallel()
		h := &HailOverlay{
			Options: []string{"alpha"},
			Input:   newTestInput(""),
		}
		got := h.HandleInput()
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("no options means single letter is literal", func(t *testing.T) {
		t.Parallel()
		h := &HailOverlay{
			Options: nil,
			Input:   newTestInput("a"),
		}
		got := h.HandleInput()
		if got != "a" {
			t.Errorf("expected %q, got %q", "a", got)
		}
	})
}

// --- Resolve tests ---

func TestHailOverlayResolve(t *testing.T) {
	t.Parallel()

	t.Run("sends response on channel", func(t *testing.T) {
		t.Parallel()
		ch := make(chan string, 1)
		h := &HailOverlay{ResponseCh: ch}
		h.Resolve("incremental")

		got := <-ch
		if got != "incremental" {
			t.Errorf("expected %q, got %q", "incremental", got)
		}
	})

	t.Run("nil channel does not panic", func(t *testing.T) {
		t.Parallel()
		h := &HailOverlay{ResponseCh: nil}
		h.Resolve("anything") // should not panic
	})
}

// --- SetContext tests ---

func TestHailOverlaySetContext(t *testing.T) {
	t.Parallel()
	h := &HailOverlay{}
	h.SetContext("q-2", 3, 5)

	if h.QuasarID != "q-2" {
		t.Errorf("expected QuasarID %q, got %q", "q-2", h.QuasarID)
	}
	if h.Cycle != 3 {
		t.Errorf("expected Cycle %d, got %d", 3, h.Cycle)
	}
	if h.MaxCycles != 5 {
		t.Errorf("expected MaxCycles %d, got %d", 5, h.MaxCycles)
	}
}

// --- View rendering tests ---

func TestHailOverlayView(t *testing.T) {
	t.Parallel()

	t.Run("contains HAIL header", func(t *testing.T) {
		t.Parallel()
		h := makeTestOverlay()
		view := h.View(80, 24)

		if !strings.Contains(view, "HAIL") {
			t.Error("expected overlay to contain 'HAIL' header")
		}
	})

	t.Run("contains phase name", func(t *testing.T) {
		t.Parallel()
		h := makeTestOverlay()
		view := h.View(80, 24)

		if !strings.Contains(view, "db-migrations") {
			t.Error("expected overlay to contain phase name")
		}
	})

	t.Run("contains discovery kind", func(t *testing.T) {
		t.Parallel()
		h := makeTestOverlay()
		view := h.View(80, 24)

		if !strings.Contains(view, "requirements_ambiguity") {
			t.Error("expected overlay to contain discovery kind")
		}
	})

	t.Run("contains labeled options", func(t *testing.T) {
		t.Parallel()
		h := makeTestOverlay()
		view := h.View(80, 24)

		if !strings.Contains(view, "a)") {
			t.Error("expected overlay to contain option label 'a)'")
		}
		if !strings.Contains(view, "b)") {
			t.Error("expected overlay to contain option label 'b)'")
		}
		if !strings.Contains(view, "incremental") {
			t.Error("expected overlay to contain option 'incremental'")
		}
	})

	t.Run("contains quasar and cycle when set", func(t *testing.T) {
		t.Parallel()
		h := makeTestOverlay()
		h.SetContext("q-2", 3, 5)
		view := h.View(80, 24)

		if !strings.Contains(view, "q-2") {
			t.Error("expected overlay to contain quasar ID")
		}
		if !strings.Contains(view, "3/5") {
			t.Error("expected overlay to contain cycle count")
		}
	})

	t.Run("renders detail text without option lines", func(t *testing.T) {
		t.Parallel()
		h := makeTestOverlay()
		view := h.View(80, 24)

		if !strings.Contains(view, "Which strategy") {
			t.Error("expected overlay to contain detail text")
		}
	})

	t.Run("renders with red border", func(t *testing.T) {
		t.Parallel()
		h := makeTestOverlay()
		view := h.View(80, 24)

		// The rounded border renders corner characters.
		if !strings.Contains(view, "╭") {
			t.Error("expected overlay to have rounded border top-left corner")
		}
		if !strings.Contains(view, "╯") {
			t.Error("expected overlay to have rounded border bottom-right corner")
		}
	})

	t.Run("narrow terminal constrains width", func(t *testing.T) {
		t.Parallel()
		h := makeTestOverlay()
		view := h.View(40, 24)

		// Should still render without error.
		if !strings.Contains(view, "HAIL") {
			t.Error("expected overlay to render in narrow terminal")
		}
	})
}

// --- AppModel integration: MsgHail shows overlay on board ---

func TestAppModelMsgHailShowsOverlay(t *testing.T) {
	t.Parallel()

	t.Run("hail creates overlay in board mode", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40
		m.ActiveTab = TabBoard
		m.Depth = DepthPhases

		msg := MsgHail{
			PhaseID: "test-phase",
			Discovery: fabric.Discovery{
				Kind:   "file_conflict",
				Detail: "Conflict detected.\n- keep mine\n- keep theirs",
			},
		}

		result, _ := m.Update(msg)
		updated := result.(AppModel)

		if updated.Hail == nil {
			t.Fatal("expected Hail overlay to be set")
		}
		if updated.Hail.PhaseID != "test-phase" {
			t.Errorf("expected PhaseID %q, got %q", "test-phase", updated.Hail.PhaseID)
		}
	})

	t.Run("hail falls back to toast outside board mode", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40

		msg := MsgHail{
			PhaseID: "test-phase",
			Discovery: fabric.Discovery{
				Kind:   "file_conflict",
				Detail: "Conflict.",
			},
		}

		result, _ := m.Update(msg)
		updated := result.(AppModel)

		if updated.Hail != nil {
			t.Error("expected Hail overlay to be nil in loop mode")
		}
		if len(updated.Toasts) == 0 {
			t.Error("expected a toast notification in loop mode")
		}
	})

	t.Run("hail falls back to toast when not at depth phases", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40
		m.ActiveTab = TabBoard
		m.Depth = DepthPhaseLoop

		msg := MsgHail{
			PhaseID: "test-phase",
			Discovery: fabric.Discovery{
				Kind:   "file_conflict",
				Detail: "Conflict.",
			},
		}

		result, _ := m.Update(msg)
		updated := result.(AppModel)

		if updated.Hail != nil {
			t.Error("expected Hail overlay to be nil when drilled into phase loop")
		}
	})
}

// --- AppModel integration: key handling ---

func TestAppModelHailKeyHandling(t *testing.T) {
	t.Parallel()

	t.Run("escape dismisses hail overlay", func(t *testing.T) {
		t.Parallel()
		m := makeModelWithHail()

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
		updated := result.(AppModel)

		if updated.Hail != nil {
			t.Error("expected Hail overlay to be dismissed on Esc")
		}
	})

	t.Run("enter with option letter resolves", func(t *testing.T) {
		t.Parallel()
		ch := make(chan string, 1)
		m := makeModelWithHailCh(ch)

		// Type "a" into the input.
		m.Hail.Input.SetValue("a")

		// Press Enter.
		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		updated := result.(AppModel)

		if updated.Hail != nil {
			t.Error("expected Hail overlay to be resolved on Enter")
		}

		select {
		case got := <-ch:
			if got != "incremental" {
				t.Errorf("expected response %q, got %q", "incremental", got)
			}
		default:
			t.Error("expected response to be sent on channel")
		}
	})
}

// --- View integration: hail overlay dims background ---

func TestAppModelViewWithHail(t *testing.T) {
	t.Parallel()

	t.Run("view renders hail overlay", func(t *testing.T) {
		t.Parallel()
		m := makeModelWithHail()

		view := m.View()

		if !strings.Contains(view, "HAIL") {
			t.Error("expected View output to contain HAIL overlay")
		}
	})
}

// --- helpers ---

// newTestInput creates a textinput.Model pre-populated with the given value.
func newTestInput(val string) textinput.Model {
	ti := textinput.New()
	ti.SetValue(val)
	return ti
}

// makeTestOverlay creates a representative HailOverlay for view rendering tests.
func makeTestOverlay() *HailOverlay {
	msg := MsgHail{
		PhaseID: "db-migrations",
		Discovery: fabric.Discovery{
			Kind:   "requirements_ambiguity",
			Detail: "Which strategy?\n- incremental\n- full rebuild",
		},
	}
	return NewHailOverlay(msg, nil)
}

// makeModelWithHail creates an AppModel with an active hail overlay.
func makeModelWithHail() AppModel {
	return makeModelWithHailCh(nil)
}

// makeModelWithHailCh creates an AppModel with a hail overlay wired to the given channel.
func makeModelWithHailCh(ch chan<- string) AppModel {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40
	m.ActiveTab = TabBoard
	m.Depth = DepthPhases

	msg := MsgHail{
		PhaseID: "db-migrations",
		Discovery: fabric.Discovery{
			Kind:   "requirements_ambiguity",
			Detail: "Which strategy?\n- incremental\n- full rebuild\n- hybrid",
		},
	}
	m.Hail = NewHailOverlay(msg, ch)
	return m
}
