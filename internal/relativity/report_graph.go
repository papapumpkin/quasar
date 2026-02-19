package relativity

import (
	"fmt"
	"sort"
	"strings"
)

// GraphReport renders an ASCII representation of how nebulas relate to each
// other via their enables/builds_on relationships.
type GraphReport struct{}

// Render produces a markdown dependency graph of nebulas.
func (r *GraphReport) Render(catalog *Spacetime) (string, error) {
	if catalog == nil {
		return "", fmt.Errorf("catalog is nil")
	}

	var b strings.Builder

	b.WriteString("# Nebula Dependency Graph\n")

	if len(catalog.Nebulas) == 0 {
		b.WriteString("\nNo nebulas recorded yet.\n")
		return b.String(), nil
	}

	// Sort by sequence for stable output.
	sorted := make([]Entry, len(catalog.Nebulas))
	copy(sorted, catalog.Nebulas)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Sequence < sorted[j].Sequence
	})

	b.WriteString("\n")

	for _, e := range sorted {
		if len(e.Enables) > 0 {
			for _, target := range e.Enables {
				b.WriteString(fmt.Sprintf("%s → %s\n", e.Name, target))
			}
		} else if len(e.BuildsOn) > 0 {
			// Show reverse edges if no enables but has builds_on.
			for _, dep := range e.BuildsOn {
				b.WriteString(fmt.Sprintf("%s ← %s\n", e.Name, dep))
			}
		} else {
			b.WriteString(fmt.Sprintf("%s (standalone)\n", e.Name))
		}
	}

	return b.String(), nil
}
