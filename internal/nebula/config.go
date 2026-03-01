package nebula

import "github.com/papapumpkin/quasar/internal/dag"

// ResolvedExecution holds the fully resolved execution config for a single phase.
type ResolvedExecution struct {
	MaxReviewCycles int
	MaxBudgetUSD    float64
	Model           string
	RoutedTier      string  // Non-empty when auto-routing selected the model.
	ComplexityScore float64 // Zero when auto-routing was not applied.
}

// RoutingContext carries the optional data needed for adaptive model routing.
// A nil *RoutingContext disables auto-routing (backward compatible).
type RoutingContext struct {
	Routing TierConfig
	DAG     *dag.DAG // May be nil; depth signal becomes 0.
}

// DefaultMaxReviewCycles is the built-in fallback for max review cycles.
const DefaultMaxReviewCycles = 3

// DefaultMaxBudgetUSD is the built-in fallback for per-phase budget.
const DefaultMaxBudgetUSD = 5.0

// ResolveExecution merges config from phase → nebula → global, picking the first non-zero value.
// Precedence (highest wins, skipping zero/empty): phase → nebula → global → built-in defaults.
func ResolveExecution(globalCycles int, globalBudget float64, globalModel string, neb *Execution, phase *PhaseSpec) ResolvedExecution {
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

	// Phase overrides nebula.
	if phase != nil {
		if phase.MaxReviewCycles > 0 {
			r.MaxReviewCycles = phase.MaxReviewCycles
		}
		if phase.MaxBudgetUSD > 0 {
			r.MaxBudgetUSD = phase.MaxBudgetUSD
		}
		if phase.Model != "" {
			r.Model = phase.Model
		}
	}

	return r
}

// ResolveGate returns the effective gate mode for a phase.
// Precedence: phase override → manifest default → GateModeTrust.
func ResolveGate(manifest Execution, phase PhaseSpec) GateMode {
	if phase.Gate != "" {
		return phase.Gate
	}
	if manifest.Gate != "" {
		return manifest.Gate
	}
	return GateModeTrust
}
