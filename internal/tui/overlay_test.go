package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronsalm/quasar/internal/loop"
	"github.com/aaronsalm/quasar/internal/nebula"
)

// --- CompletionOverlay rendering tests ---

func TestCompletionOverlayView(t *testing.T) {
	t.Parallel()

	t.Run("success overlay shows checkmark and title", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind:     CompletionSuccess,
			Duration: 30 * time.Second,
			CostUSD:  1.50,
		}

		view := o.View(80, 24)

		if !strings.Contains(view, "✓") {
			t.Error("expected success overlay to contain checkmark icon")
		}
		if !strings.Contains(view, "Task complete") {
			t.Error("expected success overlay to contain 'Task complete'")
		}
		if !strings.Contains(view, "q to exit") {
			t.Error("expected overlay to contain exit hint")
		}
	})

	t.Run("max cycles overlay shows warning icon", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind:    CompletionMaxCycles,
			Message: "max cycles reached (5)",
		}

		view := o.View(80, 24)

		if !strings.Contains(view, "⚠") {
			t.Error("expected max cycles overlay to contain warning icon")
		}
		if !strings.Contains(view, "Max cycles reached") {
			t.Error("expected overlay to contain 'Max cycles reached'")
		}
	})

	t.Run("budget exceeded overlay shows error icon", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind:    CompletionBudgetExceeded,
			Message: "budget exceeded ($5.00 / $3.00)",
		}

		view := o.View(80, 24)

		if !strings.Contains(view, "✗") {
			t.Error("expected budget exceeded overlay to contain error icon")
		}
		if !strings.Contains(view, "Budget exceeded") {
			t.Error("expected overlay to contain 'Budget exceeded'")
		}
	})

	t.Run("error overlay shows error message", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind:    CompletionError,
			Message: "connection timeout",
		}

		view := o.View(80, 24)

		if !strings.Contains(view, "connection timeout") {
			t.Error("expected error overlay to show error message")
		}
	})

	t.Run("nebula result summary shows counts", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind:         CompletionSuccess,
			DoneCount:    3,
			FailedCount:  1,
			SkippedCount: 2,
		}

		view := o.View(80, 24)

		if !strings.Contains(view, "3 done") {
			t.Error("expected overlay to show done count")
		}
		if !strings.Contains(view, "1 failed") {
			t.Error("expected overlay to show failed count")
		}
		if !strings.Contains(view, "2 skipped") {
			t.Error("expected overlay to show skipped count")
		}
	})

	t.Run("duration and cost are displayed", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind:     CompletionSuccess,
			Duration: 2 * time.Minute,
			CostUSD:  4.25,
		}

		view := o.View(80, 24)

		if !strings.Contains(view, "2m0s") {
			t.Error("expected overlay to show duration")
		}
		if !strings.Contains(view, "$4.25") {
			t.Error("expected overlay to show cost")
		}
	})

	t.Run("renders without panic on zero dimensions", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{Kind: CompletionSuccess}

		// Should not panic.
		view := o.View(0, 0)
		if view == "" {
			t.Error("expected non-empty output even with zero dimensions")
		}
	})
}

// --- NewCompletionFromLoopDone tests ---

