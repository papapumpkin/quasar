package nebula

// ResolvedExecution holds the fully resolved execution config for a single task.
type ResolvedExecution struct {
	MaxReviewCycles int
	MaxBudgetUSD    float64
	Model           string
}

// DefaultMaxReviewCycles is the built-in fallback for max review cycles.
const DefaultMaxReviewCycles = 3

// DefaultMaxBudgetUSD is the built-in fallback for per-task budget.
const DefaultMaxBudgetUSD = 5.0

// ResolveExecution merges config from task → nebula → global, picking the first non-zero value.
// Precedence (highest wins, skipping zero/empty): task → nebula → global → built-in defaults.
func ResolveExecution(globalCycles int, globalBudget float64, globalModel string, neb *Execution, task *TaskSpec) ResolvedExecution {
	r := ResolvedExecution{
		MaxReviewCycles: DefaultMaxReviewCycles,
		MaxBudgetUSD:    DefaultMaxBudgetUSD,
	}

	// Global overrides built-in defaults.
	if globalCycles > 0 {
		r.MaxReviewCycles = globalCycles
	}
	if globalBudget > 0 {
		r.MaxBudgetUSD = globalBudget
	}
	if globalModel != "" {
		r.Model = globalModel
	}

	// Nebula execution overrides global.
	if neb != nil {
		if neb.MaxReviewCycles > 0 {
			r.MaxReviewCycles = neb.MaxReviewCycles
		}
		if neb.MaxBudgetUSD > 0 {
			r.MaxBudgetUSD = neb.MaxBudgetUSD
		}
		if neb.Model != "" {
			r.Model = neb.Model
		}
	}

	// Task overrides nebula.
	if task != nil {
		if task.MaxReviewCycles > 0 {
			r.MaxReviewCycles = task.MaxReviewCycles
		}
		if task.MaxBudgetUSD > 0 {
			r.MaxBudgetUSD = task.MaxBudgetUSD
		}
		if task.Model != "" {
			r.Model = task.Model
		}
	}

	return r
}
