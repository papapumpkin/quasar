package chat

import (
	"fmt"
	"strings"
)

// PhaseContext holds the execution state for a phase, used by ContextBuilder
// to assemble a context-aware system message for chat conversations.
type PhaseContext struct {
	PhaseID          string
	PhaseSpec        string // markdown content from the phase file
	Cycle            int
	MaxCycles        int
	LastSummary      string // extracted summary from the last agent output
	DiffStat         string // formatted diff stat (e.g. "+10 -3 across 4 files")
	ReviewerFindings string // last reviewer output/findings
	FileClaims       []string
}

// ContextBuilder assembles a structured context blob from phase execution
// state. The output is a formatted system message that gives the AI full
// situational awareness about a running phase.
type ContextBuilder struct{}

// NewContextBuilder creates a ContextBuilder.
func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{}
}

// Build assembles a PhaseContext into a formatted system message string.
// The message is structured with labeled sections so the AI can parse
// the execution state and provide informed feedback.
func (cb *ContextBuilder) Build(pc PhaseContext) string {
	var b strings.Builder

	b.WriteString("# Phase Execution Context\n\n")
	b.WriteString(fmt.Sprintf("**Phase ID:** %s\n", pc.PhaseID))

	if pc.MaxCycles > 0 {
		b.WriteString(fmt.Sprintf("**Cycle:** %d / %d\n", pc.Cycle, pc.MaxCycles))
	} else if pc.Cycle > 0 {
		b.WriteString(fmt.Sprintf("**Cycle:** %d\n", pc.Cycle))
	}

	if pc.PhaseSpec != "" {
		b.WriteString("\n## Phase Specification\n\n")
		b.WriteString(pc.PhaseSpec)
		b.WriteString("\n")
	}

	if pc.LastSummary != "" {
		b.WriteString("\n## Last Agent Summary\n\n")
		b.WriteString(pc.LastSummary)
		b.WriteString("\n")
	}

	if pc.DiffStat != "" {
		b.WriteString("\n## Current Diff\n\n")
		b.WriteString(pc.DiffStat)
		b.WriteString("\n")
	}

	if pc.ReviewerFindings != "" {
		b.WriteString("\n## Reviewer Findings\n\n")
		b.WriteString(pc.ReviewerFindings)
		b.WriteString("\n")
	}

	if len(pc.FileClaims) > 0 {
		b.WriteString("\n## File Claims\n\n")
		for _, f := range pc.FileClaims {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
	}

	b.WriteString("\n---\n")
	b.WriteString("You are assisting a developer who is monitoring this phase. ")
	b.WriteString("Use the context above to provide informed feedback. ")
	b.WriteString("If the developer asks you to change direction, provide concrete suggestions ")
	b.WriteString("they can relay to the agent.\n")

	return b.String()
}
