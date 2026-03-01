+++
id = "fix-prompt-builder"
title = "Hyper-focused fix prompt builder for filter errors"
type = "feature"
priority = 1
depends_on = ["error-parser"]
labels = ["quasar", "filter", "cost-optimization"]
scope = ["internal/loop/prompts.go", "internal/loop/prompts_test.go"]
+++

## Problem

When a filter check fails today, `runFilterChecks` creates a synthetic `ReviewFinding` with the full raw output and the coder receives it as part of the standard `buildCoderPrompt` on the next outer cycle. This means the coder gets the entire task description, all accumulated reviewer findings, AND the filter error — a bloated prompt that wastes tokens on context irrelevant to a mechanical fix.

The existing `buildLintFixPrompt` in `internal/loop/prompts.go` is a step in the right direction — it constructs a focused prompt for lint issues. But it still sends the full raw lint output without extracting structured locations. The new filter short-circuit needs a prompt builder that leverages `filter.ParseResult` to send only the specific errors and the affected file paths.

## Solution

Add a new prompt builder function to `internal/loop/prompts.go` that creates a minimal, hyper-focused fix prompt from parsed filter errors.

### New Function

```go
// buildFilterFixPrompt constructs a minimal prompt for the coder to fix
// specific filter errors. It includes only the failing check name, the
// structured errors (file, line, message), and instructions to fix them.
// When no structured errors were parsed, it falls back to including the
// raw output (similar to the current behavior but with tighter framing).
func (l *Loop) buildFilterFixPrompt(state *CycleState, parsed filter.ParseResult) string
```

### Prompt Structure

The prompt should be structured in three sections:

**1. Context header** (minimal — just bead ID and check name):
```
Task (bead BEAD-123): Fix failing {check_name} check

Your code failed the {check_name} filter check. Fix ONLY the errors listed below.
Do not refactor, do not add features, do not change anything unrelated to these errors.
```

**2. Error list** (from `ParseResult.Errors`, or raw fallback):

When structured errors exist:
```
ERRORS:
1. internal/loop/loop.go:42:15 — undefined: foo
2. internal/loop/loop.go:58:3 — cannot use x (type int) as type string
3. internal/filter/chain.go:10:1 — imported and not used: "fmt"

AFFECTED FILES:
- internal/loop/loop.go (2 errors)
- internal/filter/chain.go (1 error)
```

When no structured errors were parsed (fallback):
```
RAW OUTPUT:
{truncated raw output, max 2000 chars}
```

**3. Instructions** (action-oriented, tight scope):
```
Read each affected file, fix the listed errors, and verify your fix compiles.
Do NOT make any other changes. Stay focused on these specific errors.
```

### File Grouping

Group errors by file path and sort files by error count (descending) so the coder sees the most impactful files first. Within each file, sort by line number ascending.

### Budget Signal

The prompt should include a budget hint so the coder agent keeps its response tight:

```
This is a targeted fix pass. Keep your budget under $0.10 — read the affected files, apply minimal fixes, and stop.
```

The actual budget value comes from a new helper:

```go
// filterFixBudget returns a per-invocation budget for filter fix attempts.
// It's smaller than perAgentBudget since these should be quick, mechanical fixes.
func (l *Loop) filterFixBudget() float64 {
    full := l.perAgentBudget()
    if full <= 0 {
        return 0
    }
    // Use 1/4 of the normal per-agent budget for targeted fixes.
    return full / 4
}
```

### Integration with Existing Prompts

The existing `buildLintFixPrompt` should be refactored to delegate to `buildFilterFixPrompt` when a `ParseResult` is available, preserving backward compatibility for the `runLintFixLoop` code path. This avoids duplicating prompt logic.

## Files

- `internal/loop/prompts.go` — Add `buildFilterFixPrompt`, `filterFixBudget`; refactor `buildLintFixPrompt` to share logic
- `internal/loop/prompts_test.go` — Table-driven tests for the new prompt builder

## Acceptance Criteria

- [ ] `buildFilterFixPrompt` produces a focused prompt with structured error list when `ParseResult.Errors` is non-empty
- [ ] Falls back to raw output when `ParseResult.Errors` is empty
- [ ] Errors are grouped by file and sorted by line number
- [ ] Affected files section shows file paths with error counts
- [ ] Prompt includes budget hint derived from `filterFixBudget()`
- [ ] `filterFixBudget()` returns 1/4 of `perAgentBudget()`
- [ ] `buildLintFixPrompt` is refactored to share logic with `buildFilterFixPrompt` (no duplication)
- [ ] Table-driven tests cover structured errors, raw fallback, empty errors, and multi-file grouping
- [ ] `go build ./...` and `go test ./internal/loop/...` pass
