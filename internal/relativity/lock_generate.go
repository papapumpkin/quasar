// lock_generate.go builds a LockFile from a Spacetime catalog by resolving
// the dependency graph, assigning execution waves, and computing per-nebula
// metrics.
package relativity

import (
	"sort"
	"time"
)

// GenerateLock produces a LockFile from the given catalog and git state. The
// sourceHash should be the SHA-256 hash of the spacetime.toml file contents.
// gitHead is the current HEAD commit, and branchTips maps tracked branch names
// to their current tip commits.
func GenerateLock(st *Spacetime, sourceHash, gitHead string, branchTips map[string]string) *LockFile {
	order := topologicalOrder(st.Nebulas)
	waves := assignWaves(st.Nebulas)
	waveMap := buildWaveMap(waves)
	metrics := computeMetrics(st.Nebulas, waveMap)

	return &LockFile{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		SourceHash:  sourceHash,
		Graph: Graph{
			Order: order,
			Waves: waves,
		},
		Metrics: metrics,
		Staleness: Staleness{
			NebulaCount:   len(st.Nebulas),
			LastGitCommit: gitHead,
			BranchTips:    branchTips,
		},
	}
}

// topologicalOrder returns nebula names sorted in dependency order using
// Kahn's algorithm. Nebulas with no dependencies appear first. Ties within
// the same level are broken alphabetically for determinism.
func topologicalOrder(entries []Entry) []string {
	if len(entries) == 0 {
		return nil
	}

	// Build adjacency: buildsOn defines edges from dependency to dependent.
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
	}

	// inDegree counts how many dependencies each nebula has.
	inDegree := make(map[string]int, len(entries))
	// successors maps a nebula to those that depend on it.
	successors := make(map[string][]string, len(entries))

	for _, e := range entries {
		if _, ok := inDegree[e.Name]; !ok {
			inDegree[e.Name] = 0
		}
		for _, dep := range e.BuildsOn {
			if !names[dep] {
				continue // skip references to unknown nebulas
			}
			inDegree[e.Name]++
			successors[dep] = append(successors[dep], e.Name)
		}
	}

	// Seed the queue with all zero in-degree nodes, sorted for determinism.
	var queue []string
	for _, e := range entries {
		if inDegree[e.Name] == 0 {
			queue = append(queue, e.Name)
		}
	}
	sort.Strings(queue)

	var order []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		// Collect newly freed nodes, then sort for determinism.
		var freed []string
		for _, succ := range successors[node] {
			inDegree[succ]--
			if inDegree[succ] == 0 {
				freed = append(freed, succ)
			}
		}
		sort.Strings(freed)
		queue = append(queue, freed...)
	}

	// If there's a cycle, append remaining nodes alphabetically.
	if len(order) < len(entries) {
		var remaining []string
		ordered := make(map[string]bool, len(order))
		for _, n := range order {
			ordered[n] = true
		}
		for _, e := range entries {
			if !ordered[e.Name] {
				remaining = append(remaining, e.Name)
			}
		}
		sort.Strings(remaining)
		order = append(order, remaining...)
	}

	return order
}

// assignWaves groups nebulas into parallel execution waves. Wave 1 contains
// nebulas with no dependencies; wave N contains nebulas whose dependencies
// are all satisfied in waves 1 through N-1.
func assignWaves(entries []Entry) [][]string {
	if len(entries) == 0 {
		return nil
	}

	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
	}

	deps := make(map[string][]string, len(entries))
	for _, e := range entries {
		var validDeps []string
		for _, d := range e.BuildsOn {
			if names[d] {
				validDeps = append(validDeps, d)
			}
		}
		deps[e.Name] = validDeps
	}

	assigned := make(map[string]int, len(entries))
	var waves [][]string

	for len(assigned) < len(entries) {
		var wave []string
		for _, e := range entries {
			if _, done := assigned[e.Name]; done {
				continue
			}
			ready := true
			for _, d := range deps[e.Name] {
				if _, done := assigned[d]; !done {
					ready = false
					break
				}
			}
			if ready {
				wave = append(wave, e.Name)
			}
		}

		if len(wave) == 0 {
			// Cycle detected — break it by adding all remaining nodes.
			for _, e := range entries {
				if _, done := assigned[e.Name]; !done {
					wave = append(wave, e.Name)
				}
			}
		}

		sort.Strings(wave)
		waveNum := len(waves) + 1
		for _, n := range wave {
			assigned[n] = waveNum
		}
		waves = append(waves, wave)
	}

	return waves
}

