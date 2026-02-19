package relativity

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// DefaultPath is the conventional location for the spacetime catalog.
const DefaultPath = ".relativity/spacetime.toml"

// Load reads a spacetime catalog from the given path. If the file does not
// exist, it returns a zero-value Spacetime and no error, allowing callers to
// proceed with an empty catalog.
func Load(path string) (*Spacetime, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Spacetime{}, nil
		}
		return nil, fmt.Errorf("reading spacetime.toml: %w", err)
	}

	var st Spacetime
	if err := toml.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parsing spacetime.toml: %w", err)
	}
	return &st, nil
}

// Save writes the spacetime catalog to the given path, creating parent
// directories as needed.
func Save(path string, st *Spacetime) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	data, err := toml.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshaling spacetime.toml: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing spacetime.toml: %w", err)
	}
	return nil
}

// Merge combines auto-derived scan data with an existing catalog, preserving
// manual annotations. For each entry in scanned, if a matching entry (by name)
// exists in existing, the manual fields (Summary, Lessons) are carried forward.
// Auto-derived fields from scanned always take precedence. Entries present in
// existing but absent from scanned are marked as abandoned rather than deleted.
func Merge(existing, scanned *Spacetime) *Spacetime {
	manual := make(map[string]*Entry, len(existing.Nebulas))
	for i := range existing.Nebulas {
		manual[existing.Nebulas[i].Name] = &existing.Nebulas[i]
	}

	merged := &Spacetime{
		Relativity: scanned.Relativity,
		Nebulas:    make([]Entry, 0, len(scanned.Nebulas)+len(existing.Nebulas)),
	}

	seen := make(map[string]bool, len(scanned.Nebulas))
	for _, entry := range scanned.Nebulas {
		seen[entry.Name] = true
		if prev, ok := manual[entry.Name]; ok {
			// Preserve manual enrichment fields.
			if entry.Summary == "" {
				entry.Summary = prev.Summary
			}
			if len(entry.Lessons) == 0 {
				entry.Lessons = prev.Lessons
			}
			// Preserve manually-set relationships if scan didn't produce them.
			if len(entry.Enables) == 0 {
				entry.Enables = prev.Enables
			}
			if len(entry.BuildsOn) == 0 {
				entry.BuildsOn = prev.BuildsOn
			}
		}
		merged.Nebulas = append(merged.Nebulas, entry)
	}

	// Mark entries not in the scan as abandoned.
	for _, entry := range existing.Nebulas {
		if !seen[entry.Name] {
			entry.Status = StatusAbandoned
			merged.Nebulas = append(merged.Nebulas, entry)
		}
	}

	return merged
}
