package relativity

import (
	"fmt"
	"sort"
	"strings"
)

// TimelineReport renders a chronological narrative of the repo's evolution,
// ordered by sequence number.
type TimelineReport struct{}

// Render produces a markdown timeline ordered by nebula sequence.
func (r *TimelineReport) Render(catalog *Spacetime) (string, error) {
	if catalog == nil {
		return "", fmt.Errorf("catalog is nil")
	}

	var b strings.Builder

	b.WriteString("# Evolution Timeline\n")

	if len(catalog.Nebulas) == 0 {
		b.WriteString("\nNo nebulas recorded yet.\n")
		return b.String(), nil
	}

	// Sort by sequence for chronological order.
	sorted := make([]Entry, len(catalog.Nebulas))
	copy(sorted, catalog.Nebulas)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Sequence < sorted[j].Sequence
	})

	for _, e := range sorted {
		b.WriteString(fmt.Sprintf("\n## %d. %s (%s) — %s\n",
			e.Sequence, e.Name, e.Category, e.Status))

		if e.Summary != "" {
			b.WriteString(e.Summary + "\n")
		}

		if len(e.Areas) > 0 {
			b.WriteString(fmt.Sprintf("- Areas: %s\n", strings.Join(e.Areas, ", ")))
		}

		b.WriteString(fmt.Sprintf("- Phases: %d/%d completed\n",
			e.CompletedPhases, e.TotalPhases))

		writeDateRange(&b, e)
		writeEnables(&b, e)
	}

	return b.String(), nil
}

// writeDateRange appends the date range line for an entry.
func writeDateRange(b *strings.Builder, e Entry) {
	if e.Created.IsZero() {
		return
	}
	start := e.Created.Format("Jan 2, 2006")
	if !e.Completed.IsZero() {
		end := e.Completed.Format("Jan 2, 2006")
		b.WriteString(fmt.Sprintf("- %s – %s\n", start, end))
	} else {
		b.WriteString(fmt.Sprintf("- Started: %s\n", start))
	}
}

// writeEnables appends the enables line if the entry has downstream deps.
func writeEnables(b *strings.Builder, e Entry) {
	if len(e.Enables) > 0 {
		b.WriteString(fmt.Sprintf("- Enables: %s\n", strings.Join(e.Enables, ", ")))
	}
}
