package nebula

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/papapumpkin/quasar/internal/agent"
)

// nonAlphanumHyphenRe matches characters that are not alphanumeric or hyphens.
var nonAlphanumHyphenRe = regexp.MustCompile(`[^a-z0-9-]`)

// CorrectAndRetry runs auto-correction on validation errors and optionally
// retries the architect agent if unresolvable errors remain. It returns
// the corrected result, or the original with error annotations if
// correction fails.
func CorrectAndRetry(
	ctx context.Context,
	invoker agent.Invoker,
	req GenerateRequest,
	result *GenerateResult,
	valErrs []ValidationError,
) (*GenerateResult, error) {
	if len(valErrs) == 0 {
		return result, nil
	}

	// Step 1: Apply automatic corrections.
	corrected, fixes, remaining := correctValidationErrors(result.Phases, result.Manifest, valErrs)
	result.Phases = corrected
	result.Errors = append(result.Errors, fixes...)

	// Update the nebula's phases to match.
	if result.Nebula != nil {
		result.Nebula.Phases = corrected
	}

	if len(remaining) == 0 {
		return result, nil
	}

	// Step 2: Re-invoke the architect with feedback if errors remain.
	retried, err := retryWithFeedback(ctx, invoker, req, result, remaining)
	if err != nil {
		// Retry failed — annotate the original result with remaining errors.
		for _, ve := range remaining {
			result.Errors = append(result.Errors, ve.Error())
		}
		return result, nil
	}

	return retried, nil
}

// correctValidationErrors applies automatic fixes for common validation
// errors. It returns the corrected phases, a list of applied fixes, and
// any remaining errors that could not be auto-corrected.
func correctValidationErrors(
	phases []PhaseSpec,
	manifest Manifest,
	errs []ValidationError,
) (corrected []PhaseSpec, fixes []string, remaining []ValidationError) {
	// Work on a copy to avoid mutating the caller's slice.
	corrected = make([]PhaseSpec, len(phases))
	copy(corrected, phases)

	// Build an index of phases by ID for efficient lookup.
	idxByID := make(map[string]int, len(corrected))
	for i, p := range corrected {
		if p.ID != "" {
			idxByID[p.ID] = i
		}
	}

	for _, ve := range errs {
		switch ve.Category {
		case ValCatMissingField:
			if fix := fixMissingField(corrected, idxByID, ve, manifest); fix != "" {
				fixes = append(fixes, fix)
			} else {
				remaining = append(remaining, ve)
			}

		case ValCatDuplicateID:
			if fix := fixDuplicateID(corrected, idxByID, ve); fix != "" {
				fixes = append(fixes, fix)
			} else {
				remaining = append(remaining, ve)
			}

		case ValCatUnknownDep:
			if fix := fixUnknownDep(corrected, idxByID, ve); fix != "" {
				fixes = append(fixes, fix)
			} else {
				remaining = append(remaining, ve)
			}

		case ValCatInvalidGate:
			if fix := fixInvalidGate(corrected, idxByID, ve); fix != "" {
				fixes = append(fixes, fix)
			} else {
				remaining = append(remaining, ve)
			}

		case ValCatBoundsViolation:
			if fix := fixBoundsViolation(corrected, idxByID, ve); fix != "" {
				fixes = append(fixes, fix)
			} else {
				remaining = append(remaining, ve)
			}

		case ValCatScopeOverlap:
			if fix := fixScopeOverlap(corrected, idxByID, ve); fix != "" {
				fixes = append(fixes, fix)
			} else {
				remaining = append(remaining, ve)
			}

		case ValCatCycle:
			// Cycles cannot be auto-corrected.
			remaining = append(remaining, ve)

		default:
			remaining = append(remaining, ve)
		}
	}

	return corrected, fixes, remaining
}

// fixMissingField attempts to fill in missing required fields.
func fixMissingField(phases []PhaseSpec, idxByID map[string]int, ve ValidationError, manifest Manifest) string {
	switch ve.Field {
	case "id":
		// Find the phase by source file and fill in ID from title.
		for i := range phases {
			if phases[i].SourceFile == ve.SourceFile && phases[i].ID == "" {
				if phases[i].Title != "" {
					newID := slugifyID(phases[i].Title)
					phases[i].ID = newID
					idxByID[newID] = i
					return fmt.Sprintf("auto-corrected: derived id %q from title %q", newID, phases[i].Title)
				}
				return ""
			}
		}
		return ""

	case "title":
		if idx, ok := idxByID[ve.PhaseID]; ok {
			if phases[idx].Title == "" {
				phases[idx].Title = unslugify(phases[idx].ID)
				return fmt.Sprintf("auto-corrected: derived title %q from id %q", phases[idx].Title, phases[idx].ID)
			}
		}
		return ""

	case "type":
		if idx, ok := idxByID[ve.PhaseID]; ok {
			if phases[idx].Type == "" {
				phases[idx].Type = manifest.Defaults.Type
				return fmt.Sprintf("auto-corrected: set type to default %q for phase %q", manifest.Defaults.Type, ve.PhaseID)
			}
		}
		return ""

	default:
		return ""
	}
}

