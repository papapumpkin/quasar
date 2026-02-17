package nebula

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Validate checks a nebula for structural correctness:
// required fields, unique IDs, valid dependencies, no cycles.
func Validate(n *Nebula) []ValidationError {
	var errs []ValidationError

	if n.Manifest.Nebula.Name == "" {
		errs = append(errs, ValidationError{
			SourceFile: "nebula.toml",
			Field:      "nebula.name",
			Err:        fmt.Errorf("%w: nebula.name", ErrMissingField),
		})
	}

	seen := make(map[string]string) // id → source file
	ids := make(map[string]bool)

	for _, p := range n.Phases {
		// Required fields.
		if p.ID == "" {
			errs = append(errs, ValidationError{
				SourceFile: p.SourceFile,
				Field:      "id",
				Err:        fmt.Errorf("%w: id", ErrMissingField),
			})
			continue
		}
		if p.Title == "" {
			errs = append(errs, ValidationError{
				PhaseID:    p.ID,
				SourceFile: p.SourceFile,
				Field:      "title",
				Err:        fmt.Errorf("%w: title", ErrMissingField),
			})
		}

		// Duplicate IDs.
		if prev, ok := seen[p.ID]; ok {
			errs = append(errs, ValidationError{
				PhaseID:    p.ID,
				SourceFile: p.SourceFile,
				Err:        fmt.Errorf("%w: %q already defined in %s", ErrDuplicateID, p.ID, prev),
			})
		}
		seen[p.ID] = p.SourceFile
		ids[p.ID] = true
	}

	// Validate dependencies reference known IDs.
	for _, p := range n.Phases {
		for _, dep := range p.DependsOn {
			if !ids[dep] {
				errs = append(errs, ValidationError{
					PhaseID:    p.ID,
					SourceFile: p.SourceFile,
					Field:      "depends_on",
					Err:        fmt.Errorf("%w: %q depends on unknown phase %q", ErrUnknownDep, p.ID, dep),
				})
			}
		}
	}

	// Validate execution bounds.
	exec := n.Manifest.Execution
	if exec.MaxReviewCycles < 0 {
		errs = append(errs, ValidationError{
			SourceFile: "nebula.toml",
			Field:      "execution.max_review_cycles",
			Err:        fmt.Errorf("execution.max_review_cycles must be >= 0, got %d", exec.MaxReviewCycles),
		})
	}
	if exec.MaxBudgetUSD < 0 {
		errs = append(errs, ValidationError{
			SourceFile: "nebula.toml",
			Field:      "execution.max_budget_usd",
			Err:        fmt.Errorf("execution.max_budget_usd must be >= 0, got %f", exec.MaxBudgetUSD),
		})
	}

	// Validate manifest gate mode.
	if exec.Gate != "" && !ValidGateModes[exec.Gate] {
		errs = append(errs, ValidationError{
			SourceFile: "nebula.toml",
			Field:      "execution.gate",
			Err:        fmt.Errorf("%w: %q", ErrInvalidGate, exec.Gate),
		})
	}

	// Validate per-phase execution overrides.
	for _, p := range n.Phases {
		if p.MaxReviewCycles < 0 {
			errs = append(errs, ValidationError{
				PhaseID:    p.ID,
				SourceFile: p.SourceFile,
				Field:      "max_review_cycles",
				Err:        fmt.Errorf("max_review_cycles must be >= 0, got %d", p.MaxReviewCycles),
			})
		}
		if p.MaxBudgetUSD < 0 {
			errs = append(errs, ValidationError{
				PhaseID:    p.ID,
				SourceFile: p.SourceFile,
				Field:      "max_budget_usd",
				Err:        fmt.Errorf("max_budget_usd must be >= 0, got %f", p.MaxBudgetUSD),
			})
		}
		if p.Gate != "" && !ValidGateModes[p.Gate] {
			errs = append(errs, ValidationError{
				PhaseID:    p.ID,
				SourceFile: p.SourceFile,
				Field:      "gate",
				Err:        fmt.Errorf("%w: %q", ErrInvalidGate, p.Gate),
			})
		}
	}

	// Validate dependency entries are non-empty strings.
	for _, dep := range n.Manifest.Dependencies.RequiresBeads {
		if dep == "" {
			errs = append(errs, ValidationError{
				SourceFile: "nebula.toml",
				Field:      "dependencies.requires_beads",
				Err:        fmt.Errorf("requires_beads entries must be non-empty strings"),
			})
		}
	}
	for _, dep := range n.Manifest.Dependencies.RequiresNebulae {
		if dep == "" {
			errs = append(errs, ValidationError{
				SourceFile: "nebula.toml",
				Field:      "dependencies.requires_nebulae",
				Err:        fmt.Errorf("requires_nebulae entries must be non-empty strings"),
			})
		}
	}

	// Cycle detection via topological sort.
	if len(errs) == 0 {
		g := NewGraph(n.Phases)
		if _, err := g.Sort(); err != nil {
			errs = append(errs, ValidationError{
				SourceFile: "nebula.toml",
				Err:        err,
			})
		}
	}

	// Scope overlap detection for parallel phases.
	if len(errs) == 0 {
		errs = append(errs, validateScopeOverlaps(n.Phases)...)
	}

	return errs
}

