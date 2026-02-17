package nebula

import (
	"context"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/aaronsalm/quasar/internal/agent"
)

// ArchitectMode specifies whether the architect is creating a new phase or refactoring an existing one.
type ArchitectMode string

const (
	// ArchitectModeCreate instructs the architect to generate a new phase file.
	ArchitectModeCreate ArchitectMode = "create"
	// ArchitectModeRefactor instructs the architect to update an existing phase file.
	ArchitectModeRefactor ArchitectMode = "refactor"
)

// ArchitectRequest describes what the architect agent should produce.
type ArchitectRequest struct {
	Mode       ArchitectMode // "create" or "refactor"
	UserPrompt string        // what the user wants
	Nebula     *Nebula       // current nebula state for context
	PhaseID    string        // for refactor: which phase to modify
}

// ArchitectResult holds the parsed output from the architect agent.
type ArchitectResult struct {
	Filename  string
	PhaseSpec PhaseSpec
	Body      string
	Errors    []string
}

// Validate checks that the ArchitectResult contains a valid phase specification.
func (r *ArchitectResult) Validate() bool {
	r.Errors = nil
	if r.Filename == "" {
		r.Errors = append(r.Errors, "missing filename")
	}
	if r.PhaseSpec.ID == "" {
		r.Errors = append(r.Errors, "missing phase id")
	}
	if r.PhaseSpec.Title == "" {
		r.Errors = append(r.Errors, "missing phase title")
	}
	return len(r.Errors) == 0
}

// ArchitectAgent returns an Agent configured for the architect role.
func ArchitectAgent(budget float64, model string) agent.Agent {
	return agent.Agent{
		Role:         agent.RoleArchitect,
		SystemPrompt: architectSystemPrompt,
		MaxBudgetUSD: budget,
		Model:        model,
	}
}

// RunArchitect invokes the architect agent and parses its structured output into an ArchitectResult.
// It validates the generated phase against the existing nebula DAG before returning.
func RunArchitect(ctx context.Context, invoker agent.Invoker, req ArchitectRequest) (*ArchitectResult, error) {
	if req.Nebula == nil {
		return nil, fmt.Errorf("architect request requires a non-nil nebula")
	}
	if req.UserPrompt == "" {
		return nil, fmt.Errorf("architect request requires a non-empty user prompt")
	}

	prompt, err := buildArchitectPrompt(req)
	if err != nil {
		return nil, fmt.Errorf("failed to build architect prompt: %w", err)
	}

	agnt := ArchitectAgent(
		req.Nebula.Manifest.Execution.MaxBudgetUSD,
		req.Nebula.Manifest.Execution.Model,
	)

	result, err := invoker.Invoke(ctx, agnt, prompt, req.Nebula.Dir)
	if err != nil {
		return nil, fmt.Errorf("architect invocation failed: %w", err)
	}

	parsed, err := parseArchitectOutput(result.ResultText)
	if err != nil {
		return nil, fmt.Errorf("failed to parse architect output: %w", err)
	}

	// Apply defaults from the manifest.
	applyDefaults(&parsed.PhaseSpec, req.Nebula.Manifest.Defaults)

	// Validate the generated phase against the existing DAG.
	dagErrors := validateAgainstDAG(parsed, req)
	parsed.Errors = append(parsed.Errors, dagErrors...)

	return parsed, nil
}

