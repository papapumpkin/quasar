package nebula

import "fmt"

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
var DefaultTiers = []ModelTier{
	{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
	{Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
	{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
}

// SelectTier picks the first tier whose MaxScore >= the given complexity score.
// Tiers must be sorted by MaxScore ascending. If no tier matches (should not
// happen with a 1.0 ceiling), the last tier is returned as a fallback.
func SelectTier(score float64, tiers []ModelTier) ModelTier {
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
