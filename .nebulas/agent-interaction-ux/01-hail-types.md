+++
id = "hail-types"
title = "Define Hail types and the HailQueue interface"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Agents currently have no structured way to say "I need human input" during execution. The Discovery system in Fabric handles multi-agent coordination scenarios (file conflicts, entanglement disputes), but there's no equivalent for the common single-agent case where a coder or reviewer needs to escalate a decision to the human.

The reviewer's `NEEDS_HUMAN_REVIEW` flag is a post-hoc metadata field — it doesn't trigger any interactive behavior. Similarly, `requirements_ambiguity` discoveries are only visible in the Fabric snapshot, not surfaced as actionable prompts.

## Solution

Define a `Hail` type that represents a structured request from an agent to the human operator, and a `HailQueue` interface for posting and consuming them.

```go
// internal/loop/hail.go

type HailKind string

const (
    HailDecisionNeeded    HailKind = "decision_needed"     // Agent needs a choice made
    HailAmbiguity         HailKind = "ambiguity"           // Requirements unclear
    HailBlocker           HailKind = "blocker"             // Cannot proceed without input
    HailHumanReviewFlag   HailKind = "human_review"        // Reviewer flagged for human eyes
)

type Hail struct {
    ID          string
    PhaseID     string    // Empty in loop mode
    Cycle       int
    SourceRole  string    // "coder" or "reviewer"
    Kind        HailKind
    Summary     string    // One-line description
    Detail      string    // Full context
    Options     []string  // Optional: choices the human can pick from
    Resolution  string    // Filled by human response
    ResolvedAt  time.Time
    CreatedAt   time.Time
}

type HailQueue interface {
    Post(h Hail) error
    Unresolved() []Hail
    Resolve(id string, resolution string) error
}
```

A simple in-memory implementation is sufficient for now — hails don't need persistence across process restarts.

## Files

- `internal/loop/hail.go` — Hail type, HailKind constants, HailQueue interface, in-memory implementation

## Acceptance Criteria

- [ ] Hail type defined with all fields
- [ ] HailKind constants for the four kinds
- [ ] HailQueue interface with Post, Unresolved, Resolve
- [ ] In-memory MemoryHailQueue implementation with mutex safety
- [ ] Tests for post, unresolved filtering, and resolve lifecycle