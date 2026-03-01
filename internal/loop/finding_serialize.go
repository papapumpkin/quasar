package loop

import (
	"fmt"
	"strings"
)

// SerializeFindings renders a slice of ReviewFinding into a compact text block
// suitable for injection into the reviewer prompt. Each finding is formatted as
// a numbered entry with its ID, severity, cycle of origin, current status, and
// a truncated description. An empty slice produces an empty string.
func SerializeFindings(findings []ReviewFinding, maxDescLen int) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range findings {
		desc := truncate(f.Description, maxDescLen)
		fmt.Fprintf(&b, "%d. [%s] id=%s cycle=%d status=%s\n   %s\n",
			i+1, f.Severity, f.ID, f.Cycle, f.Status, desc)
	}
	return b.String()
}
