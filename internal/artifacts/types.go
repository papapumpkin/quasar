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
	Prompt        string // skill fragments + star body
	SourcePath    string // for error reporting
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
	Nodes       []ConstellationNode
	Edges       []ConstellationEdge
	Outputs     map[string]Expression // pre-compiled interpolation templates
	SourcePath  string
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
