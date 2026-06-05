package agent

import (
	"crypto/sha256"
	"fmt"
)

// PromptZone classifies where content belongs in the prompt layout
// for maximum cache effectiveness with Anthropic's prompt caching.
//
// Anthropic caches from the beginning of the system prompt, so all stable
// content must form a contiguous prefix. Any byte change in the prefix
// invalidates the cache for everything after it.
type PromptZone int

const (
	// ZoneStablePrefix is content placed in the system prompt that remains
	// byte-identical across all invocations within a phase. This is the
	// primary cache target — Anthropic offers a 90% discount on cached
	// input tokens.
	//
	// Content in this zone (in order):
	//   1. ProjectContext — deterministic repo snapshot from Scanner.Scan()
	//   2. basePrompt — CoderPrompt or ReviewPrompt, set once at loop init
	//   3. FabricProtocol — static instructions for fabric interaction
	//
	// Cross-phase caching: ProjectContext is identical across all phases in
	// a nebula run because it derives from the same repo state. This means
	// the leading bytes of the system prompt are shared across phases,
	// enabling cross-phase cache hits.
	ZoneStablePrefix PromptZone = iota

	// ZoneVolatileSuffix is content placed in the user prompt (-p flag)
	// that changes between cycles or invocations. This content is never
	// cached and must not appear in the system prompt.
	//
	// Content in this zone:
	//   1. Task description and bead ID
	//   2. Reviewer findings ([]ReviewFinding) from previous cycle
	//   3. Coder output summary from previous cycle
	//   4. Lint output from lint pass
	//   5. Filter output from pre-reviewer checks
	//   6. Fabric snapshot (fabric.RenderSnapshot) — claims, pulses, phase states
	ZoneVolatileSuffix
)

// String returns the human-readable name of the zone.
func (z PromptZone) String() string {
	switch z {
	case ZoneStablePrefix:
		return "stable-prefix"
	case ZoneVolatileSuffix:
		return "volatile-suffix"
	default:
		return fmt.Sprintf("unknown-zone(%d)", int(z))
	}
}

// ContentLabels enumerates the well-known content labels used in PromptManifest.Zone.
const (
	// Stable prefix labels.
	LabelProjectContext = "project-context"
	LabelBasePrompt     = "base-prompt"
	LabelFabricProtocol = "fabric-protocol"

	// Volatile suffix labels.
	LabelTaskDescription  = "task-description"
	LabelReviewerFindings = "reviewer-findings"
	LabelCoderOutput      = "coder-output"
	LabelLintOutput       = "lint-output"
	LabelFilterOutput     = "filter-output"
	LabelFabricSnapshot   = "fabric-snapshot"
)

// ContentZone maps well-known content labels to their designated zone.
// This serves as the authoritative classification for prompt content placement.
var ContentZone = map[string]PromptZone{
	// Stable prefix — system prompt.
	LabelProjectContext: ZoneStablePrefix,
	LabelBasePrompt:     ZoneStablePrefix,
	LabelFabricProtocol: ZoneStablePrefix,

	// Volatile suffix — user prompt.
	LabelTaskDescription:  ZoneVolatileSuffix,
	LabelReviewerFindings: ZoneVolatileSuffix,
	LabelCoderOutput:      ZoneVolatileSuffix,
	LabelLintOutput:       ZoneVolatileSuffix,
	LabelFilterOutput:     ZoneVolatileSuffix,
	LabelFabricSnapshot:   ZoneVolatileSuffix,
}

// PromptManifest records the content placement for a single agent invocation.
// It is used for telemetry and debugging prompt cache behavior.
type PromptManifest struct {
	// SystemPromptHash is the SHA-256 hex digest of the full system prompt.
	// When this hash is identical between two invocations, the system prompt
	// prefix is byte-identical and eligible for prompt caching.
	SystemPromptHash string

	// UserPromptLen is the length of the user prompt in bytes.
	UserPromptLen int

	// Zone maps each content label to its designated PromptZone.
	// This records what content was placed where for a given invocation,
	// enabling verification that stable content stayed in the system prompt
	// and volatile content stayed in the user prompt.
	Zone map[string]PromptZone
}

// NewPromptManifest creates a PromptManifest from a system prompt string and
// user prompt length, pre-populated with the canonical zone classification.
func NewPromptManifest(systemPrompt string, userPromptLen int) PromptManifest {
	h := sha256.Sum256([]byte(systemPrompt))
	zone := make(map[string]PromptZone, len(ContentZone))
	for k, v := range ContentZone {
		zone[k] = v
	}
	return PromptManifest{
		SystemPromptHash: fmt.Sprintf("%x", h),
		UserPromptLen:    userPromptLen,
		Zone:             zone,
	}
}
