package nebula

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	toml "github.com/pelletier/go-toml/v2"
)

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
	MaxWorkers           int        `toml:"max_workers"`
	MaxReviewCycles      int        `toml:"max_review_cycles"`
	MaxBudgetUSD         float64    `toml:"max_budget_usd"`
	MaxContextTokens     int        `toml:"max_context_tokens"` // Token budget for context injection. 0 = disabled.
	Model                string     `toml:"model"`
	Gate                 GateMode   `toml:"gate"`                   // Default gate mode for all phases
	HailTimeout          string     `toml:"hail_timeout"`           // Duration string for hail auto-resolve timeout (e.g. "5m"). Empty = default (5m). "0" = disabled.
	Routing              TierConfig `toml:"routing"`                // Auto-routing config. Zero-value = disabled.
	AutoDecompose        bool       `toml:"auto_decompose"`         // Enable auto-decomposition on struggle.
	Healing              bool       `toml:"healing"`                // Master switch for auto-healing on failure.
	HealingMaxAttempts   int        `toml:"healing_max_attempts"`   // Per-phase healing attempts (default 1).
	HealingBudgetReserve float64    `toml:"healing_budget_reserve"` // USD reserved from nebula budget for healing phases.
}

// DefaultHailTimeout is the built-in fallback for hail auto-resolution timeout.
const DefaultHailTimeout = 5 * time.Minute

// ParsedHailTimeout returns the hail timeout as a time.Duration.
// Empty string returns DefaultHailTimeout. "0" returns 0 (disabled).
// Invalid strings return DefaultHailTimeout.
func (e Execution) ParsedHailTimeout() time.Duration {
	if e.HailTimeout == "" {
		return DefaultHailTimeout
	}
	if e.HailTimeout == "0" {
		return 0
	}
	d, err := time.ParseDuration(e.HailTimeout)
	if err != nil {
		return DefaultHailTimeout
	}
	return d
}

// HealingPolicy returns the healing policy derived from the execution config.
// When healing is disabled or max attempts is zero, the returned policy has
// Enabled=false.
func (e Execution) HealingPolicy() HealingPolicy {
	maxAttempts := e.HealingMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	return HealingPolicy{
		Enabled:       e.Healing,
		MaxAttempts:   maxAttempts,
		BudgetReserve: e.HealingBudgetReserve,
	}
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
	MaxReviewCycles   int      `toml:"max_review_cycles"`        // 0 = use default
	MaxBudgetUSD      float64  `toml:"max_budget_usd"`           // 0 = use default
	Model             string   `toml:"model"`                    // "" = use default
	Gate              GateMode `toml:"gate"`                     // "" = inherit from manifest
	Blocks            []string `toml:"blocks"`                   // Reverse deps: inject as dep of listed phases
	Scope             []string `toml:"scope"`                    // Glob patterns for owned files/dirs
	AllowScopeOverlap bool     `toml:"allow_scope_overlap"`      // Override: permit overlap
	Decomposed        bool     `toml:"decomposed,omitempty"`     // true if this phase was produced by auto-decomposition
	AutoDecompose     *bool    `toml:"auto_decompose,omitempty"` // per-phase override (nil = inherit from manifest)
	Body              string   // Markdown body after +++ block
	SourceFile        string   // Relative path for error context
}

// Nebula is the fully parsed representation of a nebula directory.
type Nebula struct {
	Dir      string
	Manifest Manifest
	Phases   []PhaseSpec
}

// HasDependencies reports whether any phase in the nebula has explicit
// dependency edges. When true, the contract board is required for correct
// concurrent scheduling.
func (n *Nebula) HasDependencies() bool {
	for _, p := range n.Phases {
		if len(p.DependsOn) > 0 {
			return true
		}
	}
	return false
}

