package nebula

import (
	"context"
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/agent"
)

// retryWithFeedback re-invokes the architect agent with validation errors
// as additional context, asking it to produce corrected phases.
func retryWithFeedback(
	ctx context.Context,
	invoker agent.Invoker,
	req GenerateRequest,
	prevResult *GenerateResult,
	errors []ValidationError,
) (*GenerateResult, error) {
	prompt := buildRetryPrompt(prevResult, errors)

	agnt := ArchitectAgent(req.MaxBudgetUSD, req.Model)
	invResult, err := invoker.Invoke(ctx, agnt, prompt, req.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("retry architect invocation failed: %w", err)
	}

	results, err := parseMultiPhaseOutput(invResult.ResultText)
	if err != nil {
		return nil, fmt.Errorf("parsing retry output: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("retry architect produced no phases")
	}

	// Extract phases and apply defaults.
	manifest := prevResult.Manifest
	phases := make([]PhaseSpec, 0, len(results))
	var warnings []string
	for _, r := range results {
		applyDefaults(&r.PhaseSpec, manifest.Defaults)
		if len(r.Errors) > 0 {
			for _, e := range r.Errors {
				warnings = append(warnings, fmt.Sprintf("phase %q: %s", r.PhaseSpec.ID, e))
			}
		}
		phases = append(phases, r.PhaseSpec)
	}

	// Run dependency inference.
	inferrer := &DependencyInferrer{Phases: phases}
	inferResult, err := inferrer.InferDependencies()
	if err != nil {
		return nil, fmt.Errorf("retry dependency inference failed: %w", err)
	}
	phases = inferResult.Phases
	warnings = append(warnings, inferResult.Warnings...)

	// Assemble and validate the corrected nebula.
	neb := &Nebula{
		Dir:      req.OutputDir,
		Manifest: manifest,
		Phases:   phases,
	}

	valErrs := Validate(neb)
	for _, ve := range valErrs {
		warnings = append(warnings, ve.Error())
	}

	totalCost := prevResult.CostUSD + invResult.CostUSD

	return &GenerateResult{
		Nebula:      neb,
		Manifest:    manifest,
		Phases:      phases,
		InferResult: inferResult,
		Errors:      warnings,
		CostUSD:     totalCost,
	}, nil
}

// buildRetryPrompt constructs a prompt that includes the previously generated
// phases and the validation errors, asking the architect to fix them.
func buildRetryPrompt(prevResult *GenerateResult, valErrors []ValidationError) string {
	var b strings.Builder

	b.WriteString("# Validation Error Correction\n\n")
	b.WriteString("The previously generated nebula has validation errors that could not be automatically fixed.\n")
	b.WriteString("Please produce corrected phases that resolve these issues while preserving the overall structure.\n\n")

	b.WriteString("## Validation Errors\n\n")
	for _, ve := range valErrors {
		fmt.Fprintf(&b, "- [%s] %s\n", ve.Category, ve.Error())
	}
	b.WriteString("\n")

	b.WriteString("## Previously Generated Phases\n\n")
	for _, p := range prevResult.Phases {
		deps := "none"
		if len(p.DependsOn) > 0 {
			deps = strings.Join(p.DependsOn, ", ")
		}
		fmt.Fprintf(&b, "- **%s** (`%s`): %s [depends_on: %s]\n", p.Title, p.ID, p.Type, deps)
	}
	b.WriteString("\n")

	b.WriteString("## Instructions\n\n")
	b.WriteString("Fix the validation errors listed above.\n")
	b.WriteString("Output ALL phases (including unchanged ones) using the PHASE_FILE/END_PHASE_FILE format.\n")
	b.WriteString("Each phase must have a unique kebab-case `id` and descriptive `title`.\n")
	b.WriteString("Ensure there are no dependency cycles and all `depends_on` references point to valid phase IDs.\n\n")

	return b.String()
}
