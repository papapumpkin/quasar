package nebula

import "time"

// Manifest is parsed from nebula.toml in the nebula directory root.
type Manifest struct {
	Nebula       Info         `toml:"nebula"`
	Defaults     Defaults     `toml:"defaults"`
	Execution    Execution    `toml:"execution"`
	Context      Context      `toml:"context"`
	Dependencies Dependencies `toml:"dependencies"`
}

// Execution holds default execution parameters for the nebula.
type Execution struct {
	MaxWorkers      int      `toml:"max_workers"`
	MaxReviewCycles int      `toml:"max_review_cycles"`
	MaxBudgetUSD    float64  `toml:"max_budget_usd"`
	Model           string   `toml:"model"`
	Gate            GateMode `toml:"gate"`           // Default gate mode for all phases
	AgentMail       bool     `toml:"agentmail"`      // Enable agentmail MCP server
	AgentMailPort   int      `toml:"agentmail_port"` // Override agentmail port
}

// Context provides project-level information injected into agent prompts.
type Context struct {
	Repo        string   `toml:"repo"`
	WorkingDir  string   `toml:"working_dir"`
	Goals       []string `toml:"goals"`
	Constraints []string `toml:"constraints"`
}

// Dependencies declares external prerequisites that must be met before apply.
type Dependencies struct {
	RequiresBeads   []string `toml:"requires_beads"`
	RequiresNebulae []string `toml:"requires_nebulae"`
}

// Info holds the nebula's name and description from the manifest.
type Info struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

// Defaults holds fallback values applied to phases that omit those fields.
type Defaults struct {
	Type     string   `toml:"type"`
	Priority int      `toml:"priority"`
	Labels   []string `toml:"labels"`
	Assignee string   `toml:"assignee"`
}

// PhaseSpec is parsed from each *.md file's TOML frontmatter.
type PhaseSpec struct {
	ID                string   `toml:"id"`
	Title             string   `toml:"title"`
	Type              string   `toml:"type"`
	Priority          int      `toml:"priority"`
	DependsOn         []string `toml:"depends_on"`
	Labels            []string `toml:"labels"`
	Assignee          string   `toml:"assignee"`
	MaxReviewCycles   int      `toml:"max_review_cycles"`   // 0 = use default
	MaxBudgetUSD      float64  `toml:"max_budget_usd"`      // 0 = use default
	Model             string   `toml:"model"`               // "" = use default
	Gate              GateMode `toml:"gate"`                // "" = inherit from manifest
	Scope             []string `toml:"scope"`               // Glob patterns for owned files/dirs
	AllowScopeOverlap bool     `toml:"allow_scope_overlap"` // Override: permit overlap
	Body              string   // Markdown body after +++ block
	SourceFile        string   // Relative path for error context
}

// Nebula is the fully parsed representation of a nebula directory.
type Nebula struct {
	Dir      string
	Manifest Manifest
	Phases   []PhaseSpec
}

// GateMode controls how human involvement is handled between phases.
type GateMode string

const (
	// GateModeTrust runs fully autonomously with no pauses.
	GateModeTrust GateMode = "trust"
	// GateModeReview pauses after each phase, shows diff, and awaits approval.
	GateModeReview GateMode = "review"
	// GateModeApprove gates the plan AND each phase for human approval.
	GateModeApprove GateMode = "approve"
	// GateModeWatch streams diffs in real time without blocking execution.
	GateModeWatch GateMode = "watch"
)

// ValidGateModes is the set of recognized gate mode values.
var ValidGateModes = map[GateMode]bool{
	GateModeTrust:   true,
	GateModeReview:  true,
	GateModeApprove: true,
	GateModeWatch:   true,
}

// PhaseStatus represents the lifecycle of a phase within a nebula.
type PhaseStatus string

const (
	PhaseStatusPending    PhaseStatus = "pending"
	PhaseStatusCreated    PhaseStatus = "created"
	PhaseStatusInProgress PhaseStatus = "in_progress"
	PhaseStatusDone       PhaseStatus = "done"
	PhaseStatusFailed     PhaseStatus = "failed"
	PhaseStatusSkipped    PhaseStatus = "skipped"
)

// State is persisted in nebula.state.toml, mapping phase IDs to bead IDs.
type State struct {
	Version      int                    `toml:"version"`
	NebulaName   string                 `toml:"nebula_name"`
	TotalCostUSD float64                `toml:"total_cost_usd,omitempty"`
	Phases       map[string]*PhaseState `toml:"phases"`
}

// PhaseState tracks the current status and bead association for a single phase.
type PhaseState struct {
	BeadID    string        `toml:"bead_id"`
	Status    PhaseStatus   `toml:"status"`
	CreatedAt time.Time     `toml:"created_at"`
	UpdatedAt time.Time     `toml:"updated_at"`
	Report    *ReviewReport `toml:"report,omitempty"`
}

// ReviewReport captures structured metadata from the reviewer's REPORT: block.
type ReviewReport struct {
	Satisfaction     string `toml:"satisfaction"`
	Risk             string `toml:"risk"`
	NeedsHumanReview bool   `toml:"needs_human_review"`
	Summary          string `toml:"summary"`
}

// ActionType describes what apply will do for a phase.
type ActionType string

const (
	ActionCreate ActionType = "create"
	ActionUpdate ActionType = "update"
	ActionSkip   ActionType = "skip"
	ActionClose  ActionType = "close"
	ActionRetry  ActionType = "retry"
)

// Action is a single planned change.
type Action struct {
	PhaseID string
	Type    ActionType
	Reason  string // Human-readable explanation
}

// Plan is the diff between desired nebula state and actual beads state.
type Plan struct {
	NebulaName string
	Actions    []Action
}

// WorkerResult records the outcome of a single worker execution.
type WorkerResult struct {
	PhaseID string
	BeadID  string
	Err     error
	Report  *ReviewReport
}
