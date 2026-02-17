package tui

import "unicode/utf8"

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
	DetailCollapseHeight = 20
	// SidePanelMinWidth is the minimum terminal width to show the S-B side panel art.
	SidePanelMinWidth = 120
	// BannerSMinWidth is the minimum width for the S-A Wide Ellipse top banner.
	BannerSMinWidth = 90
)

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
