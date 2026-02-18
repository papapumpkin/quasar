package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders the persistent top bar with task name, progress, budget, elapsed.
type StatusBar struct {
	Name         string
	BeadID       string
	Cycle        int
	MaxCycles    int
	Completed    int
	Total        int
	InProgress   int // phases currently being worked on
	CostUSD      float64
	BudgetUSD    float64
	StartTime    time.Time
	FinalElapsed time.Duration
	Width        int
	Paused       bool
	Stopping     bool
	Resources    ResourceSnapshot
	Thresholds   ResourceThresholds

	// Home mode fields.
	HomeMode        bool // true when displaying the home landing page
	HomeNebulaCount int  // number of discovered nebulas
}

// View renders the status bar as a single line.
// Adapts to narrow terminals by truncating the name and dropping low-priority
// segments (elapsed → cost → progress) to guarantee single-line rendering.
func (s StatusBar) View() string {
	compact := s.Width < CompactWidth

	// The outer styleStatusBar applies Padding(0,1), consuming 2 columns.
	// All inner content must fit within this inner width.
	const barPadding = 2
	innerWidth := s.Width - barPadding
	if innerWidth < 0 {
		innerWidth = 0
	}

	// Build the right-side segments first so we know how much space remains for the name.
	rightSegments := s.buildRightSegments(compact)
	right := joinSegments(rightSegments)

	// Build the fixed left prefix (logo + mode label).
	// All spaces are styled with the bar background to prevent gaps.
	barBg := lipgloss.NewStyle().Background(colorSurface)
	logo := barBg.Render(" ") + Logo() + barBg.Render("  ")
	logoWidth := lipgloss.Width(logo)

	// State indicator (STOPPING / PAUSED) — appended after the name.
	stateIndicator := s.renderStateIndicator()

	// Compute available width for the name segment.
	rightWidth := lipgloss.Width(right)
	stateWidth := lipgloss.Width(stateIndicator)
	fixedLeft := s.buildFixedLeftPrefix(compact)
	fixedLeftWidth := logoWidth + lipgloss.Width(fixedLeft)

	// Minimum padding between left and right.
	const minGap = 1
	availableForName := innerWidth - fixedLeftWidth - stateWidth - rightWidth - minGap

	// Build the name segment, truncated to fit.
	nameSegment := s.buildNameSegment(compact, availableForName)

	// If even after truncation we overflow, drop right segments progressively.
	left := logo + fixedLeft + nameSegment + stateIndicator
	leftWidth := lipgloss.Width(left)
	if leftWidth+rightWidth+minGap > innerWidth {
		rightSegments = dropSegments(rightSegments, innerWidth-leftWidth-minGap)
		right = joinSegments(rightSegments)
		rightWidth = lipgloss.Width(right)
	}

	// Pad the gap between left and right.
	// The gap spaces are styled with the bar background to prevent visible breaks.
	gap := innerWidth - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}
	padding := barBg.Render(strings.Repeat(" ", gap))

	line := left + padding + right

	// Safety clamp: if the assembled line still exceeds inner width, hard-truncate.
	if lipgloss.Width(line) > innerWidth {
		line = truncateToWidth(line, innerWidth)
	}

	return styleStatusBar.Width(s.Width).Render(line)
}

// statusSegment represents a styled segment of the status bar with a drop priority.
// Lower priority values are dropped first when the terminal is too narrow.
type statusSegment struct {
	text     string
	priority int // higher = keep longer; elapsed=1, cost=2, progress=3
}