// PhasesByID returns a map from phase ID to phase pointer for quick lookup.
func PhasesByID(phases []PhaseSpec) map[string]*PhaseSpec {
	m := make(map[string]*PhaseSpec, len(phases))
	for i := range phases {
		m[phases[i].ID] = &phases[i]
	}
	return m
}

// Snapshot returns a deep copy of the Nebula that is safe to read without
// holding any lock. Scalar fields and the Manifest struct are shallow-copied
// (they are value types). Each PhaseSpec and its sub-slices are independently
// allocated so mutations to the original do not affect the snapshot.
func (n *Nebula) Snapshot() *Nebula {
	cp := *n
	if n.Phases != nil {
		cp.Phases = make([]PhaseSpec, len(n.Phases))
		for i, p := range n.Phases {
			cp.Phases[i] = p
			if p.DependsOn != nil {
				cp.Phases[i].DependsOn = append([]string{}, p.DependsOn...)
			}
			if p.Labels != nil {
				cp.Phases[i].Labels = append([]string{}, p.Labels...)
			}
			if p.Scope != nil {
				cp.Phases[i].Scope = append([]string{}, p.Scope...)
			}
			if p.Blocks != nil {
				cp.Phases[i].Blocks = append([]string{}, p.Blocks...)
			}
			if p.AutoDecompose != nil {
				v := *p.AutoDecompose
				cp.Phases[i].AutoDecompose = &v
			}
		}
	}
	return &cp
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
	// PhaseStatusDecomposed indicates the phase was broken into sub-phases.
	PhaseStatusDecomposed PhaseStatus = "decomposed"
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
	BeadID    string              `toml:"bead_id"`
	Status    PhaseStatus         `toml:"status"`
	CreatedAt time.Time           `toml:"created_at"`
	UpdatedAt time.Time           `toml:"updated_at"`
	Report    *agent.ReviewReport `toml:"report,omitempty"`
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
	Report  *agent.ReviewReport

	// Populated on failure for healing analysis. Nil on success.
	TaskResult *PhaseRunnerResult
}

const stateFileName = "nebula.state.toml"

// legacyState mirrors State but with the old "tasks" TOML key for backward compatibility.
type legacyState struct {
	Version      int                    `toml:"version"`
	NebulaName   string                 `toml:"nebula_name"`
	TotalCostUSD float64                `toml:"total_cost_usd,omitempty"`
	Tasks        map[string]*PhaseState `toml:"tasks"`
}

// LoadState reads the state file from the nebula directory.
// Returns an empty state if the file does not exist.
// For backward compatibility, accepts both [phases] and legacy [tasks] sections,
// preferring [phases]. A deprecation warning is emitted via stderr when [tasks] is encountered.
func LoadState(dir string) (*State, error) {
	path := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Version: 1,
				Phases:  make(map[string]*PhaseState),
			}, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state State
	if err := toml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	// Backward compatibility: if Phases is empty, try loading legacy [tasks] section.
	if len(state.Phases) == 0 {
		var legacy legacyState
		if err := toml.Unmarshal(data, &legacy); err == nil && len(legacy.Tasks) > 0 {
			fmt.Fprintf(os.Stderr, "warning: state file uses deprecated [tasks] section; migrate to [phases]\n")
			state.Phases = legacy.Tasks
		}
	}

	if state.Phases == nil {
		state.Phases = make(map[string]*PhaseState)
	}

	return &state, nil
}

// SaveState writes the state file atomically (write temp + rename).
func SaveState(dir string, state *State) error {
	data, err := toml.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	path := filepath.Join(dir, stateFileName)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing temp state file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

// SetPhaseState updates or creates a phase's state entry.
func (s *State) SetPhaseState(phaseID, beadID string, status PhaseStatus) {
	now := time.Now()
	ps, ok := s.Phases[phaseID]
	if !ok {
		ps = &PhaseState{
			CreatedAt: now,
		}
		s.Phases[phaseID] = ps
	}
	ps.BeadID = beadID
	ps.Status = status
	ps.UpdatedAt = now
}
