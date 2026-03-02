// Package checkpoint defines the serializable format for persisting
// coder-reviewer loop state across process restarts.
package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/loop"
)

var (
	// ErrIncompatibleVersion indicates the checkpoint was written by a newer
	// or older schema version that this code does not support.
	ErrIncompatibleVersion = errors.New("checkpoint version not supported")

	// ErrGitSHAMismatch indicates the checkpoint's git SHA does not match the
	// current HEAD, meaning the repository has changed since the checkpoint.
	ErrGitSHAMismatch = errors.New("checkpoint git SHA does not match current HEAD")

	// ErrInvalidCheckpoint indicates the checkpoint contains internally
	// inconsistent data (e.g. cycle < 1 or invalid phase).
	ErrInvalidCheckpoint = errors.New("checkpoint state is invalid")
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
	TaskBeadID    string   `toml:"task_bead_id"`
	TaskTitle     string   `toml:"task_title"`
	Cycle         int      `toml:"cycle"`
	MaxCycles     int      `toml:"max_cycles"`
	Phase         int      `toml:"phase"` // loop.Phase as int
	TotalCostUSD  float64  `toml:"total_cost_usd"`
	MaxBudgetUSD  float64  `toml:"max_budget_usd"`
	CoderOutput   string   `toml:"coder_output"`
	ReviewOutput  string   `toml:"review_output"`
	LintOutput    string   `toml:"lint_output"`
	BaseCommitSHA string   `toml:"base_commit_sha"`
	CycleCommits  []string `toml:"cycle_commits"`
	FilterHistory []string `toml:"filter_history"` // accumulated FilterCheckName per cycle (index = cycle-1)
	ChildBeadIDs  []string `toml:"child_bead_ids"`
	Refactored    bool     `toml:"refactored"`

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
	if len(cs.FilterHistory) > 0 {
		cp.FilterHistory = make([]string, len(cs.FilterHistory))
		copy(cp.FilterHistory, cs.FilterHistory)
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
	if len(c.FilterHistory) > 0 {
		cs.FilterHistory = make([]string, len(c.FilterHistory))
		copy(cs.FilterHistory, c.FilterHistory)
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

// checkpointPrefix is the common prefix for checkpoint file names.
const checkpointPrefix = "checkpoint."

// CheckpointPath returns the file path for a checkpoint in the given directory.
// When phaseID is non-empty the file is named checkpoint.<phaseID>.toml;
// otherwise it is simply checkpoint.toml.
func CheckpointPath(dir, phaseID string) string {
	if phaseID == "" {
		return filepath.Join(dir, "checkpoint.toml")
	}
	return filepath.Join(dir, checkpointPrefix+phaseID+".toml")
}

// Save atomically writes the checkpoint to the given directory.
// The file is named checkpoint.<phaseID>.toml (or checkpoint.toml if phaseID is empty).
// It uses a write-tmp-then-rename pattern to prevent partial writes.
func Save(dir string, cp *Checkpoint) error {
	data, err := toml.Marshal(cp)
	if err != nil {
		return fmt.Errorf("marshaling checkpoint: %w", err)
	}

	path := CheckpointPath(dir, cp.PhaseID)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing temp checkpoint file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming checkpoint file: %w", err)
	}

	return nil
}

// Load reads a checkpoint file from the given directory for the specified phase.
// Returns nil, nil if no checkpoint file exists (not an error, just nothing to resume).
func Load(dir, phaseID string) (*Checkpoint, error) {
	path := CheckpointPath(dir, phaseID)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading checkpoint %s: %w", path, err)
	}

	var cp Checkpoint
	if err := toml.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("parsing checkpoint %s: %w", path, err)
	}

	return &cp, nil
}

// LoadAll returns all checkpoint files found in the given directory, keyed by
// phase ID. Used by nebula-level resume to discover which phases have
// in-flight checkpoints. A checkpoint without a phase ID (standalone) is
// stored under the empty string key.
func LoadAll(dir string) (map[string]*Checkpoint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing checkpoint directory %s: %w", dir, err)
	}

	result := make(map[string]*Checkpoint)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, checkpointPrefix) || !strings.HasSuffix(name, ".toml") {
			continue
		}

		// Extract phase ID from "checkpoint.<phaseID>.toml" or "" from "checkpoint.toml".
		phaseID := extractPhaseID(name)

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("reading checkpoint %s: %w", name, err)
		}

		var cp Checkpoint
		if err := toml.Unmarshal(data, &cp); err != nil {
			return nil, fmt.Errorf("parsing checkpoint %s: %w", name, err)
		}

		result[phaseID] = &cp
	}

	return result, nil
}

// extractPhaseID derives the phase ID from a checkpoint file name.
// "checkpoint.toml" -> "", "checkpoint.my-phase.toml" -> "my-phase".
func extractPhaseID(name string) string {
	// Strip "checkpoint." prefix and ".toml" suffix.
	trimmed := strings.TrimPrefix(name, checkpointPrefix)
	trimmed = strings.TrimSuffix(trimmed, ".toml")
	// "checkpoint.toml" → trimmed becomes "toml" → standalone.
	if trimmed == "toml" {
		return ""
	}
	return trimmed
}

// Validate checks whether a checkpoint is safe to resume from.
// It verifies version compatibility, git SHA match, and internal consistency.
func Validate(cp *Checkpoint, currentGitSHA string) error {
	if cp.Version != Version {
		return fmt.Errorf("%w: got %d, want %d", ErrIncompatibleVersion, cp.Version, Version)
	}

	if cp.GitSHA != currentGitSHA {
		return fmt.Errorf("%w: checkpoint=%s, current=%s", ErrGitSHAMismatch, cp.GitSHA, currentGitSHA)
	}

	if cp.Cycle < 1 {
		return fmt.Errorf("%w: cycle must be >= 1, got %d", ErrInvalidCheckpoint, cp.Cycle)
	}

	phase := loop.Phase(cp.Phase)
	if phase < loop.PhaseIdle || phase > loop.PhaseError {
		return fmt.Errorf("%w: phase %d is out of range [%d, %d]",
			ErrInvalidCheckpoint, cp.Phase, int(loop.PhaseIdle), int(loop.PhaseError))
	}

	return nil
}

// Remove deletes the checkpoint file for the given phase. It returns nil if
// the file does not exist (already cleaned up).
func Remove(dir, phaseID string) error {
	path := CheckpointPath(dir, phaseID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing checkpoint %s: %w", path, err)
	}
	return nil
}

// CurrentGitSHA returns HEAD's SHA in the given working directory.
// It uses exec.CommandContext for cancellation support.
func CurrentGitSHA(ctx context.Context, workDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
