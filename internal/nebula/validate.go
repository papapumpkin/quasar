package nebula

import "fmt"

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

	for _, t := range n.Tasks {
		// Required fields.
		if t.ID == "" {
			errs = append(errs, ValidationError{
				SourceFile: t.SourceFile,
				Field:      "id",
				Err:        fmt.Errorf("%w: id", ErrMissingField),
			})
			continue
		}
		if t.Title == "" {
			errs = append(errs, ValidationError{
				TaskID:     t.ID,
				SourceFile: t.SourceFile,
				Field:      "title",
				Err:        fmt.Errorf("%w: title", ErrMissingField),
			})
		}

		// Duplicate IDs.
		if prev, ok := seen[t.ID]; ok {
			errs = append(errs, ValidationError{
				TaskID:     t.ID,
				SourceFile: t.SourceFile,
				Err:        fmt.Errorf("%w: %q already defined in %s", ErrDuplicateID, t.ID, prev),
			})
		}
		seen[t.ID] = t.SourceFile
		ids[t.ID] = true
	}

	// Validate dependencies reference known IDs.
	for _, t := range n.Tasks {
		for _, dep := range t.DependsOn {
			if !ids[dep] {
				errs = append(errs, ValidationError{
					TaskID:     t.ID,
					SourceFile: t.SourceFile,
					Field:      "depends_on",
					Err:        fmt.Errorf("%w: %q depends on unknown task %q", ErrUnknownDep, t.ID, dep),
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

	// Validate per-task execution overrides.
	for _, t := range n.Tasks {
		if t.MaxReviewCycles < 0 {
			errs = append(errs, ValidationError{
				TaskID:     t.ID,
				SourceFile: t.SourceFile,
				Field:      "max_review_cycles",
				Err:        fmt.Errorf("max_review_cycles must be >= 0, got %d", t.MaxReviewCycles),
			})
		}
		if t.MaxBudgetUSD < 0 {
			errs = append(errs, ValidationError{
				TaskID:     t.ID,
				SourceFile: t.SourceFile,
				Field:      "max_budget_usd",
				Err:        fmt.Errorf("max_budget_usd must be >= 0, got %f", t.MaxBudgetUSD),
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
		g := NewGraph(n.Tasks)
		if _, err := g.Sort(); err != nil {
			errs = append(errs, ValidationError{
				SourceFile: "nebula.toml",
				Err:        err,
			})
		}
	}

	return errs
}
