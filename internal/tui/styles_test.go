package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

func TestSemanticColorsDefined(t *testing.T) {
	t.Parallel()
	// Verify all semantic colors are non-empty strings.
	colors := map[string]lipgloss.Color{
		"colorPrimary":       colorPrimary,
		"colorAccent":        colorAccent,
		"colorSuccess":       colorSuccess,
		"colorDanger":        colorDanger,
		"colorMuted":         colorMuted,
		"colorMutedLight":    colorMutedLight,
		"colorWhite":         colorWhite,
		"colorBrightWhite":   colorBrightWhite,
		"colorSurface":       colorSurface,
		"colorSurfaceBright": colorSurfaceBright,
		"colorSurfaceDim":    colorSurfaceDim,
		"colorBlue":          colorBlue,
		"colorStarYellow":    colorStarYellow,
		"colorNebula":        colorNebula,
		"colorNebulaDeep":    colorNebulaDeep,
	}
	for name, c := range colors {
		if string(c) == "" {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestStatusBarStyleProperties(t *testing.T) {
	t.Parallel()
	// Verify structural properties of the status bar style.
	bg := styleStatusBar.GetBackground()
	if _, noColor := bg.(lipgloss.NoColor); noColor {
		t.Error("status bar should have a background color set")
	}
}

func TestDetailBorderIsRounded(t *testing.T) {
	t.Parallel()
	border := styleDetailBorder.GetBorderStyle()
	rounded := lipgloss.RoundedBorder()
	if border.TopLeft != rounded.TopLeft || border.TopRight != rounded.TopRight {
		t.Error("detail panel should use rounded border")
	}
}

func TestFooterStyleHasTopBorder(t *testing.T) {
	t.Parallel()
	if !styleFooter.GetBorderTop() {
		t.Error("footer style should have a top border")
	}
	// Bottom, left, right borders should not be set.
	if styleFooter.GetBorderBottom() {
		t.Error("footer should not have a bottom border")
	}
}

func TestSelectionIndicatorConstant(t *testing.T) {
	t.Parallel()
	if selectionIndicator == "" {
		t.Error("selectionIndicator should not be empty")
	}
	if selectionIndicator != "▎" {
		t.Errorf("selectionIndicator = %q, want %q", selectionIndicator, "▎")
	}
}

func TestStatusIcons(t *testing.T) {
	t.Parallel()
	icons := map[string]string{
		"iconDone":    iconDone,
		"iconFailed":  iconFailed,
		"iconWorking": iconWorking,
		"iconWaiting": iconWaiting,
		"iconGate":    iconGate,
		"iconSkipped": iconSkipped,
	}
	for name, icon := range icons {
		if icon == "" {
			t.Errorf("%s is empty", name)
		}
	}
	if iconDone != "✓" {
		t.Errorf("iconDone = %q, want %q", iconDone, "✓")
	}
	if iconFailed != "✗" {
		t.Errorf("iconFailed = %q, want %q", iconFailed, "✗")
	}
}

func TestBreadcrumbStyleProperties(t *testing.T) {
	t.Parallel()
	bg := styleBreadcrumb.GetBackground()
	if _, noColor := bg.(lipgloss.NoColor); noColor {
		t.Error("breadcrumb style should have a background color set")
	}
}

func TestRowStylesHaveDistinctColors(t *testing.T) {
	t.Parallel()
	// Verify that row styles use distinct foreground colors
	// (comparing style properties rather than rendered output,
	// since lipgloss degrades to no-color in non-TTY environments).
	styles := map[string]lipgloss.Style{
		"done":    styleRowDone,
		"working": styleRowWorking,
		"failed":  styleRowFailed,
		"gate":    styleRowGate,
		"waiting": styleRowWaiting,
	}

	colors := make(map[string]string)
	for name, s := range styles {
		fg := s.GetForeground()
		colorStr := ""
		if c, ok := fg.(lipgloss.Color); ok {
			colorStr = string(c)
		}
		colors[name] = colorStr
	}

	// Each status style should have a unique foreground color.
	seen := make(map[string]string) // color -> style name
	for name, c := range colors {
		if c == "" {
			continue // skip if no color (shouldn't happen)
		}
		if prevName, exists := seen[c]; exists {
			t.Errorf("styles %q and %q use the same foreground color %q", name, prevName, c)
		}
		seen[c] = name
	}
}

func TestLoopViewRendersSelectionIndicator(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.FinishAgent("coder", 0.5, 5000)
	lv.Width = 80
	lv.Cursor = 0 // select cycle header

	output := lv.View()
	if !strings.Contains(output, selectionIndicator) {
		t.Error("selected row should contain the selection indicator")
	}
	if !strings.Contains(output, iconDone) {
		t.Error("finished agent should show done icon")
	}
}

func TestLoopViewWorkingAgentShowsIcon(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.Width = 80

	output := lv.View()
	if !strings.Contains(output, iconWorking) {
		t.Error("working agent should show working icon")
	}
}

func TestLoopViewUnselectedRowHasNoIndicator(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.FinishAgent("coder", 0.5, 5000)
	lv.StartCycle(2)
	lv.StartAgent("coder")
	lv.Width = 80
	lv.Cursor = 0 // only first row selected

	output := lv.View()
	lines := strings.Split(output, "\n")

	// Count how many lines contain the selection indicator.
	// Only the cursor row (line 0) should have it.
	indicatorCount := 0
	for _, line := range lines {
		if strings.Contains(line, selectionIndicator) {
			indicatorCount++
		}
	}
	if indicatorCount != 1 {
		t.Errorf("expected exactly 1 line with selection indicator, got %d", indicatorCount)
	}
}

func TestNebulaViewRendersStatusIcons(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "done-phase", Title: "Done"},
		{ID: "fail-phase", Title: "Failed"},
		{ID: "wait-phase", Title: "Waiting"},
	})
	nv.SetPhaseStatus("done-phase", PhaseDone)
	nv.SetPhaseStatus("fail-phase", PhaseFailed)
	nv.Width = 80

	output := nv.View()
	if !strings.Contains(output, iconDone) {
		t.Error("done phase should show done icon (✓)")
	}
	if !strings.Contains(output, iconFailed) {
		t.Error("failed phase should show failed icon (✗)")
	}
	if !strings.Contains(output, iconWaiting) {
		t.Error("waiting phase should show waiting icon (·)")
	}
}

