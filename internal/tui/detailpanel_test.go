package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHighlightOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantSub string // substring that should appear in output
	}{
		{
			name:    "APPROVED line gets highlighted",
			input:   "Code APPROVED — no issues",
			wantSub: "APPROVED",
		},
		{
			name:    "ISSUE: line gets highlighted",
			input:   "ISSUE: missing error handling",
			wantSub: "ISSUE:",
		},
		{
			name:    "SEVERITY: critical gets highlighted",
			input:   "SEVERITY: critical — data loss risk",
			wantSub: "SEVERITY: critical",
		},
		{
			name:    "plain line passes through",
			input:   "This is a normal line",
			wantSub: "This is a normal line",
		},
		{
			name:    "case insensitive approved",
			input:   "approved by reviewer",
			wantSub: "approved",
		},
		{
			name:    "case insensitive issue",
			input:   "issue: something wrong",
			wantSub: "issue:",
		},
		{
			name:    "case insensitive severity critical",
			input:   "severity: critical bug found",
			wantSub: "severity: critical",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := HighlightOutput(tt.input)
			if !strings.Contains(result, tt.wantSub) {
				t.Errorf("HighlightOutput(%q) = %q, want substring %q", tt.input, result, tt.wantSub)
			}
		})
	}
}

func TestHighlightOutputMultiline(t *testing.T) {
	t.Parallel()
	input := "normal line\nAPPROVED\nISSUE: bug\nSEVERITY: critical"
	result := HighlightOutput(input)
	lines := strings.Split(result, "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 lines, got %d", len(lines))
	}
	// First line should be unchanged (no highlight keywords).
	if !strings.Contains(lines[0], "normal line") {
		t.Error("first line should contain 'normal line'")
	}
}

func TestHighlightLinePriority(t *testing.T) {
	t.Parallel()
	// SEVERITY: critical takes priority over ISSUE:.
	// In a non-TTY environment lipgloss may not apply ANSI codes,
	// so we verify the function runs without error and the line content
	// is preserved (the styling may be stripped).
	line := "ISSUE: SEVERITY: critical stuff"
	result := highlightLine(line)
	if !strings.Contains(result, "SEVERITY: critical") {
		t.Error("expected line content to be preserved")
	}
}

func TestTruncateOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		lines     int
		maxLines  int
		wantTrunc bool
	}{
		{"under limit", 5, 10, false},
		{"at limit", 10, 10, false},
		{"over limit", 15, 10, true},
		{"way over limit", 1000, 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var sb strings.Builder
			for i := range tt.lines {
				if i > 0 {
					sb.WriteString("\n")
				}
				fmt.Fprintf(&sb, "line %d", i+1)
			}
			result := TruncateOutput(sb.String(), tt.maxLines)
			hasTruncIndicator := strings.Contains(result, "truncated")
			if tt.wantTrunc && !hasTruncIndicator {
				t.Error("expected truncation indicator")
			}
			if !tt.wantTrunc && hasTruncIndicator {
				t.Error("unexpected truncation indicator")
			}
			if tt.wantTrunc {
				remaining := tt.lines - tt.maxLines
				wantMsg := fmt.Sprintf("%d more lines", remaining)
				if !strings.Contains(result, wantMsg) {
					t.Errorf("expected %q in truncation message, got %q", wantMsg, result)
				}
			}
		})
	}
}

func TestTruncateOutputPreservesContent(t *testing.T) {
	t.Parallel()
	input := "line1\nline2\nline3\nline4\nline5"
	result := TruncateOutput(input, 3)
	if !strings.Contains(result, "line1") {
		t.Error("should contain first line")
	}
	if !strings.Contains(result, "line3") {
		t.Error("should contain third line")
	}
	// line4 should not appear in the visible portion
	// (it could be in the truncation indicator message).
}

func TestFormatAgentHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ctx     AgentContext
		wantSub []string
	}{
		{
			name: "complete agent",
			ctx: AgentContext{
				Role: "coder", Cycle: 2, DurationMs: 5500,
				CostUSD: 0.1234, IssueCount: 3, Done: true,
			},
			wantSub: []string{"coder", "2", "5.5s", "$0.1234", "3"},
		},
		{
			name: "working agent (not done)",
			ctx: AgentContext{
				Role: "reviewer", Cycle: 1, Done: false,
			},
			wantSub: []string{"reviewer", "1"},
		},
		{
			name: "no cycle",
			ctx: AgentContext{
				Role: "coder", Done: true, DurationMs: 1000,
			},
			wantSub: []string{"coder", "1.0s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FormatAgentHeader(tt.ctx)
			for _, sub := range tt.wantSub {
				if !strings.Contains(result, sub) {
					t.Errorf("FormatAgentHeader result %q missing substring %q", result, sub)
				}
			}
		})
	}
}

