package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCockpitTabLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tab  CockpitTab
		want string
	}{
		{TabBoard, "board"},
		{TabEntanglements, "entanglements"},
		{TabScratchpad, "scratchpad"},
		{CockpitTab(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.tab.Label(); got != tt.want {
				t.Errorf("CockpitTab(%d).Label() = %q, want %q", tt.tab, got, tt.want)
			}
		})
	}
}

func TestCockpitTabNext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		start CockpitTab
		want  CockpitTab
	}{
		{TabBoard, TabEntanglements},
		{TabEntanglements, TabScratchpad},
		{TabScratchpad, TabBoard}, // wraps around
	}
	for _, tt := range tests {
		t.Run(tt.start.Label()+"->next", func(t *testing.T) {
			t.Parallel()
			if got := tt.start.Next(); got != tt.want {
				t.Errorf("CockpitTab(%d).Next() = %d, want %d", tt.start, got, tt.want)
			}
		})
	}
}

func TestCockpitTabPrev(t *testing.T) {
	t.Parallel()
	tests := []struct {
		start CockpitTab
		want  CockpitTab
	}{
		{TabBoard, TabScratchpad}, // wraps around
		{TabEntanglements, TabBoard},
		{TabScratchpad, TabEntanglements},
	}
	for _, tt := range tests {
		t.Run(tt.start.Label()+"->prev", func(t *testing.T) {
			t.Parallel()
			if got := tt.start.Prev(); got != tt.want {
				t.Errorf("CockpitTab(%d).Prev() = %d, want %d", tt.start, got, tt.want)
			}
		})
	}
}

func TestTabFromNumber(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n       int
		wantTab CockpitTab
		wantOK  bool
	}{
		{1, TabBoard, true},
		{2, TabEntanglements, true},
		{3, TabScratchpad, true},
		{0, TabBoard, false},
		{4, TabBoard, false},
		{-1, TabBoard, false},
	}
	for _, tt := range tests {
		t.Run(strings.Repeat("n=", 1)+string(rune('0'+tt.n)), func(t *testing.T) {
			t.Parallel()
			gotTab, gotOK := TabFromNumber(tt.n)
			if gotTab != tt.wantTab || gotOK != tt.wantOK {
				t.Errorf("TabFromNumber(%d) = (%d, %v), want (%d, %v)",
					tt.n, gotTab, gotOK, tt.wantTab, tt.wantOK)
			}
		})
	}
}

func TestTabBarView(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		activeTab CockpitTab
		wantParts []string
	}{
		{
			name:      "board active",
			activeTab: TabBoard,
			wantParts: []string{"[1] board", "[2] entanglements", "[3] scratchpad"},
		},
		{
			name:      "entanglements active",
			activeTab: TabEntanglements,
			wantParts: []string{"[1] board", "[2] entanglements", "[3] scratchpad"},
		},
		{
			name:      "scratchpad active",
			activeTab: TabScratchpad,
			wantParts: []string{"[1] board", "[2] entanglements", "[3] scratchpad"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tb := TabBar{ActiveTab: tt.activeTab, Width: 80}
			got := tb.View()
			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("TabBar.View() missing %q in output:\n%s", part, got)
				}
			}
		})
	}
}

func TestTabBarAllTabsPresent(t *testing.T) {
	t.Parallel()
	tb := TabBar{ActiveTab: TabBoard, Width: 100}
	got := tb.View()

	// Verify all three tab labels appear in the output.
	for i := 0; i < cockpitTabCount; i++ {
		label := CockpitTab(i).Label()
		if !strings.Contains(got, label) {
			t.Errorf("TabBar.View() missing tab label %q", label)
		}
	}
}

func TestTabBarZeroWidth(t *testing.T) {
	t.Parallel()
	// Should not panic with zero width.
	tb := TabBar{ActiveTab: TabBoard, Width: 0}
	got := tb.View()
	if got == "" {
		t.Error("TabBar.View() returned empty string for zero width")
	}
}

func TestCockpitTabCycleRoundTrip(t *testing.T) {
	t.Parallel()
	// Cycling forward cockpitTabCount times should return to start.
	start := TabBoard
	tab := start
	for i := 0; i < cockpitTabCount; i++ {
		tab = tab.Next()
	}
	if tab != start {
		t.Errorf("round-trip Next() %d times: got %d, want %d", cockpitTabCount, tab, start)
	}

	// Cycling backward cockpitTabCount times should also return to start.
	tab = start
	for i := 0; i < cockpitTabCount; i++ {
		tab = tab.Prev()
	}
	if tab != start {
		t.Errorf("round-trip Prev() %d times: got %d, want %d", cockpitTabCount, tab, start)
	}
}

// nebulaModel creates an AppModel in nebula mode at DepthPhases with a window
// size large enough to render the tab bar.
func nebulaModel() AppModel {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40
	return m
}

