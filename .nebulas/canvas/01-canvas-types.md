+++
id = "canvas-types"
title = "Define core canvas data types for conversational nebula authoring"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/canvas/**"]
+++

## Problem

Quasar has a mature nebula system (`internal/nebula/types.go`) with `Nebula`, `Manifest`, `PhaseSpec`, and related types for representing finalized, on-disk nebula specifications. However, there is no data model for the conversational authoring process itself — the evolving state of a session where a developer and an architect agent iteratively shape a nebula from scratch.

The authoring process needs mutable draft types that track conversation history, accumulate draft phases, and hold intermediate state that hasn't been validated or serialized yet. These types are fundamentally different from the immutable `nebula.PhaseSpec` (which represents a parsed, validated phase file) because drafts are in-progress, may be incomplete, and change as the conversation evolves.

## Solution

Create `internal/canvas/canvas.go` with the core types that model a canvas session.

### Session

The root type representing an entire conversational authoring session:

```go
// Session represents a conversational nebula authoring session between
// a developer and an architect agent. It tracks the full conversation
// history and the evolving draft nebula being constructed.
type Session struct {
    ID        string       `json:"id"`
    Name      string       `json:"name"`
    State     SessionState `json:"state"`
    Turns     []Turn       `json:"turns"`
    Draft     DraftNebula  `json:"draft"`
    CreatedAt time.Time    `json:"created_at"`
    UpdatedAt time.Time    `json:"updated_at"`
}
```

### SessionState

An enum tracking the session lifecycle:

```go
type SessionState string

const (
    SessionStateActive    SessionState = "active"
    SessionStatePaused    SessionState = "paused"
    SessionStateGenerated SessionState = "generated" // nebula files written
    SessionStateAbandoned SessionState = "abandoned"
)
```

### Turn

A single exchange in the conversation:

```go
// Turn represents one message in the canvas conversation.
// Turns alternate between the user describing what they want
// and the architect agent proposing structure, asking questions,
// or refining draft phases.
type Turn struct {
    Role      TurnRole  `json:"role"`
    Content   string    `json:"content"`
    Timestamp time.Time `json:"timestamp"`
}

type TurnRole string

const (
    TurnRoleUser      TurnRole = "user"
    TurnRoleArchitect TurnRole = "architect"
)
```

### DraftNebula

A mutable representation of the nebula being constructed. Unlike `nebula.Manifest`, all fields are optional and progressively filled in:

```go
// DraftNebula holds the evolving nebula specification being
// constructed during a canvas session. Fields are progressively
// populated as the conversation clarifies requirements.
type DraftNebula struct {
    Name        string       `json:"name"`
    Description string       `json:"description"`
    Goals       []string     `json:"goals"`
    Constraints []string     `json:"constraints"`
    Phases      []DraftPhase `json:"phases"`
    Execution   DraftExecution `json:"execution"`
}
```

### DraftPhase

A mutable draft of a phase that mirrors `nebula.PhaseSpec` but is designed for in-progress editing:

```go
// DraftPhase is a mutable representation of a nebula phase being
// constructed during a canvas session. It mirrors nebula.PhaseSpec
// but all fields are optional and evolve during the conversation.
type DraftPhase struct {
    ID          string   `json:"id"`
    Title       string   `json:"title"`
    Type        string   `json:"type"`
    Priority    int      `json:"priority"`
    DependsOn   []string `json:"depends_on"`
    Scope       []string `json:"scope"`
    Body        string   `json:"body"`        // Markdown body (Problem/Solution/Files/Criteria)
    BudgetUSD   float64  `json:"budget_usd"`
    Model       string   `json:"model"`
    Gate        string   `json:"gate"`
}
```

### DraftExecution

Execution configuration for the draft nebula:

```go
// DraftExecution holds execution configuration being refined
// during the canvas conversation.
type DraftExecution struct {
    MaxWorkers      int     `json:"max_workers"`
    MaxReviewCycles int     `json:"max_review_cycles"`
    MaxBudgetUSD    float64 `json:"max_budget_usd"`
    Model           string  `json:"model"`
    Gate            string  `json:"gate"`
}
```

### Constructor and Helper Methods

```go
// NewSession creates a new canvas session with a generated UUID and
// active state.
func NewSession(name string) *Session

// AddTurn appends a new turn to the session and updates the timestamp.
func (s *Session) AddTurn(role TurnRole, content string)

// AddPhase appends a draft phase to the session's draft nebula.
func (s *Session) AddPhase(phase DraftPhase)

// UpdatePhase replaces a draft phase by ID. Returns an error if
// the phase ID is not found.
func (s *Session) UpdatePhase(phase DraftPhase) error

// RemovePhase removes a draft phase by ID. Returns an error if
// the phase ID is not found.
func (s *Session) RemovePhase(id string) error

// PhaseIDs returns the list of draft phase IDs in order.
func (s *Session) PhaseIDs() []string

// ConversationContext builds a string representation of the full
// conversation history suitable for inclusion in an architect prompt.
func (s *Session) ConversationContext() string
```

The `ConversationContext` method uses `strings.Builder` to format turns as:
```
[user]: <content>
[architect]: <content>
...
```

### Conversion to Nebula Types

```go
// ToManifest converts the draft nebula into a nebula.Manifest
// suitable for writing to nebula.toml.
func (d *DraftNebula) ToManifest() nebula.Manifest

// ToPhaseSpec converts a draft phase into a nebula.PhaseSpec
// suitable for writing to a phase markdown file.
func (p *DraftPhase) ToPhaseSpec() nebula.PhaseSpec
```

These conversion methods bridge the mutable canvas types to the immutable nebula types, enabling validation and serialization through the existing `nebula.Validate()` and `nebula.MarshalPhaseFile()` functions.

## Files

- `internal/canvas/canvas.go` — all types (`Session`, `Turn`, `DraftNebula`, `DraftPhase`, `DraftExecution`, `SessionState`, `TurnRole`), constructors, and methods
- `internal/canvas/canvas_test.go` — table-driven tests for `NewSession`, `AddTurn`, `AddPhase`, `UpdatePhase`, `RemovePhase`, `ConversationContext`, `ToManifest`, `ToPhaseSpec`

## Acceptance Criteria

- [ ] `Session` type has `ID`, `Name`, `State`, `Turns`, `Draft`, `CreatedAt`, `UpdatedAt` fields with JSON tags
- [ ] `Turn` type has `Role`, `Content`, `Timestamp` fields with JSON tags
- [ ] `DraftNebula` has `Name`, `Description`, `Goals`, `Constraints`, `Phases`, `Execution` fields
- [ ] `DraftPhase` mirrors `nebula.PhaseSpec` fields relevant to authoring (`ID`, `Title`, `Type`, `Priority`, `DependsOn`, `Scope`, `Body`, `BudgetUSD`, `Model`, `Gate`)
- [ ] `NewSession` generates a UUID-based ID and sets state to `SessionStateActive`
- [ ] `AddTurn` appends a turn with the current timestamp and updates `Session.UpdatedAt`
- [ ] `UpdatePhase` returns an error when the phase ID is not found
- [ ] `RemovePhase` returns an error when the phase ID is not found
- [ ] `ConversationContext` formats turns as `[role]: content` separated by newlines
- [ ] `ToManifest` produces a valid `nebula.Manifest` with all populated fields mapped correctly
- [ ] `ToPhaseSpec` produces a valid `nebula.PhaseSpec` with `Body` field preserved
- [ ] All types are JSON-serializable (needed for session persistence in phase 5)
- [ ] `go test ./internal/canvas/...` passes with at least 10 test cases
- [ ] `go vet ./internal/canvas/...` reports no issues