// buildWaveMap creates a lookup from nebula name to its wave number.
func buildWaveMap(waves [][]string) map[string]int {
	m := make(map[string]int)
	for i, wave := range waves {
		for _, name := range wave {
			m[name] = i + 1
		}
	}
	return m
}

// computeMetrics calculates impact scores, centrality, downstream counts,
// and area overlap for each nebula.
func computeMetrics(entries []Entry, waveMap map[string]int) []Metric {
	if len(entries) == 0 {
		return nil
	}

	byName := make(map[string]*Entry, len(entries))
	for i := range entries {
		byName[entries[i].Name] = &entries[i]
	}

	// Compute raw values for normalization.
	maxAreas := 0
	maxEnables := 0
	for _, e := range entries {
		if len(e.Areas) > maxAreas {
			maxAreas = len(e.Areas)
		}
		if len(e.Enables) > maxEnables {
			maxEnables = len(e.Enables)
		}
	}

	// Build area index: area -> set of nebula names.
	areaIndex := make(map[string][]string)
	for _, e := range entries {
		for _, area := range e.Areas {
			areaIndex[area] = append(areaIndex[area], e.Name)
		}
	}

	// Compute downstream counts via transitive closure of enables.
	downstreamCounts := computeDownstreamCounts(entries)

	metrics := make([]Metric, 0, len(entries))
	for _, e := range entries {
		m := Metric{
			Name:            e.Name,
			Wave:            waveMap[e.Name],
			ImpactScore:     computeImpactScore(e, maxAreas),
			Centrality:      computeCentrality(e, maxEnables),
			DownstreamCount: downstreamCounts[e.Name],
			AreaOverlap:     computeAreaOverlap(e, areaIndex),
		}
		metrics = append(metrics, m)
	}

	// Sort metrics by wave then name for deterministic output.
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].Wave != metrics[j].Wave {
			return metrics[i].Wave < metrics[j].Wave
		}
		return metrics[i].Name < metrics[j].Name
	})

	return metrics
}

// computeImpactScore returns a normalized 0-1 score based on the number of
// areas touched relative to the maximum across all nebulas.
func computeImpactScore(e Entry, maxAreas int) float64 {
	if maxAreas == 0 {
		return 0
	}
	return float64(len(e.Areas)) / float64(maxAreas)
}

// computeCentrality returns a normalized 0-1 score based on how many
// nebulas this one enables (in-degree of dependents).
func computeCentrality(e Entry, maxEnables int) float64 {
	if maxEnables == 0 {
		return 0
	}
	return float64(len(e.Enables)) / float64(maxEnables)
}

// computeDownstreamCounts calculates the transitive count of nebulas enabled
// by each nebula.
func computeDownstreamCounts(entries []Entry) map[string]int {
	// Build set of known nebula names.
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
	}

	// Build enables adjacency, filtering to known names only.
	enables := make(map[string][]string, len(entries))
	for _, e := range entries {
		var valid []string
		for _, n := range e.Enables {
			if names[n] {
				valid = append(valid, n)
			}
		}
		enables[e.Name] = valid
	}

	counts := make(map[string]int, len(entries))
	for _, e := range entries {
		visited := make(map[string]bool)
		countDownstream(e.Name, enables, visited)
		// Exclude the node itself from the count.
		counts[e.Name] = len(visited) - 1
		if counts[e.Name] < 0 {
			counts[e.Name] = 0
		}
	}
	return counts
}

// countDownstream performs a DFS to find all transitively enabled nebulas.
func countDownstream(name string, enables map[string][]string, visited map[string]bool) {
	if visited[name] {
		return
	}
	visited[name] = true
	for _, downstream := range enables[name] {
		countDownstream(downstream, enables, visited)
	}
}

// computeAreaOverlap returns the sorted list of areas that this nebula shares
// with at least one other nebula.
func computeAreaOverlap(e Entry, areaIndex map[string][]string) []string {
	var overlap []string
	for _, area := range e.Areas {
		users := areaIndex[area]
		for _, u := range users {
			if u != e.Name {
				overlap = append(overlap, area)
				break
			}
		}
	}
	sort.Strings(overlap)
	return overlap
}
