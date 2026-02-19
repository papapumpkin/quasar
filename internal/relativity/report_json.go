package relativity

import (
	"encoding/json"
	"fmt"
)

// JSONReport renders the full catalog as machine-readable JSON for external
// tooling or AI consumption.
type JSONReport struct{}

// jsonOutput is the top-level structure for JSON report output.
type jsonOutput struct {
	Version int         `json:"version"`
	Repo    string      `json:"repo"`
	Nebulas []jsonEntry `json:"nebulas"`
}

// jsonEntry is the JSON representation of a single nebula entry.
type jsonEntry struct {
	Name             string   `json:"name"`
	Sequence         int      `json:"sequence"`
	Status           string   `json:"status"`
	Category         string   `json:"category"`
	Created          string   `json:"created,omitempty"`
	Completed        string   `json:"completed,omitempty"`
	Branch           string   `json:"branch,omitempty"`
	Areas            []string `json:"areas"`
	PackagesAdded    []string `json:"packages_added"`
	PackagesModified []string `json:"packages_modified"`
	TotalPhases      int      `json:"total_phases"`
	CompletedPhases  int      `json:"completed_phases"`
	Enables          []string `json:"enables"`
	BuildsOn         []string `json:"builds_on"`
	Summary          string   `json:"summary,omitempty"`
	Lessons          []string `json:"lessons,omitempty"`
}

// Render produces a JSON string of the full catalog.
func (r *JSONReport) Render(catalog *Spacetime) (string, error) {
	if catalog == nil {
		return "", fmt.Errorf("catalog is nil")
	}

	out := jsonOutput{
		Version: catalog.Relativity.Version,
		Repo:    catalog.Relativity.Repo,
		Nebulas: make([]jsonEntry, len(catalog.Nebulas)),
	}

	for i, e := range catalog.Nebulas {
		je := jsonEntry{
			Name:             e.Name,
			Sequence:         e.Sequence,
			Status:           e.Status,
			Category:         e.Category,
			Branch:           e.Branch,
			Areas:            emptyIfNil(e.Areas),
			PackagesAdded:    emptyIfNil(e.PackagesAdded),
			PackagesModified: emptyIfNil(e.PackagesModified),
			TotalPhases:      e.TotalPhases,
			CompletedPhases:  e.CompletedPhases,
			Enables:          emptyIfNil(e.Enables),
			BuildsOn:         emptyIfNil(e.BuildsOn),
			Summary:          e.Summary,
			Lessons:          e.Lessons,
		}
		if !e.Created.IsZero() {
			je.Created = e.Created.Format("2006-01-02")
		}
		if !e.Completed.IsZero() {
			je.Completed = e.Completed.Format("2006-01-02")
		}
		out.Nebulas[i] = je
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling JSON report: %w", err)
	}

	return string(data) + "\n", nil
}

// emptyIfNil returns an empty slice if the input is nil, ensuring JSON
// arrays are rendered as [] instead of null.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