// ValidateHotAdd checks whether a new phase can be safely inserted into a
// running nebula. It validates required fields, ID uniqueness against the
// existing registry, and cycle detection against the live graph.
// On success, the phase's nodes and edges (including blocks edges) remain in
// the graph. On failure, they are rolled back. The caller is responsible for
// removing any blocks edges that should not be kept (e.g. for in-flight or
// done phases).
func ValidateHotAdd(phase PhaseSpec, existingIDs map[string]bool, graph *Graph) []ValidationError {
	var errs []ValidationError

	if phase.ID == "" {
		errs = append(errs, ValidationError{
			SourceFile: phase.SourceFile,
			Field:      "id",
			Err:        fmt.Errorf("%w: id", ErrMissingField),
		})
		return errs
	}
	if phase.Title == "" {
		errs = append(errs, ValidationError{
			PhaseID:    phase.ID,
			SourceFile: phase.SourceFile,
			Field:      "title",
			Err:        fmt.Errorf("%w: title", ErrMissingField),
		})
	}
	if existingIDs[phase.ID] {
		errs = append(errs, ValidationError{
			PhaseID:    phase.ID,
			SourceFile: phase.SourceFile,
			Err:        fmt.Errorf("%w: %q", ErrDuplicateID, phase.ID),
		})
	}
	if len(errs) > 0 {
		return errs
	}

	// Tentatively add the node and edges to check for cycles.
	graph.AddNode(phase.ID)
	for _, dep := range phase.DependsOn {
		graph.AddEdge(phase.ID, dep)
	}
	for _, blocked := range phase.Blocks {
		graph.AddEdge(blocked, phase.ID)
	}

	if _, err := graph.Sort(); err != nil {
		errs = append(errs, ValidationError{
			PhaseID:    phase.ID,
			SourceFile: phase.SourceFile,
			Err:        fmt.Errorf("%w: adding %q would create a cycle", ErrDependencyCycle, phase.ID),
		})
		// Roll back the tentative graph mutations.
		rollbackHotAdd(graph, phase)
	}

	return errs
}

// rollbackHotAdd removes a phase node and its edges from the graph.
func rollbackHotAdd(graph *Graph, phase PhaseSpec) {
	for _, dep := range phase.DependsOn {
		delete(graph.adjacency[phase.ID], dep)
		if graph.reverse[dep] != nil {
			delete(graph.reverse[dep], phase.ID)
		}
	}
	for _, blocked := range phase.Blocks {
		delete(graph.adjacency[blocked], phase.ID)
		if graph.reverse[phase.ID] != nil {
			delete(graph.reverse[phase.ID], blocked)
		}
	}
	delete(graph.adjacency, phase.ID)
	delete(graph.reverse, phase.ID)
}

// validateScopeOverlaps checks that parallel phases (not connected by
// dependencies) do not declare overlapping file scopes.
func validateScopeOverlaps(phases []PhaseSpec) []ValidationError {
	var errs []ValidationError

	// Collect only phases with non-empty scopes.
	var scoped []PhaseSpec
	for _, p := range phases {
		if len(p.Scope) > 0 {
			scoped = append(scoped, p)
		}
	}
	if len(scoped) < 2 {
		return nil
	}

	g := NewGraph(phases)

	// Check each unordered pair of scoped phases.
	for i := 0; i < len(scoped); i++ {
		for j := i + 1; j < len(scoped); j++ {
			a, b := scoped[i], scoped[j]

			// Serialized by dependency — no conflict possible.
			if g.Connected(a.ID, b.ID) {
				continue
			}

			// Either phase opts out of overlap checking.
			if a.AllowScopeOverlap || b.AllowScopeOverlap {
				continue
			}

			if patA, patB, overlaps := scopesOverlap(a.Scope, b.Scope); overlaps {
				pattern := patA
				if patA != patB {
					pattern = patA + " / " + patB
				}
				errs = append(errs, ValidationError{
					PhaseID:    a.ID,
					SourceFile: a.SourceFile,
					Field:      "scope",
					Err: fmt.Errorf(
						"%w: phases %q and %q both match %q; add a dependency or narrow scopes",
						ErrScopeOverlap, a.ID, b.ID, pattern),
				})
			}
		}
	}

	return errs
}

