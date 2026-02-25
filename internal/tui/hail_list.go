package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/ui"
)

// HailListOverlay renders a navigable list of pending hails. When multiple
// hails are unresolved, the user can browse and select one to view its full
// context and optionally acknowledge it.
type HailListOverlay struct {
	Hails  []ui.HailInfo
	Cursor int
	Width  int
}

// NewHailListOverlay creates a hail list overlay from a slice of pending hails.
func NewHailListOverlay(hails []ui.HailInfo) *HailListOverlay {
	return &HailListOverlay{
		Hails:  hails,
		Cursor: 0,
	}
}

// MoveUp moves the cursor up by one, clamping at the top.
func (h *HailListOverlay) MoveUp() {
	if h.Cursor > 0 {
		h.Cursor--
	}
}

// MoveDown moves the cursor down by one, clamping at the bottom.
func (h *HailListOverlay) MoveDown() {
	if h.Cursor < len(h.Hails)-1 {
		h.Cursor++
	}
}

// Selected returns the currently highlighted hail, or nil if the list is empty.
func (h *HailListOverlay) Selected() *ui.HailInfo {
	if len(h.Hails) == 0 || h.Cursor < 0 || h.Cursor >= len(h.Hails) {
		return nil
	}
	return &h.Hails[h.Cursor]
}

// View renders the hail list as a centered overlay box.
func (h HailListOverlay) View(width, _ int) string {
	var b strings.Builder

	// Constrain overlay width.
	overlayWidth := 64
	if width > 0 && width < overlayWidth+4 {
		overlayWidth = width - 4
	}
	if overlayWidth < 30 {
		overlayWidth = 30
	}

	// Header.
	header := styleHailHeader.Render(fmt.Sprintf("⚠  HAILS (%d pending)", len(h.Hails)))
	b.WriteString(header)
	b.WriteString("\n\n")

	if len(h.Hails) == 0 {
		b.WriteString(styleHailKind.Render("  No pending hails."))
		b.WriteString("\n")
	} else {
		// List each hail with cursor indicator.
		for i, hail := range h.Hails {
			cursor := "  "
			nameStyle := styleHailDetail
			kindStyle := styleHailKind
			if i == h.Cursor {
				cursor = "▸ "
				nameStyle = lipgloss.NewStyle().Foreground(colorBrightWhite).Bold(true)
				kindStyle = lipgloss.NewStyle().Foreground(colorAccent)
			}

			// Kind badge.
			kindBadge := kindBadgeFor(hail.Kind)

			// Summary line: "▸ [blocker] Unable to find dependency..."
			summaryLine := fmt.Sprintf("%s%s %s", cursor, kindBadge, truncateHailSummary(hail.Summary, overlayWidth-10))
			b.WriteString(nameStyle.Render(summaryLine))
			b.WriteString("\n")

			// Context line: "    from: coder · cycle 3"
			var context string
			if hail.SourceRole != "" {
				context = fmt.Sprintf("    from: %s", hail.SourceRole)
			}
			if hail.Cycle > 0 {
				if context != "" {
					context += fmt.Sprintf(" · cycle %d", hail.Cycle)
				} else {
					context = fmt.Sprintf("    cycle %d", hail.Cycle)
				}
			}
			if context != "" {
				b.WriteString(kindStyle.Render(context))
				b.WriteString("\n")
			}

			// Add a blank line between items (but not after the last).
			if i < len(h.Hails)-1 {
				b.WriteString("\n")
			}
		}
	}

	// Footer hints.
	b.WriteString("\n")
	hintStyle := styleHailKind
	b.WriteString(hintStyle.Render("  ↑/↓ navigate · enter view · esc close"))

	return styleHailListOverlay.Width(overlayWidth).Render(b.String())
}

// kindBadgeFor returns a styled kind badge label.
func kindBadgeFor(kind string) string {
	var color lipgloss.Color
	switch kind {
	case "blocker":
		color = colorDanger
	case "human_review":
		color = colorAccent
	case "decision_needed":
		color = colorStarYellow
	case "ambiguity":
		color = colorBlueshift
	default:
		color = colorMuted
	}
	style := lipgloss.NewStyle().Foreground(color)
	return style.Render("[" + kind + "]")
}

// truncateHailSummary truncates a summary string to fit within maxWidth.
func truncateHailSummary(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if len(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return s[:maxWidth]
	}
	return s[:maxWidth-3] + "..."
}

// styleHailListOverlay wraps the hail list with an amber/orange rounded border
// to distinguish it from the red-bordered single hail overlay.
var styleHailListOverlay = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colorAccent).
	Padding(1, 2)
