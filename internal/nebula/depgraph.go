package nebula

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/papapumpkin/quasar/internal/dag"
)

// DependencyInferrer analyzes a set of phases and infers missing dependency
// edges based on scope overlap, file ownership, and ordering heuristics.
type DependencyInferrer struct {
	Phases []PhaseSpec
}

// InferenceResult contains the corrected phases and any warnings about
// dependency adjustments that were made.
type InferenceResult struct {
	Phases   []PhaseSpec // Phases with corrected DependsOn fields
	Added    []DepEdge   // Edges that were added by inference
	Warnings []string    // Human-readable warnings about the corrections
}

// DepEdge represents a single dependency edge between two phases.
type DepEdge struct {
	From   string // Phase ID that depends on another
	To     string // Phase ID that is depended upon
	Reason string // Why this edge was inferred
}

// InferDependencies analyzes scope overlaps, file ownership patterns, and
// explicit depends_on declarations to produce a corrected dependency graph.
// It returns an error if the resulting graph contains cycles that cannot
// be resolved.
func (d *DependencyInferrer) InferDependencies() (*InferenceResult, error) {
	phases := clonePhases(d.Phases)
	byID := indexPhases(phases)
	var added []DepEdge
	var warnings []string

	// Step 1: Expand blocks into depends_on entries on target phases.
	blockEdges := expandBlocks(phases, byID)
	added = append(added, blockEdges...)

	// Step 2: Add edges for scope overlaps between unordered phases.
	overlapEdges := inferScopeOverlapEdges(phases)
	added = append(added, overlapEdges...)
	applyEdges(phases, byID, overlapEdges)

	// Step 3: Add edges when a phase body mentions files owned by another phase's scope.
	mentionEdges := inferFileMentionEdges(phases)
	added = append(added, mentionEdges...)
	applyEdges(phases, byID, mentionEdges)

	// Step 4: Verify the result is a DAG (no cycles).
	d2, err := phasesToDAG(phases)
	if err != nil {
		return nil, fmt.Errorf("dependency inference produced a cycle: %w", err)
	}

	// Step 5: Transitive reduction to remove redundant edges.
	reductionCount := transitiveReduction(phases, d2)
	if reductionCount > 0 {
		warnings = append(warnings, fmt.Sprintf("removed %d redundant transitive edge(s)", reductionCount))
	}

	return &InferenceResult{
		Phases:   phases,
		Added:    added,
		Warnings: warnings,
	}, nil
}

// clonePhases returns a deep copy of the phase slice, including
// DependsOn slices so mutations don't affect the originals.
func clonePhases(phases []PhaseSpec) []PhaseSpec {
	out := make([]PhaseSpec, len(phases))
	for i, p := range phases {
		out[i] = p
		if len(p.DependsOn) > 0 {
			out[i].DependsOn = make([]string, len(p.DependsOn))
			copy(out[i].DependsOn, p.DependsOn)
		}
		if len(p.Scope) > 0 {
			out[i].Scope = make([]string, len(p.Scope))
			copy(out[i].Scope, p.Scope)
		}
		if len(p.Blocks) > 0 {
			out[i].Blocks = make([]string, len(p.Blocks))
			copy(out[i].Blocks, p.Blocks)
		}
	}
	return out
}

// indexPhases builds a map from phase ID to its index in the slice.
func indexPhases(phases []PhaseSpec) map[string]int {
	m := make(map[string]int, len(phases))
	for i, p := range phases {
		m[p.ID] = i
	}
	return m
}

// expandBlocks converts Blocks fields into DependsOn entries on the target
// phases. For each phase A with blocks = ["B"], phase B gets A added to its
// DependsOn.
func expandBlocks(phases []PhaseSpec, byID map[string]int) []DepEdge {
	var added []DepEdge
	for _, p := range phases {
		for _, target := range p.Blocks {
			idx, ok := byID[target]
			if !ok {
				continue
			}
			if hasDep(phases[idx].DependsOn, p.ID) {
				continue
			}
			phases[idx].DependsOn = append(phases[idx].DependsOn, p.ID)
			added = append(added, DepEdge{
				From:   target,
				To:     p.ID,
				Reason: fmt.Sprintf("phase %q declares blocks = [%q]", p.ID, target),
			})
		}
	}
	return added
}

