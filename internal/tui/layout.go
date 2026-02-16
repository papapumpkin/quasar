package tui

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
)

// TruncateWithEllipsis truncates s to maxLen characters, appending "..." if truncated.
// If maxLen is less than 4, returns s truncated to maxLen without ellipsis.
// Returns s unchanged if it fits within maxLen.
func TruncateWithEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 4 {
		if maxLen <= 0 {
			return ""
		}
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
