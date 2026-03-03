+++
id = "architect-agent"
title = "Build canvas architect agent with nebula-authoring-specific prompts"
type = "feature"
priority = 1
depends_on = ["canvas-types"]
scope = ["internal/canvas/agent.go", "internal/canvas/agent_test.go"]
+++

## Problem

The existing architect agent in `internal/nebula/architect.go` is designed for generating or refactoring individual phase files within an already-defined nebula. It operates in modes like `ArchitectModeCreate`, `ArchitectModeRefactor`, and `ArchitectModeDecompose` — all of which assume a `nebula.Nebula` already exists with a manifest and context.

Canvas needs a fundamentally different architect interaction: a conversational partner that starts from nothing, asks clarifying questions, proposes high-level phase breakdowns, suggests dependency graphs and scope boundaries, and iteratively refines the entire nebula structure. This is not a one-shot generation — it is a multi-turn dialogue where each architect response may update multiple draft phases simultaneously.

The existing `agent.Invoker` interface (`Invoke(ctx, agent, prompt, workDir)`) and `agent.Agent` struct (with `Role`, `SystemPrompt`, `Model`, `MaxBudgetUSD`) are the right building blocks. The canvas architect needs its own system prompt and prompt construction logic, but reuses the same invocation mechanism.

## Solution

Create `internal/canvas/agent.go` with a canvas-specific architect agent and prompt engineering.

### System Prompt

Define a `canvasArchitectSystemPrompt` constant that instructs the architect to:

1. **Understand nebula format**: Know that a nebula is a multi-phase task specification with `nebula.toml` manifest + `*.md` phase files with `+++` TOML frontmatter.
2. **Ask clarifying questions**: When the user's description is vague, ask specific questions about scope, constraints, testing requirements, and dependencies rather than guessing.
3. **Propose phase breakdowns**: Suggest how to decompose the work into 3-10 phases with clear boundaries and single responsibilities.
4. **Design dependency graphs**: Identify which phases depend on others and suggest a DAG that maximizes parallelism while respecting ordering constraints.
5. **Suggest scope isolation**: Recommend file glob patterns for each phase's `scope` field to prevent parallel workers from conflicting.
6. **Estimate budgets**: Based on phase complexity, suggest per-phase `max_budget_usd` and `max_review_cycles` values.
7. **Output structured updates**: When proposing changes to the draft, output them in a parseable JSON block so the canvas system can update `DraftNebula` state.

```go
const canvasArchitectSystemPrompt = `You are a senior software architect helping a developer plan a multi-phase coding project.

You are building a "nebula" — a structured task specification that will be executed by AI coding agents.

## Your Role

You are a conversational partner, not a one-shot generator. Your job is to:
1. Listen to the developer's goals and ask clarifying questions
2. Propose how to decompose their work into well-scoped phases
3. Design dependency graphs that maximize safe parallelism
4. Suggest file scope boundaries to prevent worker conflicts
5. Estimate budgets and review cycle counts per phase

## Nebula Structure

A nebula has:
- A manifest (nebula.toml) with name, description, goals, constraints, execution config
- Phase files (*.md) with TOML frontmatter (id, title, type, priority, depends_on, scope) and markdown body (Problem, Solution, Files, Acceptance Criteria)

## Conversation Guidelines

- Ask at most 2-3 questions per turn — don't overwhelm the developer
- After 2-3 turns of clarification, propose a concrete phase breakdown
- When proposing phases, explain WHY you chose the decomposition
- Flag potential scope overlaps between phases
- Suggest gate modes: "trust" for low-risk, "review" for standard, "approve" for high-risk

## Output Format

When you have concrete updates to propose, include a JSON block:

` + "```" + `json
{
  "action": "update_draft",
  "nebula": {
    "name": "...",
    "description": "...",
    "goals": ["..."],
    "constraints": ["..."]
  },
  "phases": [
    {
      "id": "phase-id",
      "title": "Short title",
      "type": "feature",
      "priority": 2,
      "depends_on": [],
      "scope": ["internal/foo/**"],
      "body": "## Problem\n...\n## Solution\n...",
      "budget_usd": 15.0,
      "gate": "review"
    }
  ],
  "execution": {
    "max_workers": 2,
    "max_review_cycles": 5,
    "max_budget_usd": 80.0,
    "gate": "review"
  }
}
` + "```" + `

Only include the JSON block when you have concrete proposals. During early clarification turns, just converse naturally.
`
```