// inferScopeOverlapEdges detects pairs of phases with overlapping scopes
// (where neither has AllowScopeOverlap set) and adds a dependency edge from
// the later phase to the earlier one (by slice position).
func inferScopeOverlapEdges(phases []PhaseSpec) []DepEdge {
	var edges []DepEdge
	for i := 0; i < len(phases); i++ {
		if len(phases[i].Scope) == 0 {
			continue
		}
		for j := i + 1; j < len(phases); j++ {
			if len(phases[j].Scope) == 0 {
				continue
			}
			// Skip if either opts out.
			if phases[i].AllowScopeOverlap || phases[j].AllowScopeOverlap {
				continue
			}
			// Skip if already ordered by an existing dependency.
			if hasDep(phases[j].DependsOn, phases[i].ID) || hasDep(phases[i].DependsOn, phases[j].ID) {
				continue
			}
			if _, _, overlaps := scopesOverlap(phases[i].Scope, phases[j].Scope); overlaps {
				edges = append(edges, DepEdge{
					From:   phases[j].ID,
					To:     phases[i].ID,
					Reason: fmt.Sprintf("scope overlap between phases %q and %q", phases[i].ID, phases[j].ID),
				})
			}
		}
	}
	return edges
}

// filesMentionRegex matches lines in a ## Files section that start with a
// markdown list marker followed by a backtick-quoted path.
var filesMentionRegex = regexp.MustCompile("(?m)^\\s*[-*]\\s+`([^`]+)`")

// extractFileMentions parses a phase body's ## Files section and returns
// the file paths mentioned.
func extractFileMentions(body string) []string {
	// Find the ## Files section.
	idx := strings.Index(body, "## Files")
	if idx < 0 {
		return nil
	}
	section := body[idx:]

	// The section extends until the next ## heading or end of body.
	if nextH2 := strings.Index(section[len("## Files"):], "\n## "); nextH2 >= 0 {
		section = section[:len("## Files")+nextH2]
	}

	matches := filesMentionRegex.FindAllStringSubmatch(section, -1)
	var paths []string
	for _, m := range matches {
		paths = append(paths, m[1])
	}
	return paths
}

// inferFileMentionEdges parses each phase's body for file mentions and
// cross-references them against other phases' scopes. If phase B mentions
// a file owned by phase A's scope, B depends on A.
func inferFileMentionEdges(phases []PhaseSpec) []DepEdge {
	var edges []DepEdge
	for i := range phases {
		mentions := extractFileMentions(phases[i].Body)
		if len(mentions) == 0 {
			continue
		}
		for j := range phases {
			if i == j {
				continue
			}
			if len(phases[j].Scope) == 0 {
				continue
			}
			// Skip if already depends on.
			if hasDep(phases[i].DependsOn, phases[j].ID) {
				continue
			}
			for _, mention := range mentions {
				if _, _, overlaps := scopesOverlap([]string{mention}, phases[j].Scope); overlaps {
					edges = append(edges, DepEdge{
						From:   phases[i].ID,
						To:     phases[j].ID,
						Reason: fmt.Sprintf("phase %q mentions file %q owned by phase %q scope", phases[i].ID, mention, phases[j].ID),
					})
					break // One edge per pair is sufficient.
				}
			}
		}
	}
	return edges
}

// transitiveReduction removes redundant edges from the dependency graph.
// An edge A→B is redundant if there exists an alternative path A→...→B
// through other dependencies. Returns the number of edges removed.
func transitiveReduction(phases []PhaseSpec, d *dag.DAG) int {
	removed := 0
	for i := range phases {
		if len(phases[i].DependsOn) < 2 {
			continue
		}
		var keep []string
		for _, dep := range phases[i].DependsOn {
			redundant := false
			for _, other := range phases[i].DependsOn {
				if other == dep {
					continue
				}
				// If 'other' has a path to 'dep', then the edge phases[i]→dep
				// is redundant because phases[i]→other→...→dep already exists.
				if d.HasPath(other, dep) {
					redundant = true
					break
				}
			}
			if redundant {
				d.RemoveEdge(phases[i].ID, dep)
				removed++
			} else {
				keep = append(keep, dep)
			}
		}
		phases[i].DependsOn = keep
	}
	return removed
}

// applyEdges adds the given edges to the phases' DependsOn slices,
// skipping duplicates.
func applyEdges(phases []PhaseSpec, byID map[string]int, edges []DepEdge) {
	for _, e := range edges {
		idx, ok := byID[e.From]
		if !ok {
			continue
		}
		if hasDep(phases[idx].DependsOn, e.To) {
			continue
		}
		phases[idx].DependsOn = append(phases[idx].DependsOn, e.To)
	}
}

// hasDep reports whether deps contains the given ID.
func hasDep(deps []string, id string) bool {
	for _, d := range deps {
		if d == id {
			return true
		}
	}
	return false
}
