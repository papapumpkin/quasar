// Package checkpoint defines the serializable format for persisting
// coder-reviewer loop state across process restarts.
package checkpoint

import (
	"time"

	"github.com/papapumpkin/quasar/internal/loop"
)

// Version is the current checkpoint schema version.
const Version = 1

// Checkpoint captures the full state of a coder-reviewer loop at a
// significant transition point, enabling resume after crash or restart.
type Checkpoint struct {
	Version    int       `toml:"version"`     // schema version (start at 1)
	PhaseID    string    `toml:"phase_id"`    // nebula phase ID (empty for standalone runs)
	NebulaName string    `toml:"nebula_name"` // nebula name (empty for standalone runs)
	CreatedAt  time.Time `toml:"created_at"`  // when this checkpoint was written
	GitSHA     string    `toml:"git_sha"`     // HEAD at checkpoint time

	// CycleState fields (mirrored from loop.CycleState).
	TaskBeadID    string  `toml:"task_bead_id"`
	TaskTitle     string  `toml:"task_title"`
	Cycle         int     `toml:"cycle"`
	MaxCycles     int     `toml:"max_cycles"`
	Phase         int     `toml:"phase"` // loop.Phase as int
	TotalCostUSD  float64 `toml:"total_cost_usd"`
	MaxBudgetUSD  float64 `toml:"max_budget_usd"`
	CoderOutput   string  `toml:"coder_output"`
	ReviewOutput  string  `toml:"review_output"`
	LintOutput    string  `toml:"lint_output"`
	BaseCommitSHA string  `toml:"base_commit_sha"`
	CycleCommits  []string `toml:"cycle_commits"`
	ChildBeadIDs  []string `toml:"child_bead_ids"`
	Refactored    bool    `toml:"refactored"`

	Findings    []CheckpointFinding `toml:"findings"`
	AllFindings []CheckpointFinding `toml:"all_findings"`
}

// CheckpointFinding is the TOML-serializable form of loop.ReviewFinding.
type CheckpointFinding struct {
	ID          string `toml:"id"`
	Severity    string `toml:"severity"`
	Description string `toml:"description"`
	Cycle       int    `toml:"cycle"`
	Status      string `toml:"status"`
}

// FromCycleState builds a Checkpoint from a live CycleState plus metadata
// needed for resume validation.
func FromCycleState(cs *loop.CycleState, phaseID, nebulaName, gitSHA string) *Checkpoint {
	cp := &Checkpoint{
		Version:    Version,
		PhaseID:    phaseID,
		NebulaName: nebulaName,
		CreatedAt:  time.Now(),
		GitSHA:     gitSHA,

		TaskBeadID:    cs.TaskBeadID,
		TaskTitle:     cs.TaskTitle,
		Cycle:         cs.Cycle,
		MaxCycles:     cs.MaxCycles,
		Phase:         int(cs.Phase),
		TotalCostUSD:  cs.TotalCostUSD,
		MaxBudgetUSD:  cs.MaxBudgetUSD,
		CoderOutput:   cs.CoderOutput,
		ReviewOutput:  cs.ReviewOutput,
		LintOutput:    cs.LintOutput,
		BaseCommitSHA: cs.BaseCommitSHA,
		Refactored:    cs.Refactored,
	}

	// Copy slices to avoid aliasing the original.
	if len(cs.CycleCommits) > 0 {
		cp.CycleCommits = make([]string, len(cs.CycleCommits))
		copy(cp.CycleCommits, cs.CycleCommits)
	}
	if len(cs.ChildBeadIDs) > 0 {
		cp.ChildBeadIDs = make([]string, len(cs.ChildBeadIDs))
		copy(cp.ChildBeadIDs, cs.ChildBeadIDs)
	}

	cp.Findings = findingsFromReview(cs.Findings)
	cp.AllFindings = findingsFromReview(cs.AllFindings)

	return cp
}

// ToCycleState reconstructs a CycleState from a checkpoint. Transient fields
// (lastCycleSHA, bridgedDiscoveryIDs, cache stats, etc.) are zeroed.
func (c *Checkpoint) ToCycleState() *loop.CycleState {
	cs := &loop.CycleState{
		TaskBeadID:    c.TaskBeadID,
		TaskTitle:     c.TaskTitle,
		Cycle:         c.Cycle,
		MaxCycles:     c.MaxCycles,
		Phase:         loop.Phase(c.Phase),
		TotalCostUSD:  c.TotalCostUSD,
		MaxBudgetUSD:  c.MaxBudgetUSD,
		CoderOutput:   c.CoderOutput,
		ReviewOutput:  c.ReviewOutput,
		LintOutput:    c.LintOutput,
		BaseCommitSHA: c.BaseCommitSHA,
		Refactored:    c.Refactored,
	}

	// Copy slices to avoid aliasing.
	if len(c.CycleCommits) > 0 {
		cs.CycleCommits = make([]string, len(c.CycleCommits))
		copy(cs.CycleCommits, c.CycleCommits)
	}
	if len(c.ChildBeadIDs) > 0 {
		cs.ChildBeadIDs = make([]string, len(c.ChildBeadIDs))
		copy(cs.ChildBeadIDs, c.ChildBeadIDs)
	}

	cs.Findings = findingsToReview(c.Findings)
	cs.AllFindings = findingsToReview(c.AllFindings)

	return cs
}

// FindingFromReview converts a single loop.ReviewFinding to CheckpointFinding.
func FindingFromReview(f loop.ReviewFinding) CheckpointFinding {
	return CheckpointFinding{
		ID:          f.ID,
		Severity:    f.Severity,
		Description: f.Description,
		Cycle:       f.Cycle,
		Status:      string(f.Status),
	}
}

// ToReviewFinding converts a CheckpointFinding back to loop.ReviewFinding.
func (f CheckpointFinding) ToReviewFinding() loop.ReviewFinding {
	return loop.ReviewFinding{
		ID:          f.ID,
		Severity:    f.Severity,
		Description: f.Description,
		Cycle:       f.Cycle,
		Status:      loop.FindingStatus(f.Status),
	}
}

// findingsFromReview converts a slice of ReviewFinding to CheckpointFinding.
func findingsFromReview(fs []loop.ReviewFinding) []CheckpointFinding {
	if len(fs) == 0 {
		return nil
	}
	out := make([]CheckpointFinding, len(fs))
	for i, f := range fs {
		out[i] = FindingFromReview(f)
	}
	return out
}

// findingsToReview converts a slice of CheckpointFinding to ReviewFinding.
func findingsToReview(fs []CheckpointFinding) []loop.ReviewFinding {
	if len(fs) == 0 {
		return nil
	}
	out := make([]loop.ReviewFinding, len(fs))
	for i, f := range fs {
		out[i] = f.ToReviewFinding()
	}
	return out
}
