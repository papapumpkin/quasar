package nebula

import "time"

// Manifest is parsed from nebula.toml in the nebula directory root.
type Manifest struct {
	Nebula       NebulaInfo   `toml:"nebula"`
	Defaults     Defaults     `toml:"defaults"`
	Execution    Execution    `toml:"execution"`
	Context      Context      `toml:"context"`
	Dependencies Dependencies `toml:"dependencies"`
}

// Execution holds default execution parameters for the nebula.
type Execution struct {
	MaxWorkers      int     `toml:"max_workers"`
	MaxReviewCycles int     `toml:"max_review_cycles"`
	MaxBudgetUSD    float64 `toml:"max_budget_usd"`
	Model           string  `toml:"model"`
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

// NebulaInfo holds the nebula's name and description from the manifest.
type NebulaInfo struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

// Defaults holds fallback values applied to tasks that omit those fields.
type Defaults struct {
	Type     string   `toml:"type"`
	Priority int      `toml:"priority"`
	Labels   []string `toml:"labels"`
	Assignee string   `toml:"assignee"`
}

// TaskSpec is parsed from each *.md file's TOML frontmatter.
type TaskSpec struct {
	ID              string   `toml:"id"`
	Title           string   `toml:"title"`
	Type            string   `toml:"type"`
	Priority        int      `toml:"priority"`
	DependsOn       []string `toml:"depends_on"`
	Labels          []string `toml:"labels"`
	Assignee        string   `toml:"assignee"`
	MaxReviewCycles int      `toml:"max_review_cycles"` // 0 = use default
	MaxBudgetUSD    float64  `toml:"max_budget_usd"`    // 0 = use default
	Model           string   `toml:"model"`             // "" = use default
	Body            string   // Markdown body after +++ block
	SourceFile      string   // Relative path for error context
}

// Nebula is the fully parsed representation of a nebula directory.
type Nebula struct {
	Dir      string
	Manifest Manifest
	Tasks    []TaskSpec
}

// TaskStatus represents the lifecycle of a task within a nebula.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusCreated    TaskStatus = "created"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusFailed     TaskStatus = "failed"
)

// State is persisted in nebula.state.toml, mapping task IDs to bead IDs.
type State struct {
	Version      int                   `toml:"version"`
	NebulaName   string                `toml:"nebula_name"`
	TotalCostUSD float64               `toml:"total_cost_usd,omitempty"`
	Tasks        map[string]*TaskState `toml:"tasks"`
}

// TaskState tracks the current status and bead association for a single task.
type TaskState struct {
	BeadID    string        `toml:"bead_id"`
	Status    TaskStatus    `toml:"status"`
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

// ActionType describes what apply will do for a task.
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
	TaskID string
	Type   ActionType
	Reason string // Human-readable explanation
}

// Plan is the diff between desired nebula state and actual beads state.
type Plan struct {
	NebulaName string
	Actions    []Action
}

// WorkerResult records the outcome of a single worker execution.
type WorkerResult struct {
	TaskID string
	BeadID string
	Err    error
	Report *ReviewReport
}
