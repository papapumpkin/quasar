package relativity

import "fmt"

// ReportFormat defines how catalog data is rendered into a human- or
// machine-readable string.
type ReportFormat interface {
	// Render produces the full report content from the catalog.
	Render(catalog *Spacetime) (string, error)
}

// FormatByName returns the ReportFormat implementation for the given name.
// Supported names: timeline, heatmap, graph, json, onboarding.
func FormatByName(name string) (ReportFormat, error) {
	switch name {
	case "timeline":
		return &TimelineReport{}, nil
	case "heatmap":
		return &HeatmapReport{}, nil
	case "graph":
		return &GraphReport{}, nil
	case "json":
		return &JSONReport{}, nil
	case "onboarding":
		return &OnboardingReport{}, nil
	default:
		return nil, fmt.Errorf("unknown report format: %q", name)
	}
}

// FormatNames returns the list of all supported report format names.
func FormatNames() []string {
	return []string{"timeline", "heatmap", "graph", "json", "onboarding"}
}
