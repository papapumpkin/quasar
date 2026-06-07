+++
id = "model-routing"
title = "Route cheap sub-decisions to Haiku — file lookup, test-coverage scanning, lint triage don't need an Opus coder"
type = "task"
priority = 2
depends_on = ["context-budgeting"]
scope = [
    "internal/agent/router.go",
    "internal/agent/router_test.go",
    "internal/artifacts/defaults/stars/file-finder.md",
    "internal/artifacts/defaults/stars/test-mapper.md",
    "internal/artifacts/defaults/skills/router-aware.md",
]
+++

## Problem

A coder using Opus or Sonnet to answer "which file declares the `Sensor` interface?" is overkill. So is "which tests cover `internal/sensors/scheduler.go`?" These are bounded, deterministic questions a Haiku model answers correctly at a fraction of the cost.

But routing isn't automatic — there's no mechanism today to delegate a sub-question to a cheaper model and route the result back to the calling coder. Every Read, Grep, and inference happens at the coder's model tier.

This phase introduces a small router that lets the coder say "delegate THIS question to Haiku" and stitches the response back into the coder's context as if it had done the work itself.

## Solution

### The router

`internal/agent/router.go`:

```go
// Router executes a sub-prompt against a cheaper model and returns the
// structured result. Used by coder/reviewer stars to delegate bounded
// questions without paying for premium-model inference.
type Router struct {
    invoker      Invoker
    haikuPath    string  // path to claude CLI; same binary, different --model flag
    defaultModel string  // claude-haiku-4-5-20251001
    cache        *resultCache  // in-process LRU keyed by sub-prompt hash
}

func (r *Router) Ask(ctx context.Context, q SubQuestion) (Answer, error)

type SubQuestion struct {
    Kind        SubKind   // file_finder | test_mapper | lint_triage | symbol_finder
    Query       string    // free-text question
    Workdir     string
    Scope       []string  // optional file/dir scope
    MaxLatency  time.Duration  // default 15s
}

type Answer struct {
    Result      string    // structured (JSON or table) per Kind
    InputTokens int       // for telemetry — confirm Haiku is cheaper
    OutputTokens int
    ModelUsed   string
}
```

The router invokes `claude --model claude-haiku-4-5-20251001 -p <sub-prompt> --output-format json` and parses the result. It uses a small purpose-specific system prompt per `SubKind` so Haiku has the right rubric.

### Pre-defined sub-stars

Four routed sub-questions ship with the system, each as a tiny star under `internal/artifacts/defaults/stars/`:

| Star | Question | Output |
|---|---|---|
| `file-finder.md` | "Where is X declared / implemented?" | List of `<path>:<line>` |
| `test-mapper.md` | "What tests cover this file/function?" | List of `<path>:<testfunc>` |
| `lint-triage.md` | "What's the highest-priority issue in this output?" | JSON `{file, line, severity, category, summary}` |
| `symbol-finder.md` | "Which package owns this symbol?" | `<package>` |

Each star's prompt is < 1 KB and tuned for Haiku. They're invoked by the coder via a new `RouteQuery` tool exposed only when `router-aware` skill is loaded.

### Coder integration

A new skill `internal/artifacts/defaults/skills/router-aware.md`:

```markdown
+++
name = "router-aware"
tools_add = ["RouteQuery"]
+++

When you need to answer a bounded factual question about the codebase
(where a symbol lives, what tests cover a file, what package owns a
type), use the RouteQuery tool instead of issuing Grep/Read directly.
The router uses a cheaper model and returns a structured result. Use it
for: file lookup, test mapping, symbol resolution. Do NOT use it for:
making edits, writing code, reading file contents you actually need to
modify.
```

The default `coder` star adds `router-aware` to its skills list, so every coder gets the routing tool by default.

### Telemetry to prove it's worth it

Every router call records to `internal/telemetry/cache_metrics.go` (Phase 0's store, extended):

```go
type RouterMetric struct {
    SubKind        SubKind
    HaikuInTokens  int
    HaikuOutTokens int
    LatencyMs      int64
    CacheHit       bool
}
```

After 10 invocations of the same query in a phase, the in-process LRU cache returns the cached answer without firing Haiku at all — measured as `CacheHit=true`.

A `quasar cache report --router` flag reports cost savings: estimated tokens-not-spent-at-Opus-tier per phase, based on assumed token counts the coder would have used.

### What this does NOT do

- Does not route the main coder/reviewer/architect prompts. Only sub-decisions.
- Does not auto-detect which questions are "bounded" — the coder must explicitly call `RouteQuery`. Auto-routing is a future phase if the manual approach proves to be high-leverage.
- Does not change model selection at the run level. The coder is still Opus/Sonnet; only its sub-questions are Haiku.

## Files

- `internal/agent/router.go` (new)
- `internal/agent/router_test.go` (new)
- `internal/artifacts/defaults/stars/file-finder.md` (new)
- `internal/artifacts/defaults/stars/test-mapper.md` (new)
- `internal/artifacts/defaults/stars/lint-triage.md` (new)
- `internal/artifacts/defaults/stars/symbol-finder.md` (new)
- `internal/artifacts/defaults/skills/router-aware.md` (new)
- `internal/artifacts/defaults/stars/coder.md` (modify) — add `router-aware` to skills list
- `internal/telemetry/cache_metrics.go` (modify) — extend with RouterMetric
- `cmd/cache.go` (modify) — add `--router` flag to report

## Acceptance Criteria

- [ ] `Router.Ask(ctx, q)` invokes claude with `--model claude-haiku-4-5-20251001`
- [ ] Repeated identical `SubQuestion` hits the in-process LRU on the second call (verified by latency: ms vs ms)
- [ ] `file-finder` star receives a `Query` like "Where is the Sensor interface declared?" and returns a single `<path>:<line>` result that resolves to a real file
- [ ] `test-mapper` star, given a target file, returns a list of test function references
- [ ] `RouteQuery` tool is only exposed when `router-aware` skill is loaded
- [ ] Coder star's default skills include `router-aware`
- [ ] `RouterMetric` is recorded for every router invocation
- [ ] `quasar cache report --router` prints estimated tokens-saved per phase
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
