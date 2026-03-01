+++
id = "checkpoint-format"
title = "Define Checkpoint struct and TOML serialization format"
type = "feature"
priority = 1
depends_on = []
labels = ["quasar", "checkpoint", "reliability"]
+++

## Problem

Quasar has no mechanism to persist the in-flight state of a coder-reviewer loop. If the process crashes mid-cycle, all progress for that phase is lost and the entire loop restarts from scratch. We need a well-defined, serializable checkpoint format that captures everything required to resume a loop mid-flight.

## Solution

Create a new `internal/checkpoint` package with a `Checkpoint` struct that captures the full resumable state. The struct embeds or mirrors the fields of `loop.CycleState` (defined in `internal/loop/state.go`) plus metadata needed for validation on resume.

Define the following in `internal/checkpoint/checkpoint.go`:

```go
// Checkpoint captures the full state of a coder-reviewer loop at a
// significant transition point, enabling resume after crash or restart.
type Checkpoint struct {
    Version       int       `toml:"version"`        // schema version (start at 1)
    PhaseID       string    `toml:"phase_id"`        // nebula phase ID (empty for standalone runs)
    NebulaName    string    `toml:"nebula_name"`     // nebula name (empty for standalone runs)
    CreatedAt     time.Time `toml:"created_at"`      // when this checkpoint was written
    GitSHA        string    `toml:"git_sha"`         // HEAD at checkpoint time

    // CycleState fields (mirrored from loop.CycleState)
    TaskBeadID    string    `toml:"task_bead_id"`
    TaskTitle     string    `toml:"task_title"`
    Cycle         int       `toml:"cycle"`
    MaxCycles     int       `toml:"max_cycles"`
    Phase         int       `toml:"phase"`           // loop.Phase as int
    TotalCostUSD  float64   `toml:"total_cost_usd"`
    MaxBudgetUSD  float64   `toml:"max_budget_usd"`
    CoderOutput   string    `toml:"coder_output"`
    ReviewOutput  string    `toml:"review_output"`
    LintOutput    string    `toml:"lint_output"`
    BaseCommitSHA string    `toml:"base_commit_sha"`
    CycleCommits  []string  `toml:"cycle_commits"`
    ChildBeadIDs  []string  `toml:"child_bead_ids"`
    Refactored    bool      `toml:"refactored"`

    Findings      []CheckpointFinding `toml:"findings"`
    AllFindings   []CheckpointFinding `toml:"all_findings"`
}

// CheckpointFinding is the TOML-serializable form of loop.ReviewFinding.
type CheckpointFinding struct {
    Severity    string `toml:"severity"`
    Description string `toml:"description"`
    Cycle       int    `toml:"cycle"`
}
```

The checkpoint file is written to the nebula directory as `checkpoint.<phase-id>.toml` (or `.quasar/checkpoint.toml` for standalone runs). Use the same atomic write-tmp-then-rename pattern used by `nebula.SaveState` in `internal/nebula/state.go`.

Add conversion helpers:

- `FromCycleState(cs *loop.CycleState, phaseID, nebulaName, gitSHA string) *Checkpoint` -- builds a `Checkpoint` from a live `CycleState`.
- `(c *Checkpoint) ToCycleState() *loop.CycleState` -- reconstructs a `CycleState` from a checkpoint.
- `FindingFromReview(f loop.ReviewFinding) CheckpointFinding` -- converts a single finding.
- `(f CheckpointFinding) ToReviewFinding() loop.ReviewFinding` -- converts back.

## Files

- `internal/checkpoint/checkpoint.go` -- `Checkpoint` struct, `CheckpointFinding` struct, conversion helpers
- `internal/checkpoint/checkpoint_test.go` -- round-trip tests: `CycleState` -> `Checkpoint` -> TOML -> `Checkpoint` -> `CycleState`

## Acceptance Criteria

- [ ] `Checkpoint` struct serializes to and deserializes from TOML without data loss
- [ ] `FromCycleState` and `ToCycleState` produce a faithful round-trip for all `CycleState` fields that are relevant across restarts (transient fields like `lastCycleSHA` and `bridgedDiscoveryIDs` are excluded)
- [ ] `CheckpointFinding` round-trips with `loop.ReviewFinding`
- [ ] Version field is set to 1
- [ ] `CreatedAt` is populated with `time.Now()` in `FromCycleState`
- [ ] Table-driven tests cover: empty state, mid-cycle state with findings, multi-cycle state with commits
