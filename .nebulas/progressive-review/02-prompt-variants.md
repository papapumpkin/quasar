+++
id = "prompt-variants"
title = "Create tier-specific reviewer system prompt variants"
type = "task"
priority = 2
depends_on = ["strictness-tiers"]
labels = ["quasar", "reviewer", "cost-optimization"]
+++

## Problem

The current `DefaultReviewerSystemPrompt` contains all four review dimensions (Architecture, Code Quality, Tests, Performance) and applies them uniformly on every cycle. There is no way to tell the reviewer "focus on approach correctness" in early cycles or "do a polish pass" in late cycles. We need prompt variants that retain the same structured output format (ISSUE/APPROVED/REPORT blocks) but narrow the reviewer's focus per tier.

## Solution

Add a `ReviewerPromptForStrictness(strictness ReviewStrictness) string` function in `internal/agent/reviewer.go` that returns the appropriate system prompt for the given tier. Each variant shares the same preamble ("You are a senior software engineer...") and the same output format (Issue Format, Approval, Report Block sections from `DefaultReviewerSystemPrompt`), but differs in the Review Dimensions section and the approval threshold guidance.

The three variants:

### StrictnessLenient (cycles 1-2)

Replace the Review Dimensions section with a focused block:

```
## Review Focus — Early Cycle (Lenient)

This is an early iteration. Focus on whether the approach is fundamentally sound.
Approve if the implementation is directionally correct, even if polish is lacking.

Evaluate ONLY:
- Is the overall approach/architecture reasonable for the task?
- Are there any critical correctness bugs that would make the code wrong?
- Are there any security vulnerabilities or data-loss risks?

DO NOT flag: naming style, missing docs, minor edge cases, test coverage gaps,
or performance concerns — those will be caught in later cycles.
```

### StrictnessStandard (cycles 3-4)

Replace the Review Dimensions section with:

```
## Review Focus — Mid Cycle (Standard)

The approach has been established. Now verify implementation quality with normal rigor.

Evaluate:
### 1. Correctness
- Does the code do what the task requires? Are there logic errors?
- Are error paths handled with context? Any silently discarded errors?

### 2. Error Handling
- Are errors propagated with context (fmt.Errorf wrapping)?
- Are boundary conditions and nil/empty inputs handled?

### 3. Test Coverage
- Are there tests for the new/changed code?
- Do tests cover failure modes, not just the happy path?
- Are tests well-structured (table-driven, clear assertions)?

DO NOT flag: minor naming preferences, documentation style, or performance
micro-optimizations unless they introduce correctness issues.
```

### StrictnessStrict (cycles 5+)

Use the existing `DefaultReviewerSystemPrompt` as-is — it already covers all four dimensions with full rigor. Prepend a short preamble:

```
## Review Focus — Late Cycle (Strict)

This is a late-cycle polish pass. Apply full rigor across all dimensions.
The code should be near-final quality. Flag anything that does not meet
production standards.
```

Then include all four original dimensions (Architecture, Code Quality, Tests, Performance) verbatim.

### Implementation approach

Build each variant by composing shared sections. Extract the preamble (lines 1-5 of `DefaultReviewerSystemPrompt`), the Issue Format / Approval / Report Block sections, and the dimension-specific content as internal string constants. `ReviewerPromptForStrictness` assembles the right combination using `strings.Builder`.

```go
// ReviewerPromptForStrictness returns the reviewer system prompt tuned for the
// given strictness tier. The output format (ISSUE/APPROVED/REPORT blocks) is
// identical across all tiers; only the review focus and approval threshold change.
func ReviewerPromptForStrictness(s ReviewStrictness) string {
    switch s {
    case StrictnessLenient:
        return buildReviewerVariant(reviewFocusLenient)
    case StrictnessStandard:
        return buildReviewerVariant(reviewFocusStandard)
    default:
        return buildReviewerVariant(reviewFocusStrict)
    }
}
```

Where `buildReviewerVariant` is an unexported helper that concatenates: shared preamble + focus section + shared output format.

## Files

- `internal/agent/reviewer.go` — Add `ReviewerPromptForStrictness` function, unexported `buildReviewerVariant` helper, and the three focus section constants (`reviewFocusLenient`, `reviewFocusStandard`, `reviewFocusStrict`). Keep `DefaultReviewerSystemPrompt` unchanged for backward compatibility.

## Acceptance Criteria

- [ ] `ReviewerPromptForStrictness(StrictnessLenient)` returns a prompt that mentions "early cycle" and omits Tests/Performance dimensions
- [ ] `ReviewerPromptForStrictness(StrictnessStandard)` returns a prompt that mentions "mid cycle" and includes Correctness, Error Handling, Test Coverage
- [ ] `ReviewerPromptForStrictness(StrictnessStrict)` returns a prompt that includes all four original review dimensions
- [ ] All three variants contain the ISSUE format block, APPROVED block, and REPORT block from `DefaultReviewerSystemPrompt`
- [ ] `DefaultReviewerSystemPrompt` is not modified — `ReviewerPromptForStrictness(StrictnessStrict)` uses it as a base, not a replacement
- [ ] Unit tests verify each tier's prompt contains expected keywords and omits others
- [ ] `go build ./...` and `go vet ./...` pass
