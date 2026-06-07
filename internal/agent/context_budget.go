package agent

import "strings"

// Context-budget defaults. They mirror the per-star [context_budget] TOML
// block and are deliberately conservative (see docs and the default coder
// star). Reviewers get a larger tool-result cap because they must see fuller
// context to judge a change; only the architect needs the whole nebula.
const (
	defaultMaxReadsBeforeEdit  = 8
	defaultMaxGrepsBeforeEdit  = 6
	defaultMaxTotalReads       = 30
	coderToolResultMaxBytes    = 16 * 1024
	reviewerToolResultMaxBytes = 32 * 1024
)

// ContextBudget is the per-role/per-star configuration that bounds how much
// context an agent invocation consumes. It is populated from the star's
// [context_budget] TOML frontmatter, falling back to BudgetForRole defaults.
type ContextBudget struct {
	// MaxReadsBeforeEdit is the soft Read-thrash limit before an Edit.
	MaxReadsBeforeEdit int `toml:"max_reads_before_edit"`
	// MaxGrepsBeforeEdit is the soft Grep-thrash limit before an Edit.
	MaxGrepsBeforeEdit int `toml:"max_greps_before_edit"`
	// MaxTotalReads is the hard cap on Reads per invocation.
	MaxTotalReads int `toml:"max_total_reads"`
	// ToolResultMaxBytes caps the size of a single tool result reaching the
	// agent (see claude.TruncationPolicy).
	ToolResultMaxBytes int `toml:"tool_result_max_bytes"`
	// IncludeSiblingPhases controls phase-spec injection. When false (the
	// coder/reviewer default) only the current phase's spec is injected; when
	// true (architect) every phase is included.
	IncludeSiblingPhases bool `toml:"include_sibling_phases"`
	// EnableToolHook, when true, instructs the invoker to install a Claude CLI
	// PreToolUse hook that enforces the soft/hard Read/Grep caps on the live
	// subprocess tool stream. It defaults to false because the hook depends on
	// the Claude CLI's hook contract, which the conservative default avoids
	// relying on. The soft/hard caps above still apply once it is enabled.
	EnableToolHook bool `toml:"enable_tool_hook"`
	// ResultIsStructured marks an invocation whose result is consumed as a whole
	// structured payload (e.g. the reviewer/master-reviewer JSON decision parsed
	// by the reviewer_decision builtin). Such results MUST NOT be byte-truncated:
	// inserting a head+tail marker into the middle of a JSON document produces
	// invalid JSON and hard-fails the downstream decode. When true the invoker
	// bypasses tool-result truncation entirely, trading a larger handoff for the
	// guarantee that the payload still parses. Prose/diff results (the coder)
	// leave this false so they remain bounded.
	ResultIsStructured bool `toml:"result_is_structured"`
}

// RoleNeedsFullNebula reports whether a role must see every phase spec rather
// than only the current phase. Only the architect, which authors and refactors
// the whole plan, needs the full nebula; coder and reviewer act on one phase.
func RoleNeedsFullNebula(role Role) bool {
	return role == RoleArchitect
}

// BudgetForRole returns the default ContextBudget for a role. Unknown roles
// receive the (most restrictive) coder defaults.
func BudgetForRole(role Role) ContextBudget {
	b := ContextBudget{
		MaxReadsBeforeEdit:   defaultMaxReadsBeforeEdit,
		MaxGrepsBeforeEdit:   defaultMaxGrepsBeforeEdit,
		MaxTotalReads:        defaultMaxTotalReads,
		ToolResultMaxBytes:   coderToolResultMaxBytes,
		IncludeSiblingPhases: RoleNeedsFullNebula(role),
	}
	if role == RoleReviewer {
		b.ToolResultMaxBytes = reviewerToolResultMaxBytes
		// The reviewer's result is a strict JSON decision consumed whole by the
		// reviewer_decision builtin, so it must never be byte-truncated.
		b.ResultIsStructured = true
	}
	return b
}

// PhaseContextInput is the data RenderPhaseContext assembles into an agent's
// volatile-suffix phase context.
type PhaseContextInput struct {
	// Role determines whether sibling phases are included (architect) or
	// elided (coder/reviewer).
	Role Role
	// Goals and Constraints are the nebula's short, global context.
	Goals       []string
	Constraints []string
	// CurrentPhase is the spec/body of the phase being worked.
	CurrentPhase string
	// SiblingPhases holds the specs of every other phase. They are included
	// only when RoleNeedsFullNebula(Role) is true.
	SiblingPhases []string
}

// RenderPhaseContext builds the phase-context block injected into an agent's
// user prompt. For coder/reviewer invocations only the current phase's spec is
// included — sibling specs are elided because the agent cannot touch them,
// saving 25-40% of per-cycle input tokens. Architect invocations include every
// phase so the planner can reason about the whole nebula.
//
// When there are no goals, constraints, or sibling phases to add, the current
// phase body is returned verbatim so callers see no formatting overhead.
func RenderPhaseContext(in PhaseContextInput) string {
	includeSiblings := RoleNeedsFullNebula(in.Role) && len(in.SiblingPhases) > 0
	if len(in.Goals) == 0 && len(in.Constraints) == 0 && !includeSiblings {
		return in.CurrentPhase
	}

	var b strings.Builder
	if len(in.Goals) > 0 || len(in.Constraints) > 0 {
		b.WriteString("PROJECT CONTEXT:\n")
		writeBullets(&b, "Goals", in.Goals)
		writeBullets(&b, "Constraints", in.Constraints)
		b.WriteString("\n")
	}

	b.WriteString("PHASE:\n")
	b.WriteString(in.CurrentPhase)

	if includeSiblings {
		b.WriteString("\n\nOTHER PHASES:\n")
		for _, s := range in.SiblingPhases {
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// writeBullets appends a "Label:\n- item\n" block when items is non-empty.
func writeBullets(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(label)
	b.WriteString(":\n")
	for _, it := range items {
		b.WriteString("- ")
		b.WriteString(it)
		b.WriteString("\n")
	}
}