// buildRightSegments assembles the right-side segments in display order.
func (s StatusBar) buildRightSegments(compact bool) []statusSegment {
	barBg := lipgloss.NewStyle().Background(colorSurface)
	var segments []statusSegment

	// Cost segment (priority 2).
	// When a budget is set, color-code the cost based on consumption ratio.
	if s.BudgetUSD > 0 && !compact {
		ratio := s.CostUSD / s.BudgetUSD
		costColor := budgetColor(ratio)
		costStyle := lipgloss.NewStyle().Background(colorSurface).Foreground(costColor)
		budgetBar := renderBudgetBar(s.CostUSD, s.BudgetUSD, 10)
		costText := costStyle.Render(fmt.Sprintf("$%.2f", s.CostUSD)) + barBg.Render(" ") +
			budgetBar + barBg.Render(" ") +
			costStyle.Render(fmt.Sprintf("$%.2f", s.BudgetUSD))
		segments = append(segments, statusSegment{text: costText, priority: 2})
	} else {
		segments = append(segments, statusSegment{
			text:     styleStatusCost.Render(fmt.Sprintf("$%.2f", s.CostUSD)),
			priority: 2,
		})
	}

	// Resource indicator segment (priority 0 — dropped before elapsed).
	resText := s.renderResourceSegment(compact)
	if resText != "" {
		segments = append(segments, statusSegment{text: barBg.Render("  ") + resText, priority: 0})
	}

	// Elapsed segment (priority 1 — dropped after resources).
	var elapsed time.Duration
	if s.FinalElapsed > 0 {
		elapsed = s.FinalElapsed
	} else if !s.StartTime.IsZero() {
		elapsed = time.Since(s.StartTime).Truncate(time.Second)
	}
	if elapsed > 0 {
		segments = append(segments, statusSegment{
			text:     styleStatusElapsed.Render(fmt.Sprintf("  %s", elapsed)),
			priority: 1,
		})
	}

	return segments
}

// renderResourceSegment renders the compact resource indicator with color coding.
// Format: "◈2  48MB  3.2%" with optional "⚡2 quasars" suffix.
func (s StatusBar) renderResourceSegment(compact bool) string {
	indicator := FormatResourceIndicator(s.Resources)
	if indicator == "" {
		return ""
	}

	barBg := lipgloss.NewStyle().Background(colorSurface)

	// Choose style based on worst resource level.
	style := resourceLevelStyle(s.Resources.WorstLevel(s.Thresholds))
	result := style.Render(indicator)

	// Multi-quasar detection (skip in compact mode to save space).
	if !compact {
		qCount := FormatQuasarCount(s.Resources.QuasarCount)
		if qCount != "" {
			result += barBg.Render("  ") + styleResourceWarning.Render(qCount)
		}
	}

	return result
}

// buildFixedLeftPrefix returns the mode label + progress text (without the name).
func (s StatusBar) buildFixedLeftPrefix(compact bool) string {
	if s.HomeMode {
		return styleStatusMode.Render("home: ")
	}
	if s.Total > 0 {
		// Nebula mode.
		if compact {
			pct := s.Completed * 100 / s.Total
			ratio := float64(s.Completed) / float64(s.Total)
			pColor := progressColor(ratio)
			progStyle := lipgloss.NewStyle().Background(colorSurface).Foreground(pColor)
			if ratio >= 1 {
				progStyle = progStyle.Bold(true)
			}
			return styleStatusMode.Render("nebula: ") + progStyle.Render(fmt.Sprintf("%d%% ", pct))
		}
		return styleStatusMode.Render("nebula: ")
	}
	if s.BeadID != "" {
		if compact {
			return styleStatusMode.Render("task ") +
				styleStatusProgress.Render(fmt.Sprintf("%d/%d ", s.Cycle, s.MaxCycles))
		}
		return styleStatusMode.Render("task ")
	}
	return ""
}

