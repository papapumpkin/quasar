+++
id = "validation-integration"
title = "Integrate validation feedback loop and self-healing into generation pipeline"
type = "feature"
priority = 3
depends_on = ["multi-phase-architect", "nebula-writer", "cli-command"]
scope = ["internal/nebula/correct.go", "internal/nebula/correct_test.go", "internal/nebula/validate.go"]
allow_scope_overlap = true
+++

## Problem

The `Generate` function (from phase 03) calls `nebula.Validate` at the end and reports errors in `GenerateResult.Errors`, but it does not attempt to fix them. LLM-generated nebulas frequently have validation issues: duplicate phase IDs, dangling dependency references, invalid gate modes, missing required fields, or scope overlaps that cause DAG construction failures. Currently, these errors would be surfaced to the user who must manually edit the generated files — defeating the purpose of automated generation.

The generation pipeline needs a self-healing feedback loop: after initial generation and validation, if errors are found, the pipeline should attempt automatic correction for common issues (deduplication, fixing dangling deps, filling required fields) and optionally re-invoke the architect with the validation errors to produce corrected output. This keeps the user experience smooth — `quasar nebula generate` should produce a valid nebula on the first try in the vast majority of cases.

Additionally, the `Validate` function itself needs a small extension to return structured error types that the correction logic can pattern-match on, rather than just string formatting.

## Solution

### 1. Structured Validation Error Categories

Extend `internal/nebula/validate.go` to tag `ValidationError` values with a category:

```go
// ValidationCategory classifies a validation error for programmatic handling.
type ValidationCategory string

const (
    ValCatMissingField  ValidationCategory = "missing_field"
    ValCatDuplicateID   ValidationCategory = "duplicate_id"
    ValCatUnknownDep    ValidationCategory = "unknown_dep"
    ValCatCycle          ValidationCategory = "cycle"
    ValCatInvalidGate   ValidationCategory = "invalid_gate"
    ValCatScopeOverlap  ValidationCategory = "scope_overlap"
    ValCatBoundsViolation ValidationCategory = "bounds_violation"
)
```

Add a `Category` field to `ValidationError`:

```go
type ValidationError struct {
    Category   ValidationCategory // Machine-readable category for programmatic handling
    PhaseID    string
    SourceFile string
    Field      string
    Err        error
}
```

Update each `errs = append(errs, ValidationError{...})` call in `Validate` to include the appropriate `Category`. This is a backward-compatible addition — existing code that reads `Err` or `Field` continues to work.

### 2. Auto-Correction Engine

Create `internal/nebula/correct.go` with the correction and retry logic:

```go
// correctValidationErrors applies automatic fixes for common validation
// errors. It returns the corrected phases, a list of applied fixes, and
// any remaining errors that could not be auto-corrected.
func correctValidationErrors(
    phases []PhaseSpec,
    manifest Manifest,
    errs []ValidationError,
) (corrected []PhaseSpec, fixes []string, remaining []ValidationError)
```

Correction strategies by category:

- **`missing_field`**: If `id` is empty, derive from title (slugify). If `title` is empty, derive from id (un-slugify). If `type` is empty, apply default.
- **`duplicate_id`**: Append a numeric suffix to the duplicate (`-2`, `-3`, etc.). Update all `depends_on` references that pointed to the original ID.
- **`unknown_dep`**: Remove the dangling `depends_on` entry. Log a warning that the dependency was dropped.
- **`invalid_gate`**: Reset to empty string (inherit from manifest default).
- **`bounds_violation`**: Clamp negative values to 0.
- **`scope_overlap`**: If both phases can safely overlap (neither modifies shared state), set `allow_scope_overlap = true` on both. Otherwise, add a dependency edge from the lower-priority phase to the higher-priority one.
- **`cycle`**: Cannot be auto-corrected — this remains in the `remaining` slice.

Also in `internal/nebula/correct.go`, add the retry function:

```go
// retryWithFeedback re-invokes the architect agent with validation errors
// as additional context, asking it to produce corrected phases.
func retryWithFeedback(
    ctx context.Context,
    invoker agent.Invoker,
    req GenerateRequest,
    prevResult *GenerateResult,
    errors []ValidationError,
) (*GenerateResult, error)
```

This builds a prompt that includes:
- The previously generated phases (as context)
- The specific validation errors
- Instructions to fix the issues while preserving the overall structure

The retry is attempted at most once (`maxRetries = 1`) to avoid infinite loops and budget exhaustion.

### 3. Integration into Generate Pipeline

Add a top-level `CorrectAndRetry` function in `internal/nebula/correct.go` that the `Generate` function (from phase 03) calls:

```go
// CorrectAndRetry runs auto-correction on validation errors and optionally
// retries the architect agent if unresolvable errors remain. It returns
// the corrected result, or the original with error annotations if
// correction fails.
func CorrectAndRetry(
    ctx context.Context,
    invoker agent.Invoker,
    req GenerateRequest,
    result *GenerateResult,
    valErrs []ValidationError,
) (*GenerateResult, error)
```

This function encapsulates the full correction pipeline:
1. Run `correctValidationErrors` for automatic fixes
2. If remaining errors exist, call `retryWithFeedback` (at most once)
3. Return the final result with all fix annotations

The `Generate` function in `generate.go` calls `CorrectAndRetry` after initial validation, keeping the correction logic cleanly separated in its own file.

### Testing

Create `internal/nebula/correct_test.go`:

- **Auto-correct missing fields**: Generate phases with empty IDs/titles, verify `correctValidationErrors` fills them in.
- **Auto-correct duplicate IDs**: Two phases with ID `"setup"` become `"setup"` and `"setup-2"`, and `depends_on` references are updated.
- **Auto-correct dangling deps**: A phase depending on nonexistent `"foo"` has the dependency removed with a warning.
- **Auto-correct invalid gate**: Phase with `gate = "invalid"` gets reset to `""`.
- **Remaining cycle errors**: Circular dependencies are not auto-corrected and appear in `remaining`.
- **Retry with feedback**: Mock invoker returns corrected output on second call. Verify the retry prompt includes error descriptions.
- **No retry on success**: When auto-correction resolves all errors, no retry invocation occurs.

Add a `Category` field test in the correction tests:
- Verify each error type in `Validate` has the correct `Category` set.

## Files

- `internal/nebula/correct.go` — New file: `correctValidationErrors`, `retryWithFeedback`, `CorrectAndRetry`
- `internal/nebula/correct_test.go` — New file: tests for auto-correction and retry logic
- `internal/nebula/validate.go` — Modify: add `ValidationCategory` type, constants, and `Category` field to `ValidationError`
- `internal/nebula/generate.go` — Modify (minor): call `CorrectAndRetry` after validation in `Generate`

## Acceptance Criteria

- [ ] `ValidationError` has a `Category` field of type `ValidationCategory`
- [ ] All `Validate` error sites set the appropriate `Category` value
- [ ] `correctValidationErrors` fixes missing fields, duplicate IDs, dangling deps, invalid gates, and bounds violations
- [ ] Duplicate ID correction updates all `depends_on` references to the renamed ID
- [ ] Dangling dependency removal logs a warning string in the `fixes` return value
- [ ] Cycle errors are not auto-corrected and appear in the `remaining` slice
- [ ] `retryWithFeedback` re-invokes the architect with validation error context and processes the corrected output
- [ ] Retry is attempted at most once to prevent budget exhaustion
- [ ] The `Generate` function integrates auto-correction and retry transparently
- [ ] Adding `Category` to `ValidationError` is backward-compatible (existing code compiles without changes)
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` reports no issues
