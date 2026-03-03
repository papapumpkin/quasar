+++
id = "phase-generation"
title = "Generate validated nebula files from canvas draft state"
type = "feature"
priority = 2
depends_on = ["canvas-types"]
scope = ["internal/canvas/writer.go", "internal/canvas/writer_test.go"]
+++

## Problem

Once a canvas conversation has produced a `DraftNebula` with well-defined phases, the draft needs to be materialized into actual nebula files on disk — a `nebula.toml` manifest and per-phase `*.md` files in `.nebulas/<name>/`. The generated output must be valid according to `nebula.Validate()` so that `quasar nebula apply` can execute it without manual fixup.

The existing `nebula.MarshalPhaseFile(spec PhaseSpec) ([]byte, error)` function in `internal/nebula/parse.go` handles serializing a single phase to the `+++` frontmatter format. The manifest serialization uses `toml.Marshal`. These should be reused rather than reimplemented.

## Solution

Create `internal/canvas/writer.go` with functions to convert a `DraftNebula` into validated nebula files.

### Writer

```go
// Writer generates nebula files from a DraftNebula. It validates
// the output before writing to ensure the nebula is executable.
type Writer struct {
    baseDir string // Base directory for nebulas (typically ".nebulas")
}

// NewWriter creates a Writer that writes nebula files under the
// given base directory.
func NewWriter(baseDir string) *Writer
```

### Generation Pipeline

```go
// Generate converts a DraftNebula into nebula files on disk.
// It creates the directory .nebulas/<name>/, writes nebula.toml,
// writes each phase as NN-<id>.md, validates the result, and
// returns the output directory path.
//
// If validation fails, the files are still written but validation
// errors are returned alongside the path so the developer can
// review and fix them.
func (w *Writer) Generate(draft *DraftNebula) (dir string, errs []error, err error)
```

The `Generate` method follows these steps:

1. **Convert types** — call `draft.ToManifest()` for the manifest and `phase.ToPhaseSpec()` for each draft phase
2. **Create directory** — `os.MkdirAll(.nebulas/<name>/, 0o755)`
3. **Write manifest** — marshal `nebula.Manifest` with `toml.Marshal` and write to `nebula.toml`
4. **Write phase files** — for each phase, call `nebula.MarshalPhaseFile(spec)` and write to `NN-<id>.md` where `NN` is a zero-padded sequence number
5. **Validate** — load the written nebula with `nebula.Load(dir)` and validate with `nebula.Validate(n)`, collecting any `ValidationError` values
6. **Return** — return the directory path, validation errors (as `[]error`), and any fatal I/O error

### Phase File Naming

Phase files are named with a two-digit prefix for sort ordering, matching the convention used across existing nebulas (e.g., `01-canvas-types.md`, `02-architect-agent.md`):

```go
// phaseFilename generates the filename for a phase file given its
// index (0-based) and phase ID. Format: "NN-<id>.md"
func phaseFilename(index int, id string) string {
    return fmt.Sprintf("%02d-%s.md", index+1, id)
}
```

### Manifest Construction

The `DraftNebula.ToManifest()` method (defined in phase 1) maps draft fields to `nebula.Manifest`:

```go
// Mapping:
// DraftNebula.Name        → Manifest.Nebula.Name
// DraftNebula.Description → Manifest.Nebula.Description
// DraftNebula.Goals       → Manifest.Context.Goals
// DraftNebula.Constraints → Manifest.Context.Constraints
// DraftNebula.Execution   → Manifest.Execution (MaxWorkers, MaxReviewCycles, etc.)
//
// Fixed values:
// Manifest.Context.Repo      = "github.com/papapumpkin/quasar"
// Manifest.Context.WorkingDir = "."
// Manifest.Defaults.Type      = "feature" (or from DraftExecution)
// Manifest.Defaults.Priority  = 2
// Manifest.Defaults.Labels    = ["quasar"]
```

### Dry Run Support

```go
// Preview generates the nebula files in memory without writing to
// disk. Returns the manifest TOML and a map of filename → content
// for each phase file. Useful for showing the developer what will
// be generated before committing.
func (w *Writer) Preview(draft *DraftNebula) (manifest string, phases map[string]string, err error)
```

`Preview` runs the same conversion pipeline as `Generate` but writes to `strings.Builder` instead of files. This is used by the REPL's `generate` command to show a preview before writing.

### Validation Integration

```go
// ValidateDraft checks a DraftNebula for issues without writing
// any files. Returns validation errors from nebula.Validate().
func ValidateDraft(draft *DraftNebula) []error
```

`ValidateDraft` converts the draft to `nebula.Nebula` in memory and runs `nebula.Validate()`. This provides early feedback during the conversation — the REPL can warn the developer about issues (missing phase IDs, circular dependencies, scope overlaps) before they trigger `generate`.

## Files

- `internal/canvas/writer.go` — `Writer`, `NewWriter`, `Generate`, `Preview`, `ValidateDraft`, `phaseFilename`
- `internal/canvas/writer_test.go` — tests for: successful generation (writes nebula.toml + phase files), validation error collection (missing depends_on target, circular deps), preview without disk writes, phase filename generation, manifest field mapping, empty draft handling

## Acceptance Criteria

- [ ] `Generate` creates `.nebulas/<name>/nebula.toml` with correct TOML structure
- [ ] `Generate` creates `NN-<id>.md` files for each draft phase with valid `+++` frontmatter
- [ ] Generated phase files use `nebula.MarshalPhaseFile` for serialization (no reimplementation)
- [ ] `Generate` runs `nebula.Load` + `nebula.Validate` on the written files and returns validation errors
- [ ] Validation errors are returned as `[]error` alongside the directory path (not fatal)
- [ ] Fatal I/O errors (permission denied, disk full) are returned as the `err` return value
- [ ] `Preview` returns manifest TOML and phase file contents without writing to disk
- [ ] `ValidateDraft` catches circular dependencies in draft phases
- [ ] `ValidateDraft` catches references to non-existent phase IDs in `depends_on`
- [ ] `phaseFilename(0, "api-types")` returns `"01-api-types.md"`
- [ ] Generated manifest includes repo, working_dir, goals, constraints, and execution config
- [ ] `go test ./internal/canvas/...` passes with at least 8 writer-specific test cases
- [ ] `go vet ./internal/canvas/...` reports no issues