func TestTabKeyTabCyclesForward(t *testing.T) {
	t.Parallel()
	m := nebulaModel()
	if m.ActiveTab != TabBoard {
		t.Fatalf("initial ActiveTab = %d, want TabBoard(%d)", m.ActiveTab, TabBoard)
	}

	msg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(msg)
	m = updated.(AppModel)
	if m.ActiveTab != TabEntanglements {
		t.Errorf("after Tab: ActiveTab = %d, want TabEntanglements(%d)", m.ActiveTab, TabEntanglements)
	}

	updated, _ = m.Update(msg)
	m = updated.(AppModel)
	if m.ActiveTab != TabScratchpad {
		t.Errorf("after 2x Tab: ActiveTab = %d, want TabScratchpad(%d)", m.ActiveTab, TabScratchpad)
	}

	updated, _ = m.Update(msg)
	m = updated.(AppModel)
	if m.ActiveTab != TabBoard {
		t.Errorf("after 3x Tab (wrap): ActiveTab = %d, want TabBoard(%d)", m.ActiveTab, TabBoard)
	}
}

func TestTabKeyShiftTabCyclesBackward(t *testing.T) {
	t.Parallel()
	m := nebulaModel()
	msg := tea.KeyMsg{Type: tea.KeyShiftTab}

	updated, _ := m.Update(msg)
	m = updated.(AppModel)
	if m.ActiveTab != TabScratchpad {
		t.Errorf("after Shift+Tab: ActiveTab = %d, want TabScratchpad(%d)", m.ActiveTab, TabScratchpad)
	}
}

func TestTabKeyNumberDirectJump(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key     string
		wantTab CockpitTab
	}{
		{"1", TabBoard},
		{"2", TabEntanglements},
		{"3", TabScratchpad},
	}
	for _, tt := range tests {
		t.Run("key-"+tt.key, func(t *testing.T) {
			t.Parallel()
			m := nebulaModel()
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			updated, _ := m.Update(msg)
			m = updated.(AppModel)
			if m.ActiveTab != tt.wantTab {
				t.Errorf("after pressing %q: ActiveTab = %d, want %d", tt.key, m.ActiveTab, tt.wantTab)
			}
		})
	}
}

func TestTabKeyIgnoredOutsideNebulaDepthPhases(t *testing.T) {
	t.Parallel()

	t.Run("loop mode", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeLoop)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40
		msg := tea.KeyMsg{Type: tea.KeyTab}
		updated, _ := m.Update(msg)
		m = updated.(AppModel)
		if m.ActiveTab != TabBoard {
			t.Errorf("loop mode: Tab should not change ActiveTab, got %d", m.ActiveTab)
		}
	})

	t.Run("nebula drilled into phase", func(t *testing.T) {
		t.Parallel()
		m := nebulaModel()
		m.Depth = DepthPhaseLoop
		msg := tea.KeyMsg{Type: tea.KeyTab}
		updated, _ := m.Update(msg)
		m = updated.(AppModel)
		if m.ActiveTab != TabBoard {
			t.Errorf("DepthPhaseLoop: Tab should not change ActiveTab, got %d", m.ActiveTab)
		}
	})
}

func TestTabBarRenderedInNebulaView(t *testing.T) {
	t.Parallel()
	m := nebulaModel()
	view := m.View()
	if !strings.Contains(view, "[1] board") {
		t.Error("tab bar not rendered in nebula mode View()")
	}
	if !strings.Contains(view, "[2] entanglements") {
		t.Error("tab bar missing entanglements label")
	}
	if !strings.Contains(view, "[3] scratchpad") {
		t.Error("tab bar missing scratchpad label")
	}
}

func TestTabBarNotRenderedInLoopMode(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40
	view := m.View()
	if strings.Contains(view, "[1] board") {
		t.Error("tab bar should not appear in loop mode")
	}
}

func TestTabBarNotRenderedWhenDrilledDown(t *testing.T) {
	t.Parallel()
	m := nebulaModel()
	m.Depth = DepthPhaseLoop
	view := m.View()
	if strings.Contains(view, "[1] board") {
		t.Error("tab bar should not appear at DepthPhaseLoop")
	}
}

func TestScratchpadTabRendersEmptyState(t *testing.T) {
	t.Parallel()
	m := nebulaModel()
	m.ActiveTab = TabScratchpad
	view := m.View()
	if !strings.Contains(view, "No events yet") {
		t.Errorf("expected 'No events yet' placeholder for scratchpad tab, got:\n%s", view)
	}
}

// TestEntanglementTabRendersEmptyState verifies that the entanglements tab
// shows the "No entanglements" placeholder when no entanglement data exists.
func TestEntanglementTabRendersEmptyState(t *testing.T) {
	t.Parallel()
	m := nebulaModel()
	m.ActiveTab = TabEntanglements
	view := m.View()
	if !strings.Contains(view, "No entanglements") {
		t.Errorf("expected 'No entanglements' for empty entanglement tab, got:\n%s", view)
	}
}
