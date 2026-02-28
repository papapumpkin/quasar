package agent

import "strings"

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