func TestNebulaViewSelectedRowHasIndicator(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	})
	nv.Cursor = 0
	nv.Width = 80

	output := nv.View()
	if !strings.Contains(output, selectionIndicator) {
		t.Error("selected phase row should contain selection indicator")
	}
}

func TestNebulaViewSkippedPhaseIcon(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "skip-phase", Title: "Skipped"},
	})
	nv.SetPhaseStatus("skip-phase", PhaseSkipped)
	nv.Cursor = -1 // no selection
	nv.Width = 80

	output := nv.View()
	if !strings.Contains(output, iconSkipped) {
		t.Error("skipped phase should show skipped icon (–)")
	}
}

func TestFooterRendersKeyDescParts(t *testing.T) {
	t.Parallel()
	km := DefaultKeyMap()
	f := Footer{
		Width:    80,
		Bindings: LoopFooterBindings(km),
	}
	output := f.View()
	// Footer should contain key names and separators.
	if !strings.Contains(output, ":") {
		t.Error("footer should contain colon separators between keys and descriptions")
	}
}

func TestBreadcrumbRendering(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.FocusedPhase = "setup"
	m.Depth = DepthPhaseLoop

	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, "phases") {
		t.Error("breadcrumb should contain 'phases'")
	}
	if !strings.Contains(bc, "setup") {
		t.Error("breadcrumb should contain focused phase name")
	}
	if !strings.Contains(bc, "›") {
		t.Error("breadcrumb should contain separator")
	}
}

func TestBreadcrumbAtAgentOutputDepth(t *testing.T) {
	t.Parallel()
	m := NewAppModel(ModeNebula)
	m.Detail = NewDetailPanel(80, 10)
	m.Width = 80
	m.Height = 24
	m.FocusedPhase = "auth"
	m.Depth = DepthAgentOutput

	bc := m.renderBreadcrumb()
	if !strings.Contains(bc, "output") {
		t.Error("breadcrumb at agent output depth should contain 'output'")
	}
	if !strings.Contains(bc, "auth") {
		t.Error("breadcrumb should contain focused phase name")
	}
}

func TestRowSelectedStyleIsBold(t *testing.T) {
	t.Parallel()
	if !styleRowSelected.GetBold() {
		t.Error("selected row style should be bold")
	}
}

func TestRowFailedStyleIsBold(t *testing.T) {
	t.Parallel()
	if !styleRowFailed.GetBold() {
		t.Error("failed row style should be bold for emphasis")
	}
}

func TestSelectionIndicatorStyleIsBold(t *testing.T) {
	t.Parallel()
	if !styleSelectionIndicator.GetBold() {
		t.Error("selection indicator style should be bold")
	}
}

func TestSectionBorderHasTopOnly(t *testing.T) {
	t.Parallel()
	if !styleSectionBorder.GetBorderTop() {
		t.Error("section border should have top border")
	}
	if styleSectionBorder.GetBorderBottom() {
		t.Error("section border should not have bottom border")
	}
	if styleSectionBorder.GetBorderLeft() {
		t.Error("section border should not have left border")
	}
	if styleSectionBorder.GetBorderRight() {
		t.Error("section border should not have right border")
	}
}

