+++
id = "advisor-agent"
title = "Advisor agent: natural language feedback to concrete FeedbackActions"
type = "feature"
priority = 2
depends_on = ["feedback-types"]
scope = ["internal/feedback/advisor.go", "internal/feedback/advisor_test.go"]
+++

## Problem

The `FeedbackQueue` (phase 4) carries typed `FeedbackAction`s, but user input arrives as natural language — "make that auth phase more conservative", "skip the migration", "add a test phase for the API". There is no mechanism to translate unstructured human intent into the structured `ActionKind` + `PhaseID` + `Payload` format that workers consume.

A human could manually construct `FeedbackAction`s, but that defeats the purpose of a conversational interface. An AI agent needs to interpret the natural language, understand the current nebula context (phase names, statuses, descriptions), and emit precise actions.

## Solution

### Advisor struct

```go
// internal/feedback/advisor.go

package feedback

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    "github.com/aaronsalm/quasar/internal/agent"
)

// NebulaSummary provides the advisor with execution context so it
// can resolve phase names and understand the current state.
type NebulaSummary struct {
    Name        string
    Phases      []PhaseSummary
    TotalCost   float64
    ElapsedTime string
}

// PhaseSummary is a lightweight view of a phase for advisor context.
type PhaseSummary struct {
    ID          string
    Title       string
    Status      string
    CyclesUsed  int
    MaxCycles   int
    CostUSD     float64
    Description string // First 200 chars of phase body.
}

// Advisor interprets natural language feedback items into concrete
// FeedbackActions. It uses an AI agent to understand the user's
// intent in the context of the running nebula.
type Advisor struct {
    invoker agent.Invoker
    model   string

    // ConfidenceThreshold controls the minimum confidence for
    // auto-applying actions. Actions below this threshold are
    // flagged for human confirmation.
    ConfidenceThreshold float64
}

// NewAdvisor creates an advisor with the given invoker and model.
func NewAdvisor(invoker agent.Invoker, model string) *Advisor {
    return &Advisor{
        invoker:             invoker,
        model:               model,
        ConfidenceThreshold: 0.7,
    }
}
```

### Interpret method

```go
// Interpret translates a FeedbackItem into zero or more FeedbackActions.
// It considers the nebula context to resolve phase references and
// determine appropriate action types.
//
// A single feedback item may produce multiple actions (e.g., "pause auth
// and skip migration" → two actions). Returns an empty slice if the
// input cannot be interpreted.
func (a *Advisor) Interpret(ctx context.Context, item FeedbackItem, nebula NebulaSummary) ([]FeedbackAction, error) {
    prompt := a.buildPrompt(item, nebula)

    resp, err := a.invoker.Invoke(ctx, prompt, agent.InvokeOptions{
        Model: a.model,
    })
    if err != nil {
        return nil, fmt.Errorf("advisor invoke: %w", err)
    }

    actions, err := a.parseActions(resp, item.ID)
    if err != nil {
        return nil, fmt.Errorf("advisor parse: %w", err)
    }

    return actions, nil
}
```

### Prompt construction

```go
func (a *Advisor) buildPrompt(item FeedbackItem, nebula NebulaSummary) string {
    var b strings.Builder
    b.WriteString("You are the Advisor agent for Quasar. Interpret user feedback into actions.\n\n")

    b.WriteString("## Current nebula: " + nebula.Name + "\n")
    b.WriteString(fmt.Sprintf("Cost: $%.4f | Elapsed: %s\n\n", nebula.TotalCost, nebula.ElapsedTime))

    b.WriteString("## Phases:\n")
    for _, p := range nebula.Phases {
        b.WriteString(fmt.Sprintf("- %s: \"%s\" [%s] %d/%d cycles $%.4f\n",
            p.ID, p.Title, p.Status, p.CyclesUsed, p.MaxCycles, p.CostUSD))
    }

    b.WriteString("\n## User feedback:\n")
    b.WriteString(item.Content + "\n")
    if item.TargetPhaseID != "" {
        b.WriteString(fmt.Sprintf("(targeting phase: %s)\n", item.TargetPhaseID))
    }

    b.WriteString("\n## Available actions:\n")
    b.WriteString("- refactor-phase: Modify a phase's description/constraints\n")
    b.WriteString("- adjust-priority: Change a phase's scheduling priority (1-4)\n")
    b.WriteString("- adjust-budget: Change max budget or cycle limit\n")
    b.WriteString("- add-constraint: Append a constraint to a phase\n")
    b.WriteString("- pause: Pause a phase or the nebula\n")
    b.WriteString("- resume: Resume a paused phase or nebula\n")
    b.WriteString("- skip: Skip a phase entirely\n")
    b.WriteString("- add-phase: Add a new phase dynamically\n")
    b.WriteString("- annotate: Add a human note to a phase\n")

    b.WriteString("\nRespond with a JSON array of actions:\n")
    b.WriteString(`[{"kind": "...", "phase_id": "...", "payload": "...", "confidence": 0.0-1.0}]`)
    b.WriteString("\n\nIf the feedback is unclear or you cannot determine an action, return an empty array [].\n")

    return b.String()
}
```

