+++
id = "checkpoint-loader"
title = "Implement checkpoint loader with validation"
type = "feature"
priority = 1
depends_on = ["checkpoint-format"]
labels = ["quasar", "checkpoint", "reliability"]
+++

## Problem

Checkpoints are written but there is no way to read them back and validate they are safe to resume from. We need a loader that reads checkpoint files, validates them against the current repository state, and reports whether resumption is safe.

## Solution

Add loading and validation functions to `internal/checkpoint/checkpoint.go`:

```go
// Load reads a checkpoint file from the given directory for the specified phase.
// Returns nil, nil if no checkpoint file exists (not an error, just nothing to resume).
func Load(dir, phaseID string) (*Checkpoint, error)

// LoadAll returns all checkpoint files found in the given directory,
// keyed by phase ID. Used by nebula-level resume to discover which phases
// have in-flight checkpoints.
func LoadAll(dir string) (map[string]*Checkpoint, error)

// Validate checks whether a checkpoint is safe to resume from.
// It verifies:
//   - Version compatibility (must be 1)
//   - Git SHA matches current HEAD (or baseCommitSHA is an ancestor of HEAD)
//   - CycleState fields are internally consistent (Cycle > 0, Phase is valid)
func Validate(cp *Checkpoint, currentGitSHA string) error
```

### Validation rules

`Validate` returns a descriptive error if any check fails:

1. **Version check**: `cp.Version != 1` returns `ErrIncompatibleVersion`.
2. **Git SHA check**: If `cp.GitSHA != currentGitSHA`, return `ErrGitSHAMismatch` with both SHAs in the message. The caller may choose to override this with a `--force` flag.
3. **Cycle sanity**: `cp.Cycle < 1` returns `ErrInvalidCheckpoint`.
4. **Phase range**: `cp.Phase` must be a valid `loop.Phase` constant (between `PhaseIdle` and `PhaseApproved`).

Define sentinel errors:

```go
var (
    ErrIncompatibleVersion = errors.New("checkpoint version not supported")
    ErrGitSHAMismatch      = errors.New("checkpoint git SHA does not match current HEAD")
    ErrInvalidCheckpoint   = errors.New("checkpoint state is invalid")
)
```

### Cleanup

```go
// Remove deletes the checkpoint file for the given phase. Called after
// successful task completion so stale checkpoints don't linger.
func Remove(dir, phaseID string) error
```

## Files

- `internal/checkpoint/checkpoint.go` -- add `Load`, `LoadAll`, `Validate`, `Remove`, sentinel errors
- `internal/checkpoint/checkpoint_test.go` -- add tests for loading, validation (valid, version mismatch, SHA mismatch, invalid cycle), removal, `LoadAll` with multiple checkpoint files

## Acceptance Criteria

- [ ] `Load` returns `nil, nil` when no checkpoint file exists
- [ ] `Load` returns a parsed `Checkpoint` when the file exists
- [ ] `LoadAll` discovers all `checkpoint.*.toml` files in a directory
- [ ] `Validate` returns nil for a valid checkpoint with matching git SHA
- [ ] `Validate` returns `ErrIncompatibleVersion` for version != 1
- [ ] `Validate` returns `ErrGitSHAMismatch` when SHAs differ
- [ ] `Validate` returns `ErrInvalidCheckpoint` for cycle < 1 or invalid phase
- [ ] `Remove` deletes the file and returns nil; no error if file already absent
- [ ] Sentinel errors are package-level vars following Go conventions
