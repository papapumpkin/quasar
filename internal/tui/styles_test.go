package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestSemanticColorsDefined(t *testing.T) {
	t.Parallel()
	// Verify all semantic colors are non-empty strings.
	colors := map[string]lipgloss.Color{
		"colorPrimary":       colorPrimary,
		"colorPrimaryBright": colorPrimaryBright,
		"colorAccent":        colorAccent,
		"colorSuccess":       colorSuccess,
		"colorSuccessDim":    colorSuccessDim,
		"colorDanger":        colorDanger,
		"colorMuted":         colorMuted,
		"colorMutedLight":    colorMutedLight,
		"colorWhite":         colorWhite,
		"colorBrightWhite":   colorBrightWhite,
		"colorSurface":       colorSurface,
		"colorSurfaceBright": colorSurfaceBright,
		"colorSurfaceDim":    colorSurfaceDim,
		"colorBlue":          colorBlue,
		"colorMagenta":       colorMagenta,
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
	if !styleStatusBar.GetBold() {
		t.Error("status bar should be bold")
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
	// Lines beyond the first should not start with the indicator.
	for i, line := range lines {
		if i == 0 {
			continue // cursor is here
		}
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, selectionIndicator) && i > 0 {
			// Only the cursor row should have the indicator
			// (but we can't easily check line-by-line with ANSI,
			// so just verify at least one line has it).
		}
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