func TestRenderProgressBar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		completed int
		total     int
		width     int
		wantEmpty bool
		wantFull  bool
	}{
		{"zero total", 0, 0, 10, true, false},
		{"zero width", 5, 10, 0, true, false},
		{"no progress", 0, 10, 10, false, false},
		{"half progress", 5, 10, 10, false, false},
		{"full progress", 10, 10, 10, false, true},
		{"over progress", 15, 10, 10, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := renderProgressBar(tt.completed, tt.total, tt.width)
			if tt.wantEmpty && result != "" {
				t.Errorf("expected empty bar, got %q", result)
			}
			if !tt.wantEmpty && result == "" {
				t.Error("expected non-empty bar")
			}
			if tt.wantFull && !strings.Contains(result, "━") {
				t.Error("full bar should contain filled segments")
			}
		})
	}
}

func TestRenderBudgetBar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		spent     float64
		budget    float64
		width     int
		wantEmpty bool
	}{
		{"zero budget", 0, 0, 10, true},
		{"zero width", 5, 10, 0, true},
		{"low spend", 1, 10, 10, false},
		{"mid spend", 5, 10, 10, false},
		{"high spend", 8, 10, 10, false},
		{"over budget", 12, 10, 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := renderBudgetBar(tt.spent, tt.budget, tt.width)
			if tt.wantEmpty && result != "" {
				t.Errorf("expected empty bar, got %q", result)
			}
			if !tt.wantEmpty && result == "" {
				t.Error("expected non-empty bar")
			}
		})
	}
}

func TestRenderCycleBar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		cycle     int
		maxCycles int
		wantEmpty bool
	}{
		{"zero max", 0, 0, true},
		{"first cycle", 1, 5, false},
		{"last cycle", 5, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := renderCycleBar(tt.cycle, tt.maxCycles)
			if tt.wantEmpty && result != "" {
				t.Errorf("expected empty bar, got %q", result)
			}
			if !tt.wantEmpty {
				if !strings.Contains(result, "[") || !strings.Contains(result, "]") {
					t.Error("cycle bar should be wrapped in brackets")
				}
			}
		})
	}
}

func TestProgressColor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		ratio float64
		want  lipgloss.Color
	}{
		{"zero", 0.0, colorMutedLight},
		{"low", 0.1, colorMutedLight},
		{"below half", 0.4, colorMutedLight},
		{"at half", 0.5, colorSuccess},
		{"high", 0.9, colorSuccess},
		{"full", 1.0, colorSuccess},
		{"over", 1.5, colorSuccess},
		{"negative", -0.1, colorMutedLight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := progressColor(tt.ratio)
			if got != tt.want {
				t.Errorf("progressColor(%f) = %v, want %v", tt.ratio, got, tt.want)
			}
		})
	}
}

func TestBudgetColor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		ratio float64
		want  lipgloss.Color
	}{
		{"low spend", 0.2, colorAccent},
		{"just under half", 0.49, colorAccent},
		{"at half", 0.5, colorBudgetWarn},
		{"mid spend", 0.6, colorBudgetWarn},
		{"high spend", 0.79, colorBudgetWarn},
		{"at danger", 0.8, colorDanger},
		{"critical", 0.95, colorDanger},
		{"over budget", 1.1, colorDanger},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := budgetColor(tt.ratio)
			if got != tt.want {
				t.Errorf("budgetColor(%f) = %v, want %v", tt.ratio, got, tt.want)
			}
		})
	}
}

func TestFormatElapsed(t *testing.T) {
	t.Parallel()
	t.Run("zero time", func(t *testing.T) {
		t.Parallel()
		result := formatElapsed(time.Time{})
		if result != "" {
			t.Errorf("expected empty string for zero time, got %q", result)
		}
	})
	t.Run("recent time", func(t *testing.T) {
		t.Parallel()
		// Use a fixed time in the past to produce a known duration.
		start := time.Now().Add(-5 * time.Second)
		result := formatElapsed(start)
		// Should contain 's' for seconds.
		if !strings.Contains(result, "s") {
			t.Errorf("expected elapsed to contain 's', got %q", result)
		}
	})
}

func TestRoleColoredSpinner(t *testing.T) {
	t.Parallel()
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	// Just verify it doesn't panic and returns non-empty.
	coderResult := roleColoredSpinner("coder", s)
	if coderResult == "" {
		t.Error("coder spinner should not be empty")
	}
	reviewerResult := roleColoredSpinner("reviewer", s)
	if reviewerResult == "" {
		t.Error("reviewer spinner should not be empty")
	}
}

func TestAgentEntryHasStartedAt(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	if len(lv.Cycles) == 0 || len(lv.Cycles[0].Agents) == 0 {
		t.Fatal("expected agent to be added")
	}
	agent := lv.Cycles[0].Agents[0]
	if agent.StartedAt.IsZero() {
		t.Error("StartedAt should be set when agent starts")
	}
}

