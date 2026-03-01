+++
id = "nebula-writer"
title = "Implement disk writer for generated nebulas"
type = "feature"
priority = 2
depends_on = ["multi-phase-architect"]
scope = ["internal/nebula/writer.go", "internal/nebula/writer_test.go"]
+++

## Problem

After the `Generate` function produces a `GenerateResult` containing a `Manifest` and a slice of `PhaseSpec` values, these need to be serialized to disk as a proper nebula directory: a `nebula.toml` manifest and numbered phase files (`01-phase-id.md`, `02-phase-id.md`, etc.). The existing `MarshalPhaseFile` function in `internal/nebula/parse.go` handles individual phase serialization, and TOML marshaling is available via `github.com/pelletier/go-toml/v2`, but there is no orchestration layer that creates the directory, numbers the files, writes the manifest, and writes all phases atomically.

If the generation process fails partway through writing, the user could be left with a partial nebula directory that fails validation. The writer must either succeed completely or clean up after itself. Additionally, the writer must handle the case where the output directory already exists — it should refuse to overwrite unless explicitly told to, preventing accidental destruction of hand-written nebulas.

## Solution

Create `internal/nebula/writer.go` with a `WriteNebula` function that atomically writes a generated nebula to disk.

### Types

```go
// WriteOptions controls how a nebula is written to disk.
type WriteOptions struct {
    Overwrite bool // If true, overwrite an existing nebula directory
}
```

### Core Function

```go
// WriteNebula writes a complete nebula (manifest + phase files) to the given
// output directory. Phase files are numbered sequentially in topological order
// of their dependencies (01-phase-id.md, 02-phase-id.md, etc.).
//
// If the directory already exists and opts.Overwrite is false, WriteNebula
// returns an error. On failure, any partially written directory is removed.
func WriteNebula(result *GenerateResult, outputDir string, opts WriteOptions) error
```

Implementation approach:

1. **Pre-flight check**: If `outputDir` exists and `opts.Overwrite` is false, return `fmt.Errorf("directory %s already exists; use --force to overwrite", outputDir)`.
2. **Topological sort**: Sort the phases in topological order using the dependency graph. Phases with no dependencies come first. Within the same topological level, sort by priority (lower number = higher priority), then alphabetically by ID for determinism.
3. **Create temp directory**: Write to a temporary directory (e.g., `outputDir + ".tmp"`) first, then atomically rename on success.
4. **Write manifest**: Marshal `result.Manifest` to TOML using `toml.Marshal` and write as `nebula.toml`.
5. **Write phase files**: For each phase (in topological order), format the filename as `%02d-%s.md` (e.g., `01-setup-types.md`). Use the existing `MarshalPhaseFile` function to serialize each `PhaseSpec`.
6. **Atomic swap**: `os.Rename` the temp directory to `outputDir`. If `opts.Overwrite` is true and the directory exists, remove it first.
7. **Cleanup on failure**: If any step fails, remove the temp directory via `defer`.

### Manifest Marshaling

```go
// marshalManifest serializes a Manifest to TOML bytes suitable for writing
// as nebula.toml.
func marshalManifest(m Manifest) ([]byte, error)
```

This uses `toml.Marshal(m)` directly, since the `Manifest` struct has proper TOML tags. The function exists as a named helper for testability and to handle any post-processing (e.g., ensuring trailing newline).

### Topological Sort Helper

```go
// topoSortPhases returns phases sorted in topological order by their
// DependsOn relationships. Within a topological level, phases are sorted
// by priority (ascending) then by ID (alphabetical).
func topoSortPhases(phases []PhaseSpec) ([]PhaseSpec, error)
```

This uses Kahn's algorithm (BFS with in-degree tracking) and returns an error if the graph contains cycles — though this should have been caught earlier by `Validate`.

### Testing

Write `internal/nebula/writer_test.go` with:

- **Happy path**: Write a `GenerateResult` with 3 phases to a temp directory. Verify the directory contains `nebula.toml`, `01-first-phase.md`, `02-second-phase.md`, `03-third-phase.md`. Re-read with `nebula.Load` and verify the round-trip.
- **Topological ordering**: Phases with dependencies are numbered correctly — a phase that depends on another always has a higher number.
- **Overwrite protection**: Writing to an existing directory without `Overwrite: true` returns an error.
- **Overwrite allowed**: Writing to an existing directory with `Overwrite: true` succeeds and replaces the content.
- **Atomic cleanup**: Simulate a write failure (e.g., read-only filesystem) and verify no partial directory is left behind.
- **Round-trip fidelity**: Write a nebula, load it back with `nebula.Load`, and verify all manifest fields and phase specs match.

## Files

- `internal/nebula/writer.go` — New file: `WriteOptions`, `WriteNebula`, `marshalManifest`, `topoSortPhases`
- `internal/nebula/writer_test.go` — New file: table-driven tests for writing, ordering, overwrite protection, and round-trip fidelity

## Acceptance Criteria

- [ ] `WriteNebula` creates the output directory with `nebula.toml` and numbered phase files
- [ ] Phase files are numbered in topological order of dependencies
- [ ] Filename format is `%02d-%s.md` where the ID portion is the phase ID in kebab-case
- [ ] Writing to an existing directory without `Overwrite: true` returns a descriptive error
- [ ] Writing with `Overwrite: true` replaces the existing directory
- [ ] Partial writes are cleaned up on failure (no orphaned temp directories)
- [ ] Round-trip test: `WriteNebula` followed by `nebula.Load` produces equivalent data
- [ ] `marshalManifest` produces valid TOML that `toml.Unmarshal` can parse back
- [ ] `topoSortPhases` correctly orders phases and returns an error on cycles
- [ ] All new types and functions have GoDoc comments
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` reports no issues
