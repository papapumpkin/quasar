package dag

import "sort"

// Track represents an independent subset of the DAG whose nodes share
// no dependencies with nodes in other tracks. Tracks can be executed
// in parallel by different agents without risk of conflict.
type Track struct {
	// ID is the integer identifier assigned to this track, starting at 0.
	ID int

	// NodeIDs lists the node IDs in this track, sorted in topological
	// order (dependencies before dependents).
	NodeIDs []string

	// AggregateImpact is the sum of Impact scores for all nodes in the
	// track. Call DAG.ComputeImpact before ComputeTracks for meaningful
	// values.
	AggregateImpact float64

	// MaxImpact is the highest Impact score among all nodes in the track.
	MaxImpact float64
}

// ComputeTracks partitions the DAG into independent tracks using
// Union-Find. Each track contains nodes that are transitively
// connected through edges; nodes in different tracks share no
// dependencies. The method assigns Node.TrackID on every node and
// returns the resulting Track list sorted by descending aggregate
// impact.
//
// The topological ordering within each track respects the global
// topological sort of the DAG. Returns an error if the DAG contains a
// cycle (from the underlying topological sort).
func (d *DAG) ComputeTracks() ([]Track, error) {
	if len(d.nodes) == 0 {
		return nil, nil
	}

	// Build a global topological order to reuse within each track.
	topoOrder, err := d.TopologicalSort()
	if err != nil {
		return nil, err
	}

	// Create position map for stable ordering within tracks.
	topoPos := make(map[string]int, len(topoOrder))
	for i, id := range topoOrder {
		topoPos[id] = i
	}

	// Partition using Union-Find: union each node with its dependencies.
	uf := NewUnionFind()
	for id := range d.nodes {
		uf.Add(id)
	}
	for from, deps := range d.adjacency {
		for to := range deps {
			uf.Union(from, to)
		}
	}

	// Group nodes by component root.
	components := uf.Components()

	// Sort component roots deterministically for stable track ID assignment.
	roots := make([]string, 0, len(components))
	for root := range components {
		roots = append(roots, root)
	}
	sort.Strings(roots)

	// Build tracks from components.
	tracks := make([]Track, 0, len(components))
	for _, root := range roots {
		members := components[root]
		// Sort members by topological position.
		sort.Slice(members, func(i, j int) bool {
			return topoPos[members[i]] < topoPos[members[j]]
		})

		var aggImpact, maxImpact float64
		for _, id := range members {
			impact := d.nodes[id].Impact
			aggImpact += impact
			if impact > maxImpact {
				maxImpact = impact
			}
		}

		tracks = append(tracks, Track{
			NodeIDs:         members,
			AggregateImpact: aggImpact,
			MaxImpact:       maxImpact,
		})
	}

	// Sort tracks by descending aggregate impact for prioritized execution,
	// with component size as secondary (larger first), then alphabetical
	// first node as tiebreaker for determinism.
	sort.Slice(tracks, func(i, j int) bool {
		if tracks[i].AggregateImpact != tracks[j].AggregateImpact {
			return tracks[i].AggregateImpact > tracks[j].AggregateImpact
		}
		if len(tracks[i].NodeIDs) != len(tracks[j].NodeIDs) {
			return len(tracks[i].NodeIDs) > len(tracks[j].NodeIDs)
		}
		return tracks[i].NodeIDs[0] < tracks[j].NodeIDs[0]
	})

	// Assign stable integer IDs after sorting and update Node.TrackID.
	for i := range tracks {
		tracks[i].ID = i
		for _, id := range tracks[i].NodeIDs {
			d.nodes[id].TrackID = i
		}
	}

	return tracks, nil
}
