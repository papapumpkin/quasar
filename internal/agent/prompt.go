package agent

import (
	"strings"
	"time"
)

// CoordinationNote is one sibling-intent advisory injected into a coder's
// volatile prompt suffix before it starts work. The constellation pre-flight
// (internal/constellations.Check) builds these from active entanglements that
// intersect the dispatching phase's scope; AppendCoordinationNotes renders them
// into a `## Coordination notes` block.
//
// The render-relevant fields (Name, Status, SiblingPhaseID, CurrentSignature,
// Advice) drive the prompt block; the provenance fields (SiblingRunID, Kind,
// Package, DeclaredAt, UpdatedAt) are carried for telemetry and audit so the
// operator can see which run/symbol produced a note.
type CoordinationNote struct {
	// What the sibling is doing.
	SiblingRunID     string
	SiblingPhaseID   string
	Kind             string // "func" | "type" | "interface" | ...
	Name             string
	CurrentSignature string

	// What the sibling's intent is.
	Status string // declared | claimed | in_flight | deprecated
	Advice string // human-readable guidance derived from Status

	// Provenance for the operator (telemetry + audit).
	Producer   string
	Package    string
	DeclaredAt time.Time
	UpdatedAt  time.Time
}

// AdviceForStatus maps an entanglement lifecycle status to the one-line advice a
// coder reads in its coordination notes. It is the single source of truth for
// the advice text so the rendered prompt and any telemetry-stamped Advice field
// can never drift. Unknown statuses fall back to a conservative generic note.
func AdviceForStatus(status string) string {
	switch status {
	case "declared":
		return "A sibling phase intends to produce this symbol. Avoid the name collision."
	case "claimed":
		return "A sibling coder has picked up work on this symbol. Coordinate or wait."
	case "in_flight":
		return "Use the current signature shown above."
	case "deprecated":
		return "Do not introduce new uses. Use the replacement noted in the producer's spec."
	default:
		return "A sibling phase is working on this symbol. Treat it as a constraint."
	}
}

// coordinationNotesHeader is the fixed preamble of the `## Coordination notes`
// block. It is a constant so the rendered block is byte-stable for golden tests.
const coordinationNotesHeader = `## Coordination notes
Other phases are currently in flight on symbols that overlap your scope.
Both their work and yours are valid — treat these as constraints, not
optional guidance.
`

// AppendCoordinationNotes appends a deterministic `## Coordination notes` block
// describing the given sibling intents to a coder's user prompt. Notes are
// rendered in the order supplied (the Check supplies them recency-ordered), so
// the most recently-updated intent appears first. An empty slice returns the
// prompt unchanged — no header, no trailing whitespace — so a coder with no
// overlapping siblings sees no difference.
func AppendCoordinationNotes(prompt string, notes []CoordinationNote) string {
	if len(notes) == 0 {
		return prompt
	}

	var b strings.Builder
	b.WriteString(prompt)
	if !strings.HasSuffix(prompt, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(coordinationNotesHeader)

	for _, n := range notes {
		b.WriteString("\n- **")
		b.WriteString(n.Name)
		b.WriteString("** (")
		b.WriteString(n.Status)
		b.WriteString(", phase `")
		b.WriteString(n.SiblingPhaseID)
		b.WriteString("`)\n")
		if n.CurrentSignature != "" {
			b.WriteString("  Current signature: `")
			b.WriteString(n.CurrentSignature)
			b.WriteString("`\n")
		}
		b.WriteString("  Advice: ")
		b.WriteString(adviceText(n))
		b.WriteString("\n")
	}

	return b.String()
}

// adviceText returns the note's explicit Advice when the Check populated it,
// otherwise derives it from Status. This keeps the renderer usable both with
// fully-built notes and with notes carrying only a status.
func adviceText(n CoordinationNote) string {
	if n.Advice != "" {
		return n.Advice
	}
	return AdviceForStatus(n.Status)
}

// FabricProtocol is the coordination protocol injected into agent system
// prompts when fabric-based multi-quasar execution is enabled. It instructs
// the agent how to interact with entanglements, claims, discoveries, and pulses.
const FabricProtocol = `## Fabric Protocol

You are one of several concurrent coders working on this codebase.

BEFORE starting implementation:
  Run: quasar fabric entanglements
  Review the interfaces you must conform to (inbound) and produce (outbound).
  Do not deviate from entangled signatures.

BEFORE modifying any file:
  Run: quasar fabric claim --file <path>
  If the claim fails, STOP and post a discovery:
    quasar discovery --kind file_conflict --detail "<explanation>"

WHEN you complete your task:
  Run: quasar fabric post --from-file <path> --exports
  for every file containing exported interfaces you created or modified.

WHEN you discover an entanglement is wrong or insufficient:
  Run: quasar discovery --kind entanglement_dispute --detail "<explanation>"
  Then STOP and wait for resolution.

WHEN you cannot proceed without a product/requirements decision:
  Run: quasar discovery --kind requirements_ambiguity --detail "<question>"
  Then STOP and wait for resolution.

WHEN you encounter an unexpected issue outside your task scope:
  Run: quasar discovery --kind missing_dependency --detail "<what you need>"
  Then STOP and wait for resolution.

SHARE context with other quasars via pulses:
  Run: quasar pulse emit --kind decision "switched approach because..."
  Run: quasar pulse emit --kind failure "approach X failed because..."
  Run: quasar pulse emit --kind note "important: this function has a subtle nil case"
  Run: quasar pulse emit --kind reviewer_feedback "reviewer said: add context.Context"

RULES:
  - Never modify files you haven't claimed.
  - Never change an entangled interface without posting a discovery.
  - Emit pulses for decisions, failures, and observations that other quasars should know about.
  - Only STOP for genuine blockers. If you're uncertain but can write compilable code, proceed.
`

// PromptOpts controls optional sections appended to the agent system prompt.
type PromptOpts struct {
	FabricEnabled  bool   // When true, the fabric protocol block is appended.
	TaskID         string // Injected as QUASAR_TASK_ID context when non-empty.
	ProjectContext string // Deterministic project snapshot prepended for prompt caching.
}

// BuildSystemPrompt constructs the full system prompt for an agent by
// combining the base prompt with optional sections based on opts.
// The ordering is: [ProjectContext] → [base prompt] → [fabric protocol].
// Project context is placed first because it is stable across all invocations,
// maximizing Anthropic prompt cache hit rates.
func BuildSystemPrompt(basePrompt string, opts PromptOpts) string {
	var b strings.Builder

	if opts.ProjectContext != "" {
		b.WriteString(opts.ProjectContext)
		b.WriteString("\n\n---\n\n")
	}

	b.WriteString(basePrompt)

	if opts.FabricEnabled {
		b.WriteString("\n\n")
		b.WriteString(FabricProtocol)
	}

	return b.String()
}
