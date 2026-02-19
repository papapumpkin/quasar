package dag

// ScoringOptions configures composite impact scoring.
type ScoringOptions struct {
	// Alpha is the weight for PageRank in the composite score.
	// The betweenness weight is (1 - Alpha). Must be in [0, 1].
	Alpha float64

	// PageRank holds configuration for the underlying PageRank pass.
	PageRank PageRankOptions
}

// DefaultScoringOptions returns production defaults: alpha 0.6 (slightly
// favor influence over bottleneck detection) with standard PageRank settings.
func DefaultScoringOptions() ScoringOptions {
	return ScoringOptions{
		Alpha:    0.6,
		PageRank: DefaultPageRankOptions(),
	}
}

// ComputeImpact calculates a composite impact score for every node and
// stores the result on each Node.Impact field. The score combines
// normalized PageRank (influence) and betweenness centrality (bottleneck):
//
//	Impact = Alpha * NormalizedPageRank + (1-Alpha) * Betweenness
//
// PageRank is normalized to [0, 1] by dividing by the maximum observed
// value. Betweenness is already normalized by BetweennessCentrality.
func (d *DAG) ComputeImpact(opts ScoringOptions) {
	if len(d.nodes) == 0 {
		return
	}

	pr := d.PageRank(opts.PageRank)
	bc := d.BetweennessCentrality()

	// Normalize PageRank to [0, 1] by dividing by the maximum value.
	maxPR := 0.0
	for _, v := range pr {
		if v > maxPR {
			maxPR = v
		}
	}
	if maxPR > 0 {
		for id := range pr {
			pr[id] /= maxPR
		}
	}

	for id, node := range d.nodes {
		node.Impact = opts.Alpha*pr[id] + (1-opts.Alpha)*bc[id]
	}
}
