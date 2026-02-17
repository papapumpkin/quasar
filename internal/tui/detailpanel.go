package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// maxOutputLines is the maximum number of lines shown before truncation.
const maxOutputLines = 500

// DetailPanel wraps a viewport for scrollable content display.
type DetailPanel struct {
	viewport    viewport.Model
	title       string
	ready       bool
	totalLines  int // total lines of content (before viewport clipping)
	emptyHint   string
	headerBlock string // rendered header (above viewport content)
}

// NewDetailPanel creates a detail panel with the given dimensions.
func NewDetailPanel(width, height int) DetailPanel {
	vp := viewport.New(width, height)
	vp.SetContent("")
	return DetailPanel{
		viewport: vp,
		ready:    true,
	}
}

// SetSize updates the viewport dimensions.
func (d *DetailPanel) SetSize(width, height int) {
	d.viewport.Width = width
	d.viewport.Height = height
}

// SetContent updates the displayed text and title.
func (d *DetailPanel) SetContent(title, content string) {
	d.title = title
	d.emptyHint = ""
	d.headerBlock = ""
	d.totalLines = strings.Count(content, "\n") + 1
	d.viewport.SetContent(content)
	d.viewport.GotoTop()
}

// SetContentWithHeader updates the detail panel with a header block above the body.
func (d *DetailPanel) SetContentWithHeader(title, header, body string) {
	d.title = title
	d.emptyHint = ""
	d.headerBlock = header

	combined := body
	if header != "" {
		sep := styleDetailSep.Render(strings.Repeat("─", 40))
		combined = header + "\n" + sep + "\n" + body
	}

	d.totalLines = strings.Count(combined, "\n") + 1
	d.viewport.SetContent(combined)
	d.viewport.GotoTop()
}

// SetEmpty sets the detail panel to show an empty-state hint.
func (d *DetailPanel) SetEmpty(hint string) {
	d.title = ""
	d.headerBlock = ""
	d.emptyHint = hint
	d.totalLines = 0
	d.viewport.SetContent("")
	d.viewport.GotoTop()
}

// Update handles viewport scroll messages.
// Home/g and End/G are handled explicitly because the viewport's built-in
// KeyMap does not bind those keys — only GotoTop()/GotoBottom() methods exist.
func (d *DetailPanel) Update(msg tea.Msg) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "home", "g":
			d.viewport.GotoTop()
			return
		case "end", "G":
			d.viewport.GotoBottom()
			return
		}
	}
	d.viewport, _ = d.viewport.Update(msg)
}

// View renders the detail panel with a rounded border and scroll indicators.
func (d DetailPanel) View() string {
	if d.emptyHint != "" {
		content := styleDetailDim.Render(d.emptyHint)
		return styleDetailBorder.Render(content)
	}

	var b strings.Builder

	if d.title != "" {
		b.WriteString(styleDetailTitle.Render(d.title))
		b.WriteString("\n")
	}

	// Scroll-up indicator.
	if upMore := d.linesAbove(); upMore > 0 {
		b.WriteString(styleScrollIndicator.Render(fmt.Sprintf("↑ %d more", upMore)))
		b.WriteString("\n")
	}

	b.WriteString(d.viewport.View())

	// Scroll-down indicator.
	if downMore := d.linesBelow(); downMore > 0 {
		b.WriteString("\n")
		b.WriteString(styleScrollIndicator.Render(fmt.Sprintf("↓ %d more", downMore)))
	}

	return styleDetailBorder.Render(b.String())
}

// linesAbove returns the number of content lines above the viewport.
func (d DetailPanel) linesAbove() int {
	return d.viewport.YOffset
}

// linesBelow returns the number of content lines below the viewport.
func (d DetailPanel) linesBelow() int {
	below := d.totalLines - d.viewport.YOffset - d.viewport.Height
	if below < 0 {
		return 0
	}
	return below
}

// --- Formatting helpers ---

// AgentContext holds the contextual information for a selected agent entry.
type AgentContext struct {
	Role       string
	Cycle      int
	DurationMs int64
	CostUSD    float64
	IssueCount int
	Done       bool
}

// PhaseContext holds the contextual information for a selected phase.
type PhaseContext struct {
	ID        string
	Title     string
	Status    PhaseStatus
	CostUSD   float64
	Cycles    int
	BlockedBy string
}

