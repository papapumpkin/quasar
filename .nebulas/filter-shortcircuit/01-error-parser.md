+++
id = "error-parser"
title = "Structured error parser for Go toolchain output"
type = "feature"
priority = 1
labels = ["quasar", "filter", "cost-optimization"]
scope = ["internal/filter/errors.go", "internal/filter/errors_test.go"]
+++

## Problem

When a filter check fails, `runFilterChecks` in `internal/loop/loop.go` receives the raw `CheckResult.Output` string — an unstructured blob of text from `go build`, `go vet`, `golangci-lint`, or `go test`. This output is currently stuffed into a synthetic `ReviewFinding` with a `truncate(failure.Output, 3000)` call and bounced to the coder as a full-cycle retry.

The coder then receives the entire raw output in its next prompt and must re-read all project context to figure out what went wrong. This is wasteful: a build error on line 42 of `internal/loop/loop.go` should not require the coder to re-read the whole task description, reviewer findings, and 3KB of raw output. We need a parser that extracts structured error locations from each tool's output format.

## Solution

Create `internal/filter/errors.go` with types and parsers for structured error extraction.

### Types

```go
// FilterError represents a single structured error extracted from tool output.
type FilterError struct {
    File    string // relative file path (e.g. "internal/loop/loop.go")
    Line    int    // 1-based line number, 0 if unknown
    Column  int    // 1-based column, 0 if unknown
    Message string // the error message text
    Tool    string // which tool produced this: "build", "vet", "lint", "test"
}

// ParseResult holds all errors extracted from a single check's output.
type ParseResult struct {
    Errors  []FilterError // structured errors, may be empty if parsing fails
    RawOutput string      // original output preserved as fallback
    CheckName string      // "build", "vet", "lint", "test"
}
```

### Parser Functions

```go
// ParseCheckOutput extracts structured errors from a CheckResult.
// It dispatches to the appropriate format parser based on CheckResult.Name.
// If no structured errors can be extracted, ParseResult.Errors will be empty
// and callers should fall back to RawOutput.
func ParseCheckOutput(cr CheckResult) ParseResult

// parseBuildErrors handles `go build ./...` output.
// Format: <file>:<line>:<col>: <message>
// Example: ./internal/loop/loop.go:42:15: undefined: foo
func parseBuildErrors(output string) []FilterError

// parseVetErrors handles `go vet ./...` output.
// Format: <file>:<line>:<col>: <message> (often prefixed with "vet: ")
// Also handles the "# <package>" header lines.
func parseVetErrors(output string) []FilterError

// parseLintErrors handles `golangci-lint run` output.
// Format: <file>:<line>:<col>: <message> (<linter-name>)
// Example: internal/loop/loop.go:42:15: SA1029: ... (staticcheck)
func parseLintErrors(output string) []FilterError

// parseTestErrors handles `go test ./...` output.
// Extracts both compilation errors (same as build) and test failure locations.
// Format for failures: --- FAIL: TestName (0.00s)
//   <file>:<line>: <assertion message>
// Also extracts panic stack traces: <file>.go:<line> +0x...
func parseTestErrors(output string) []FilterError
```

### Parsing Strategy

All four Go tools share the `file:line:col: message` convention for compilation errors. The core regex pattern is:

```go
var errLineRe = regexp.MustCompile(`^([^\s:]+\.go):(\d+):(\d+):\s*(.+)$`)
```

Additional patterns per tool:
- **build/vet**: Skip lines starting with `#` (package headers). Vet sometimes prefixes with `vet:`.
- **lint**: Strip trailing `(<linter-name>)` from the message, but preserve the linter name for context.
- **test**: Also match `<file>:<line>: <message>` (2-field, no column) for test assertion failures. Match `--- FAIL: TestName` to associate failures with test names.

When a line doesn't match any pattern, skip it — the raw output fallback covers unstructured cases.

### Deduplication

Multiple errors on the same `File:Line` should be preserved (they may be different errors). But identical `File:Line:Message` triples should be deduplicated, since `go build` and `go vet` can repeat the same error when multiple packages import a broken file.

## Files

- `internal/filter/errors.go` — **NEW**: `FilterError`, `ParseResult`, `ParseCheckOutput`, per-tool parsers
- `internal/filter/errors_test.go` — **NEW**: table-driven tests with real tool output samples

## Acceptance Criteria

- [ ] `FilterError` struct captures file, line, column, message, and tool name
- [ ] `ParseCheckOutput` dispatches to the correct parser based on `CheckResult.Name`
- [ ] `parseBuildErrors` correctly parses `go build` output with `file:line:col: message` format
- [ ] `parseVetErrors` handles `go vet` output including `# package` header lines
- [ ] `parseLintErrors` handles `golangci-lint` output and extracts linter names
- [ ] `parseTestErrors` handles both compilation errors and test assertion failures
- [ ] Duplicate `File:Line:Message` triples are deduplicated
- [ ] Unparseable lines are silently skipped (graceful degradation to RawOutput fallback)
- [ ] Table-driven tests cover each parser with real-world output samples
- [ ] `go build ./...` and `go test ./internal/filter/...` pass