func TestNewCompletionFromLoopDone(t *testing.T) {
	t.Parallel()

	t.Run("success when no error", func(t *testing.T) {
		t.Parallel()
		o := NewCompletionFromLoopDone(MsgLoopDone{}, 10*time.Second, 1.0)

		if o.Kind != CompletionSuccess {
			t.Errorf("expected CompletionSuccess, got %d", o.Kind)
		}
	})

	t.Run("max cycles error", func(t *testing.T) {
		t.Parallel()
		o := NewCompletionFromLoopDone(
			MsgLoopDone{Err: loop.ErrMaxCycles},
			10*time.Second, 1.0,
		)

		if o.Kind != CompletionMaxCycles {
			t.Errorf("expected CompletionMaxCycles, got %d", o.Kind)
		}
	})

	t.Run("wrapped max cycles error", func(t *testing.T) {
		t.Parallel()
		o := NewCompletionFromLoopDone(
			MsgLoopDone{Err: fmt.Errorf("loop failed: %w", loop.ErrMaxCycles)},
			10*time.Second, 1.0,
		)

		if o.Kind != CompletionMaxCycles {
			t.Errorf("expected CompletionMaxCycles for wrapped error, got %d", o.Kind)
		}
	})

	t.Run("budget exceeded error", func(t *testing.T) {
		t.Parallel()
		o := NewCompletionFromLoopDone(
			MsgLoopDone{Err: loop.ErrBudgetExceeded},
			10*time.Second, 1.0,
		)

		if o.Kind != CompletionBudgetExceeded {
			t.Errorf("expected CompletionBudgetExceeded, got %d", o.Kind)
		}
	})

	t.Run("generic error", func(t *testing.T) {
		t.Parallel()
		o := NewCompletionFromLoopDone(
			MsgLoopDone{Err: errors.New("something went wrong")},
			10*time.Second, 1.0,
		)

		if o.Kind != CompletionError {
			t.Errorf("expected CompletionError, got %d", o.Kind)
		}
		if o.Message != "something went wrong" {
			t.Errorf("expected error message, got %q", o.Message)
		}
	})
}

// --- NewCompletionFromNebulaDone tests ---

func TestNewCompletionFromNebulaDone(t *testing.T) {
	t.Parallel()

	t.Run("success with all phases done", func(t *testing.T) {
		t.Parallel()
		msg := MsgNebulaDone{
			Results: []nebula.WorkerResult{
				{PhaseID: "a"},
				{PhaseID: "b"},
			},
		}
		o := NewCompletionFromNebulaDone(msg, 10*time.Second, 2.0, 2)

		if o.Kind != CompletionSuccess {
			t.Errorf("expected CompletionSuccess, got %d", o.Kind)
		}
		if o.DoneCount != 2 {
			t.Errorf("expected DoneCount=2, got %d", o.DoneCount)
		}
		if o.FailedCount != 0 {
			t.Errorf("expected FailedCount=0, got %d", o.FailedCount)
		}
	})

	t.Run("error kind when results include failures", func(t *testing.T) {
		t.Parallel()
		msg := MsgNebulaDone{
			Results: []nebula.WorkerResult{
				{PhaseID: "a"},
				{PhaseID: "b", Err: errors.New("failed")},
			},
		}
		o := NewCompletionFromNebulaDone(msg, 10*time.Second, 2.0, 2)

		if o.Kind != CompletionError {
			t.Errorf("expected CompletionError when failures present, got %d", o.Kind)
		}
		if o.DoneCount != 1 {
			t.Errorf("expected DoneCount=1, got %d", o.DoneCount)
		}
		if o.FailedCount != 1 {
			t.Errorf("expected FailedCount=1, got %d", o.FailedCount)
		}
	})

	t.Run("error when top-level error present", func(t *testing.T) {
		t.Parallel()
		msg := MsgNebulaDone{
			Err: errors.New("nebula failed"),
			Results: []nebula.WorkerResult{
				{PhaseID: "a"},
			},
		}
		o := NewCompletionFromNebulaDone(msg, 10*time.Second, 2.0, 1)

		if o.Kind != CompletionError {
			t.Errorf("expected CompletionError, got %d", o.Kind)
		}
		if o.Message != "nebula failed" {
			t.Errorf("expected error message, got %q", o.Message)
		}
	})

	t.Run("computes skipped phases from total", func(t *testing.T) {
		t.Parallel()
		msg := MsgNebulaDone{
			Results: []nebula.WorkerResult{
				{PhaseID: "a"},
				{PhaseID: "b", Err: errors.New("failed")},
			},
		}
		o := NewCompletionFromNebulaDone(msg, 10*time.Second, 2.0, 5)

		if o.DoneCount != 1 {
			t.Errorf("expected DoneCount=1, got %d", o.DoneCount)
		}
		if o.FailedCount != 1 {
			t.Errorf("expected FailedCount=1, got %d", o.FailedCount)
		}
		if o.SkippedCount != 3 {
			t.Errorf("expected SkippedCount=3, got %d", o.SkippedCount)
		}
	})
}

// --- Nebula picker tests ---

