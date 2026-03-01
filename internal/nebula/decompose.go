package nebula

import (
	"context"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/agent"
)

// DecomposeFinding is a minimal finding representation used for decomposition context.
// It avoids importing internal/loop to prevent circular dependencies.
type DecomposeFinding struct {
	Severity    string
	Description string
	Cycle       int
}

// DecomposeResult holds the output of a decomposition architect invocation.
type DecomposeResult struct {
	OriginalPhaseID string
	SubPhases       []ArchitectResult // 2-3 sub-phases
	Errors          []string
}

// decomposeSystemPrompt instructs the architect to decompose a struggling phase.
const decomposeSystemPrompt = `You are a nebula phase architect specializing in decomposition.
Your job is to take a struggling phase that cannot be completed as-is, and split it into
2-3 smaller, more tractable sub-phases.

## Phase File Format

Phase files are Markdown with TOML frontmatter between +++ delimiters.
Required fields: id (kebab-case), title (human-readable).
Optional fields: type, priority, depends_on, max_review_cycles, max_budget_usd, model, blocks, scope.
The body should contain: Problem, Solution, Files, and Acceptance Criteria sections.

## Output Format

Produce exactly 2 or 3 sub-phases using this format:

PHASE_FILE: <filename.md>
+++
id = "<phase-id>"
title = "<Phase Title>"
depends_on = [<dependency IDs>]
scope = [<glob patterns for owned files>]
+++

<markdown body>
END_PHASE_FILE

## Decomposition Rules

1. Produce exactly 2 or 3 sub-phases — not 1, not 4 or more.
2. Each sub-phase ID MUST be prefixed with the original phase ID (e.g., "original-id-part-1").
3. Use kebab-case for phase IDs and filenames.
4. Sub-phases may declare depends_on edges among themselves if ordering matters.
5. The union of all sub-phase acceptance criteria MUST cover the original phase's criteria.
6. Each sub-phase must be a coherent, self-contained unit of work for one coder-reviewer cycle.
7. Keep descriptions focused and actionable.
8. Do not include fields that match defaults — only override when necessary.
`

// RunDecompose invokes the architect in decompose mode and parses the output
// into multiple sub-phases. It validates that 2-3 phases are produced and that
// their IDs are prefixed with the original phase ID.
func RunDecompose(ctx context.Context, invoker agent.Invoker, req ArchitectRequest) (*DecomposeResult, error) {
	if req.Nebula == nil {
		return nil, fmt.Errorf("decompose request requires a non-nil nebula")
	}
	if req.PhaseID == "" {
		return nil, fmt.Errorf("decompose request requires a non-empty phase ID")
	}

	prompt, err := buildDecomposePrompt(req)
	if err != nil {
		return nil, fmt.Errorf("building decompose prompt: %w", err)
	}

	agnt := agent.Agent{
		Role:         agent.RoleArchitect,
		SystemPrompt: decomposeSystemPrompt,
		MaxBudgetUSD: req.Nebula.Manifest.Execution.MaxBudgetUSD,
		Model:        req.Nebula.Manifest.Execution.Model,
	}

	result, err := invoker.Invoke(ctx, agnt, prompt, req.Nebula.Dir)
	if err != nil {
		return nil, fmt.Errorf("decompose invocation failed: %w", err)
	}

	parsed, err := parseDecomposeOutput(result.ResultText)
	if err != nil {
		return nil, fmt.Errorf("parsing decompose output: %w", err)
	}

	decomp := &DecomposeResult{
		OriginalPhaseID: req.PhaseID,
		SubPhases:       parsed,
	}

	// Validate sub-phase count.
	if len(decomp.SubPhases) < 2 {
		return nil, fmt.Errorf("decomposition produced %d sub-phases, need at least 2", len(decomp.SubPhases))
	}
	if len(decomp.SubPhases) > 3 {
		return nil, fmt.Errorf("decomposition produced %d sub-phases, maximum is 3", len(decomp.SubPhases))
	}

	// Apply defaults and validate each sub-phase.
	subIDs := make(map[string]bool, len(decomp.SubPhases))
	for i := range decomp.SubPhases {
		sp := &decomp.SubPhases[i]
		applyDefaults(&sp.PhaseSpec, req.Nebula.Manifest.Defaults)

		// Validate ID prefix.
		if !strings.HasPrefix(sp.PhaseSpec.ID, req.PhaseID+"-") {
			decomp.Errors = append(decomp.Errors,
				fmt.Sprintf("sub-phase %q is not prefixed with %q", sp.PhaseSpec.ID, req.PhaseID+"-"))
		}
		subIDs[sp.PhaseSpec.ID] = true
	}

	// Validate internal dependencies: depends_on targets must either exist in
	// the nebula or refer to a sibling sub-phase.
	existingIDs := make(map[string]bool, len(req.Nebula.Phases))
	for _, p := range req.Nebula.Phases {
		existingIDs[p.ID] = true
	}
	for _, sp := range decomp.SubPhases {
		for _, dep := range sp.PhaseSpec.DependsOn {
			if !existingIDs[dep] && !subIDs[dep] {
				decomp.Errors = append(decomp.Errors,
					fmt.Sprintf("sub-phase %q depends on unknown phase %q", sp.PhaseSpec.ID, dep))
			}
		}
	}

	// Validate against the existing DAG (non-fatal).
	d, _ := phasesToDAG(req.Nebula.Phases)
	_ = d.Remove(req.PhaseID) // Remove original phase so sub-phases can replace it.
	for _, sp := range decomp.SubPhases {
		dagErrors := ValidateHotAdd(sp.PhaseSpec, existingIDs, d)
		for _, ve := range dagErrors {
			decomp.Errors = append(decomp.Errors, ve.Error())
		}
	}

	return decomp, nil
}

