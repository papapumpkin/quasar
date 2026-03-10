package tui

import (
	"fmt"
	"strings"
)

// activityFromRole returns a default activity string based on the agent role.
func activityFromRole(role string) string {
	switch role {
	case "coder":
		return "coding..."
	case "reviewer":
		return "reviewing..."
	default:
		return "working..."
	}
}

// richActivity builds an informative activity string that includes the agent
// role plus contextual information from the phase title or file claims.
// Example outputs: "coding: Wire streaming invoker", "reviewing: 3 files in internal/tui/".
func richActivity(role, phaseTitle string, claims []string) string {
	prefix := activityPrefix(role)

	// Prefer phase title for context.
	if phaseTitle != "" {
		return prefix + phaseTitle
	}

	// Fall back to file claim summary.
	if len(claims) > 0 {
		dir := commonDir(claims)
		if dir != "" {
			return fmt.Sprintf("%s%d files in %s", prefix, len(claims), dir)
		}
		return fmt.Sprintf("%s%d files", prefix, len(claims))
	}

	return activityFromRole(role)
}

// activityPrefix returns the role-based prefix for rich activity strings.
func activityPrefix(role string) string {
	switch role {
	case "coder":
		return "coding: "
	case "reviewer":
		return "reviewing: "
	default:
		return ""
	}
}

// commonDir extracts the longest common directory prefix from a list of file paths.
// Returns an empty string if no common directory exists.
func commonDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	// Start with the directory portion of the first path.
	slash := strings.LastIndex(paths[0], "/")
	if slash < 0 {
		return ""
	}
	prefix := paths[0][:slash+1]

	for _, p := range paths[1:] {
		for prefix != "" && !strings.HasPrefix(p, prefix) {
			// Strip the trailing slash, then find the parent directory.
			trimmed := prefix[:len(prefix)-1]
			slash := strings.LastIndex(trimmed, "/")
			if slash < 0 {
				return ""
			}
			prefix = trimmed[:slash+1]
		}
	}
	return prefix
}
