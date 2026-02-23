package nebula

import (
	"fmt"

	"github.com/papapumpkin/quasar/internal/dag"
)

// NewDAGFromPhases constructs a *dag.DAG from phase specs. It adds all
// phases as nodes (using their priority) and all dependency edges.
// Returns an error if a dependency edge would create a cycle or reference
// a missing node.
func NewDAGFromPhases(phases []PhaseSpec) (*dag.DAG, error) {
	d := dag.New()
	for _, p := range phases {
		d.AddNodeIdempotent(p.ID, p.Priority)
	}
	for _, p := range phases {
		for _, dep := range p.DependsOn {
			if err := d.AddEdge(p.ID, dep); err != nil {
				return nil, fmt.Errorf("edge %s → %s: %w", p.ID, dep, err)
			}
		}
	}
	return d, nil
}