func TestLoopViewWorkingAgentShowsElapsed(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.Width = 80

	output := lv.View()
	if !strings.Contains(output, "working…") {
		t.Error("working agent should show 'working…'")
	}
	// Should contain seconds indicator.
	if !strings.Contains(output, "s") {
		t.Error("working agent should show elapsed time with 's'")
	}
}

func TestStatusBarNebulaShowsProgressBar(t *testing.T) {
	t.Parallel()
	sb := StatusBar{
		Name:      "test",
		Total:     10,
		Completed: 5,
		Width:     100,
	}
	output := sb.View()
	if !strings.Contains(output, "5/10") {
		t.Error("nebula status bar should show completed/total count")
	}
}

func TestStatusBarLoopShowsCycleBar(t *testing.T) {
	t.Parallel()
	sb := StatusBar{
		BeadID:    "bead-123",
		Cycle:     2,
		MaxCycles: 5,
		Width:     100,
	}
	output := sb.View()
	if !strings.Contains(output, "cycle 2/5") {
		t.Error("loop status bar should show cycle count")
	}
	if !strings.Contains(output, "[") {
		t.Error("loop status bar should show cycle bar brackets")
	}
}

func TestStatusBarBudgetShowsBar(t *testing.T) {
	t.Parallel()
	sb := StatusBar{
		CostUSD:   3.50,
		BudgetUSD: 10.00,
		Width:     100,
	}
	output := sb.View()
	if !strings.Contains(output, "$3.50") {
		t.Error("budget bar should show current cost")
	}
	if !strings.Contains(output, "$10.00") {
		t.Error("budget bar should show budget limit")
	}
}

func TestStatusBarSegmentStylesDistinct(t *testing.T) {
	t.Parallel()
	// Verify that status bar segment styles use distinct foreground colors
	// so the bar is visually scannable (at least 4 distinct colors).
	styles := map[string]lipgloss.Style{
		"mode":     styleStatusMode,
		"name":     styleStatusName,
		"progress": styleStatusProgress,
		"cost":     styleStatusCost,
		"elapsed":  styleStatusElapsed,
	}

	colors := make(map[string]string)
	for name, s := range styles {
		fg := s.GetForeground()
		if c, ok := fg.(lipgloss.Color); ok {
			colors[name] = string(c)
		}
	}

	// Count distinct colors — need at least 4 for visual scannability.
	unique := make(map[string]bool)
	for _, c := range colors {
		unique[c] = true
	}
	if len(unique) < 4 {
		t.Errorf("expected at least 4 distinct foreground colors in status bar, got %d: %v", len(unique), colors)
	}
}

func TestResourceStylesDistinct(t *testing.T) {
	t.Parallel()
	// Verify that resource styles use different colors for each severity level.
	normalFg := styleResourceNormal.GetForeground()
	warningFg := styleResourceWarning.GetForeground()
	dangerFg := styleResourceDanger.GetForeground()

	normalColor, _ := normalFg.(lipgloss.Color)
	warningColor, _ := warningFg.(lipgloss.Color)
	dangerColor, _ := dangerFg.(lipgloss.Color)

	if string(normalColor) == string(warningColor) {
		t.Errorf("normal and warning resource styles should have different colors, both are %q", string(normalColor))
	}
	if string(normalColor) == string(dangerColor) {
		t.Errorf("normal and danger resource styles should have different colors, both are %q", string(normalColor))
	}
	if string(warningColor) == string(dangerColor) {
		t.Errorf("warning and danger resource styles should have different colors, both are %q", string(warningColor))
	}
}

func TestResourceLevelStyleColors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		level ResourceLevel
		want  lipgloss.Style
	}{
		{"normal", ResourceNormal, styleResourceNormal},
		{"warning", ResourceWarning, styleResourceWarning},
		{"danger", ResourceDanger, styleResourceDanger},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resourceLevelStyle(tt.level)
			gotFg := got.GetForeground()
			wantFg := tt.want.GetForeground()
			if gotFg != wantFg {
				t.Errorf("resourceLevelStyle(%v) foreground = %v, want %v", tt.level, gotFg, wantFg)
			}
		})
	}
}

func TestNewColorsDefined(t *testing.T) {
	t.Parallel()
	if string(colorBudgetWarn) == "" {
		t.Error("colorBudgetWarn should not be empty")
	}
	if string(colorReviewer) == "" {
		t.Error("colorReviewer should not be empty")
	}
	if string(colorStarYellow) == "" {
		t.Error("colorStarYellow should not be empty")
	}
	if string(colorNebula) == "" {
		t.Error("colorNebula should not be empty")
	}
	if string(colorNebulaDeep) == "" {
		t.Error("colorNebulaDeep should not be empty")
	}
}
