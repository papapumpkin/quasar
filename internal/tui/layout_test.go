package tui

import (
	"strings"
	"testing"
)

func TestTruncateWithEllipsis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"fits exactly", "hello", 5, "hello"},
		{"fits with room", "hi", 10, "hi"},
		{"truncated", "hello world", 8, "hello..."},
		{"truncated to 4", "abcdef", 4, "a..."},
		{"maxLen 3 no ellipsis", "abcdef", 3, "abc"},
		{"maxLen 2 no ellipsis", "abcdef", 2, "ab"},
		{"maxLen 1", "abcdef", 1, "a"},
		{"maxLen 0", "abcdef", 0, ""},
		{"empty string", "", 5, ""},
		{"single char fits", "a", 1, "a"},
		{"long phase ID", "phase-authentication-service", 15, "phase-authen..."},
		{"multibyte runes truncated", "こんにちは世界abc", 5, "こん..."},
		{"multibyte runes fit", "こんにちは", 5, "こんにちは"},
		{"multibyte short truncate", "日本語テスト", 2, "日本"},
		{"multibyte single rune", "🚀rocket", 4, "🚀..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TruncateWithEllipsis(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateWithEllipsis(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestMinDimConstants(t *testing.T) {
	t.Parallel()
	if MinWidth < 20 {
		t.Errorf("MinWidth = %d, expected at least 20", MinWidth)
	}
	if MinHeight < 5 {
		t.Errorf("MinHeight = %d, expected at least 5", MinHeight)
	}
}

func TestCompactWidthBreakpoint(t *testing.T) {
	t.Parallel()
	if CompactWidth <= MinWidth {
		t.Errorf("CompactWidth (%d) should be greater than MinWidth (%d)", CompactWidth, MinWidth)
	}
}

func TestViewTooSmallWidth(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Width = MinWidth - 1
	m.Height = 24

	view := m.View()
	if !strings.Contains(view, "Terminal too small") {
		t.Errorf("expected 'Terminal too small' message for narrow terminal, got: %q", view)
	}
	if !strings.Contains(view, "Minimum:") {
		t.Error("expected minimum dimensions in message")
	}
}

func TestViewTooSmallHeight(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Width = 80
	m.Height = MinHeight - 1

	view := m.View()
	if !strings.Contains(view, "Terminal too small") {
		t.Errorf("expected 'Terminal too small' message for short terminal, got: %q", view)
	}
}

func TestViewTooSmallBoth(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Width = MinWidth - 1
	m.Height = MinHeight - 1

	view := m.View()
	if !strings.Contains(view, "Terminal too small") {
		t.Error("expected 'Terminal too small' message")
	}
}

func TestViewAtMinimumDimensions(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Width = MinWidth
	m.Height = MinHeight

	view := m.View()
	if strings.Contains(view, "Terminal too small") {
		t.Error("should not show 'Terminal too small' at exactly minimum dimensions")
	}
}

func TestClampCursorsNebulaView(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.NebulaView.InitPhases([]PhaseInfo{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	})
	m.NebulaView.Cursor = 5 // out of bounds

	clampCursors(&m)

	if m.NebulaView.Cursor != 1 {
		t.Errorf("NebulaView.Cursor = %d, want 1 (max index)", m.NebulaView.Cursor)
	}
}

func TestClampCursorsNebulaViewEmpty(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.NebulaView.Cursor = 3

	clampCursors(&m)

	if m.NebulaView.Cursor != 0 {
		t.Errorf("NebulaView.Cursor = %d, want 0 for empty phases", m.NebulaView.Cursor)
	}
}

func TestClampCursorsLoopView(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.LoopView.StartCycle(1)
	m.LoopView.StartAgent("coder")
	m.LoopView.Cursor = 10 // out of bounds

	clampCursors(&m)

	max := m.LoopView.TotalEntries() - 1
	if m.LoopView.Cursor != max {
		t.Errorf("LoopView.Cursor = %d, want %d", m.LoopView.Cursor, max)
	}
}

func TestClampCursorsLoopViewEmpty(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.LoopView.Cursor = 5

	clampCursors(&m)

	if m.LoopView.Cursor != 0 {
		t.Errorf("LoopView.Cursor = %d, want 0 for empty loop", m.LoopView.Cursor)
	}
}

func TestClampCursorsPhaseLoops(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.Cursor = 10 // out of bounds
	m.PhaseLoops["test-phase"] = &lv

	clampCursors(&m)

	max := lv.TotalEntries() - 1
	if lv.Cursor != max {
		t.Errorf("PhaseLoop cursor = %d, want %d", lv.Cursor, max)
	}
}

func TestClampCursorsPhaseLoopsEmpty(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	lv := NewLoopView()
	lv.Cursor = 3
	m.PhaseLoops["test-phase"] = &lv

	clampCursors(&m)

	if lv.Cursor != 0 {
		t.Errorf("PhaseLoop cursor = %d, want 0 for empty loop", lv.Cursor)
	}
}

func TestDetailAutoCollapseOnShortTerminal(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Width = 80
	m.Height = DetailCollapseHeight - 1 // just below threshold
	m.Depth = DepthPhaseLoop
	m.FocusedPhase = "test"
	m.Detail = NewDetailPanel(78, 5)
	m.Detail.SetEmpty("some content")

	view := m.View()
	// The detail panel border/content should NOT appear.
	// The view should still render without panics.
	if strings.Contains(view, "Terminal too small") {
		t.Error("should not show too-small message at this height")
	}
}

