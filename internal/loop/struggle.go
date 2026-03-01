package loop

import (
	"fmt"
	"math"
	"strings"
)

// StruggleSignal represents the result of analyzing a CycleState for struggle indicators.
type StruggleSignal struct {
	Triggered      bool    // true if the combined score exceeds the threshold
	Score          float64 // composite struggle score in [0.0, 1.0]
	FilterRepeat   int     // count of consecutive cycles failing the same filter check
	FindingOverlap float64 // ratio of findings in the current cycle that duplicate a prior cycle's findings
	BudgetBurnRate float64 // ratio of budget consumed per cycle vs remaining budget
	Reason         string  // human-readable summary of why the signal triggered
}

// StruggleConfig holds tunable thresholds for struggle detection.
type StruggleConfig struct {
	Enabled                 bool    // master switch; false = never trigger
	MinCyclesBeforeCheck    int     // do not evaluate until this many cycles have run (default: 2)
	FilterRepeatThreshold   int     // consecutive same-filter failures to flag (default: 2)
	FindingOverlapThreshold float64 // overlap ratio above which findings are considered stuck (default: 0.6)
	BudgetBurnThreshold     float64 // fraction of budget per cycle that is considered excessive (default: 0.3)
	CompositeThreshold      float64 // combined score above which decomposition triggers (default: 0.6)
}

// Signal weights for composite score calculation.
const (
	filterRepeatWeight  = 0.35
	findingOverlapWeight = 0.40
	budgetBurnWeight    = 0.25
)

// Jaccard similarity threshold for finding overlap detection.
const findingJaccardThreshold = 0.8

// DefaultStruggleConfig returns a StruggleConfig with sensible defaults.
func DefaultStruggleConfig() StruggleConfig {
	return StruggleConfig{
		Enabled:                 false,
		MinCyclesBeforeCheck:    2,
		FilterRepeatThreshold:   2,
		FindingOverlapThreshold: 0.6,
		BudgetBurnThreshold:     0.3,
		CompositeThreshold:      0.6,
	}
}

// EvaluateStruggle analyzes a CycleState for struggle signals.
// It returns a StruggleSignal indicating whether the phase should be paused for decomposition.
// The function is pure — it reads CycleState fields but does not mutate them.
func EvaluateStruggle(state *CycleState, cfg StruggleConfig) StruggleSignal {
	if !cfg.Enabled {
		return StruggleSignal{}
	}
	if state.Cycle < cfg.MinCyclesBeforeCheck {
		return StruggleSignal{}
	}

	filterRepeat := countTrailingFilterRepeats(state.FilterHistory)
	findingOverlap := computeFindingOverlap(state.Findings, state.AllFindings)
	burnRate := computeBudgetBurnRate(state)

	// Normalize each signal to [0.0, 1.0] relative to its threshold.
	filterNorm := clamp01(float64(filterRepeat) / float64(cfg.FilterRepeatThreshold))
	overlapNorm := clamp01(findingOverlap / cfg.FindingOverlapThreshold)
	burnNorm := clamp01(burnRate / cfg.BudgetBurnThreshold)

	score := filterRepeatWeight*filterNorm +
		findingOverlapWeight*overlapNorm +
		budgetBurnWeight*burnNorm

	signal := StruggleSignal{
		Score:          score,
		FilterRepeat:   filterRepeat,
		FindingOverlap: findingOverlap,
		BudgetBurnRate: burnRate,
	}

	if score >= cfg.CompositeThreshold {
		signal.Triggered = true
		signal.Reason = buildStruggleReason(signal, cfg)
	}

	return signal
}

// countTrailingFilterRepeats counts how many consecutive entries at the
// tail of history share the same non-empty value.
func countTrailingFilterRepeats(history []string) int {
	if len(history) == 0 {
		return 0
	}
	last := history[len(history)-1]
	if last == "" {
		return 0
	}
	count := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i] != last {
			break
		}
		count++
	}
	return count
}

// computeFindingOverlap calculates the ratio of current-cycle findings that
// overlap with prior-cycle findings. Two findings overlap when their
// Description fields have a Jaccard similarity above the threshold.
func computeFindingOverlap(current []ReviewFinding, allPrior []ReviewFinding) float64 {
	if len(current) == 0 {
		return 0.0
	}
	overlapping := 0
	for _, cur := range current {
		for _, prior := range allPrior {
			if jaccardSimilarity(cur.Description, prior.Description) >= findingJaccardThreshold {
				overlapping++
				break // one match is enough for this finding
			}
		}
	}
	return float64(overlapping) / float64(len(current))
}

// computeBudgetBurnRate computes the ratio of actual per-cycle cost to the
// ideal even distribution of budget across cycles. Returns 0 if MaxBudgetUSD
// or MaxCycles is zero to avoid division by zero.
func computeBudgetBurnRate(state *CycleState) float64 {
	if state.MaxBudgetUSD <= 0 || state.MaxCycles <= 0 || state.Cycle <= 0 {
		return 0.0
	}
	actualPerCycle := state.TotalCostUSD / float64(state.Cycle)
	idealPerCycle := state.MaxBudgetUSD / float64(state.MaxCycles)
	if idealPerCycle <= 0 {
		return 0.0
	}
	return actualPerCycle / idealPerCycle
}

// jaccardSimilarity computes the Jaccard similarity of two strings
// tokenized on whitespace. Returns a value in [0.0, 1.0].
func jaccardSimilarity(a, b string) float64 {
	tokensA := tokenize(a)
	tokensB := tokenize(b)
	if len(tokensA) == 0 && len(tokensB) == 0 {
		return 1.0
	}
	if len(tokensA) == 0 || len(tokensB) == 0 {
		return 0.0
	}

	intersection := 0
	for tok := range tokensA {
		if tokensB[tok] {
			intersection++
		}
	}
	union := len(tokensA) + len(tokensB) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// tokenize splits a string on whitespace and returns the set of unique tokens.
func tokenize(s string) map[string]bool {
	words := strings.Fields(s)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[strings.ToLower(w)] = true
	}
	return set
}

// clamp01 clamps v to the range [0.0, 1.0].
func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0.0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}

// buildStruggleReason constructs a human-readable explanation of why the
// struggle signal triggered.
func buildStruggleReason(signal StruggleSignal, cfg StruggleConfig) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("struggle detected (score=%.2f, threshold=%.2f):", signal.Score, cfg.CompositeThreshold))

	if signal.FilterRepeat > 0 {
		b.WriteString(fmt.Sprintf(" filter-repeat=%d/%d;", signal.FilterRepeat, cfg.FilterRepeatThreshold))
	}
	if signal.FindingOverlap > 0 {
		b.WriteString(fmt.Sprintf(" finding-overlap=%.0f%%/%.0f%%;", signal.FindingOverlap*100, cfg.FindingOverlapThreshold*100))
	}
	if signal.BudgetBurnRate > 0 {
		b.WriteString(fmt.Sprintf(" budget-burn=%.2f/%.2f;", signal.BudgetBurnRate, cfg.BudgetBurnThreshold))
	}

	return b.String()
}