func TestCompletionOverlayNebulaPicker(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "auth-feature", Path: "/tmp/auth", Status: "ready", Phases: 5, Done: 0},
		{Name: "tui-visual", Path: "/tmp/tui", Status: "in_progress", Phases: 7, Done: 3},
		{Name: "backend-api", Path: "/tmp/api", Status: "done", Phases: 4, Done: 4},
	}

	t.Run("shows nebula list when choices available", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind:          CompletionSuccess,
			NebulaChoices: choices,
			PickerCursor:  0,
		}

		view := o.View(80, 30)

		if !strings.Contains(view, "Run another nebula?") {
			t.Error("expected picker header")
		}
		if !strings.Contains(view, "auth-feature") {
			t.Error("expected auth-feature in list")
		}
		if !strings.Contains(view, "tui-visual") {
			t.Error("expected tui-visual in list")
		}
		if !strings.Contains(view, "backend-api") {
			t.Error("expected backend-api in list")
		}
		if !strings.Contains(view, "enter:launch") {
			t.Error("expected launch hint")
		}
	})

	t.Run("shows cursor indicator on selected item", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind:          CompletionSuccess,
			NebulaChoices: choices,
			PickerCursor:  1,
		}

		view := o.View(80, 30)

		if !strings.Contains(view, "▎") {
			t.Error("expected cursor indicator in view")
		}
	})

	t.Run("no picker when no choices", func(t *testing.T) {
		t.Parallel()
		o := &CompletionOverlay{
			Kind: CompletionSuccess,
		}

		view := o.View(80, 24)

		if strings.Contains(view, "Run another nebula?") {
			t.Error("should not show picker header without choices")
		}
		if !strings.Contains(view, "q to exit") {
			t.Error("expected standard exit hint")
		}
	})
}

func TestOverlayPickerKeyHandling(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "first", Path: "/tmp/first", Status: "ready", Phases: 3},
		{Name: "second", Path: "/tmp/second", Status: "ready", Phases: 2},
	}

	t.Run("down key moves cursor in picker", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Splash = false
		m.Overlay = &CompletionOverlay{Kind: CompletionSuccess, NebulaChoices: choices}
		m.AvailableNebulae = choices
		m.PickerCursor = 0
		m.Width = 80
		m.Height = 24

		result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		rm := result.(AppModel)

		if rm.PickerCursor != 1 {
			t.Errorf("expected cursor at 1, got %d", rm.PickerCursor)
		}
	})

	t.Run("up key moves cursor up in picker", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Splash = false
		m.Overlay = &CompletionOverlay{Kind: CompletionSuccess, NebulaChoices: choices}
		m.AvailableNebulae = choices
		m.PickerCursor = 1
		m.Width = 80
		m.Height = 24

		result, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		rm := result.(AppModel)

		if rm.PickerCursor != 0 {
			t.Errorf("expected cursor at 0, got %d", rm.PickerCursor)
		}
	})

	t.Run("cursor does not go below list", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Splash = false
		m.Overlay = &CompletionOverlay{Kind: CompletionSuccess, NebulaChoices: choices}
		m.AvailableNebulae = choices
		m.PickerCursor = 1
		m.Width = 80
		m.Height = 24

		result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		rm := result.(AppModel)

		if rm.PickerCursor != 1 {
			t.Errorf("expected cursor clamped at 1, got %d", rm.PickerCursor)
		}
	})

	t.Run("cursor does not go above 0", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Splash = false
		m.Overlay = &CompletionOverlay{Kind: CompletionSuccess, NebulaChoices: choices}
		m.AvailableNebulae = choices
		m.PickerCursor = 0
		m.Width = 80
		m.Height = 24

		result, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		rm := result.(AppModel)

		if rm.PickerCursor != 0 {
			t.Errorf("expected cursor clamped at 0, got %d", rm.PickerCursor)
		}
	})

	t.Run("enter selects nebula and quits", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Splash = false
		m.Overlay = &CompletionOverlay{Kind: CompletionSuccess, NebulaChoices: choices}
		m.AvailableNebulae = choices
		m.PickerCursor = 1
		m.Width = 80
		m.Height = 24

		result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		rm := result.(AppModel)

		if rm.NextNebula != "/tmp/second" {
			t.Errorf("expected NextNebula='/tmp/second', got %q", rm.NextNebula)
		}
		// cmd should be tea.Quit.
		if cmd == nil {
			t.Error("expected quit command")
		}
	})
}

