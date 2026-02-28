package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestScratchpadView_EmptyRendersPlaceholder(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 20)

	view := sv.View()

	if !strings.Contains(view, "No events yet") {
		t.Errorf("expected placeholder text, got: %q", view)
	}
}

func TestScratchpadView_EntryRendersTimestamp(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 20)

	ts := time.Date(2026, 2, 23, 12, 34, 5, 0, time.UTC)
	sv.AddEntry(MsgScratchpadEntry{
		Timestamp: ts,
		PhaseID:   "phase-a",
		Text:      "discovery: requirements_ambiguity",
	})

	view := sv.View()

	if !strings.Contains(view, "12:34:05") {
		t.Errorf("expected timestamp in view, got: %q", view)
	}
}

func TestScratchpadView_EntryRendersPhaseID(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 20)

	sv.AddEntry(MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   "phase-x",
		Text:      "running → review",
	})

	view := sv.View()

	if !strings.Contains(view, "phase-x") {
		t.Errorf("expected phase ID in view, got: %q", view)
	}
}

func TestScratchpadView_EntryRendersText(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(120, 20)

	sv.AddEntry(MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   "phase-a",
		Text:      "entanglement: FooService published by phase-a",
	})

	view := sv.View()

	if !strings.Contains(view, "entanglement: FooService published by phase-a") {
		t.Errorf("expected entry text in view, got: %q", view)
	}
}

func TestScratchpadView_SystemEventUsesSystemLabel(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 20)

	sv.AddEntry(MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   "",
		Text:      "nebula epoch started",
	})

	view := sv.View()

	if !strings.Contains(view, "system") {
		t.Errorf("expected 'system' label for empty phase ID, got: %q", view)
	}
}

func TestScratchpadView_MultipleEntries(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(120, 20)

	ts1 := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 2, 23, 10, 0, 5, 0, time.UTC)
	ts3 := time.Date(2026, 2, 23, 10, 0, 10, 0, time.UTC)

	sv.AddEntry(MsgScratchpadEntry{Timestamp: ts1, PhaseID: "p1", Text: "first note"})
	sv.AddEntry(MsgScratchpadEntry{Timestamp: ts2, PhaseID: "p2", Text: "second note"})
	sv.AddEntry(MsgScratchpadEntry{Timestamp: ts3, PhaseID: "p1", Text: "third note"})

	view := sv.View()

	if !strings.Contains(view, "first note") {
		t.Errorf("expected first entry text in view")
	}
	if !strings.Contains(view, "second note") {
		t.Errorf("expected second entry text in view")
	}
	if !strings.Contains(view, "third note") {
		t.Errorf("expected third entry text in view")
	}

	// Entries should appear in chronological order.
	idx1 := strings.Index(view, "first note")
	idx2 := strings.Index(view, "second note")
	idx3 := strings.Index(view, "third note")
	if idx1 >= idx2 || idx2 >= idx3 {
		t.Errorf("expected entries in chronological order")
	}
}

func TestScratchpadView_AutoScrollToBottom(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 3) // small viewport to force scrolling

	// Add many entries to overflow the viewport.
	for i := 0; i < 20; i++ {
		sv.AddEntry(MsgScratchpadEntry{
			Timestamp: time.Now(),
			PhaseID:   "p1",
			Text:      strings.Repeat("x", 10),
		})
	}

	// Should be at bottom (auto-scroll).
	if !sv.isAtBottom() {
		t.Errorf("expected viewport to auto-scroll to bottom after adding entries")
	}
}

func TestScratchpadView_NoAutoScrollWhenUserScrolledUp(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 3)

	// Add enough entries to enable scrolling.
	for i := 0; i < 20; i++ {
		sv.AddEntry(MsgScratchpadEntry{
			Timestamp: time.Now(),
			PhaseID:   "p1",
			Text:      "entry line",
		})
	}

	// Simulate user scrolling up.
	sv.Update(tea.KeyMsg{Type: tea.KeyUp})
	sv.Update(tea.KeyMsg{Type: tea.KeyUp})
	sv.Update(tea.KeyMsg{Type: tea.KeyUp})

	// Record the offset after user scroll.
	offsetAfterScroll := sv.viewport.YOffset

	// Add a new entry — should NOT auto-scroll since user scrolled up.
	sv.AddEntry(MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   "p2",
		Text:      "new entry after scroll",
	})

	if sv.viewport.YOffset != offsetAfterScroll {
		t.Errorf("expected viewport to stay at user's scroll position %d, got %d",
			offsetAfterScroll, sv.viewport.YOffset)
	}
}