// scopesOverlap reports whether any pattern in a overlaps with any pattern in b.
// It returns the first overlapping pair and true, or empty strings and false.
func scopesOverlap(a, b []string) (string, string, bool) {
	for _, pa := range a {
		for _, pb := range b {
			if patternsOverlap(pa, pb) {
				return pa, pb, true
			}
		}
	}
	return "", "", false
}

// patternsOverlap reports whether two scope patterns refer to overlapping
// file regions. It handles directory containment, glob patterns, and exact
// matches.
func patternsOverlap(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)

	// Exact match after cleaning.
	if ca == cb {
		return true
	}

	// Directory containment: one is a prefix of the other.
	if dirContains(ca, cb) || dirContains(cb, ca) {
		return true
	}

	// Glob overlap: try matching each pattern against the other's
	// directory prefix. For ** patterns, compare directory prefixes.
	if isGlob(ca) || isGlob(cb) {
		return globsOverlap(ca, cb)
	}

	return false
}

// dirContains reports whether directory parent contains child as a sub-path.
func dirContains(parent, child string) bool {
	// Ensure parent ends with separator for proper prefix matching.
	p := parent
	if !strings.HasSuffix(p, string(filepath.Separator)) {
		p += string(filepath.Separator)
	}
	return strings.HasPrefix(child, p)
}

// isGlob reports whether the pattern contains glob metacharacters.
func isGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// globsOverlap checks whether two patterns (at least one a glob) can match
// overlapping file sets.
func globsOverlap(a, b string) bool {
	// For ** patterns, strip the glob suffix and compare directory prefixes.
	if strings.Contains(a, "**") || strings.Contains(b, "**") {
		da := globDirPrefix(a)
		db := globDirPrefix(b)
		// If either directory prefix contains the other, they can overlap.
		if da == db || dirContains(da, db) || dirContains(db, da) {
			return true
		}
	}

	// Try filepath.Match in both directions — a literal might match a glob.
	if matchedAB, _ := filepath.Match(a, b); matchedAB {
		return true
	}
	if matchedBA, _ := filepath.Match(b, a); matchedBA {
		return true
	}

	// Compare directory prefixes of single-* globs for containment.
	// When both are single-* globs in the same directory, cross-match
	// to avoid false positives (e.g., internal/*.go vs internal/*.ts).
	if strings.Contains(a, "*") || strings.Contains(b, "*") {
		da := globDirPrefix(a)
		db := globDirPrefix(b)
		if da == db {
			// Same directory: check if patterns can co-match.
			// Use the glob suffix of each as a representative to
			// match against the other pattern.
			return globSuffixesOverlap(a, b)
		}
		if dirContains(da, db) || dirContains(db, da) {
			return true
		}
	}

	return false
}

// globDirPrefix extracts the directory portion before any glob metacharacter.
func globDirPrefix(pattern string) string {
	// Find the first metacharacter.
	idx := strings.IndexAny(pattern, "*?[")
	if idx < 0 {
		return pattern
	}
	prefix := pattern[:idx]
	// Trim to last separator to get a clean directory.
	if i := strings.LastIndex(prefix, string(filepath.Separator)); i >= 0 {
		return prefix[:i]
	}
	return "."
}

// globSuffixesOverlap reports whether two glob patterns in the same directory
// can match overlapping files. It constructs a representative filename from
// each pattern's glob suffix and checks if it matches the other pattern.
// For example, "internal/*.go" and "internal/*.ts" do not overlap because
// "x.go" does not match "*.ts" and "x.ts" does not match "*.go".
func globSuffixesOverlap(a, b string) bool {
	repA := globRepresentative(a)
	repB := globRepresentative(b)

	// If we can't derive a representative, conservatively report overlap.
	if repA == "" || repB == "" {
		return true
	}

	// Check if a representative of A matches pattern B, or vice versa.
	if m, _ := filepath.Match(b, repA); m {
		return true
	}
	if m, _ := filepath.Match(a, repB); m {
		return true
	}
	return false
}

// globRepresentative builds a concrete filename that would match the given
// glob pattern by replacing each '*' with a fixed placeholder. Returns ""
// if the pattern uses '?' or '[' metacharacters that are hard to invert.
func globRepresentative(pattern string) string {
	if strings.ContainsAny(pattern, "?[") {
		return ""
	}
	return strings.ReplaceAll(pattern, "*", "x")
}