// --- Toast tests ---

func TestNewToast(t *testing.T) {
	t.Parallel()

	t.Run("creates toast with unique ID", func(t *testing.T) {
		t.Parallel()
		t1, _ := NewToast("error 1", true)
		t2, _ := NewToast("error 2", true)

		if t1.ID == t2.ID {
			t.Error("expected toasts to have unique IDs")
		}
	})

	t.Run("toast has correct message", func(t *testing.T) {
		t.Parallel()
		toast, _ := NewToast("something failed", true)

		if toast.Message != "something failed" {
			t.Errorf("expected message 'something failed', got %q", toast.Message)
		}
		if !toast.IsError {
			t.Error("expected IsError to be true")
		}
	})

	t.Run("returns non-nil command", func(t *testing.T) {
		t.Parallel()
		_, cmd := NewToast("test", false)

		if cmd == nil {
			t.Error("expected non-nil command for auto-dismiss")
		}
	})
}

func TestRemoveToast(t *testing.T) {
	t.Parallel()

	t.Run("removes matching toast", func(t *testing.T) {
		t.Parallel()
		toasts := []Toast{
			{ID: 1, Message: "a"},
			{ID: 2, Message: "b"},
			{ID: 3, Message: "c"},
		}

		result := removeToast(toasts, 2)

		if len(result) != 2 {
			t.Fatalf("expected 2 toasts, got %d", len(result))
		}
		for _, toast := range result {
			if toast.ID == 2 {
				t.Error("expected toast with ID 2 to be removed")
			}
		}
	})

	t.Run("no-op when ID not found", func(t *testing.T) {
		t.Parallel()
		toasts := []Toast{
			{ID: 1, Message: "a"},
		}

		result := removeToast(toasts, 99)

		if len(result) != 1 {
			t.Fatalf("expected 1 toast, got %d", len(result))
		}
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()
		result := removeToast(nil, 1)

		if len(result) != 0 {
			t.Errorf("expected 0 toasts, got %d", len(result))
		}
	})
}

func TestRenderToasts(t *testing.T) {
	t.Parallel()

	t.Run("empty toasts returns empty string", func(t *testing.T) {
		t.Parallel()
		result := RenderToasts(nil, 80)

		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("renders toast message", func(t *testing.T) {
		t.Parallel()
		toasts := []Toast{{ID: 1, Message: "something broke", IsError: true}}

		result := RenderToasts(toasts, 80)

		if !strings.Contains(result, "something broke") {
			t.Error("expected rendered toast to contain message")
		}
	})

	t.Run("renders multiple toasts", func(t *testing.T) {
		t.Parallel()
		toasts := []Toast{
			{ID: 1, Message: "error 1", IsError: true},
			{ID: 2, Message: "error 2", IsError: true},
		}

		result := RenderToasts(toasts, 80)

		if !strings.Contains(result, "error 1") || !strings.Contains(result, "error 2") {
			t.Error("expected both toast messages to be rendered")
		}
	})
}

// --- Model integration tests ---

func TestOverlayAppearsOnLoopDone(t *testing.T) {
	t.Parallel()

	t.Run("sets overlay on MsgLoopDone", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Width = 80
		m.Height = 24

		updated, _ := m.Update(MsgLoopDone{})
		model := updated.(AppModel)

		if model.Overlay == nil {
			t.Fatal("expected overlay to be set after MsgLoopDone")
		}
		if model.Overlay.Kind != CompletionSuccess {
			t.Errorf("expected CompletionSuccess, got %d", model.Overlay.Kind)
		}
	})

	t.Run("sets overlay with error on MsgLoopDone", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Width = 80
		m.Height = 24

		updated, _ := m.Update(MsgLoopDone{Err: loop.ErrMaxCycles})
		model := updated.(AppModel)

		if model.Overlay == nil {
			t.Fatal("expected overlay to be set")
		}
		if model.Overlay.Kind != CompletionMaxCycles {
			t.Errorf("expected CompletionMaxCycles, got %d", model.Overlay.Kind)
		}
	})
}