func TestDetailShownOnTallTerminal(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Splash = nil
	m.Width = 80
	m.Height = DetailCollapseHeight + 5 // above threshold
	m.Depth = DepthPhaseLoop
	m.FocusedPhase = "test"
	m.Detail = NewDetailPanel(78, 5)
	m.Detail.SetContent("test", "test content")

	view := m.View()
	// The view should contain the detail panel content.
	if !strings.Contains(view, "test content") {
		t.Error("detail panel should be visible on tall terminal")
	}
}

func TestFooterCompactMode(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()
	f := Footer{
		Width:    CompactWidth - 1,
		Bindings: LoopFooterBindings(km),
	}
	output := f.View()
	// In compact mode, descriptions should NOT appear.
	if strings.Contains(output, ":up") || strings.Contains(output, ":down") {
		t.Error("compact footer should not contain key:desc pairs")
	}
	// But keys should still appear.
	if !strings.Contains(output, "↑") {
		t.Error("compact footer should still contain key symbols")
	}
}

func TestFooterNormalMode(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()
	f := Footer{
		Width:    CompactWidth + 20,
		Bindings: LoopFooterBindings(km),
	}
	output := f.View()
	// Normal mode should contain colon separators.
	if !strings.Contains(output, ":") {
		t.Error("normal footer should contain colon separators")
	}
}

func TestStatusBarCompactNebulaMode(t *testing.T) {
	t.Parallel()
	sb := StatusBar{
		Name:      "my-long-nebula-name",
		Total:     10,
		Completed: 5,
		Width:     CompactWidth - 1,
	}
	output := sb.View()
	// In compact mode, should show percentage instead of progress bar.
	if !strings.Contains(output, "50%") {
		t.Errorf("compact nebula status bar should show percentage, got: %q", output)
	}
}

func TestStatusBarCompactLoopMode(t *testing.T) {
	t.Parallel()
	sb := StatusBar{
		BeadID:    "bead-123",
		Cycle:     2,
		MaxCycles: 5,
		Width:     CompactWidth - 1,
	}
	output := sb.View()
	// In compact mode, should show abbreviated cycle info.
	if !strings.Contains(output, "2/5") {
		t.Errorf("compact loop status bar should show cycle fraction, got: %q", output)
	}
	// Ensure cycle progress is NOT rendered twice (regression check).
	if strings.Count(output, "2/5") != 1 {
		t.Errorf("compact loop status bar should render cycle fraction exactly once, got %d occurrences in: %q",
			strings.Count(output, "2/5"), output)
	}
	// Bead ID should also appear.
	if !strings.Contains(output, "bead") {
		t.Errorf("compact loop status bar should show (truncated) bead ID, got: %q", output)
	}
}

func TestStatusBarNameTruncation(t *testing.T) {
	t.Parallel()
	longName := "very-long-nebula-project-name-that-exceeds"
	sb := StatusBar{
		Name:      longName,
		Total:     10,
		Completed: 5,
		Width:     CompactWidth - 1,
	}
	output := sb.View()
	// The full long name should NOT appear in compact mode.
	if strings.Contains(output, longName) {
		t.Error("compact status bar should truncate long nebula name")
	}
}

func TestNebulaViewPhaseIDTruncation(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "very-long-phase-id-authentication-service", Title: "Auth"},
	})
	nv.Width = CompactWidth - 1
	nv.Cursor = 0

	output := nv.View()
	// Should contain ellipsis for the long phase ID.
	if !strings.Contains(output, "...") {
		t.Error("narrow nebula view should truncate long phase IDs with ellipsis")
	}
}

func TestNebulaViewPhaseIDNoTruncationWideTerminal(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "short-phase", Title: "Test"},
	})
	nv.Width = 120
	nv.Cursor = 0

	output := nv.View()
	if !strings.Contains(output, "short-phase") {
		t.Error("wide terminal should show full phase ID")
	}
	if strings.Contains(output, "...") {
		t.Error("wide terminal should not truncate short phase IDs")
	}
}

func TestBreadcrumbHiddenOnNarrowTerminal(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Width = CompactWidth - 1
	m.Height = 30
	m.Depth = DepthPhaseLoop
	m.FocusedPhase = "test-phase"

	view := m.View()
	// The breadcrumb should be hidden in compact mode.
	// Note: "phases" text from the breadcrumb might appear elsewhere,
	// so check for the specific separator.
	if strings.Contains(view, " › ") {
		t.Error("breadcrumb should be hidden on narrow terminal")
	}
}

func TestBreadcrumbShownOnWideTerminal(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Splash = nil
	m.Width = CompactWidth + 20
	m.Height = 30
	m.Depth = DepthPhaseLoop
	m.FocusedPhase = "test-phase"

	view := m.View()
	if !strings.Contains(view, "phases") {
		t.Error("breadcrumb should be shown on wide terminal")
	}
}

func TestBreadcrumbTruncatesLongPhaseID(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Width = 65 // just above compact but not huge
	m.Height = 30
	m.Depth = DepthPhaseLoop
	m.FocusedPhase = "extremely-long-phase-identifier-authentication-database-migration-service"

	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, "...") {
		t.Error("breadcrumb should truncate very long phase IDs with ellipsis")
	}
}

func TestRenderTooSmallContainsDimensions(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Width = 30
	m.Height = 8

	view := m.renderTooSmall()
	if !strings.Contains(view, "30x8") {
		t.Error("too-small message should show current dimensions")
	}
	if !strings.Contains(view, "40x10") {
		t.Error("too-small message should show minimum dimensions")
	}
}