func TestScratchpadView_ViewportScrollKeys(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 3)

	// Add many entries.
	for i := 0; i < 30; i++ {
		sv.AddEntry(MsgScratchpadEntry{
			Timestamp: time.Now(),
			PhaseID:   "p1",
			Text:      "scroll test entry",
		})
	}

	// Go to top.
	sv.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if sv.viewport.YOffset != 0 {
		t.Errorf("expected YOffset 0 after 'g', got %d", sv.viewport.YOffset)
	}

	// Go to bottom.
	sv.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if !sv.isAtBottom() {
		t.Errorf("expected viewport at bottom after 'G'")
	}

	// Home key goes to top.
	sv.Update(tea.KeyMsg{Type: tea.KeyHome})
	if sv.viewport.YOffset != 0 {
		t.Errorf("expected YOffset 0 after Home, got %d", sv.viewport.YOffset)
	}

	// End key goes to bottom.
	sv.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !sv.isAtBottom() {
		t.Errorf("expected viewport at bottom after End")
	}
}

func TestScratchpadView_LongPhaseIDTruncated(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 20)

	sv.AddEntry(MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   "very-long-phase-identifier-name",
		Text:      "truncation test",
	})

	view := sv.View()

	// The full phase ID should NOT appear (it's longer than maxPhaseIDWidth).
	if strings.Contains(view, "very-long-phase-identifier-name") {
		t.Errorf("expected long phase ID to be truncated")
	}
	// An ellipsis should indicate truncation.
	if !strings.Contains(view, "…") {
		t.Errorf("expected ellipsis for truncated phase ID")
	}
}

func TestScratchpadView_SetSizeRefreshes(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(80, 20)

	sv.AddEntry(MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   "p1",
		Text:      "before resize",
	})

	// Resize should not lose entries.
	sv.SetSize(120, 30)

	view := sv.View()
	if !strings.Contains(view, "before resize") {
		t.Errorf("expected entry to survive resize, got: %q", view)
	}
}

func TestWrapText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		width    int
		expected int // expected number of lines
	}{
		{
			name:     "short text no wrap",
			text:     "hello",
			width:    80,
			expected: 1,
		},
		{
			name:     "exact width no wrap",
			text:     "hello",
			width:    5,
			expected: 1,
		},
		{
			name:     "wraps on space",
			text:     "hello world",
			width:    6,
			expected: 2,
		},
		{
			name:     "wraps long word",
			text:     "abcdefghij",
			width:    5,
			expected: 2,
		},
		{
			name:     "zero width returns as-is",
			text:     "hello",
			width:    0,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := wrapText(tt.text, tt.width)
			lines := strings.Split(result, "\n")
			if len(lines) != tt.expected {
				t.Errorf("wrapText(%q, %d) = %d lines, want %d; result: %q",
					tt.text, tt.width, len(lines), tt.expected, result)
			}
		})
	}
}

func TestWrapText_PreservesContent(t *testing.T) {
	t.Parallel()
	text := "the quick brown fox jumps over the lazy dog"
	result := wrapText(text, 15)

	// All words should be present.
	for _, word := range strings.Fields(text) {
		if !strings.Contains(result, word) {
			t.Errorf("wrapText lost word %q; result: %q", word, result)
		}
	}
}

func TestScratchpadView_UpdateNoopWhenNotReady(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	// Don't call SetSize — viewport not ready.

	// Should not panic.
	sv.Update(tea.KeyMsg{Type: tea.KeyUp})
	sv.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
}

func TestScratchpadView_ViewNoEntriesNoSize(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()

	view := sv.View()

	if !strings.Contains(view, "No events yet") {
		t.Errorf("expected placeholder for uninitialized view, got: %q", view)
	}
}

func TestScratchpadView_RenderContentEmpty(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()

	content := sv.renderContent()

	if content != "" {
		t.Errorf("expected empty content, got: %q", content)
	}
}

func TestScratchpadView_FormatEntryLayout(t *testing.T) {
	t.Parallel()
	sv := NewScratchpadView()
	sv.SetSize(120, 20)

	ts := time.Date(2026, 1, 15, 8, 5, 30, 0, time.UTC)
	entry := MsgScratchpadEntry{
		Timestamp: ts,
		PhaseID:   "phase-1",
		Text:      "filter: build failed for phase-x",
	}

	formatted := sv.formatEntry(entry)

	// Should contain the formatted timestamp.
	if !strings.Contains(formatted, "[08:05:30]") {
		t.Errorf("expected formatted timestamp [08:05:30], got: %q", formatted)
	}
	// Should contain the phase ID.
	if !strings.Contains(formatted, "phase-1") {
		t.Errorf("expected phase ID in formatted entry, got: %q", formatted)
	}
	// Should contain the text.
	if !strings.Contains(formatted, "filter: build failed for phase-x") {
		t.Errorf("expected text in formatted entry, got: %q", formatted)
	}
}
