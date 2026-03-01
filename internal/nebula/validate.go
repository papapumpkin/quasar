package nebula

import (
	"errors"
	"fmt"

	"github.com/papapumpkin/quasar/internal/dag"
)

// Validate checks a nebula for structural correctness:
// required fields, unique IDs, valid dependencies, no cycles.
func Validate(n *Nebula) []ValidationError {
	var errs []ValidationError

	if n.Manifest.Nebula.Name == "" {
		errs = append(errs, ValidationError{
			Category:   ValCatMissingField,
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
				Category:   ValCatMissingField,
				SourceFile: p.SourceFile,
				Field:      "id",
				Err:        fmt.Errorf("%w: id", ErrMissingField),
			})
			continue
		}
		if p.Title == "" {
			errs = append(errs, ValidationError{
				Category:   ValCatMissingField,
				PhaseID:    p.ID,
				SourceFile: p.SourceFile,
				Field:      "title",
				Err:        fmt.Errorf("%w: title", ErrMissingField),
			})
		}

		// Duplicate IDs.
		if prev, ok := seen[p.ID]; ok {
			errs = append(errs, ValidationError{
				Category:   ValCatDuplicateID,
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
					Category:   ValCatUnknownDep,
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
			Category:   ValCatBoundsViolation,
			SourceFile: "nebula.toml",
			Field:      "execution.max_review_cycles",
			Err:        fmt.Errorf("execution.max_review_cycles must be >= 0, got %d", exec.MaxReviewCycles),
		})
	}
	if exec.MaxBudgetUSD < 0 {
		errs = append(errs, ValidationError{
			Category:   ValCatBoundsViolation,
			SourceFile: "nebula.toml",
			Field:      "execution.max_budget_usd",
			Err:        fmt.Errorf("execution.max_budget_usd must be >= 0, got %f", exec.MaxBudgetUSD),
		})
	}

	// Validate manifest gate mode.
	if exec.Gate != "" && !ValidGateModes[exec.Gate] {
		errs = append(errs, ValidationError{
			Category:   ValCatInvalidGate,
			SourceFile: "nebula.toml",
			Field:      "execution.gate",
			Err:        fmt.Errorf("%w: %q", ErrInvalidGate, exec.Gate),
		})
	}

	// Validate per-phase execution overrides.
	for _, p := range n.Phases {
		if p.MaxReviewCycles < 0 {
			errs = append(errs, ValidationError{
				Category:   ValCatBoundsViolation,
				PhaseID:    p.ID,
				SourceFile: p.SourceFile,
				Field:      "max_review_cycles",
				Err:        fmt.Errorf("max_review_cycles must be >= 0, got %d", p.MaxReviewCycles),
			})
		}
		if p.MaxBudgetUSD < 0 {
			errs = append(errs, ValidationError{
				Category:   ValCatBoundsViolation,
				PhaseID:    p.ID,
				SourceFile: p.SourceFile,
				Field:      "max_budget_usd",
				Err:        fmt.Errorf("max_budget_usd must be >= 0, got %f", p.MaxBudgetUSD),
			})
		}
		if p.Gate != "" && !ValidGateModes[p.Gate] {
			errs = append(errs, ValidationError{
				Category:   ValCatInvalidGate,
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
				Category:   ValCatMissingField,
				SourceFile: "nebula.toml",
				Field:      "dependencies.requires_beads",
				Err:        fmt.Errorf("requires_beads entries must be non-empty strings"),
			})
		}
	}
	for _, dep := range n.Manifest.Dependencies.RequiresNebulae {
		if dep == "" {
			errs = append(errs, ValidationError{
				Category:   ValCatMissingField,
				SourceFile: "nebula.toml",
				Field:      "dependencies.requires_nebulae",
				Err:        fmt.Errorf("requires_nebulae entries must be non-empty strings"),
			})
		}
	}

	// Cycle detection via DAG construction.
	var d *dag.DAG
	if len(errs) == 0 {
		var err error
		d, err = phasesToDAG(n.Phases)
		if err != nil {
			errs = append(errs, ValidationError{
				Category:   ValCatCycle,
				SourceFile: "nebula.toml",
				Err:        err,
			})
		}
	}

	// Scope overlap detection for parallel phases.
	if len(errs) == 0 {
		errs = append(errs, validateScopeOverlaps(n.Phases, d)...)
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
func ValidateHotAdd(phase PhaseSpec, existingIDs map[string]bool, d *dag.DAG) []ValidationError {
	var errs []ValidationError

	if phase.ID == "" {
		errs = append(errs, ValidationError{
			Category:   ValCatMissingField,
			SourceFile: phase.SourceFile,
			Field:      "id",
			Err:        fmt.Errorf("%w: id", ErrMissingField),
		})
		return errs
	}
	if phase.Title == "" {
		errs = append(errs, ValidationError{
			Category:   ValCatMissingField,
			PhaseID:    phase.ID,
			SourceFile: phase.SourceFile,
			Field:      "title",
			Err:        fmt.Errorf("%w: title", ErrMissingField),
		})
	}
	if existingIDs[phase.ID] {
		errs = append(errs, ValidationError{
			Category:   ValCatDuplicateID,
			PhaseID:    phase.ID,
			SourceFile: phase.SourceFile,
			Err:        fmt.Errorf("%w: %q", ErrDuplicateID, phase.ID),
		})
	}
	if len(errs) > 0 {
		return errs
	}

	// Tentatively add the node and edges to check for cycles.
	d.AddNodeIdempotent(phase.ID, phase.Priority)
	for _, dep := range phase.DependsOn {
		if err := d.AddEdge(phase.ID, dep); err != nil {
			if errors.Is(err, dag.ErrCycle) {
				errs = append(errs, ValidationError{
					Category:   ValCatCycle,
					PhaseID:    phase.ID,
					SourceFile: phase.SourceFile,
					Err:        fmt.Errorf("%w: adding %q would create a cycle", ErrDependencyCycle, phase.ID),
				})
				rollbackHotAdd(d, phase)
				return errs
			}
		}
	}
	for _, blocked := range phase.Blocks {
		if err := d.AddEdge(blocked, phase.ID); err != nil {
			if errors.Is(err, dag.ErrCycle) {
				errs = append(errs, ValidationError{
					Category:   ValCatCycle,
					PhaseID:    phase.ID,
					SourceFile: phase.SourceFile,
					Err:        fmt.Errorf("%w: adding %q would create a cycle", ErrDependencyCycle, phase.ID),
				})
				rollbackHotAdd(d, phase)
				return errs
			}
		}
	}

	return errs
}

// rollbackHotAdd removes a phase node and all its edges from the DAG.
func rollbackHotAdd(d *dag.DAG, phase PhaseSpec) {
	_ = d.Remove(phase.ID)
}