### CanvasAgent Constructor

```go
// CanvasAgent returns an agent.Agent configured for conversational
// nebula authoring with the canvas-specific system prompt.
func CanvasAgent(budget float64, model string) agent.Agent {
    return agent.Agent{
        Role:         agent.RoleArchitect,
        SystemPrompt: canvasArchitectSystemPrompt,
        MaxBudgetUSD: budget,
        Model:        model,
    }
}
```

### Prompt Builder

Build the user prompt for each turn by combining the conversation history with the current draft state:

```go
// BuildCanvasPrompt constructs the user prompt for a canvas architect
// invocation. It includes the full conversation history, the current
// draft nebula state (if any phases exist), and the user's latest message.
func BuildCanvasPrompt(session *Session, userMessage string) string
```

The prompt builder uses `strings.Builder` and includes:
1. **Conversation history** via `session.ConversationContext()` — provides full context of prior turns
2. **Current draft state** — serialized `DraftNebula` as JSON so the architect can see what's been proposed and accepted
3. **Latest user message** — the new input from the developer

### Response Parser

Parse the architect's response to extract both conversational text and structured draft updates:

```go
// CanvasResponse holds the parsed architect response, separating
// conversational text from structured draft updates.
type CanvasResponse struct {
    Text        string       // Conversational text (questions, explanations)
    DraftUpdate *DraftUpdate // Structured update, nil if none proposed
}

// DraftUpdate represents a structured proposal from the architect
// to modify the draft nebula.
type DraftUpdate struct {
    Action    string         `json:"action"`
    Nebula    *DraftNebula   `json:"nebula,omitempty"`
    Phases    []DraftPhase   `json:"phases,omitempty"`
    Execution *DraftExecution `json:"execution,omitempty"`
}

// ParseCanvasResponse extracts conversational text and any structured
// JSON draft update block from the architect's raw response.
func ParseCanvasResponse(raw string) (*CanvasResponse, error)
```

`ParseCanvasResponse` scans for a fenced JSON code block (` ```json ... ``` `), extracts and unmarshals it as a `DraftUpdate`, and returns the remaining text as conversational content. If no JSON block is found, `DraftUpdate` is nil (the architect is just asking questions or explaining).

### Applying Updates

```go
// ApplyUpdate merges a DraftUpdate into the session's draft nebula.
// It adds new phases, updates existing phases (matched by ID), and
// updates nebula-level fields if provided.
func ApplyUpdate(session *Session, update *DraftUpdate)
```

`ApplyUpdate` handles three cases:
- New phases (ID not in current draft): appended
- Existing phases (ID matches): fields merged (non-zero values overwrite)
- Nebula-level fields (name, description, goals, constraints, execution): overwrite if non-empty

## Files

- `internal/canvas/agent.go` — `canvasArchitectSystemPrompt`, `CanvasAgent`, `BuildCanvasPrompt`, `CanvasResponse`, `DraftUpdate`, `ParseCanvasResponse`, `ApplyUpdate`
- `internal/canvas/agent_test.go` — tests for prompt construction, response parsing (with/without JSON blocks, malformed JSON, multiple JSON blocks), and update application (new phases, updated phases, nebula-level updates)

## Acceptance Criteria

- [ ] `CanvasAgent` returns an `agent.Agent` with `Role == agent.RoleArchitect` and the canvas-specific system prompt
- [ ] `BuildCanvasPrompt` includes conversation history, current draft state, and the latest user message
- [ ] `ParseCanvasResponse` correctly extracts a JSON `DraftUpdate` block from architect output
- [ ] `ParseCanvasResponse` returns `DraftUpdate == nil` when no JSON block is present (clarification turns)
- [ ] `ParseCanvasResponse` returns an error for malformed JSON blocks (not silently ignored)
- [ ] `ApplyUpdate` adds new phases that don't exist in the current draft
- [ ] `ApplyUpdate` updates existing phases by ID, merging non-zero field values
- [ ] `ApplyUpdate` updates nebula-level fields (name, description, goals, constraints) when provided
- [ ] The system prompt instructs the architect to ask clarifying questions before generating phases
- [ ] The system prompt documents the expected JSON output format with all `DraftPhase` fields
- [ ] `go test ./internal/canvas/...` passes with at least 12 test cases covering prompt building, response parsing, and update application
- [ ] `go vet ./internal/canvas/...` reports no issues
