package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aaronsalm/quasar/internal/nebula"
)

// CompletionOverlay displays the outcome when the loop or nebula finishes.
type CompletionOverlay struct {
	Kind     CompletionKind
	Message  string
	Duration time.Duration
	CostUSD  float64
	// Nebula-specific result summary.
	DoneCount    int
	FailedCount  int
	SkippedCount int
}

// CompletionKind classifies the completion outcome for styling.
type CompletionKind int

const (
	// CompletionSuccess indicates the task completed without error.
	CompletionSuccess CompletionKind = iota
	// CompletionMaxCycles indicates the cycle limit was reached.
	CompletionMaxCycles
	// CompletionBudgetExceeded indicates cost exceeded the budget.
	CompletionBudgetExceeded
	// CompletionError indicates the task ended with an error.
	CompletionError
)

// View renders the completion overlay as a centered box.
func (o *CompletionOverlay) View(width, height int) string {
	var b strings.Builder

	icon, titleText, style := o.styling()

	// Title line.
	title := styleOverlayTitle.Foreground(style.GetBorderTopForeground()).
		Render(icon + " " + titleText)
	b.WriteString(title)
	b.WriteString("\n\n")

	// Message body.
	if o.Message != "" {
		b.WriteString(o.Message)
		b.WriteString("\n")
	}

	// Summary stats.
	if o.DoneCount > 0 || o.FailedCount > 0 || o.SkippedCount > 0 {
		b.WriteString(o.renderResultSummary())
		b.WriteString("\n")
	}

	// Duration and cost.
	if o.Duration > 0 || o.CostUSD > 0 {
		b.WriteString(o.renderStats())
		b.WriteString("\n")
	}

	// Exit hint.
	b.WriteString("\n")
	b.WriteString(styleOverlayHint.Render("Press q to exit"))

	// Render the box.
	boxContent := style.Render(b.String())

	// Center the box on the screen.
	return centerOverlay(boxContent, width, height)
}

// styling returns the icon, title, and border style for this completion kind.
func (o *CompletionOverlay) styling() (icon string, title string, style lipgloss.Style) {
	switch o.Kind {
	case CompletionSuccess:
		return "✓", "Task complete", styleOverlaySuccess
	case CompletionMaxCycles:
		return "⚠", "Max cycles reached", styleOverlayWarning
	case CompletionBudgetExceeded:
		return "✗", "Budget exceeded", styleOverlayError
	case CompletionError:
		return "✗", "Error", styleOverlayError
	default:
		return "·", "Done", styleOverlaySuccess
	}
}

// renderResultSummary renders the nebula phase result counts.
func (o *CompletionOverlay) renderResultSummary() string {
	var parts []string
	if o.DoneCount > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSuccess).
			Render(fmt.Sprintf("%s %d done", iconDone, o.DoneCount)))
	}
	if o.FailedCount > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorDanger).
			Render(fmt.Sprintf("%s %d failed", iconFailed, o.FailedCount)))
	}
	if o.SkippedCount > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorMuted).
			Render(fmt.Sprintf("%s %d skipped", iconSkipped, o.SkippedCount)))
	}
	return strings.Join(parts, "  ")
}

// renderStats renders the duration and cost line.
func (o *CompletionOverlay) renderStats() string {
	var parts []string
	if o.Duration > 0 {
		parts = append(parts, fmt.Sprintf("Duration: %s", o.Duration.Truncate(time.Second)))
	}
	if o.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("Cost: $%.2f", o.CostUSD))
	}
	return styleDetailDim.Render(strings.Join(parts, "  "))
}

// NewCompletionFromLoopDone creates a CompletionOverlay from a MsgLoopDone.
func NewCompletionFromLoopDone(msg MsgLoopDone, duration time.Duration, costUSD float64) *CompletionOverlay {
	o := &CompletionOverlay{
		Duration: duration,
		CostUSD:  costUSD,
	}

	if msg.Err == nil {
		o.Kind = CompletionSuccess
		return o
	}

	errMsg := msg.Err.Error()
	switch {
	case strings.Contains(errMsg, "max cycles"):
		o.Kind = CompletionMaxCycles
		o.Message = errMsg
	case strings.Contains(errMsg, "budget"):
		o.Kind = CompletionBudgetExceeded
		o.Message = errMsg
	default:
		o.Kind = CompletionError
		o.Message = errMsg
	}
	return o
}

// NewCompletionFromNebulaDone creates a CompletionOverlay from a MsgNebulaDone.
func NewCompletionFromNebulaDone(msg MsgNebulaDone, duration time.Duration, costUSD float64) *CompletionOverlay {
	o := &CompletionOverlay{
		Duration: duration,
		CostUSD:  costUSD,
	}

	// Count results by outcome.
	o.DoneCount, o.FailedCount, o.SkippedCount = buildNebulaResultCounts(msg.Results)

	if msg.Err != nil {
		o.Kind = CompletionError
		o.Message = msg.Err.Error()
	} else if o.FailedCount > 0 {
		o.Kind = CompletionError
	} else {
		o.Kind = CompletionSuccess
	}

	return o
}

// centerOverlay places content in the center of the given dimensions.
func centerOverlay(content string, width, height int) string {
	contentWidth := lipgloss.Width(content)
	contentHeight := lipgloss.Height(content)

	if width <= 0 || height <= 0 {
		return content
	}

	// Horizontal centering.
	leftPad := 0
	if contentWidth < width {
		leftPad = (width - contentWidth) / 2
	}

	// Vertical centering.
	topPad := 0
	if contentHeight < height {
		topPad = (height - contentHeight) / 2
	}

	return lipgloss.NewStyle().
		PaddingLeft(leftPad).
		PaddingTop(topPad).
		Render(content)
}

// Toast represents a brief notification displayed at the bottom of the screen.
type Toast struct {
	ID      int
	Message string
	IsError bool
}

// toastDismissDelay is how long a toast stays visible.
const toastDismissDelay = 5 * time.Second

// nextToastID is a simple counter for toast IDs.
var nextToastID int

// NewToast creates a new toast notification and returns it along with
// a tea.Cmd that will fire MsgToastExpired after the dismiss delay.
func NewToast(message string, isError bool) (Toast, tea.Cmd) {
	nextToastID++
	id := nextToastID
	t := Toast{
		ID:      id,
		Message: message,
		IsError: isError,
	}
	cmd := tea.Tick(toastDismissDelay, func(_ time.Time) tea.Msg {
		return MsgToastExpired{ID: id}
	})
	return t, cmd
}

// RenderToasts renders the toast stack at the bottom of the screen.
func RenderToasts(toasts []Toast, width int) string {
	if len(toasts) == 0 {
		return ""
	}

	var lines []string
	for _, t := range toasts {
		msg := t.Message
		// Truncate if wider than available space (accounting for padding).
		maxWidth := width - 4
		if maxWidth > 0 && len(msg) > maxWidth {
			msg = msg[:maxWidth-1] + "…"
		}
		lines = append(lines, styleToast.Width(width).Render(msg))
	}
	return strings.Join(lines, "\n")
}

// removeToast filters out the toast with the given ID.
func removeToast(toasts []Toast, id int) []Toast {
	result := make([]Toast, 0, len(toasts))
	for _, t := range toasts {
		if t.ID != id {
			result = append(result, t)
		}
	}
	return result
}

// buildNebulaResultCounts counts done, failed, and skipped phases from worker results.
func buildNebulaResultCounts(results []nebula.WorkerResult) (done, failed, skipped int) {
	for _, r := range results {
		if r.Err != nil {
			failed++
		} else {
			done++
		}
	}
	return done, failed, skipped
}
