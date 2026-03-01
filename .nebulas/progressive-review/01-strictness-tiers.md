+++
id = "strictness-tiers"
title = "Define ReviewStrictness type and tier constants"
type = "task"
priority = 1
depends_on = []
labels = ["quasar", "reviewer", "cost-optimization"]
+++

## Problem

The reviewer currently applies the same level of scrutiny on every cycle. This wastes tokens on early cycles where the code is still directionally evolving, and under-scrutinizes late cycles where polish matters. There is no concept of reviewer strictness in the codebase — `DefaultReviewerSystemPrompt` in `internal/agent/reviewer.go` is a single monolithic constant used for all cycles.

## Solution

Introduce a `ReviewStrictness` type and three sentinel tier values in `internal/agent/reviewer.go`, co-located with the existing `DefaultReviewerSystemPrompt` constant. Each tier describes the reviewer's focus area and approval threshold for a range of cycles.

Add the following to `internal/agent/reviewer.go`:

```go
// ReviewStrictness represents how strictly the reviewer evaluates code.
type ReviewStrictness int

const (
    // StrictnessLenient is for early cycles (1-2). The reviewer focuses on
    // approach correctness and approves if the implementation is directionally
    // right, even if naming or docs are imperfect.
    StrictnessLenient ReviewStrictness = iota

    // StrictnessStandard is for mid cycles (3-4). The reviewer verifies
    // correctness, error handling, and test coverage with normal rigor.
    StrictnessStandard

    // StrictnessStrict is for late cycles (5+). The reviewer performs a
    // polish pass covering naming, documentation, edge cases, and
    // performance — the full review dimension set.
    StrictnessStrict
)
```

Also add a `String()` method on `ReviewStrictness` so it can be logged and displayed by `ui.Printer`:

```go
func (s ReviewStrictness) String() string {
    switch s {
    case StrictnessLenient:
        return "lenient"
    case StrictnessStandard:
        return "standard"
    case StrictnessStrict:
        return "strict"
    default:
        return "unknown"
    }
}
```

## Files

- `internal/agent/reviewer.go` — Add `ReviewStrictness` type, three constants (`StrictnessLenient`, `StrictnessStandard`, `StrictnessStrict`), and `String()` method below the existing `DefaultReviewerSystemPrompt` constant.

## Acceptance Criteria

- [ ] `ReviewStrictness` is an exported `int` type in package `agent`
- [ ] Three exported constants (`StrictnessLenient`, `StrictnessStandard`, `StrictnessStrict`) use `iota`
- [ ] Each constant has a GoDoc comment describing the cycle range and reviewer behavior
- [ ] `String()` returns `"lenient"`, `"standard"`, or `"strict"` respectively
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] The existing `DefaultReviewerSystemPrompt` constant is unchanged