// buildDecomposePrompt constructs the user prompt for the decompose architect.
func buildDecomposePrompt(req ArchitectRequest) (string, error) {
	var b strings.Builder

	b.WriteString("# Decomposition Request\n\n")

	// Original phase context.
	b.WriteString("## Original Phase\n\n")
	fmt.Fprintf(&b, "**Phase ID**: `%s`\n\n", req.PhaseID)

	if p, ok := PhasesByID(req.Nebula.Phases)[req.PhaseID]; ok {
		fmt.Fprintf(&b, "**Title**: %s\n", p.Title)
		fmt.Fprintf(&b, "**Type**: %s\n", p.Type)
		fmt.Fprintf(&b, "**Priority**: %d\n\n", p.Priority)

		if len(p.DependsOn) > 0 {
			fmt.Fprintf(&b, "**Dependencies**: %s\n\n", strings.Join(p.DependsOn, ", "))
		}

		if p.Body != "" {
			b.WriteString("### Phase Body\n\n")
			b.WriteString(p.Body)
			b.WriteString("\n\n")
		}
	}

	// Struggle context.
	b.WriteString("## Struggle Context\n\n")
	b.WriteString("This phase is being decomposed because it is struggling to complete.\n\n")

	if req.StruggleReason != "" {
		fmt.Fprintf(&b, "**Reason**: %s\n\n", req.StruggleReason)
	}

	fmt.Fprintf(&b, "**Cycles used**: %d\n", req.CyclesUsed)
	fmt.Fprintf(&b, "**Cost so far**: $%.2f\n\n", req.CostSoFar)

	// Accumulated findings.
	if len(req.AllFindings) > 0 {
		b.WriteString("### Accumulated Review Findings\n\n")
		for _, f := range req.AllFindings {
			fmt.Fprintf(&b, "- [%s] (cycle %d): %s\n", f.Severity, f.Cycle, f.Description)
		}
		b.WriteString("\n")
	}

	// Nebula context.
	b.WriteString("## Nebula Context\n\n")
	manifest := req.Nebula.Manifest
	fmt.Fprintf(&b, "**Nebula**: %s\n", manifest.Nebula.Name)
	if manifest.Nebula.Description != "" {
		fmt.Fprintf(&b, "**Description**: %s\n", manifest.Nebula.Description)
	}
	b.WriteString("\n")

	// Existing phases for dependency awareness.
	b.WriteString("### Existing Phases\n\n")
	if len(req.Nebula.Phases) == 0 {
		b.WriteString("No other phases exist.\n\n")
	} else {
		for _, p := range req.Nebula.Phases {
			if p.ID == req.PhaseID {
				continue // skip the phase being decomposed
			}
			deps := "none"
			if len(p.DependsOn) > 0 {
				deps = strings.Join(p.DependsOn, ", ")
			}
			fmt.Fprintf(&b, "- **%s** (`%s`): %s [depends_on: %s]\n", p.Title, p.ID, p.Type, deps)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Instructions\n\n")
	b.WriteString("Decompose the original phase into exactly 2 or 3 smaller sub-phases.\n")
	b.WriteString("Each sub-phase ID must be prefixed with the original phase ID.\n")
	b.WriteString("The union of sub-phase acceptance criteria must cover the original phase's criteria.\n")
	b.WriteString("Output ALL sub-phases using the PHASE_FILE/END_PHASE_FILE format.\n\n")

	return b.String(), nil
}

// parseDecomposeOutput splits multi-phase PHASE_FILE/END_PHASE_FILE-delimited
// architect output into individual ArchitectResult values.
func parseDecomposeOutput(raw string) ([]ArchitectResult, error) {
	const (
		startMarker = "PHASE_FILE:"
		endMarker   = "END_PHASE_FILE"
	)

	var results []ArchitectResult

	remaining := raw
	for {
		startIdx := strings.Index(remaining, startMarker)
		if startIdx < 0 {
			break
		}

		// Extract filename from the PHASE_FILE line.
		afterMarker := remaining[startIdx+len(startMarker):]
		newlineIdx := strings.IndexByte(afterMarker, '\n')
		if newlineIdx < 0 {
			return nil, fmt.Errorf("decompose output missing newline after %q", startMarker)
		}
		filename := strings.TrimSpace(afterMarker[:newlineIdx])

		if err := validateFilename(filename); err != nil {
			return nil, fmt.Errorf("invalid filename %q: %w", filename, err)
		}

		// Extract content between PHASE_FILE line and END_PHASE_FILE.
		contentStart := startIdx + len(startMarker) + newlineIdx + 1
		endIdx := strings.Index(remaining[contentStart:], endMarker)
		if endIdx < 0 {
			return nil, fmt.Errorf("decompose output missing %q marker for %q", endMarker, filename)
		}
		phaseContent := remaining[contentStart : contentStart+endIdx]

		// Parse using existing frontmatter parser.
		frontmatter, body, err := splitFrontmatter(phaseContent)
		if err != nil {
			return nil, fmt.Errorf("parsing frontmatter for %q: %w", filename, err)
		}

		var spec PhaseSpec
		if err := toml.Unmarshal([]byte(frontmatter), &spec); err != nil {
			return nil, fmt.Errorf("parsing TOML for %q: %w", filename, err)
		}

		spec.Body = strings.TrimSpace(body)
		spec.SourceFile = filename

		result := ArchitectResult{
			Filename:  filename,
			PhaseSpec: spec,
			Body:      spec.Body,
		}
		result.Validate()
		results = append(results, result)

		// Advance past the END_PHASE_FILE marker.
		remaining = remaining[contentStart+endIdx+len(endMarker):]
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("decompose output contains no %q markers", startMarker)
	}

	return results, nil
}