// buildArchitectPrompt constructs the full prompt sent to the architect agent,
// including nebula context and mode-specific instructions.
func buildArchitectPrompt(req ArchitectRequest) (string, error) {
	var b strings.Builder

	b.WriteString("# Nebula Context\n\n")

	// Write project info.
	manifest := req.Nebula.Manifest
	fmt.Fprintf(&b, "**Nebula**: %s\n", manifest.Nebula.Name)
	if manifest.Nebula.Description != "" {
		fmt.Fprintf(&b, "**Description**: %s\n", manifest.Nebula.Description)
	}
	b.WriteString("\n")

	// Write goals and constraints.
	if len(manifest.Context.Goals) > 0 {
		b.WriteString("**Goals**:\n")
		for _, g := range manifest.Context.Goals {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		b.WriteString("\n")
	}
	if len(manifest.Context.Constraints) > 0 {
		b.WriteString("**Constraints**:\n")
		for _, c := range manifest.Context.Constraints {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}

	// Write existing phases.
	b.WriteString("## Existing Phases\n\n")
	if len(req.Nebula.Phases) == 0 {
		b.WriteString("No phases exist yet.\n\n")
	} else {
		for _, p := range req.Nebula.Phases {
			deps := "none"
			if len(p.DependsOn) > 0 {
				deps = strings.Join(p.DependsOn, ", ")
			}
			fmt.Fprintf(&b, "- **%s** (`%s`): %s [depends_on: %s]\n", p.Title, p.ID, p.Type, deps)
		}
		b.WriteString("\n")
	}

	// Write defaults.
	b.WriteString("## Defaults\n\n")
	fmt.Fprintf(&b, "- type: %s\n", manifest.Defaults.Type)
	fmt.Fprintf(&b, "- priority: %d\n", manifest.Defaults.Priority)
	b.WriteString("\n")

	// Mode-specific section.
	switch req.Mode {
	case ArchitectModeCreate:
		b.WriteString("## Task: Create a New Phase\n\n")
		fmt.Fprintf(&b, "User request: %s\n\n", req.UserPrompt)
		b.WriteString("Generate a new phase file based on the user's request. ")
		b.WriteString("Choose appropriate `depends_on` based on the existing phases above.\n\n")
	case ArchitectModeRefactor:
		b.WriteString("## Task: Refactor an Existing Phase\n\n")
		fmt.Fprintf(&b, "Phase to refactor: `%s`\n\n", req.PhaseID)

		// Find the existing phase body.
		for _, p := range req.Nebula.Phases {
			if p.ID == req.PhaseID {
				b.WriteString("### Current Phase Body\n\n")
				b.WriteString(p.Body)
				b.WriteString("\n\n")
				break
			}
		}

		fmt.Fprintf(&b, "User change request: %s\n\n", req.UserPrompt)
		b.WriteString("Produce an updated phase file that incorporates the user's feedback while preserving relevant context.\n\n")
	default:
		return "", fmt.Errorf("unknown architect mode: %q", req.Mode)
	}

	return b.String(), nil
}

// parseArchitectOutput extracts the phase file from the architect's structured output.
// Expected format:
//
//	PHASE_FILE: <filename>
//	+++
//	<TOML frontmatter>
//	+++
//
//	<markdown body>
//	END_PHASE_FILE
func parseArchitectOutput(output string) (*ArchitectResult, error) {
	const (
		startMarker = "PHASE_FILE:"
		endMarker   = "END_PHASE_FILE"
	)

	// Find the PHASE_FILE line.
	startIdx := strings.Index(output, startMarker)
	if startIdx < 0 {
		return nil, fmt.Errorf("architect output missing %q marker", startMarker)
	}

	// Extract the filename from the PHASE_FILE line.
	afterMarker := output[startIdx+len(startMarker):]
	newlineIdx := strings.IndexByte(afterMarker, '\n')
	if newlineIdx < 0 {
		return nil, fmt.Errorf("architect output missing newline after %q", startMarker)
	}
	filename := strings.TrimSpace(afterMarker[:newlineIdx])

	// Sanitize the filename to prevent path traversal.
	if err := validateFilename(filename); err != nil {
		return nil, fmt.Errorf("invalid filename %q: %w", filename, err)
	}

	// Extract content between the PHASE_FILE line and END_PHASE_FILE.
	contentStart := startIdx + len(startMarker) + newlineIdx + 1
	endIdx := strings.Index(output[contentStart:], endMarker)
	if endIdx < 0 {
		return nil, fmt.Errorf("architect output missing %q marker", endMarker)
	}
	phaseContent := output[contentStart : contentStart+endIdx]

	// Parse the phase content using the existing frontmatter parser.
	frontmatter, body, err := splitFrontmatter(phaseContent)
	if err != nil {
		return nil, fmt.Errorf("parsing architect output frontmatter: %w", err)
	}

	var spec PhaseSpec
	if err := toml.Unmarshal([]byte(frontmatter), &spec); err != nil {
		return nil, fmt.Errorf("parsing architect TOML: %w", err)
	}

	spec.Body = strings.TrimSpace(body)
	spec.SourceFile = filename

	result := &ArchitectResult{
		Filename:  filename,
		PhaseSpec: spec,
		Body:      spec.Body,
	}

	result.Validate()
	return result, nil
}

// validateFilename rejects filenames that contain path traversal components or
// directory separators, and ensures the filename ends with ".md".
func validateFilename(name string) error {
	if name == "" {
		return fmt.Errorf("filename is empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("filename must not contain path separators")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("filename must not contain '..' components")
	}
	if !strings.HasSuffix(name, ".md") {
		return fmt.Errorf("filename must end with .md")
	}
	return nil
}

// applyDefaults fills in zero-valued fields from the manifest defaults.
func applyDefaults(spec *PhaseSpec, defaults Defaults) {
	if spec.Type == "" {
		spec.Type = defaults.Type
	}
	if spec.Priority == 0 {
		spec.Priority = defaults.Priority
	}
	if len(spec.Labels) == 0 && len(defaults.Labels) > 0 {
		spec.Labels = make([]string, len(defaults.Labels))
		copy(spec.Labels, defaults.Labels)
	}
	if spec.Assignee == "" {
		spec.Assignee = defaults.Assignee
	}
}

// validateAgainstDAG checks that the generated phase's dependencies are valid
// and won't create cycles in the existing DAG.
func validateAgainstDAG(result *ArchitectResult, req ArchitectRequest) []string {
	var errs []string

	existingIDs := make(map[string]bool, len(req.Nebula.Phases))
	for _, p := range req.Nebula.Phases {
		existingIDs[p.ID] = true
	}

	// For refactor mode, remove the refactored phase's ID from existingIDs
	// so that ValidateHotAdd does not flag it as a duplicate.
	if req.Mode == ArchitectModeRefactor {
		delete(existingIDs, req.PhaseID)
	}

	// Validate that all depends_on targets exist (or will exist).
	for _, dep := range result.PhaseSpec.DependsOn {
		if !existingIDs[dep] && dep != result.PhaseSpec.ID {
			errs = append(errs, fmt.Sprintf("dependency %q does not exist", dep))
		}
	}

	// Check for duplicates and cycles using the graph.
	graph := NewGraph(req.Nebula.Phases)
	// For refactor mode, remove the old phase from the graph so it can be re-added cleanly.
	if req.Mode == ArchitectModeRefactor {
		graph.RemoveNode(req.PhaseID)
	}
	validationErrs := ValidateHotAdd(result.PhaseSpec, existingIDs, graph)
	for _, ve := range validationErrs {
		errs = append(errs, ve.Error())
	}

	return errs
}

// architectSystemPrompt is the system prompt for the architect agent.
const architectSystemPrompt = `You are a nebula phase architect. Your job is to create and refactor phase files
for a multi-phase AI coding orchestration system called "quasar nebula."

## Phase File Format

Phase files are Markdown files with TOML frontmatter between +++ delimiters.

### Required Frontmatter Fields
- id: A kebab-case identifier (e.g., "implement-auth", "fix-parsing-bug")
- title: A human-readable title for the phase

### Optional Frontmatter Fields
- type: Phase type (default inherited from nebula config)
- priority: Integer priority (default inherited from nebula config)
- depends_on: Array of phase IDs this phase depends on
- max_review_cycles: Override for max review cycles
- max_budget_usd: Override for max budget
- model: Override for AI model
- blocks: Array of phase IDs this phase should block (reverse dependency)
- scope: Array of glob patterns for files this phase owns

### Markdown Body
The body should contain:
- **Problem**: What needs to be done and why
- **Solution**: How to approach it
- **Files to Modify**: Which files need changes (if known)
- **Acceptance Criteria**: How to verify the work is complete

## Output Format

You MUST output exactly one phase file in this format:

PHASE_FILE: <filename.md>
+++
id = "<phase-id>"
title = "<Phase Title>"
depends_on = [<list of dependency IDs if any>]
+++

<markdown body>
END_PHASE_FILE

## Rules

1. Use kebab-case for phase IDs and filenames
2. Analyze existing phases to choose appropriate depends_on values
3. Only depend on phases that must complete before this one can start
4. Keep phase descriptions focused and actionable
5. The filename should be descriptive (e.g., "implement-user-auth.md")
6. For refactors, preserve the original phase ID unless explicitly asked to change it
7. Do not include fields that match the defaults — only override when necessary
`
