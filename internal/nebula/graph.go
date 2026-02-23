package nebula

import (
	"errors"
	"fmt"

	"github.com/papapumpkin/quasar/internal/dag"
)

// Wave is an alias for dag.Wave, bridging the nebula and dag packages.
// Within nebula, waves group phases whose dependencies are all in prior waves.
type Wave = dag.Wave

// phasesToDAG constructs a *dag.DAG from a slice of phase specs.
// It returns an error wrapping ErrDependencyCycle if adding edges
// reveals a cycle.
func phasesToDAG(phases []PhaseSpec) (*dag.DAG, error) {
	d := dag.New()
	for _, p := range phases {
		d.AddNodeIdempotent(p.ID, p.Priority)
	}
	for _, p := range phases {
		for _, dep := range p.DependsOn {
			if err := d.AddEdge(p.ID, dep); err != nil {
				if errors.Is(err, dag.ErrCycle) {
					return d, fmt.Errorf("%w: %v", ErrDependencyCycle, err)
				}
				return d, fmt.Errorf("phase %q → %q: %w", p.ID, dep, err)
			}
		}
	}
	return d, nil
}
