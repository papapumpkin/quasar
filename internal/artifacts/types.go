package artifacts

import "time"

// NodeType classifies a constellation node by what the runtime does when it
// reaches that node. The directory-as-discriminator principle stops at the
// file level; inside a constellation, the node's type field selects behavior.
type NodeType string

// Constellation node types. A star node invokes an agent; a constellation node
// runs a named sub-constellation; a phase_iterator fans a sub-constellation out
// across the nebula's phases; a builtin runs a Go-implemented runtime op.
const (
	NodeStar          NodeType = "star"
	NodeConstellation NodeType = "constellation"
	NodePhaseIterator NodeType = "phase_iterator"
	NodeBuiltin       NodeType = "builtin"
)

// Reserved edge targets. These are terminal pseudo-nodes the runtime
// recognizes rather than ordinary node IDs, so cycle detection and
// unknown-node checks must exempt them.
const (
	TermDone          = "_done"
	TermFailed        = "_failed"
	TermAwaitingHuman = "_awaiting_human"
	TermPaused        = "_paused"
)

// IsTerminal reports whether target names a reserved terminal pseudo-node
// rather than an ordinary constellation node.
func IsTerminal(target string) bool {
	switch target {
	case TermDone, TermFailed, TermAwaitingHuman, TermPaused:
		return true
	default:
		return false
	}
}

// Star is a fully resolved agent definition: a model, its tool allow/deny
// lists, defaults, and a prompt. The prompt is the concatenation of every
// referenced skill's fragment followed by the star's own Markdown body, and
// Tools.Allowed is the union of the star's base allowlist with each skill's
// tools_add. Resolution happens at load time so the runtime sees a flat Star.
type Star struct {
	Name          string
	Model         string
	FallbackModel string
	Skills        []string // skill names referenced in frontmatter, pre-resolution
	Tools         StarTools
	Defaults      StarDefaults
	ContextBudget StarContextBudget
	Health        StarHealthPolicy
	Prompt        string // skill fragments + star body
	SourcePath    string // for error reporting
}

// StarHealthPolicy overrides the dead-coder healthcheck thresholds for a star,
// parsed from its [health] frontmatter block. Zero values mean "unset"; the
// runtime falls back to claude.DefaultHealthPolicy for any unset field.
// Durations are authored as Go duration strings (e.g. "25m", "90s").
type StarHealthPolicy struct {
	WallClockCap         time.Duration // absolute lifetime cap
	FileWriteIdleCap     time.Duration // longest quiet stretch under the workdir
	TokenRateFloor       float64       // min stream-token rate (tokens/sec)
	TokenRateWindow      time.Duration // averaging window for the token rate
	ToolCallRatioCeiling float64       // max Read:Edit ratio
	ToolCallWindow       int           // recent tool calls feeding the ratio
	CPUIdleCap           time.Duration // longest sub-1%-CPU stretch
}

// StarContextBudget bounds how much context a star's invocation consumes. It
// is parsed from the star's [context_budget] frontmatter block. Zero values
// mean "unset"; callers fall back to per-role defaults (see
// agent.BudgetForRole).
type StarContextBudget struct {
	MaxReadsBeforeEdit   int  // soft Read-thrash limit before an Edit
	MaxGrepsBeforeEdit   int  // soft Grep-thrash limit before an Edit
	MaxTotalReads        int  // hard cap on Reads per invocation
	ToolResultMaxBytes   int  // cap on a single tool result's size in bytes
	IncludeSiblingPhases bool // when true, inject every phase spec (architect)
	EnableToolHook       bool // when true, enforce Read/Grep caps via a CLI PreToolUse hook
}

// StarTools is a star's tool permission policy.
type StarTools struct {
	Allowed []string
	Denied  []string
}

// StarDefaults holds per-invocation defaults a star applies unless overridden.
type StarDefaults struct {
	MaxBudgetUSD float64
	Effort       string
}

// Skill is a reusable prompt fragment plus the tools it grants. Stars compose
// skills by name; the loader merges a skill's PromptFragment and ToolsAdd into
// the resolved Star.
type Skill struct {
	Name           string
	ToolsAdd       []string
	PromptFragment string // the Markdown body
	SourcePath     string
}

// Constellation is a parsed workflow DAG with every embedded expression
// pre-compiled. Nodes are the work units; Edges wire them together with
// optional When guards; Outputs expose named results to a parent constellation.
type Constellation struct {
	Name        string
	Description string
	// Meta is the constellation's declarative [meta] table: operator-tunable
	// scalars (e.g. max_cycles) the runtime resolves at Fire time and surfaces
	// to edge guards as meta.<key>. Kept as an untyped map so new meta keys need
	// no loader change; the runtime coerces the values it understands.
	Meta       map[string]any
	Nodes      []ConstellationNode
	Edges      []ConstellationEdge
	Outputs    map[string]Expression // pre-compiled interpolation templates
	SourcePath string
}

// ConstellationNode is a single node in a constellation DAG. Exactly one of
// Star/Ref/Op is meaningful, selected by Type.
type ConstellationNode struct {
	ID     string
	Type   NodeType
	Star   string                // set when Type == NodeStar
	Ref    string                // set when Type == NodeConstellation (sub-constellation name)
	Op     string                // set when Type == NodeBuiltin
	Inputs map[string]Expression // pre-compiled interpolation templates
}

// ConstellationEdge is a directed transition between nodes. When is nil for an
// unconditional edge; otherwise it must evaluate truthy for the runtime to
// follow the edge. To may be a node ID or a reserved terminal target.
type ConstellationEdge struct {
	From string
	To   string
	When Expression // nil means unconditional
}

// SensorInstance is a per-repo sensor configuration. Type names a Go-registered
// sensor; Config is opaque and handed to the sensor's Configure step unparsed —
// the loader deliberately does not validate it against any type schema.
type SensorInstance struct {
	Name         string
	Type         string
	PollInterval time.Duration
	MaxInflight  int // cap on concurrent in-flight constellation triggers; 0 = scheduler default
	Config       map[string]any
	Triggers     []SensorTrigger
	SourcePath   string
}

// SensorTrigger maps a sensor event to the constellation it launches. When is a
// simple event-name equality match for v1 (e.g. "new_item").
type SensorTrigger struct {
	Constellation string
	When          string
}
