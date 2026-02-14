package nebula

import "time"

// Manifest is parsed from nebula.toml in the nebula directory root.
type Manifest struct {
	Nebula   NebulaInfo `toml:"nebula"`
	Defaults Defaults   `toml:"defaults"`
}

type NebulaInfo struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

type Defaults struct {
	Type     string   `toml:"type"`
	Priority int      `toml:"priority"`
	Labels   []string `toml:"labels"`
	Assignee string   `toml:"assignee"`
}

// TaskSpec is parsed from each *.md file's TOML frontmatter.
type TaskSpec struct {
	ID         string   `toml:"id"`
	Title      string   `toml:"title"`
	Type       string   `toml:"type"`
	Priority   int      `toml:"priority"`
	DependsOn  []string `toml:"depends_on"`
	Labels     []string `toml:"labels"`
	Assignee   string   `toml:"assignee"`
	Body       string   // Markdown body after +++ block
	SourceFile string   // Relative path for error context
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
	Version    int                   `toml:"version"`
	NebulaName string                `toml:"nebula_name"`
	Tasks      map[string]*TaskState `toml:"tasks"`
}

type TaskState struct {
	BeadID    string     `toml:"bead_id"`
	Status    TaskStatus `toml:"status"`
	CreatedAt time.Time  `toml:"created_at"`
	UpdatedAt time.Time  `toml:"updated_at"`
}

// ActionType describes what apply will do for a task.
type ActionType string

const (
	ActionCreate ActionType = "create"
	ActionUpdate ActionType = "update"
	ActionSkip   ActionType = "skip"
	ActionClose  ActionType = "close"
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
}
