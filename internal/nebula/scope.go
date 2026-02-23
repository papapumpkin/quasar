package nebula

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/papapumpkin/quasar/internal/dag"
)

// validateScopeOverlaps checks that parallel phases (not connected by
// dependencies) do not declare overlapping file scopes.
func validateScopeOverlaps(phases []PhaseSpec, d *dag.DAG) []ValidationError {
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

	// Check each unordered pair of scoped phases.
	for i := 0; i < len(scoped); i++ {
		for j := i + 1; j < len(scoped); j++ {
			a, b := scoped[i], scoped[j]

			// Serialized by dependency — no conflict possible.
			if d.Connected(a.ID, b.ID) {
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