### Response parsing

```go
// advisorResponse is the JSON structure returned by the AI agent.
type advisorResponse struct {
    Kind       string  `json:"kind"`
    PhaseID    string  `json:"phase_id"`
    Payload    string  `json:"payload"`
    Confidence float64 `json:"confidence"`
}

func (a *Advisor) parseActions(resp *agent.Response, sourceItemID string) ([]FeedbackAction, error) {
    // Extract JSON array from response text.
    text := resp.Text
    start := strings.Index(text, "[")
    end := strings.LastIndex(text, "]")
    if start < 0 || end < 0 || end <= start {
        return nil, nil // No parseable actions.
    }

    var raw []advisorResponse
    if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
        return nil, fmt.Errorf("parse advisor JSON: %w", err)
    }

    actions := make([]FeedbackAction, 0, len(raw))
    for _, r := range raw {
        action := FeedbackAction{
            Kind:         ActionKind(r.Kind),
            SourceItemID: sourceItemID,
            PhaseID:      r.PhaseID,
            Payload:      r.Payload,
            Confidence:   r.Confidence,
            Timestamp:    time.Now(),
        }
        // Validate action kind.
        if !isValidActionKind(action.Kind) {
            continue
        }
        actions = append(actions, action)
    }
    return actions, nil
}

func isValidActionKind(kind ActionKind) bool {
    switch kind {
    case ActionRefactorPhase, ActionAdjustPriority, ActionAdjustBudget,
        ActionAddConstraint, ActionPause, ActionResume, ActionSkip,
        ActionAddPhase, ActionAnnotate:
        return true
    }
    return false
}
```

### Pipeline: item → advisor → queue

```go
// Process is a convenience method that interprets a FeedbackItem and
// enqueues the resulting actions. Items below the confidence threshold
// are flagged with a low-confidence marker.
func (a *Advisor) Process(ctx context.Context, item FeedbackItem, nebula NebulaSummary, queue *FeedbackQueue) error {
    queue.RecordItem(item)

    actions, err := a.Interpret(ctx, item, nebula)
    if err != nil {
        return err
    }

    for _, action := range actions {
        if err := queue.Enqueue(action); err != nil {
            return err
        }
    }
    return nil
}
```

## Files

- `internal/feedback/advisor.go` — `Advisor`, `NebulaSummary`, `PhaseSummary`, `NewAdvisor`, `Interpret`, `Process`, `buildPrompt`, `parseActions`, `isValidActionKind`
- `internal/feedback/advisor_test.go` — tests for: mock invoker returns valid JSON actions, multiple actions from one input, empty array for unclear input, invalid action kind filtered, confidence threshold respected, phase ID resolution, malformed JSON returns empty slice

## Acceptance Criteria

- [ ] `Advisor.Interpret` translates natural language into typed `FeedbackAction`s
- [ ] A single feedback item can produce multiple actions
- [ ] Invalid action kinds are filtered out (not crash)
- [ ] Malformed JSON from the AI gracefully returns empty slice (not error)
- [ ] The prompt includes full nebula context: phase IDs, titles, statuses, costs
- [ ] `Process` records the item in history and enqueues resulting actions
- [ ] `ConfidenceThreshold` is configurable (default 0.7)
- [ ] `isValidActionKind` accepts all nine defined action kinds
- [ ] `go test ./internal/feedback/...` passes
- [ ] `go vet ./internal/feedback/...` passes
