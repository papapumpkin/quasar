package nebula

import (
	"context"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/agent"
)

// GenerateRequest holds the inputs for generating a complete nebula.
type GenerateRequest struct {
	UserPrompt   string            // What the user wants built
	NebulaName   string            // Name for the generated nebula (kebab-case)
	OutputDir    string            // Directory where the nebula will be written
	WorkDir      string            // Repository root for codebase analysis
	Analysis     *CodebaseAnalysis // Pre-computed codebase analysis (may be nil)
	Model        string            // Model override (empty = default)
	MaxBudgetUSD float64           // Budget cap for generation itself
}

// GenerateResult holds the output of nebula generation.
type GenerateResult struct {
	Nebula      *Nebula          // The complete generated nebula
	Manifest    Manifest         // Generated manifest
	Phases      []PhaseSpec      // Generated phases with corrected dependencies
	InferResult *InferenceResult // Dependency inference report
	Errors      []string         // Non-fatal warnings
	CostUSD     float64          // Total cost of architect invocations
}

// Generate produces a complete nebula from a natural-language prompt.
// It invokes the architect agent to decompose the prompt into phases,
// runs dependency inference, and validates the result.
func Generate(ctx context.Context, invoker agent.Invoker, req GenerateRequest) (*GenerateResult, error) {
	if req.UserPrompt == "" {
		return nil, fmt.Errorf("generate request requires a non-empty user prompt")
	}
	if req.NebulaName == "" {
		return nil, fmt.Errorf("generate request requires a non-empty nebula name")
	}

	// Step 1: Build scaffold manifest.
	manifest := buildManifest(req)

	// Step 2: Build the architect prompt with codebase context.
	archReq := ArchitectRequest{
		Mode:       ArchitectModeGenerate,
		UserPrompt: req.UserPrompt,
		Nebula: &Nebula{
			Dir:      req.OutputDir,
			Manifest: manifest,
		},
		Analysis: req.Analysis,
	}
	prompt, err := buildArchitectPrompt(archReq)
	if err != nil {
		return nil, fmt.Errorf("building generate prompt: %w", err)
	}

	// Step 3: Invoke the architect agent.
	agnt := ArchitectAgent(req.MaxBudgetUSD, req.Model)
	invResult, err := invoker.Invoke(ctx, agnt, prompt, req.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("architect invocation failed: %w", err)
	}

	// Step 4: Parse multi-phase output.
	results, err := parseMultiPhaseOutput(invResult.ResultText)
	if err != nil {
		return nil, fmt.Errorf("parsing multi-phase output: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("architect produced no phases")
	}

	// Step 5: Extract phases and apply defaults.
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

	// Step 6: Run dependency inference to correct the DAG.
	inferrer := &DependencyInferrer{Phases: phases}
	inferResult, err := inferrer.InferDependencies()
	if err != nil {
		return nil, fmt.Errorf("dependency inference failed: %w", err)
	}
	phases = inferResult.Phases
	warnings = append(warnings, inferResult.Warnings...)

	// Step 7: Assemble the complete nebula and validate.
	neb := &Nebula{
		Dir:      req.OutputDir,
		Manifest: manifest,
		Phases:   phases,
	}

	validationErrs := Validate(neb)

	result := &GenerateResult{
		Nebula:      neb,
		Manifest:    manifest,
		Phases:      phases,
		InferResult: inferResult,
		Errors:      warnings,
		CostUSD:     invResult.CostUSD,
	}

	// Step 8: Auto-correct validation errors and retry if needed.
	if len(validationErrs) > 0 {
		result, err = CorrectAndRetry(ctx, invoker, req, result, validationErrs)
		if err != nil {
			return nil, fmt.Errorf("correction failed: %w", err)
		}
	}

	return result, nil
}

// parseMultiPhaseOutput extracts multiple phase files from architect output.
// Each phase is delimited by PHASE_FILE: <filename> ... END_PHASE_FILE markers.
func parseMultiPhaseOutput(output string) ([]*ArchitectResult, error) {
	const (
		startMarker = "PHASE_FILE:"
		endMarker   = "END_PHASE_FILE"
	)

	var results []*ArchitectResult
	remaining := output

	for {
		startIdx := strings.Index(remaining, startMarker)
		if startIdx < 0 {
			break
		}

		// Extract the filename from the PHASE_FILE line.
		afterMarker := remaining[startIdx+len(startMarker):]
		newlineIdx := strings.IndexByte(afterMarker, '\n')
		if newlineIdx < 0 {
			return nil, fmt.Errorf("missing newline after %q marker in phase block %d", startMarker, len(results)+1)
		}
		filename := strings.TrimSpace(afterMarker[:newlineIdx])

		if err := validateFilename(filename); err != nil {
			return nil, fmt.Errorf("invalid filename %q in phase block %d: %w", filename, len(results)+1, err)
		}

		// Find the END_PHASE_FILE marker.
		contentStart := startIdx + len(startMarker) + newlineIdx + 1
		endIdx := strings.Index(remaining[contentStart:], endMarker)
		if endIdx < 0 {
			return nil, fmt.Errorf("missing %q marker for phase %q (block %d)", endMarker, filename, len(results)+1)
		}
		phaseContent := remaining[contentStart : contentStart+endIdx]

		// Parse the frontmatter and body.
		frontmatter, body, err := splitFrontmatter(phaseContent)
		if err != nil {
			return nil, fmt.Errorf("parsing frontmatter for phase %q: %w", filename, err)
		}

		var spec PhaseSpec
		if err := toml.Unmarshal([]byte(frontmatter), &spec); err != nil {
			return nil, fmt.Errorf("parsing TOML for phase %q: %w", filename, err)
		}

		spec.Body = strings.TrimSpace(body)
		spec.SourceFile = filename

		result := &ArchitectResult{
			Filename:  filename,
			PhaseSpec: spec,
			Body:      spec.Body,
		}
		result.Validate()
		results = append(results, result)

		// Advance past the END_PHASE_FILE marker.
		remaining = remaining[contentStart+endIdx+len(endMarker):]
	}

	return results, nil
}

// buildManifest constructs a default Manifest from a GenerateRequest.
func buildManifest(req GenerateRequest) Manifest {
	return Manifest{
		Nebula: Info{
			Name:        req.NebulaName,
			Description: truncateDescription(req.UserPrompt, 200),
		},
		Defaults: Defaults{
			Type:     "task",
			Priority: 2,
			Labels:   []string{"quasar"},
		},
		Execution: Execution{
			MaxWorkers:      2,
			MaxReviewCycles: 5,
			MaxBudgetUSD:    req.MaxBudgetUSD,
			Model:           req.Model,
			Gate:            GateModeReview,
		},
		Context: Context{
			WorkingDir: ".",
			Goals:      []string{req.UserPrompt},
		},
	}
}

// truncateDescription truncates a string to maxLen characters, adding an
// ellipsis if truncation occurs.
func truncateDescription(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