func TestFormatAgentHeaderDurationOnlyWhenDone(t *testing.T) {
	t.Parallel()
	ctx := AgentContext{
		Role: "coder", Cycle: 1, DurationMs: 5000, Done: false,
	}
	result := FormatAgentHeader(ctx)
	if strings.Contains(result, "duration") {
		t.Error("duration should not appear when agent is not done")
	}
}

func TestFormatPhaseHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ctx     PhaseContext
		wantSub []string
	}{
		{
			name: "complete phase",
			ctx: PhaseContext{
				ID: "setup", Title: "Setup models", Status: PhaseDone,
				CostUSD: 1.23, Cycles: 2,
			},
			wantSub: []string{"setup", "Setup models", "done", "$1.23", "2"},
		},
		{
			name: "waiting phase with blocker",
			ctx: PhaseContext{
				ID: "auth", Title: "Auth", Status: PhaseWaiting,
				BlockedBy: "setup +1",
			},
			wantSub: []string{"auth", "Auth", "waiting", "setup +1"},
		},
		{
			name: "working phase no cost",
			ctx: PhaseContext{
				ID: "deploy", Title: "", Status: PhaseWorking,
			},
			wantSub: []string{"deploy", "working"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FormatPhaseHeader(tt.ctx)
			for _, sub := range tt.wantSub {
				if !strings.Contains(result, sub) {
					t.Errorf("FormatPhaseHeader result %q missing substring %q", result, sub)
				}
			}
		})
	}
}