// fixDuplicateID renames the duplicate phase by appending a numeric suffix
// and updates all depends_on references.
func fixDuplicateID(phases []PhaseSpec, idxByID map[string]int, ve ValidationError) string {
	origID := ve.PhaseID

	// Find a unique new ID.
	newID := ""
	for suffix := 2; suffix <= len(phases)+2; suffix++ {
		candidate := fmt.Sprintf("%s-%d", origID, suffix)
		if _, exists := idxByID[candidate]; !exists {
			newID = candidate
			break
		}
	}
	if newID == "" {
		return ""
	}

	// Find the duplicate phase (by source file — the second occurrence).
	for i := range phases {
		if phases[i].ID == origID && phases[i].SourceFile == ve.SourceFile {
			phases[i].ID = newID
			idxByID[newID] = i
			break
		}
	}

	// Update depends_on references in the same source file (heuristic:
	// the duplicate's dependents likely intended the duplicate).
	for i := range phases {
		for j, dep := range phases[i].DependsOn {
			if dep == origID && phases[i].SourceFile == ve.SourceFile {
				phases[i].DependsOn[j] = newID
			}
		}
	}

	return fmt.Sprintf("auto-corrected: renamed duplicate id %q to %q", origID, newID)
}

// fixUnknownDep removes dangling dependency references.
func fixUnknownDep(phases []PhaseSpec, idxByID map[string]int, ve ValidationError) string {
	idx, ok := idxByID[ve.PhaseID]
	if !ok {
		return ""
	}

	unknownDep := extractUnknownDep(ve)
	if unknownDep == "" {
		return ""
	}

	filtered := make([]string, 0, len(phases[idx].DependsOn))
	for _, dep := range phases[idx].DependsOn {
		if dep != unknownDep {
			filtered = append(filtered, dep)
		}
	}
	phases[idx].DependsOn = filtered

	return fmt.Sprintf("auto-corrected: removed dangling dependency %q from phase %q", unknownDep, ve.PhaseID)
}

// fixInvalidGate resets an invalid gate mode to empty (inherit from manifest).
func fixInvalidGate(phases []PhaseSpec, idxByID map[string]int, ve ValidationError) string {
	if ve.PhaseID == "" {
		return "" // Manifest-level gate — cannot auto-fix safely.
	}
	idx, ok := idxByID[ve.PhaseID]
	if !ok {
		return ""
	}
	oldGate := string(phases[idx].Gate)
	phases[idx].Gate = ""
	return fmt.Sprintf("auto-corrected: reset invalid gate %q to default for phase %q", oldGate, ve.PhaseID)
}

// fixBoundsViolation clamps negative values to zero.
func fixBoundsViolation(phases []PhaseSpec, idxByID map[string]int, ve ValidationError) string {
	if ve.PhaseID == "" {
		return "" // Manifest-level bounds violations are not auto-corrected.
	}
	idx, ok := idxByID[ve.PhaseID]
	if !ok {
		return ""
	}
	switch ve.Field {
	case "max_review_cycles":
		phases[idx].MaxReviewCycles = 0
		return fmt.Sprintf("auto-corrected: clamped max_review_cycles to 0 for phase %q", ve.PhaseID)
	case "max_budget_usd":
		phases[idx].MaxBudgetUSD = 0
		return fmt.Sprintf("auto-corrected: clamped max_budget_usd to 0 for phase %q", ve.PhaseID)
	default:
		return ""
	}
}

// fixScopeOverlap sets AllowScopeOverlap on the reported phase.
func fixScopeOverlap(phases []PhaseSpec, idxByID map[string]int, ve ValidationError) string {
	idx, ok := idxByID[ve.PhaseID]
	if !ok {
		return ""
	}
	phases[idx].AllowScopeOverlap = true
	return fmt.Sprintf("auto-corrected: set allow_scope_overlap=true on phase %q", ve.PhaseID)
}

// extractUnknownDep extracts the unknown dependency ID from a ValidationError
// whose category is ValCatUnknownDep. It parses the error message format
// produced by Validate: `"phase-a" depends on unknown phase "missing-dep"`.
func extractUnknownDep(ve ValidationError) string {
	msg := ve.Err.Error()
	// Search for the last occurrence of `unknown phase "` to skip the
	// sentinel error prefix which also contains "unknown phase".
	const marker = `unknown phase "`
	idx := strings.LastIndex(msg, marker)
	if idx < 0 {
		return ""
	}
	rest := msg[idx+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end >= 0 {
		return rest[:end]
	}
	return ""
}

// slugifyID converts a human-readable title into a kebab-case ID.
func slugifyID(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = nonAlphanumHyphenRe.ReplaceAllString(s, "")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = s[:50]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// unslugify converts a kebab-case ID into a human-readable title by replacing
// hyphens with spaces and capitalizing the first letter of each word.
func unslugify(id string) string {
	words := strings.Split(id, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
