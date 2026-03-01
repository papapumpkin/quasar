+++
id = "integration"
title = "Wire progressive strictness into the coder-reviewer loop"
type = "task"
priority = 3
depends_on = ["prompt-variants", "cycle-selector"]
labels = ["quasar", "reviewer", "cost-optimization"]
+++

## Problem

The strictness tiers and prompt variants exist in `internal/agent`, but the loop does not use them yet. Currently, `Loop.reviewerAgent()` in `internal/loop/loop.go` always passes `l.ReviewPrompt` (which defaults to `agent.DefaultReviewerSystemPrompt`) through `agent.BuildSystemPrompt`:

```go
func (l *Loop) reviewerAgent(budget float64) agent.Agent {
    sysPrompt := agent.BuildSystemPrompt(l.ReviewPrompt, agent.PromptOpts{
        FabricEnabled:  l.FabricEnabled,
        TaskID:         l.TaskID,
        ProjectContext: l.ProjectContext,
    })
    return agent.Agent{
        Role:         agent.RoleReviewer,
        SystemPrompt: sysPrompt,
        Model:        l.Model,
        MaxBudgetUSD: budget,
        AllowedTools: []string{
            "Read", "Glob", "Grep",
            "Bash(go vet *)", "Bash(git diff *)", "Bash(git log *)",
        },
        MCP: l.MCP,
    }
}
```

And `runReviewerPhase` calls `reviewerAgent(perAgentBudget)` without any cycle awareness:

```go
func (l *Loop) runReviewerPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
    // ...
    result, err := l.Invoker.Invoke(ctx, l.reviewerAgent(perAgentBudget), prompt, l.WorkDir)
    // ...
}
```

The `buildReviewerPrompt` in `internal/loop/prompts.go` also has no tier awareness — it builds the same user-level prompt regardless of cycle.

We need to thread the cycle number through so `reviewerAgent` selects the correct system prompt variant, and `buildReviewerPrompt` can optionally adjust the user prompt to reinforce the tier's focus.

## Solution

### 1. Make `reviewerAgent` accept cycle and maxCycles

Change the signature of `reviewerAgent` to accept the current cycle and max cycles, then use `agent.StrictnessForCycle` and `agent.ReviewerPromptForStrictness` to select the right system prompt:

```go
// reviewerAgent builds the agent configuration for the reviewer role.
// The system prompt varies by cycle: early cycles use a lenient prompt
// focused on approach correctness, while later cycles apply full rigor.
func (l *Loop) reviewerAgent(budget float64, cycle, maxCycles int) agent.Agent {
    // Determine tier from cycle position.
    strictness := agent.StrictnessForCycle(cycle, maxCycles)

    // Use the tier-specific prompt unless the user has overridden ReviewPrompt
    // with a custom value (non-default means the user wants full control).
    basePrompt := agent.ReviewerPromptForStrictness(strictness)
    if l.ReviewPrompt != "" && l.ReviewPrompt != agent.DefaultReviewerSystemPrompt {
        basePrompt = l.ReviewPrompt
    }

    sysPrompt := agent.BuildSystemPrompt(basePrompt, agent.PromptOpts{
        FabricEnabled:  l.FabricEnabled,
        TaskID:         l.TaskID,
        ProjectContext: l.ProjectContext,
    })
    return agent.Agent{
        Role:         agent.RoleReviewer,
        SystemPrompt: sysPrompt,
        Model:        l.Model,
        MaxBudgetUSD: budget,
        AllowedTools: []string{
            "Read", "Glob", "Grep",
            "Bash(go vet *)", "Bash(git diff *)", "Bash(git log *)",
        },
        MCP: l.MCP,
    }
}
```

The key backward-compatibility guard: if `l.ReviewPrompt` is set to something other than `DefaultReviewerSystemPrompt`, the user has explicitly overridden the reviewer prompt and progressive strictness is bypassed.

### 2. Update `runReviewerPhase` to pass cycle info