func TestPhaseStatusString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status PhaseStatus
		want   string
	}{
		{PhaseWaiting, "waiting"},
		{PhaseDone, "done"},
		{PhaseWorking, "working"},
		{PhaseFailed, "failed"},
		{PhaseGate, "gate"},
		{PhaseSkipped, "skipped"},
		{PhaseStatus(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := phaseStatusString(tt.status)
			if got != tt.want {
				t.Errorf("phaseStatusString(%d) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestFormatAgentOutput(t *testing.T) {
	t.Parallel()
	// FormatAgentOutput combines truncation and highlighting.
	input := "APPROVED\nISSUE: missing tests\nnormal"
	result := FormatAgentOutput(input)
	if !strings.Contains(result, "APPROVED") {
		t.Error("should contain APPROVED")
	}
	if !strings.Contains(result, "ISSUE:") {
		t.Error("should contain ISSUE:")
	}
}

func TestDetailPanelSetEmpty(t *testing.T) {
	t.Parallel()
	d := NewDetailPanel(80, 10)
	d.SetEmpty("Press enter to expand details")
	view := d.View()
	if !strings.Contains(view, "Press enter to expand details") {
		t.Error("empty state should show hint")
	}
}

func TestDetailPanelSetContentWithHeader(t *testing.T) {
	t.Parallel()
	d := NewDetailPanel(80, 10)
	d.SetContentWithHeader("test title", "header info", "body content")
	view := d.View()
	if !strings.Contains(view, "test title") {
		t.Error("should show title")
	}
	if !strings.Contains(view, "header info") {
		t.Error("should show header")
	}
	if !strings.Contains(view, "body content") {
		t.Error("should show body content")
	}
	if !strings.Contains(view, "─") {
		t.Error("should contain separator between header and body")
	}
}

func TestDetailPanelScrollIndicators(t *testing.T) {
	t.Parallel()
	// Create a small viewport that can't show all content.
	d := NewDetailPanel(80, 3)
	// Generate content with many lines.
	var sb strings.Builder
	for i := range 20 {
		fmt.Fprintf(&sb, "line %d\n", i+1)
	}
	d.SetContent("test", sb.String())
	// Content has 21 lines (20 newlines + 1), viewport is 3 tall.
	// At top, linesAbove=0, linesBelow should be positive.
	below := d.linesBelow()
	if below <= 0 {
		t.Errorf("linesBelow = %d, want > 0 for overflow content", below)
	}
	above := d.linesAbove()
	if above != 0 {
		t.Errorf("linesAbove = %d, want 0 at top", above)
	}
}

func TestShowDetailPanelAtPhaseLoop(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	m.Depth = DepthPhases
	if m.showDetailPanel() {
		t.Error("should not show detail at DepthPhases")
	}

	m.Depth = DepthPhaseLoop
	if !m.showDetailPanel() {
		t.Error("should show detail at DepthPhaseLoop")
	}

	m.Depth = DepthAgentOutput
	if !m.showDetailPanel() {
		t.Error("should show detail at DepthAgentOutput")
	}
}

func TestShowDetailPanelLoopMode(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)

	m.Depth = DepthPhases
	if m.showDetailPanel() {
		t.Error("should not show detail at DepthPhases in loop mode")
	}

	m.Depth = DepthAgentOutput
	if !m.showDetailPanel() {
		t.Error("should show detail at DepthAgentOutput in loop mode")
	}
}

func TestUpdateDetailFromSelectionLoopMode(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	// No selection — empty hint.
	m.updateDetailFromSelection()
	view := m.Detail.View()
	if !strings.Contains(view, "Press enter") {
		t.Error("expected empty hint when no agent selected")
	}

	// Add an agent and select it.
	m.LoopView.StartCycle(1)
	m.LoopView.StartAgent("coder")
	m.LoopView.FinishAgent("coder", 0.5, 5000)
	m.LoopView.SetAgentOutput("coder", 1, "wrote APPROVED code")
	m.LoopView.Cursor = 1 // agent row
	m.updateDetailFromSelection()
	view = m.Detail.View()
	if !strings.Contains(view, "coder") {
		t.Error("expected agent role in header")
	}
	if !strings.Contains(view, "APPROVED") {
		t.Error("expected highlighted output")
	}
}

func TestUpdateDetailFromSelectionNebulaPhaseLoop(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	m.NebulaView.InitPhases([]PhaseInfo{
		{ID: "setup", Title: "Setup models"},
	})
	m.NebulaView.SetPhaseStatus("setup", PhaseDone)
	m.NebulaView.SetPhaseCost("setup", 1.50)
	m.NebulaView.SetPhaseCycles("setup", 2, 5)

	m.FocusedPhase = "setup"
	m.Depth = DepthPhaseLoop
	m.updateDetailFromSelection()

	view := m.Detail.View()
	if !strings.Contains(view, "setup") {
		t.Error("expected phase ID in detail")
	}
	if !strings.Contains(view, "done") {
		t.Error("expected phase status in detail")
	}
}

func TestUpdateDetailFromSelectionNebulaAgentOutput(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24

	m.NebulaView.InitPhases([]PhaseInfo{
		{ID: "setup", Title: "Setup models"},
	})
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.FinishAgent("coder", 0.5, 5000)
	lv.SetAgentOutput("coder", 1, "ISSUE: missing test")
	m.PhaseLoops["setup"] = &lv

	m.FocusedPhase = "setup"
	m.Depth = DepthAgentOutput
	lv.Cursor = 1 // agent row
	m.updateDetailFromSelection()

	view := m.Detail.View()
	if !strings.Contains(view, "ISSUE:") {
		t.Error("expected highlighted ISSUE in agent output")
	}
	if !strings.Contains(view, "coder") {
		t.Error("expected agent role in header")
	}
}

func TestDetailPanelScrollsAtDepthAgentOutput(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Splash = nil
	m.Detail = NewDetailPanel(80, 5) // small viewport
	m.Width = 80
	m.Height = 40
	m.Depth = DepthAgentOutput

	// Generate content larger than viewport.
	var sb strings.Builder
	for i := range 50 {
		fmt.Fprintf(&sb, "line %d\n", i+1)
	}
	m.Detail.SetContent("scroll test", sb.String())

	// Initially at top — linesAbove should be 0.
	if m.Detail.linesAbove() != 0 {
		t.Errorf("linesAbove before scroll = %d, want 0", m.Detail.linesAbove())
	}
	if m.Detail.linesBelow() <= 0 {
		t.Errorf("linesBelow before scroll = %d, want > 0", m.Detail.linesBelow())
	}

	// Pressing down arrow at DepthAgentOutput should scroll the detail panel.
	// AppModel.Update is a value receiver, so use the returned model.
	downKey := tea.KeyMsg{Type: tea.KeyDown}
	result, _ := m.Update(downKey)
	updated := result.(AppModel)
	if updated.Detail.linesAbove() <= 0 {
		t.Errorf("linesAbove after down = %d, want > 0", updated.Detail.linesAbove())
	}
}

func TestDetailPanelUpDownDoNotMoveCursorAtDepthAgentOutput(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Detail = NewDetailPanel(80, 5)
	m.Width = 80
	m.Height = 40
	m.Depth = DepthAgentOutput

	// Add some agents to the loop view.
	m.LoopView.StartCycle(1)
	m.LoopView.StartAgent("coder")
	m.LoopView.FinishAgent("coder", 0.5, 5000)

	initialCursor := m.LoopView.Cursor

	// Pressing up/down at DepthAgentOutput should NOT move the loop view cursor.
	downKey := tea.KeyMsg{Type: tea.KeyDown}
	result, _ := m.Update(downKey)
	updated := result.(AppModel)
	if updated.LoopView.Cursor != initialCursor {
		t.Errorf("cursor moved from %d to %d at DepthAgentOutput", initialCursor, updated.LoopView.Cursor)
	}
}

func TestDetailPanelScrollPreservesNavigationAtOtherDepths(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeLoop)
	m.Splash = nil
	m.Detail = NewDetailPanel(80, 5)
	m.Width = 80
	m.Height = 40
	m.Depth = DepthPhases // NOT DepthAgentOutput

	// Add agents so cursor can move.
	m.LoopView.StartCycle(1)
	m.LoopView.StartAgent("coder")
	m.LoopView.FinishAgent("coder", 0.5, 5000)

	// At DepthPhases, ↓ should still move the cursor, not scroll.
	downKey := tea.KeyMsg{Type: tea.KeyDown}
	result, _ := m.Update(downKey)
	updated := result.(AppModel)
	if updated.LoopView.Cursor == 0 {
		t.Error("cursor should have moved at DepthPhases")
	}
}

func TestDetailPanelScrollIndicatorsUpdate(t *testing.T) {
	t.Parallel()
	d := NewDetailPanel(80, 3)

	// Generate content larger than viewport.
	var sb strings.Builder
	for i := range 20 {
		fmt.Fprintf(&sb, "line %d\n", i+1)
	}
	d.SetContent("test", sb.String())

	// At top, no up indicator expected.
	view := d.View()
	if strings.Contains(view, "↑") {
		t.Error("should not show up indicator at top")
	}
	if !strings.Contains(view, "↓") {
		t.Error("should show down indicator when content overflows")
	}

	// Scroll down — DetailPanel.Update is a pointer receiver, so mutation is direct.
	downKey := tea.KeyMsg{Type: tea.KeyDown}
	d.Update(downKey)

	view = d.View()
	if !strings.Contains(view, "↑") {
		t.Error("should show up indicator after scrolling down")
	}
}

func TestDetailPanelGotoTopBottom(t *testing.T) {
	t.Parallel()
	d := NewDetailPanel(80, 3)

	// Generate content larger than viewport.
	var sb strings.Builder
	for i := range 30 {
		fmt.Fprintf(&sb, "line %d\n", i+1)
	}
	d.SetContent("test", sb.String())

	// Start at top.
	if d.linesAbove() != 0 {
		t.Fatalf("expected linesAbove=0 at start, got %d", d.linesAbove())
	}

	tests := []struct {
		name         string
		goBottom     tea.KeyMsg
		goTop        tea.KeyMsg
		wantAboveGtZ bool
	}{
		{
			name:         "G goes to bottom",
			goBottom:     tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}},
			goTop:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}},
			wantAboveGtZ: true,
		},
		{
			name:         "end goes to bottom",
			goBottom:     tea.KeyMsg{Type: tea.KeyEnd},
			goTop:        tea.KeyMsg{Type: tea.KeyHome},
			wantAboveGtZ: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dp := NewDetailPanel(80, 3)
			dp.SetContent("test", sb.String())

			// Go to bottom.
			dp.Update(tt.goBottom)
			if dp.linesAbove() <= 0 {
				t.Errorf("after go-to-bottom, linesAbove = %d, want > 0", dp.linesAbove())
			}
			if dp.linesBelow() != 0 {
				t.Errorf("after go-to-bottom, linesBelow = %d, want 0", dp.linesBelow())
			}

			// Go back to top.
			dp.Update(tt.goTop)
			if dp.linesAbove() != 0 {
				t.Errorf("after go-to-top, linesAbove = %d, want 0", dp.linesAbove())
			}
		})
	}
}

func TestSelectedCycleNumber(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.StartCycle(2)
	lv.StartAgent("reviewer")

	// Cursor 0 = cycle 1 header (no agent) → 0
	lv.Cursor = 0
	if got := lv.SelectedCycleNumber(); got != 0 {
		t.Errorf("at cycle header, SelectedCycleNumber = %d, want 0", got)
	}

	// Cursor 1 = coder in cycle 1 → 1
	lv.Cursor = 1
	if got := lv.SelectedCycleNumber(); got != 1 {
		t.Errorf("at coder in cycle 1, SelectedCycleNumber = %d, want 1", got)
	}

	// Cursor 3 = reviewer in cycle 2 → 2
	lv.Cursor = 3
	if got := lv.SelectedCycleNumber(); got != 2 {
		t.Errorf("at reviewer in cycle 2, SelectedCycleNumber = %d, want 2", got)
	}
}
