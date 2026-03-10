package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Minimum terminal dimensions for usable rendering.
const (
	MinWidth  = 40
	MinHeight = 10
)

// Layout breakpoints for adaptive rendering.
const (
	// CompactWidth triggers compact mode for progress bars, footer, etc.
	CompactWidth = 60
	// DetailCollapseHeight triggers auto-collapse of the detail panel.
	DetailCollapseHeight = 30
	// HomeDetailCollapseHeight triggers auto-collapse of the detail panel in home mode.
	// Higher than DetailCollapseHeight because the banner competes for vertical space.
	HomeDetailCollapseHeight = 40
	// BannerCollapseHeight is the minimum terminal height for showing the top banner.
	// Below this height, the banner is hidden to preserve content space.
	BannerCollapseHeight = 35
	// BannerSMinWidth is the minimum width for the S-A Wide Ellipse top banner.
	BannerSMinWidth = 90
	// BoardMinWidth is the minimum terminal width for the columnar board view.
	// Below this width, the TUI auto-falls back to the table view.
	BoardMinWidth = 100
)

// Split-pane sidebar layout constants.
const (
	// SidebarCollapseWidth is the minimum terminal width for showing the sidebar.
	// Below this width, the sidebar is hidden and the layout falls back to the
	// current full-width vertical layout.
	SidebarCollapseWidth = 100

	// SidebarMinWidth is the minimum width for the sidebar panel.
	SidebarMinWidth = 20

	// SidebarMaxWidth is the maximum width for the sidebar panel.
	SidebarMaxWidth = 35

	// SidebarWidthFraction is the fraction of terminal width allocated to the sidebar.
	// Applied between SidebarMinWidth and SidebarMaxWidth bounds.
	SidebarWidthFraction = 0.22

	// DetailSplitRatio is the fraction of right-area height allocated to the main view.
	// The remainder goes to the detail panel.
	DetailSplitRatio = 0.6
)

// ComputeSidebarWidth returns the sidebar width for a given terminal width.
// Returns 0 when the terminal is too narrow and the sidebar should be hidden.
func ComputeSidebarWidth(termWidth int) int {
	if termWidth < SidebarCollapseWidth {
		return 0
	}
	w := int(float64(termWidth) * SidebarWidthFraction)
	if w < SidebarMinWidth {
		w = SidebarMinWidth
	}
	if w > SidebarMaxWidth {
		w = SidebarMaxWidth
	}
	return w
}

// TruncateWithEllipsis truncates s to maxLen runes, appending "..." if truncated.
// If maxLen is less than 4, returns s truncated to maxLen runes without ellipsis.
// Returns s unchanged if it fits within maxLen runes.
// Uses rune-aware counting and slicing to avoid splitting multi-byte UTF-8 characters.
func TruncateWithEllipsis(s string, maxLen int) string {
	runeCount := utf8.RuneCountInString(s)
	if runeCount <= maxLen {
		return s
	}
	if maxLen < 4 {
		if maxLen <= 0 {
			return ""
		}
		return truncateToNRunes(s, maxLen)
	}
	return truncateToNRunes(s, maxLen-3) + "..."
}

// truncateToNRunes returns the first n runes of s as a string.
func truncateToNRunes(s string, n int) string {
	i := 0
	for j := 0; j < n; j++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

// padToWidth pads a rendered (possibly ANSI-styled) string with spaces to fill
// the given width, then applies a background color across the entire padded row.
// This ensures selected-row highlighting spans the full terminal width.
func padToWidth(s string, width int, bg lipgloss.Color) string {
	visible := lipgloss.Width(s)
	if visible < width {
		s += strings.Repeat(" ", width-visible)
	}
	return lipgloss.NewStyle().Background(bg).Render(s)
}
