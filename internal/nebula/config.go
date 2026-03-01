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

// ResolveExecution merges config from phase → nebula → global, picking the
// first non-zero value. When routing is enabled and no explicit model is set,
// complexity scoring selects the model tier.
//
// Precedence (highest wins):
//  1. Phase-level Model (explicit pin)
//  2. Nebula-level Execution.Model (blanket override)
//  3. Auto-routed model (from complexity scoring, when enabled)
//  4. Global --model flag / QUASAR_MODEL env
//  5. Built-in default (empty string — invoker picks)
func ResolveExecution(globalCycles int, globalBudget float64, globalModel string, neb *Execution, phase *PhaseSpec, rc *RoutingContext) ResolvedExecution {
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

	// Auto-routing: if enabled, no explicit model was set at any level, and we
	// have a RoutingContext, score the phase and select a tier.
	if rc != nil && rc.Routing.Enabled && r.Model == "" && phase != nil {
		signals := BuildComplexitySignals(phase, rc.DAG)
		result := ScoreComplexity(signals)
		tier := SelectTier(result.Score, rc.Routing.Tiers)
		r.Model = tier.Model
		r.RoutedTier = tier.Name
		r.ComplexityScore = result.Score
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
