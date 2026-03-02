package nebula

import (
	"fmt"

	"github.com/papapumpkin/quasar/internal/dag"
)

// Signal weights for the composite complexity score.
const (
	weightScope = 0.25
	weightBody  = 0.35
	weightDepth = 0.25
	weightType  = 0.15
)

// Saturation caps for each signal dimension.
const (
	scopeSaturation = 10.0
	bodySaturation  = 3000.0
	depthSaturation = 8.0
)

// taskTypeWeights maps phase types to their complexity weight.
var taskTypeWeights = map[string]float64{
	"task":    0.3,
	"bug":     0.4,
	"feature": 0.8,
}

// taskTypeDefault is the fallback weight for unknown phase types.
const taskTypeDefault = 0.5

// ComplexitySignals holds the raw inputs used to compute a complexity score.
type ComplexitySignals struct {
	ScopeCount int    // len(phase.Scope)
	BodyLength int    // len([]rune(phase.Body))
	DepthCount int    // len(dag.Ancestors(phase.ID))
	TaskType   string // phase.Type
}

// ComplexityResult holds the computed score and the contributing signal weights.
type ComplexityResult struct {
	Score         float64            // composite score in [0.0, 1.0]
	Signals       ComplexitySignals  // raw inputs for traceability
	Contributions map[string]float64 // per-signal weighted contribution
}

// ScoreComplexity computes a composite complexity score for a phase.
// The score is in [0.0, 1.0] where 0 is trivial and 1 is maximally complex.
// The scoring is deterministic: identical inputs always produce identical results.
func ScoreComplexity(signals ComplexitySignals) ComplexityResult {
	scopeNorm := clamp(float64(signals.ScopeCount) / scopeSaturation)
	bodyNorm := clamp(float64(signals.BodyLength) / bodySaturation)
	depthNorm := clamp(float64(signals.DepthCount) / depthSaturation)

	typeWeight, ok := taskTypeWeights[signals.TaskType]
	if !ok {
		typeWeight = taskTypeDefault
	}

	scopeContrib := weightScope * scopeNorm
	bodyContrib := weightBody * bodyNorm
	depthContrib := weightDepth * depthNorm
	typeContrib := weightType * typeWeight

	score := scopeContrib + bodyContrib + depthContrib + typeContrib

	return ComplexityResult{
		Score:   score,
		Signals: signals,
		Contributions: map[string]float64{
			"scope": scopeContrib,
			"body":  bodyContrib,
			"depth": depthContrib,
			"type":  typeContrib,
		},
	}
}

// BuildComplexitySignals extracts signals from a PhaseSpec and a DAG.
// The DAG parameter may be nil, in which case the depth contribution is 0.
func BuildComplexitySignals(phase *PhaseSpec, d *dag.DAG) ComplexitySignals {
	depthCount := 0
	if d != nil {
		ancestors := d.Ancestors(phase.ID)
		depthCount = len(ancestors)
	}

	return ComplexitySignals{
		ScopeCount: len(phase.Scope),
		BodyLength: len([]rune(phase.Body)),
		DepthCount: depthCount,
		TaskType:   phase.Type,
	}
}

// clamp restricts v to the range [0.0, 1.0].
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ModelTier represents a named tier with a complexity threshold and model identifier.
type ModelTier struct {
	Name     string  `toml:"name"`      // Human-readable name: "fast", "balanced", "heavy".
	Model    string  `toml:"model"`     // Model identifier passed to the invoker (e.g. "claude-haiku").
	MaxScore float64 `toml:"max_score"` // Phases with score <= MaxScore are routed to this tier.
}

// TierConfig holds the ordered list of tiers and the flag to enable auto-routing.
type TierConfig struct {
	Enabled bool        `toml:"enabled"`
	Tiers   []ModelTier `toml:"tiers"`
}

// DefaultTiers provides three tiers covering the full [0.0, 1.0] score range.
// DefaultTiers must not be modified.
var DefaultTiers = []ModelTier{
	{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
	{Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
	{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
}

// SelectTier picks the first tier whose MaxScore >= the given complexity score.
// Tiers must be sorted by MaxScore ascending. If tiers is empty, DefaultTiers
// is used. If no tier matches (should not happen with a 1.0 ceiling), the last
// tier is returned as a fallback.
func SelectTier(score float64, tiers []ModelTier) ModelTier {
	if len(tiers) == 0 {
		tiers = DefaultTiers
	}
	for _, t := range tiers {
		if score <= t.MaxScore {
			return t
		}
	}
	// Fallback: return the last tier.
	return tiers[len(tiers)-1]
}

// ValidateRouting checks that a TierConfig is well-formed. It returns a slice
// of ValidationError if any problems are found:
//   - Tiers must be sorted by MaxScore ascending
//   - The last tier must have MaxScore >= 1.0
//   - No duplicate tier names
//   - All Model fields must be non-empty
func ValidateRouting(cfg TierConfig) []ValidationError {
	var errs []ValidationError

	if !cfg.Enabled {
		return nil
	}

	tiers := cfg.Tiers
	if len(tiers) == 0 {
		// Enabled with no tiers — defaults will be used, nothing to validate.
		return nil
	}

	names := make(map[string]bool, len(tiers))
	for i, t := range tiers {
		// Check for empty model.
		if t.Model == "" {
			errs = append(errs, ValidationError{
				Category:   ValCatInvalidRouting,
				SourceFile: "nebula.toml",
				Field:      fmt.Sprintf("execution.routing.tiers[%d].model", i),
				Err:        fmt.Errorf("tier %q has empty model", t.Name),
			})
		}

		// Check for empty tier name.
		if t.Name == "" {
			errs = append(errs, ValidationError{
				Category:   ValCatInvalidRouting,
				SourceFile: "nebula.toml",
				Field:      fmt.Sprintf("execution.routing.tiers[%d].name", i),
				Err:        fmt.Errorf("tier at index %d has empty name", i),
			})
		}

		// Check for duplicate names.
		if t.Name != "" && names[t.Name] {
			errs = append(errs, ValidationError{
				Category:   ValCatInvalidRouting,
				SourceFile: "nebula.toml",
				Field:      fmt.Sprintf("execution.routing.tiers[%d].name", i),
				Err:        fmt.Errorf("duplicate tier name %q", t.Name),
			})
		}
		names[t.Name] = true

		// Check sort order.
		if i > 0 && t.MaxScore < tiers[i-1].MaxScore {
			errs = append(errs, ValidationError{
				Category:   ValCatInvalidRouting,
				SourceFile: "nebula.toml",
				Field:      fmt.Sprintf("execution.routing.tiers[%d].max_score", i),
				Err:        fmt.Errorf("tiers not sorted by max_score: %q (%.2f) < %q (%.2f)", t.Name, t.MaxScore, tiers[i-1].Name, tiers[i-1].MaxScore),
			})
		}
	}

	// Check that the last tier covers score 1.0.
	last := tiers[len(tiers)-1]
	if last.MaxScore < 1.0 {
		errs = append(errs, ValidationError{
			Category:   ValCatInvalidRouting,
			SourceFile: "nebula.toml",
			Field:      "execution.routing.tiers",
			Err:        fmt.Errorf("last tier %q has max_score %.2f, must be >= 1.0", last.Name, last.MaxScore),
		})
	}

	return errs
}
