package relativity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// Scanner discovers nebulas from the filesystem and git history to produce a
// Spacetime catalog.
type Scanner struct {
	// Git provides access to repository history.
	Git GitQuerier

	// NebulasDir is the path to the .nebulas/ directory.
	NebulasDir string

	// Repo is the repository identifier stored in the catalog header.
	Repo string
}

// discoveredNebula holds intermediate data gathered during filesystem scanning.
type discoveredNebula struct {
	name            string
	dir             string
	manifest        nebula.Manifest
	phaseCount      int
	phaseTypes      map[string]int
	requiresNebulae []string
}

// Scan walks the nebulas directory, queries git for timeline data, and returns
// a fully populated Spacetime catalog.
func (s *Scanner) Scan(ctx context.Context) (*Spacetime, error) {
	discovered, err := s.discover()
	if err != nil {
		return nil, fmt.Errorf("discovering nebulas: %w", err)
	}

	entries := make([]Entry, 0, len(discovered))
	for _, d := range discovered {
		entry := s.deriveEntry(ctx, d)
		entries = append(entries, entry)
	}

	assignSequences(entries)
	inferRelationships(entries, discovered)

	return &Spacetime{
		Relativity: Header{
			Version:  1,
			LastScan: time.Now().UTC(),
			Repo:     s.Repo,
		},
		Nebulas: entries,
	}, nil
}

// discover walks .nebulas/*/nebula.toml and parses each manifest.
func (s *Scanner) discover() ([]discoveredNebula, error) {
	dirEntries, err := os.ReadDir(s.NebulasDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", s.NebulasDir, err)
	}

	var discovered []discoveredNebula
	for _, e := range dirEntries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(s.NebulasDir, e.Name())
		neb, loadErr := nebula.Load(dir)
		if loadErr != nil {
			continue // skip directories without valid nebula.toml
		}
		discovered = append(discovered, discoveredNebula{
			name:            e.Name(),
			dir:             dir,
			manifest:        neb.Manifest,
			phaseCount:      len(neb.Phases),
			phaseTypes:      countPhaseTypes(neb.Phases),
			requiresNebulae: neb.Manifest.Dependencies.RequiresNebulae,
		})
	}

	return discovered, nil
}

// countPhaseTypes tallies how many phases use each type string.
func countPhaseTypes(phases []nebula.PhaseSpec) map[string]int {
	counts := make(map[string]int)
	for _, p := range phases {
		if p.Type != "" {
			counts[p.Type]++
		}
	}
	return counts
}

// deriveEntry builds a catalog Entry from a discovered nebula, querying git
// for timeline data. Git errors are treated as non-fatal; the entry is returned
// with whatever data could be derived.
func (s *Scanner) deriveEntry(ctx context.Context, d discoveredNebula) Entry {
	entry := Entry{
		Name:        d.name,
		TotalPhases: d.phaseCount,
		Category:    dominantCategory(d.phaseTypes, d.manifest.Defaults.Type),
	}

	branch := "nebula/" + d.name
	exists, _ := s.Git.BranchExists(ctx, branch)

	if exists {
		entry.Branch = branch

		if created, err := s.Git.FirstCommitOnBranch(ctx, branch); err == nil {
			entry.Created = created
		}

		if completed, err := s.Git.MergeCommitToMain(ctx, branch); err == nil {
			entry.Status = StatusCompleted
			entry.Completed = completed
		}
	}

	// Fallback: first commit touching the nebula directory.
	if entry.Created.IsZero() {
		path := filepath.Join(".nebulas", d.name)
		if created, err := s.Git.FirstCommitTouching(ctx, path); err == nil {
			entry.Created = created
		}
	}

	// Derive status if not yet set.
	if entry.Status == "" {
		if exists {
			entry.Status = StatusInProgress
		} else {
			entry.Status = StatusPlanned
		}
	}

	// Derive areas touched for branches with commits.
	if exists {
		added, modified, err := s.Git.DiffPackages(ctx, "main", branch)
		if err == nil {
			entry.Areas = mergeStringSlices(added, modified)
			entry.PackagesAdded = added
			entry.PackagesModified = modified
		}
	}

	return entry
}

// assignSequences orders entries by creation date and assigns 1-based
// sequence numbers. Entries without a creation date sort last,
// broken by name for stability.
func assignSequences(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		ci, cj := entries[i].Created, entries[j].Created
		if ci.IsZero() && cj.IsZero() {
			return entries[i].Name < entries[j].Name
		}
		if ci.IsZero() {
			return false
		}
		if cj.IsZero() {
			return true
		}
		return ci.Before(cj)
	})
	for i := range entries {
		entries[i].Sequence = i + 1
	}
}

// inferRelationships populates Enables and BuildsOn fields based on
// requires_nebulae declarations in nebula manifests.
func inferRelationships(entries []Entry, discovered []discoveredNebula) {
	requires := make(map[string][]string)
	for _, d := range discovered {
		requires[d.name] = d.requiresNebulae
	}

	// For each nebula B that requires A, A enables B and B builds_on A.
	enablesMap := make(map[string][]string)
	buildsOnMap := make(map[string][]string)

	for name, deps := range requires {
		for _, dep := range deps {
			enablesMap[dep] = append(enablesMap[dep], name)
			buildsOnMap[name] = append(buildsOnMap[name], dep)
		}
	}

	for i := range entries {
		name := entries[i].Name
		if enables, ok := enablesMap[name]; ok {
			sort.Strings(enables)
			entries[i].Enables = enables
		}
		if buildsOn, ok := buildsOnMap[name]; ok {
			sort.Strings(buildsOn)
			entries[i].BuildsOn = buildsOn
		}
	}
}

// dominantCategory returns the catalog category based on the most common phase
// type. Falls back to the manifest's default type if no phases specify a type.
func dominantCategory(phaseTypes map[string]int, defaultType string) string {
	if len(phaseTypes) == 0 {
		return typeToCategory(defaultType)
	}
	maxCount := 0
	dominant := ""
	for t, count := range phaseTypes {
		if count > maxCount {
			maxCount = count
			dominant = t
		}
	}
	return typeToCategory(dominant)
}

// typeToCategory maps nebula phase types to catalog categories.
func typeToCategory(t string) string {
	switch strings.ToLower(t) {
	case "feature":
		return CategoryFeature
	case "bug":
		return CategoryBugfix
	case "task":
		return CategoryEnhancement
	default:
		return CategoryFeature
	}
}

// mergeStringSlices combines two string slices into a single sorted slice.
func mergeStringSlices(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	result := make([]string, 0, len(a)+len(b))
	result = append(result, a...)
	result = append(result, b...)
	sort.Strings(result)
	return result
}