// FormatAgentHeader renders a contextual header for an agent entry.
func FormatAgentHeader(ctx AgentContext) string {
	var b strings.Builder

	label := styleDetailHeaderLabel.Render
	value := styleDetailHeaderValue.Render

	b.WriteString(label("role: "))
	b.WriteString(value(ctx.Role))

	if ctx.Cycle > 0 {
		b.WriteString("  ")
		b.WriteString(label("cycle: "))
		b.WriteString(value(fmt.Sprintf("%d", ctx.Cycle)))
	}

	if ctx.Done && ctx.DurationMs > 0 {
		secs := float64(ctx.DurationMs) / 1000.0
		b.WriteString("  ")
		b.WriteString(label("duration: "))
		b.WriteString(value(fmt.Sprintf("%.1fs", secs)))
	}

	if ctx.CostUSD > 0 {
		b.WriteString("  ")
		b.WriteString(label("cost: "))
		b.WriteString(value(fmt.Sprintf("$%.4f", ctx.CostUSD)))
	}

	if ctx.IssueCount > 0 {
		b.WriteString("  ")
		b.WriteString(label("issues: "))
		b.WriteString(value(fmt.Sprintf("%d", ctx.IssueCount)))
	}

	return b.String()
}

// FormatPhaseHeader renders a contextual header for a phase entry.
func FormatPhaseHeader(ctx PhaseContext) string {
	var b strings.Builder

	label := styleDetailHeaderLabel.Render
	value := styleDetailHeaderValue.Render

	b.WriteString(label("phase: "))
	b.WriteString(value(ctx.ID))

	if ctx.Title != "" {
		b.WriteString("  ")
		b.WriteString(label("title: "))
		b.WriteString(value(ctx.Title))
	}

	b.WriteString("  ")
	b.WriteString(label("status: "))
	b.WriteString(value(phaseStatusString(ctx.Status)))

	if ctx.CostUSD > 0 {
		b.WriteString("  ")
		b.WriteString(label("cost: "))
		b.WriteString(value(fmt.Sprintf("$%.2f", ctx.CostUSD)))
	}

	if ctx.Cycles > 0 {
		b.WriteString("  ")
		b.WriteString(label("cycles: "))
		b.WriteString(value(fmt.Sprintf("%d", ctx.Cycles)))
	}

	if ctx.BlockedBy != "" {
		b.WriteString("\n")
		b.WriteString(label("blocked by: "))
		b.WriteString(value(ctx.BlockedBy))
	}

	return b.String()
}

// phaseStatusString returns a human-readable string for a PhaseStatus.
func phaseStatusString(s PhaseStatus) string {
	switch s {
	case PhaseWaiting:
		return "waiting"
	case PhaseWorking:
		return "working"
	case PhaseDone:
		return "done"
	case PhaseFailed:
		return "failed"
	case PhaseGate:
		return "gate"
	case PhaseSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// HighlightOutput applies pattern highlighting to agent output text.
// APPROVED → green, ISSUE: → yellow, SEVERITY: critical → red.
func HighlightOutput(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		b.WriteString(highlightLine(line))
		b.WriteString("\n")
	}
	// Remove trailing newline added by the loop.
	result := b.String()
	if len(result) > 0 {
		result = result[:len(result)-1]
	}
	return result
}

// highlightLine applies pattern highlighting to a single line.
func highlightLine(line string) string {
	upper := strings.ToUpper(line)
	switch {
	case strings.Contains(upper, "SEVERITY: CRITICAL"), strings.Contains(upper, "SEVERITY:CRITICAL"):
		return styleHighlightCritical.Render(line)
	case strings.Contains(upper, "ISSUE:"):
		return styleHighlightIssue.Render(line)
	case strings.Contains(upper, "APPROVED"):
		return styleHighlightApproved.Render(line)
	default:
		return line
	}
}

// TruncateOutput truncates output to maxOutputLines, appending a truncation indicator.
func TruncateOutput(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	truncated := strings.Join(lines[:maxLines], "\n")
	remaining := len(lines) - maxLines
	indicator := styleDetailDim.Render(fmt.Sprintf("\n(truncated — %d more lines)", remaining))
	return truncated + indicator
}

// FormatAgentOutput applies truncation and highlighting to agent output.
func FormatAgentOutput(output string) string {
	truncated := TruncateOutput(output, maxOutputLines)
	return HighlightOutput(truncated)
}