// buildNameSegment returns the name/ID segment, truncated to fit maxWidth.
func (s StatusBar) buildNameSegment(compact bool, maxWidth int) string {
	barBg := lipgloss.NewStyle().Background(colorSurface)
	if maxWidth < 0 {
		maxWidth = 0
	}

	if s.HomeMode {
		label := fmt.Sprintf("%d nebulas", s.HomeNebulaCount)
		if s.HomeNebulaCount == 1 {
			label = "1 nebula"
		}
		label = TruncateWithEllipsis(label, maxWidth)
		return styleStatusName.Render(label)
	}

	if s.Total > 0 {
		// Nebula mode: name + progress bar + counts.
		name := s.Name
		if compact {
			name = TruncateWithEllipsis(name, min(12, maxWidth))
			return styleStatusName.Render(name)
		}

		// Full mode: "name  ━━━━░░░░ 2/5 · 2 active"
		ratio := float64(s.Completed) / float64(s.Total)
		pColor := progressColor(ratio)
		progStyle := lipgloss.NewStyle().Background(colorSurface).Foreground(pColor)
		if ratio >= 1 {
			progStyle = progStyle.Bold(true)
		}
		counterText := fmt.Sprintf(" %d/%d", s.Completed, s.Total)
		bar := renderProgressBar(s.Completed, s.InProgress, s.Total, 12)

		// Append in-progress count when phases are actively working.
		var activeSuffix string
		if s.InProgress > 0 {
			activeStyle := lipgloss.NewStyle().Background(colorSurface).Foreground(colorBlue)
			activeSuffix = progStyle.Render(" · ") + activeStyle.Render(fmt.Sprintf("%d active", s.InProgress))
		}

		fullSuffix := barBg.Render("  ") + bar + progStyle.Render(counterText) + activeSuffix
		suffixWidth := lipgloss.Width(fullSuffix)

		availableForName := maxWidth - suffixWidth
		if availableForName < 4 {
			availableForName = 4
		}
		name = TruncateWithEllipsis(name, availableForName)
		return styleStatusName.Render(name) + fullSuffix
	}

	if s.BeadID != "" {
		// Loop mode: "bead-id  cycle 2/5 [██░░░]"
		if compact {
			// Progress is already rendered by buildFixedLeftPrefix; show truncated bead ID.
			beadID := TruncateWithEllipsis(s.BeadID, maxWidth)
			return styleStatusName.Render(beadID)
		}
		cycleBar := renderCycleBar(s.Cycle, s.MaxCycles)
		suffix := fmt.Sprintf("  cycle %d/%d %s", s.Cycle, s.MaxCycles, cycleBar)
		suffixWidth := lipgloss.Width(suffix)

		availableForID := maxWidth - suffixWidth
		if availableForID < 4 {
			availableForID = 4
		}
		beadID := TruncateWithEllipsis(s.BeadID, availableForID)
		return styleStatusName.Render(beadID) +
			styleStatusMode.Render("  cycle ") +
			styleStatusProgress.Render(fmt.Sprintf("%d/%d ", s.Cycle, s.MaxCycles)) +
			cycleBar
	}

	return ""
}

// renderStateIndicator returns the styled STOPPING/PAUSED indicator, or empty string.
func (s StatusBar) renderStateIndicator() string {
	barBg := lipgloss.NewStyle().Background(colorSurface)
	if s.Stopping {
		return barBg.Render("  ") + styleStatusStopping.Render("STOPPING")
	}
	if s.Paused {
		return barBg.Render("  ") + styleStatusPaused.Render("PAUSED")
	}
	return ""
}

// joinSegments concatenates segment text with a trailing styled space.
// The trailing space carries the bar background to prevent gaps.
func joinSegments(segments []statusSegment) string {
	barBg := lipgloss.NewStyle().Background(colorSurface)
	var b strings.Builder
	for _, seg := range segments {
		b.WriteString(seg.text)
	}
	b.WriteString(barBg.Render(" "))
	return b.String()
}

// dropSegments removes lowest-priority segments until the combined width fits within maxWidth.
func dropSegments(segments []statusSegment, maxWidth int) []statusSegment {
	result := make([]statusSegment, len(segments))
	copy(result, segments)

	for totalWidth(result) > maxWidth && len(result) > 0 {
		minIdx := 0
		minPri := result[0].priority
		for i, seg := range result {
			if seg.priority < minPri {
				minPri = seg.priority
				minIdx = i
			}
		}
		result = append(result[:minIdx], result[minIdx+1:]...)
	}
	return result
}

// totalWidth computes the rendered width of all segments plus trailing space.
func totalWidth(segments []statusSegment) int {
	w := 1 // trailing space from joinSegments
	for _, seg := range segments {
		w += lipgloss.Width(seg.text)
	}
	return w
}

