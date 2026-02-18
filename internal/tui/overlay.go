package tui

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/loop"
	"github.com/papapumpkin/quasar/internal/nebula"
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
	// Post-completion git workflow status (push/checkout results).
	GitResult *nebula.PostCompletionResult
	// Nebula picker state.
	NebulaChoices []NebulaChoice
	PickerCursor  int
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

	// Git post-completion status.
	if o.GitResult != nil {
		b.WriteString(o.renderGitStatus())
		b.WriteString("\n")
	}

	// Nebula picker (if available).
	if len(o.NebulaChoices) > 0 {
		b.WriteString("\n")
		b.WriteString(styleOverlayHint.Render("Run another nebula?"))
		b.WriteString("\n\n")
		b.WriteString(o.renderNebulaPicker())
		b.WriteString("\n")
	}

	// Exit hint.
	b.WriteString("\n")
	if len(o.NebulaChoices) > 0 {
		b.WriteString(styleOverlayHint.Render("enter:launch  q:quit"))
	} else {
		b.WriteString(styleOverlayHint.Render("Press q to exit"))
	}

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

// renderGitStatus renders the post-completion git push/checkout results.
func (o *CompletionOverlay) renderGitStatus() string {
	r := o.GitResult
	var parts []string

	if r.PushErr != nil {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorDanger).
			Render(fmt.Sprintf("⚠ Push failed: %v", r.PushErr)))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSuccess).
			Render(fmt.Sprintf("✓ Pushed to origin/%s", r.PushBranch)))
	}

	if r.CheckoutErr != nil {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorDanger).
			Render(fmt.Sprintf("⚠ Checkout main failed: %v", r.CheckoutErr)))
	} else {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorSuccess).
			Render("✓ Checked out main"))
	}

	return strings.Join(parts, "\n")
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

	switch {
	case errors.Is(msg.Err, loop.ErrMaxCycles):
		o.Kind = CompletionMaxCycles
		o.Message = msg.Err.Error()
	case errors.Is(msg.Err, loop.ErrBudgetExceeded):
		o.Kind = CompletionBudgetExceeded
		o.Message = msg.Err.Error()
	default:
		o.Kind = CompletionError
		o.Message = msg.Err.Error()
	}
	return o
}

// NewCompletionFromNebulaDone creates a CompletionOverlay from a MsgNebulaDone.
func NewCompletionFromNebulaDone(msg MsgNebulaDone, duration time.Duration, costUSD float64, totalPhases int) *CompletionOverlay {
	o := &CompletionOverlay{
		Duration: duration,
		CostUSD:  costUSD,
	}

	// Count results by outcome.
	o.DoneCount, o.FailedCount, o.SkippedCount = buildNebulaResultCounts(msg.Results, totalPhases)

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

// renderNebulaPicker renders the selectable list of available nebulae.
func (o *CompletionOverlay) renderNebulaPicker() string {
	var b strings.Builder
	for i, c := range o.NebulaChoices {
		prefix := "  "
		if i == o.PickerCursor {
			prefix = "▎ "
		}

		// Format status label.
		var statusLabel string
		switch c.Status {
		case "ready":
			statusLabel = fmt.Sprintf("%d/%d ready", c.Done, c.Phases)
		case "in_progress":
			statusLabel = fmt.Sprintf("%d/%d in_progress", c.Done, c.Phases)
		case "done":
			statusLabel = "done"
		default:
			statusLabel = c.Status
		}

		line := fmt.Sprintf("%s%-20s %s", prefix, c.Name, statusLabel)
		if i == o.PickerCursor {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		} else {
			line = styleDetailDim.Render(line)
		}
		b.WriteString(line)
		if i < len(o.NebulaChoices)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
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

// RenderQuitConfirm renders a centered quit confirmation overlay.
func RenderQuitConfirm(width, height int) string {
	var b strings.Builder

	title := styleOverlayTitle.Foreground(colorAccent).
		Render("⚠  Are you sure you want to exit?")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("Nebula has in-progress phases.")
	b.WriteString("\n\n")
	b.WriteString(styleOverlayHint.Render("[y] Yes, exit    [n] Continue"))

	boxContent := styleOverlayWarning.Render(b.String())
	return centerOverlay(boxContent, width, height)
}

// compositeOverlay renders the overlay box on top of the dimmed background.
// It splits both into lines and replaces the background lines where the
// overlay content appears, producing a layered visual effect.
func compositeOverlay(bg, overlay string, _, height int) string {
	bgLines := strings.Split(bg, "\n")
	olLines := strings.Split(overlay, "\n")

	// Pad background to fill the terminal height.
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}

	// Compute vertical offset to center the overlay.
	olHeight := len(olLines)
	topOffset := 0
	if olHeight < height {
		topOffset = (height - olHeight) / 2
	}

	// Replace background lines with overlay lines.
	for i, olLine := range olLines {
		row := topOffset + i
		if row >= 0 && row < len(bgLines) {
			bgLines[row] = olLine
		}
	}

	return strings.Join(bgLines[:height], "\n")
}

// Toast represents a brief notification displayed at the bottom of the screen.
type Toast struct {
	ID      int
	Message string
	IsError bool
}

// toastDismissDelay is how long a toast stays visible.
const toastDismissDelay = 5 * time.Second

// nextToastID is an atomic counter for toast IDs, safe for concurrent use in tests.
var nextToastID atomic.Int32

// NewToast creates a new toast notification and returns it along with
// a tea.Cmd that will fire MsgToastExpired after the dismiss delay.
func NewToast(message string, isError bool) (Toast, tea.Cmd) {
	id := int(nextToastID.Add(1))
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
// Skipped is inferred as totalPhases minus the phases that reported a result (done + failed).
func buildNebulaResultCounts(results []nebula.WorkerResult, totalPhases int) (done, failed, skipped int) {
	for _, r := range results {
		if r.Err != nil {
			failed++
		} else {
			done++
		}
	}
	skipped = totalPhases - done - failed
	if skipped < 0 {
		skipped = 0
	}
	return done, failed, skipped
}