func TestOverlayAppearsOnNebulaDone(t *testing.T) {
	t.Parallel()

	t.Run("sets overlay on MsgNebulaDone", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.Width = 80
		m.Height = 24

		updated, _ := m.Update(MsgNebulaDone{
			Results: []nebula.WorkerResult{{PhaseID: "a"}},
		})
		model := updated.(AppModel)

		if model.Overlay == nil {
			t.Fatal("expected overlay to be set after MsgNebulaDone")
		}
		if model.Overlay.DoneCount != 1 {
			t.Errorf("expected DoneCount=1, got %d", model.Overlay.DoneCount)
		}
	})
}

func TestOverlayRendersInView(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeLoop)
	m.Splash = false
	m.Width = 80
	m.Height = 24
	m.Overlay = &CompletionOverlay{Kind: CompletionSuccess}

	view := m.View()

	if !strings.Contains(view, "Task complete") {
		t.Error("expected View to render completion overlay")
	}
	if !strings.Contains(view, "q to exit") {
		t.Error("expected View to render exit hint in overlay")
	}
}

func TestOverlayBlocksKeys(t *testing.T) {
	t.Parallel()

	t.Run("only q exits when overlay is active", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Splash = false
		m.Width = 80
		m.Height = 24
		m.Overlay = &CompletionOverlay{Kind: CompletionSuccess}

		// Non-q key should be ignored.
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		model := updated.(AppModel)

		if cmd != nil {
			t.Error("expected non-q key to produce nil command")
		}
		if model.Overlay == nil {
			t.Error("expected overlay to still be present")
		}
	})

	t.Run("q exits when overlay is active", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.Width = 80
		m.Height = 24
		m.Overlay = &CompletionOverlay{Kind: CompletionSuccess}

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

		// Should produce a quit command.
		if cmd == nil {
			t.Error("expected q key to produce quit command")
		}
	})
}

func TestErrorToastCreatedOnMsgError(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeLoop)
	m.Width = 80
	m.Height = 24

	updated, cmd := m.Update(MsgError{Msg: "something broke"})
	model := updated.(AppModel)

	if len(model.Toasts) != 1 {
		t.Fatalf("expected 1 toast, got %d", len(model.Toasts))
	}
	if !strings.Contains(model.Toasts[0].Message, "something broke") {
		t.Errorf("expected toast message to contain error, got %q", model.Toasts[0].Message)
	}
	if cmd == nil {
		t.Error("expected non-nil command for toast auto-dismiss")
	}
}

func TestPhaseErrorToastCreated(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Width = 80
	m.Height = 24

	updated, cmd := m.Update(MsgPhaseError{PhaseID: "build", Msg: "compile failed"})
	model := updated.(AppModel)

	if len(model.Toasts) != 1 {
		t.Fatalf("expected 1 toast, got %d", len(model.Toasts))
	}
	if !strings.Contains(model.Toasts[0].Message, "[build]") {
		t.Errorf("expected toast message to contain phase ID, got %q", model.Toasts[0].Message)
	}
	if !strings.Contains(model.Toasts[0].Message, "compile failed") {
		t.Errorf("expected toast message to contain error, got %q", model.Toasts[0].Message)
	}
	if cmd == nil {
		t.Error("expected non-nil command for toast auto-dismiss")
	}
}

func TestToastExpiredRemovesToast(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeLoop)
	m.Width = 80
	m.Height = 24
	m.Toasts = []Toast{
		{ID: 10, Message: "old error"},
		{ID: 20, Message: "new error"},
	}

	updated, _ := m.Update(MsgToastExpired{ID: 10})
	model := updated.(AppModel)

	if len(model.Toasts) != 1 {
		t.Fatalf("expected 1 toast after expiry, got %d", len(model.Toasts))
	}
	if model.Toasts[0].ID != 20 {
		t.Errorf("expected remaining toast to have ID 20, got %d", model.Toasts[0].ID)
	}
}

func TestToastsRenderedInView(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeLoop)
	m.Splash = false
	m.Width = 80
	m.Height = 24
	m.Toasts = []Toast{{ID: 1, Message: "watch out!", IsError: true}}

	view := m.View()

	if !strings.Contains(view, "watch out!") {
		t.Error("expected toast to appear in View output")
	}
}

