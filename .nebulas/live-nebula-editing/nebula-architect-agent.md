+++
id = "nebula-architect-agent"
title = "Nebula architect agent for interactive phase creation and refactoring"
type = "feature"
priority = 2
depends_on = ["dynamic-dag-insertion", "refactor-prompt-injection"]
max_review_cycles = 5
max_budget_usd = 30.0
+++

## Problem

Creating and refactoring nebula phases requires understanding the TOML frontmatter format, writing good task descriptions, and reasoning about dependencies. Users shouldn't need to manually author `.md` files and figure out where a new phase fits in the DAG — an AI agent can help with this.

## Current State

**Phase file format**:
- TOML frontmatter between `+++` delimiters: `id`, `title`, `type`, `priority`, `depends_on`, `max_review_cycles`, `max_budget_usd`, `model`
- Markdown body with problem, solution, files to modify, acceptance criteria
- Must match `Defaults` from `nebula.toml`

**Existing agent infrastructure**:
- `internal/agent/agent.go` defines `Agent{Role, SystemPrompt, MaxBudgetUSD, Model}`
- `agent.Invoker` interface with `Invoke(ctx, agent, prompt, workDir)` → `Result`
- Claude CLI invoker satisfies this interface
- The coder and reviewer are just agents with different system prompts

**DAG insertion** (from `dynamic-dag-insertion` phase):
- Code can validate and insert new phases into the live DAG
- Cycle detection, dependency resolution already handled

## Solution

### 1. Architect Agent Definition

Define a new agent role "architect" with a specialized system prompt:

```go
// internal/agent/roles.go or similar
func ArchitectAgent(budget float64, model string) Agent {
    return Agent{
        Role:         "architect",
        SystemPrompt: architectSystemPrompt,
        MaxBudgetUSD: budget,
        Model:        model,
    }
}
```

The architect's system prompt instructs it to:
- Generate valid phase `.md` files with correct TOML frontmatter
- Analyze the existing nebula (phases, dependencies, current state) to determine where a new phase fits
- For refactors: compare old and new requirements and produce a focused updated description
- Output in a structured format that code can parse and write to disk

### 2. Architect System Prompt

The architect prompt includes:
- The nebula phase file format specification (frontmatter fields, markdown structure)
- The current nebula state: all phases with their IDs, titles, dependencies, statuses
- For refactors: the current phase body
- Instructions to output a single, parseable phase file

Output format:
```
PHASE_FILE: <filename>
+++
id = "..."
title = "..."
depends_on = [...]
+++

<markdown body>
END_PHASE_FILE
```

### 3. Architect Orchestration

Create `internal/nebula/architect.go` with the orchestration logic:

```go
type ArchitectRequest struct {
    Mode        string // "create" or "refactor"
    UserPrompt  string // what the user wants
    Nebula      *Nebula // current nebula state for context
    PhaseID     string // for refactor: which phase to modify
}

type ArchitectResult struct {
    Filename    string
    PhaseSpec   PhaseSpec
    Body        string
    DependsOn   []string
    Validated   bool
    Errors      []string
}

func RunArchitect(ctx context.Context, invoker agent.Invoker, req ArchitectRequest) (*ArchitectResult, error)
```

The function:
1. Builds the architect prompt with nebula context
2. Invokes the agent
3. Parses the structured output into `ArchitectResult`
4. Validates the result (frontmatter, dependency resolution, cycle detection)
5. Returns the result for the TUI to preview before writing

### 4. Code-First DAG Placement

The architect agent suggests `depends_on` but the code makes the final decision:
- Parse the agent's suggested dependencies
- Validate against the live DAG (do they exist? are they valid?)
- Run cycle detection
- If the agent's suggestion is invalid, auto-correct based on heuristics:
  - A new phase with no deps → place in the next available wave
  - A phase that mentions existing phase names in its body → suggest those as deps
- Present the final placement to the user for confirmation

### 5. Refactor Mode

For refactoring an in-progress phase:
1. Architect receives: current phase body, user's change request, coder/reviewer output so far
2. Architect produces an updated phase body that incorporates the user's feedback
3. Code writes the updated `.md` file (which triggers `ChangeModified` in the watcher)
4. The `phase-change-pipeline` and `refactor-prompt-injection` phases handle the rest

## Files to Create

- `internal/nebula/architect.go` — `RunArchitect()`, prompt building, output parsing
- `internal/nebula/architect_test.go` — Tests with mock invoker

## Files to Modify

- `internal/agent/agent.go` — Add `RoleArchitect` constant if using role-based patterns
- `internal/nebula/prompt.go` (or new file) — Architect system prompt template

## Acceptance Criteria

- [ ] Architect agent generates valid phase `.md` files from user descriptions
- [ ] Generated phases have correct TOML frontmatter with reasonable `depends_on`
- [ ] Refactor mode produces updated descriptions preserving relevant context
- [ ] Code validates and corrects DAG placement before writing
- [ ] Output is structured and parseable (not free-form text)
- [ ] Works with the existing `agent.Invoker` interface (no new deps)
- [ ] `go build` and `go test ./...` pass
