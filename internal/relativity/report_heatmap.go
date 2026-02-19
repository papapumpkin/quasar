package relativity

import (
	"fmt"
	"sort"
	"strings"
)

// HeatmapReport renders a table showing which packages have been most active
// and through which nebulas.
type HeatmapReport struct{}

// areaInfo tracks aggregated data about a single codebase area.
type areaInfo struct {
	name       string
	nebulas    []string
	lastNebula string
	categories map[string]bool
}

// Render produces a markdown table of area activity across nebulas.
func (r *HeatmapReport) Render(catalog *Spacetime) (string, error) {
	if catalog == nil {
		return "", fmt.Errorf("catalog is nil")
	}

	var b strings.Builder

	b.WriteString("# Codebase Area Heatmap\n")

	if len(catalog.Nebulas) == 0 {
		b.WriteString("\nNo nebulas recorded yet.\n")
		return b.String(), nil
	}

	// Aggregate area data across all nebulas.
	areas := aggregateAreas(catalog.Nebulas)

	if len(areas) == 0 {
		b.WriteString("\nNo areas recorded in any nebula.\n")
		return b.String(), nil
	}

	// Sort by number of touching nebulas (descending), then by name.
	sort.Slice(areas, func(i, j int) bool {
		if len(areas[i].nebulas) != len(areas[j].nebulas) {
			return len(areas[i].nebulas) > len(areas[j].nebulas)
		}
		return areas[i].name < areas[j].name
	})

	b.WriteString("\n| Package | Nebulas Touching | Last Changed | Category Mix |\n")
	b.WriteString("|---------|-----------------|--------------|--------------|\n")

	for _, a := range areas {
		catMix := categoryMix(a.categories)
		nebulaList := strings.Join(a.nebulas, ", ")
		b.WriteString(fmt.Sprintf("| %s | %d (%s) | %s | %s |\n",
			a.name, len(a.nebulas), nebulaList, a.lastNebula, catMix))
	}

	return b.String(), nil
}

// aggregateAreas builds a list of areaInfo from all nebula entries.
func aggregateAreas(entries []Entry) []areaInfo {
	// Sort entries by sequence so "last nebula" is accurate.
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Sequence < sorted[j].Sequence
	})

	index := make(map[string]*areaInfo)
	for _, e := range sorted {
		for _, area := range e.Areas {
			info, ok := index[area]
			if !ok {
				info = &areaInfo{
					name:       area,
					categories: make(map[string]bool),
				}
				index[area] = info
			}
			info.nebulas = append(info.nebulas, e.Name)
			info.lastNebula = e.Name
			if e.Category != "" {
				info.categories[e.Category] = true
			}
		}
	}

	result := make([]areaInfo, 0, len(index))
	for _, info := range index {
		result = append(result, *info)
	}
	return result
}

// categoryMix returns a human-readable summary of the categories present.
func categoryMix(cats map[string]bool) string {
	if len(cats) == 0 {
		return "—"
	}
	if len(cats) == 1 {
		for k := range cats {
			return k
		}
	}
	names := make([]string, 0, len(cats))
	for k := range cats {
		names = append(names, k)
	}
	sort.Strings(names)
	return "mixed (" + strings.Join(names, ", ") + ")"
}
