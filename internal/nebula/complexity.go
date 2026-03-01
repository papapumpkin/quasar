package nebula

import (
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