Update the call site in `runReviewerPhase` to pass `state.Cycle` and `state.MaxCycles`:

```go
result, err := l.Invoker.Invoke(ctx, l.reviewerAgent(perAgentBudget, state.Cycle, state.MaxCycles), prompt, l.WorkDir)
```

### 3. Add tier context to `buildReviewerPrompt`

In `internal/loop/prompts.go`, add a short tier label to the reviewer's user-level prompt so the reviewer sees which mode it is operating in. This reinforces the system prompt tier and helps with debugging:

```go
func (l *Loop) buildReviewerPrompt(state *CycleState) string {
    var b strings.Builder

    // Inject strictness context so the reviewer knows its operating mode.
    strictness := agent.StrictnessForCycle(state.Cycle, state.MaxCycles)
    fmt.Fprintf(&b, "[Review mode: %s — cycle %d/%d]\n\n", strictness, state.Cycle, state.MaxCycles)

    fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskBeadID, state.TaskTitle)
    b.WriteString("The coder has completed their work. Here is their summary:\n\n")
    b.WriteString(truncate(state.CoderOutput, 3000))

    if state.LintOutput != "" {
        b.WriteString("\n\nNOTE: The following lint issues were not fully resolved by the coder:\n")
        b.WriteString(truncate(state.LintOutput, 2000))
    }

    b.WriteString("\n\nREVIEW INSTRUCTIONS:\n")
    b.WriteString("1. READ THE ACTUAL SOURCE FILES to verify the changes — do not rely solely on the summary above.\n")
    b.WriteString("2. Check for correctness, security, error handling, code quality, and edge cases.\n")
    b.WriteString("3. Check for any linting issues (`go vet`, `go fmt`). If linting problems exist, flag them as issues for the coder to fix.\n")
    b.WriteString("4. End your review with either APPROVED: or one or more ISSUE: blocks.\n")

    return b.String()
}
```

### 4. Add UI feedback for strictness tier

Log the current tier at the start of each reviewer phase so operators can see the progression in stderr output. In `runReviewerPhase`, after `l.UI.AgentStart("reviewer")`:

```go
strictness := agent.StrictnessForCycle(state.Cycle, state.MaxCycles)
l.UI.Info(fmt.Sprintf("reviewer strictness: %s (cycle %d/%d)", strictness, state.Cycle, state.MaxCycles))
```

### 5. Update existing loop tests

Any existing tests that call `reviewerAgent` directly (or mock the reviewer invocation) need to account for the new signature `reviewerAgent(budget, cycle, maxCycles)`. Search for test call sites and update accordingly.

## Files

- `internal/loop/loop.go` — Change `reviewerAgent` signature to `reviewerAgent(budget float64, cycle, maxCycles int)`, update call site in `runReviewerPhase`, add UI log for strictness tier
- `internal/loop/prompts.go` — Update `buildReviewerPrompt` to prepend `[Review mode: <tier> — cycle N/M]` header, add `agent` import if not present
- `internal/loop/loop_test.go` — Update any tests that reference `reviewerAgent` to pass cycle/maxCycles args

## Acceptance Criteria

- [ ] `reviewerAgent` accepts `cycle` and `maxCycles` parameters and selects the tier-appropriate system prompt
- [ ] When `l.ReviewPrompt` is a custom (non-default) value, progressive strictness is bypassed and the custom prompt is used
- [ ] `runReviewerPhase` passes `state.Cycle` and `state.MaxCycles` to `reviewerAgent`
- [ ] `buildReviewerPrompt` prepends a `[Review mode: ...]` header with the tier name and cycle info
- [ ] The UI logs the current strictness tier at the start of each reviewer phase
- [ ] All existing tests pass (`go test ./internal/loop/...`)
- [ ] Running `quasar run` with `max_review_cycles = 5` produces reviewer prompts that progress through lenient, standard, and strict tiers
- [ ] Running with a custom `--review-prompt` flag bypasses progressive strictness entirely