func TestOverlayTakesPrecedenceOverGate(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeNebula)
	m.Width = 80
	m.Height = 24
	m.Overlay = &CompletionOverlay{Kind: CompletionSuccess}

	// With overlay active, pressing 'a' (accept gate key) should do nothing.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model := updated.(AppModel)

	if model.Overlay == nil {
		t.Error("expected overlay to still be present after non-q key")
	}
}

func TestCenterOverlay(t *testing.T) {
	t.Parallel()

	t.Run("handles zero dimensions", func(t *testing.T) {
		t.Parallel()
		result := centerOverlay("test", 0, 0)
		if result != "test" {
			t.Errorf("expected unchanged content, got %q", result)
		}
	})

	t.Run("centers content", func(t *testing.T) {
		t.Parallel()
		result := centerOverlay("X", 80, 24)

		// Should have some padding applied.
		if len(result) <= 1 {
			t.Error("expected centered content to have padding")
		}
	})
}

func TestCompositeOverlay(t *testing.T) {
	t.Parallel()

	t.Run("overlay replaces background lines", func(t *testing.T) {
		t.Parallel()
		bg := "line1\nline2\nline3\nline4\nline5"
		overlay := "OVR"

		result := compositeOverlay(bg, overlay, 10, 5)
		lines := strings.Split(result, "\n")

		if len(lines) != 5 {
			t.Fatalf("expected 5 lines, got %d", len(lines))
		}
		// Overlay should replace the center line.
		found := false
		for _, line := range lines {
			if strings.Contains(line, "OVR") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected overlay content to appear in composited output")
		}
	})

	t.Run("background lines are preserved around overlay", func(t *testing.T) {
		t.Parallel()
		bg := "bg0\nbg1\nbg2\nbg3\nbg4\nbg5\nbg6\nbg7\nbg8\nbg9"
		overlay := "X"

		result := compositeOverlay(bg, overlay, 10, 10)
		lines := strings.Split(result, "\n")

		// First line should still be the background.
		if !strings.Contains(lines[0], "bg0") {
			t.Errorf("expected first line to be background, got %q", lines[0])
		}
	})
}

func TestBuildNebulaResultCounts(t *testing.T) {
	t.Parallel()

	t.Run("counts done and failed", func(t *testing.T) {
		t.Parallel()
		results := []nebula.WorkerResult{
			{PhaseID: "a"},
			{PhaseID: "b", Err: errors.New("fail")},
			{PhaseID: "c"},
		}

		done, failed, skipped := buildNebulaResultCounts(results, 3)

		if done != 2 {
			t.Errorf("expected done=2, got %d", done)
		}
		if failed != 1 {
			t.Errorf("expected failed=1, got %d", failed)
		}
		if skipped != 0 {
			t.Errorf("expected skipped=0, got %d", skipped)
		}
	})

	t.Run("computes skipped from total phases", func(t *testing.T) {
		t.Parallel()
		results := []nebula.WorkerResult{
			{PhaseID: "a"},
			{PhaseID: "b", Err: errors.New("fail")},
		}

		done, failed, skipped := buildNebulaResultCounts(results, 5)

		if done != 1 {
			t.Errorf("expected done=1, got %d", done)
		}
		if failed != 1 {
			t.Errorf("expected failed=1, got %d", failed)
		}
		if skipped != 3 {
			t.Errorf("expected skipped=3, got %d", skipped)
		}
	})

	t.Run("skipped never negative", func(t *testing.T) {
		t.Parallel()
		results := []nebula.WorkerResult{
			{PhaseID: "a"},
			{PhaseID: "b"},
		}

		_, _, skipped := buildNebulaResultCounts(results, 0)

		if skipped != 0 {
			t.Errorf("expected skipped=0 when totalPhases=0, got %d", skipped)
		}
	})
}

// Ensure the q key binding matches.
func TestOverlayQuitKeyMatchesKeyMap(t *testing.T) {
	t.Parallel()

	km := DefaultKeyMap()
	qKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}

	if !key.Matches(qKey, km.Quit) {
		t.Error("expected q to match Quit key binding")
	}
}
