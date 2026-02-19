package relativity

import (
	"fmt"
	"sort"
	"strings"
)

// OnboardingReport renders a prose summary designed to be pasted into an AI
// agent's context window for rapid codebase onboarding.
type OnboardingReport struct{}

// Render produces a coherent prose onboarding brief from the catalog.
func (r *OnboardingReport) Render(catalog *Spacetime) (string, error) {
	if catalog == nil {
		return "", fmt.Errorf("catalog is nil")
	}

	var b strings.Builder

	repo := catalog.Relativity.Repo
	if repo == "" {
		repo = "this project"
	}

	b.WriteString(fmt.Sprintf("# Project Onboarding: %s\n\n", repo))

	if len(catalog.Nebulas) == 0 {
		b.WriteString("This project has no recorded nebulas yet. ")
		b.WriteString("The codebase evolution history will appear here once nebulas are scanned.\n")
		return b.String(), nil
	}

	// Sort by sequence.
	sorted := make([]Entry, len(catalog.Nebulas))
	copy(sorted, catalog.Nebulas)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Sequence < sorted[j].Sequence
	})

	// Count statuses.
	counts := countStatuses(sorted)

	// Opening summary.
	b.WriteString(fmt.Sprintf("This project has evolved through %d nebula", len(sorted)))
	if len(sorted) != 1 {
		b.WriteString("s")
	}
	b.WriteString(formatStatusCounts(counts))
	b.WriteString(".\n\n")

	// Narrative of evolution.
	writeEvolutionNarrative(&b, sorted)

	// Area history.
	writeAreaHistory(&b, sorted)

	// Active work.
	writeActiveWork(&b, sorted)

	return b.String(), nil
}

// statusCounts holds the count of nebulas in each status.
type statusCounts struct {
	completed  int
	inProgress int
	planned    int
	abandoned  int
}

// countStatuses tallies the nebulas by status.
func countStatuses(entries []Entry) statusCounts {
	var c statusCounts
	for _, e := range entries {
		switch e.Status {
		case StatusCompleted:
			c.completed++
		case StatusInProgress:
			c.inProgress++
		case StatusPlanned:
			c.planned++
		case StatusAbandoned:
			c.abandoned++
		}
	}
	return c
}

// formatStatusCounts returns a parenthetical like " (2 completed, 1 planned)".
func formatStatusCounts(c statusCounts) string {
	var parts []string
	if c.completed > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", c.completed))
	}
	if c.inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", c.inProgress))
	}
	if c.planned > 0 {
		parts = append(parts, fmt.Sprintf("%d planned", c.planned))
	}
	if c.abandoned > 0 {
		parts = append(parts, fmt.Sprintf("%d abandoned", c.abandoned))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// writeEvolutionNarrative writes a prose paragraph describing the project's
// evolution through nebulas.
func writeEvolutionNarrative(b *strings.Builder, entries []Entry) {
	for i, e := range entries {
		if i == 0 {
			b.WriteString("The codebase started with ")
		} else if i == len(entries)-1 {
			b.WriteString("Most recently, ")
		} else {
			b.WriteString("Then, ")
		}

		desc := e.Summary
		if desc == "" {
			desc = fmt.Sprintf("the %s %s", e.Name, e.Category)
		}

		b.WriteString(fmt.Sprintf("%s (%s)", desc, e.Name))

		switch e.Status {
		case StatusCompleted:
			b.WriteString(", which is now completed")
		case StatusInProgress:
			b.WriteString(", which is currently in progress")
		case StatusPlanned:
			b.WriteString(", which is planned")
		case StatusAbandoned:
			b.WriteString(", which was abandoned")
		}

		b.WriteString(". ")
	}

	b.WriteString("\n\n")
}

// writeAreaHistory writes the key areas section of the onboarding brief.
func writeAreaHistory(b *strings.Builder, entries []Entry) {
	// Collect areas and which nebulas touch them.
	areaMap := make(map[string][]string)
	for _, e := range entries {
		for _, area := range e.Areas {
			areaMap[area] = append(areaMap[area], e.Name)
		}
	}

	if len(areaMap) == 0 {
		return
	}

	// Sort areas for stable output.
	areas := make([]string, 0, len(areaMap))
	for a := range areaMap {
		areas = append(areas, a)
	}
	sort.Strings(areas)

	b.WriteString("Key areas and their history:\n")
	for _, area := range areas {
		nebulas := areaMap[area]
		if len(nebulas) == 1 {
			b.WriteString(fmt.Sprintf("- %s: touched in %s\n", area, nebulas[0]))
		} else {
			b.WriteString(fmt.Sprintf("- %s: evolved across %s\n",
				area, strings.Join(nebulas, ", ")))
		}
	}

	b.WriteString("\n")
}

// writeActiveWork writes about in-progress and planned nebulas.
func writeActiveWork(b *strings.Builder, entries []Entry) {
	var active []Entry
	for _, e := range entries {
		if e.Status == StatusInProgress || e.Status == StatusPlanned {
			active = append(active, e)
		}
	}

	if len(active) == 0 {
		b.WriteString("All nebulas are completed. No active work is tracked.\n")
		return
	}

	b.WriteString("Active work:\n")
	for _, e := range active {
		status := e.Status
		if status == StatusInProgress {
			status = "in progress"
		}
		detail := ""
		if e.TotalPhases > 0 {
			detail = fmt.Sprintf(" with %d phases (%d completed)",
				e.TotalPhases, e.CompletedPhases)
		}
		b.WriteString(fmt.Sprintf("- %s is %s%s\n", e.Name, status, detail))
	}
}