// renderProgressBar creates a filled/empty bar showing completed/total progress.
// Completed segments render in green; in-progress segments in blue; empty in gray.
func renderProgressBar(completed, inProgress, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	doneRatio := float64(completed) / float64(total)
	if doneRatio > 1 {
		doneRatio = 1
	}
	activeRatio := float64(inProgress) / float64(total)
	if doneRatio+activeRatio > 1 {
		activeRatio = 1 - doneRatio
	}

	doneFilled := int(doneRatio * float64(width))
	activeFilled := int(activeRatio * float64(width))
	empty := width - doneFilled - activeFilled
	if empty < 0 {
		empty = 0
	}

	doneStyle := lipgloss.NewStyle().Background(colorSurface).Foreground(colorSuccess)
	activeStyle := lipgloss.NewStyle().Background(colorSurface).Foreground(colorBlue)
	emptyStyle := lipgloss.NewStyle().Background(colorSurface).Foreground(colorMuted)

	return doneStyle.Render(strings.Repeat("━", doneFilled)) +
		activeStyle.Render(strings.Repeat("━", activeFilled)) +
		emptyStyle.Render(strings.Repeat("░", empty))
}

// renderBudgetBar creates an inline budget consumption indicator.
// The filled portion color shifts from amber to orange to red as spending increases.
func renderBudgetBar(spent, budget float64, width int) string {
	if budget <= 0 || width <= 0 {
		return ""
	}
	ratio := spent / budget
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	empty := width - filled

	fillColor := budgetColor(ratio)
	style := lipgloss.NewStyle().Background(colorSurface).Foreground(fillColor)
	emptyStyle := lipgloss.NewStyle().Background(colorSurface).Foreground(colorMuted)

	return style.Render(strings.Repeat("━", filled)) +
		emptyStyle.Render(strings.Repeat("░", empty))
}

// renderCycleBar creates a mini progress bar for cycle progress: [██░░░].
func renderCycleBar(cycle, maxCycles int) string {
	if maxCycles <= 0 {
		return ""
	}
	const barWidth = 5
	filled := cycle * barWidth / maxCycles
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	barBg := lipgloss.NewStyle().Background(colorSurface).Foreground(colorMutedLight)
	return barBg.Render("[") +
		lipgloss.NewStyle().Background(colorSurface).Foreground(colorMutedLight).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Background(colorSurface).Foreground(colorMuted).Render(strings.Repeat("░", empty)) +
		barBg.Render("]")
}

// progressColor returns a color that shifts from muted to blue to green as progress increases.
// 0%: colorMuted (gray, hasn't started), 1-49%: colorBlue (in progress, early),
// 50-99%: colorSuccess (making good progress), 100%: colorSuccess (all done).
func progressColor(ratio float64) lipgloss.Color {
	if ratio <= 0 {
		return colorMuted
	}
	if ratio >= 1 {
		return colorSuccess
	}
	if ratio < 0.5 {
		return colorBlue
	}
	return colorSuccess
}

// resourceLevelStyle returns the appropriate style for the given resource level.
func resourceLevelStyle(level ResourceLevel) lipgloss.Style {
	switch level {
	case ResourceDanger:
		return styleResourceDanger
	case ResourceWarning:
		return styleResourceWarning
	default:
		return styleResourceNormal
	}
}

// budgetColor returns a color based on budget consumption ratio.
// Under 30%: muted (calm), 30-50%: amber, 50-80%: orange warning, over 80%: red danger.
func budgetColor(ratio float64) lipgloss.Color {
	switch {
	case ratio >= 0.8:
		return colorDanger
	case ratio >= 0.5:
		return colorBudgetWarn
	case ratio >= 0.3:
		return colorAccent
	default:
		return colorMutedLight
	}
}

// truncateToWidth hard-truncates a string (which may contain ANSI escape sequences)
// so that its rendered width does not exceed maxWidth. It walks rune-by-rune,
// skipping ANSI escape sequences from the width count, and stops once the
// visual width would exceed the limit.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	var b strings.Builder
	width := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			b.WriteRune(r)
			continue
		}
		if inEscape {
			b.WriteRune(r)
			// ESC sequences end at a letter (A-Z, a-z).
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		// Approximate: CJK and wide runes would need special handling,
		// but status bar content is ASCII/Latin only.
		if width+1 > maxWidth {
			break
		}
		b.WriteRune(r)
		width++
	}
	return b.String()
}
