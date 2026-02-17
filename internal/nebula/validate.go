package nebula

import (
	"fmt"
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
