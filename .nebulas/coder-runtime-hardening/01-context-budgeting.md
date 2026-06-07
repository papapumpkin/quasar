+++
id = "context-budgeting"
title = "Bound coder context — phase-only spec injection, tool-result truncation, soft tool-budget caps to stop Read/Grep thrash"
type = "task"
priority = 2
depends_on = ["prompt-cache-audit-and-fix"]
scope = [
    "internal/agent/prompt.go",
    "internal/agent/context_budget.go",
    "internal/agent/context_budget_test.go",
    "internal/claude/tool_truncate.go",
    "internal/loop/tool_budget.go",
]
+++

## Problem

A coder's volatile-zone prompt currently includes the entire nebula context — every phase spec, all goals, all constraints — even though it can only work on one phase at a time. The result: ~30% of the input tokens per invocation are spent on phases the coder cannot touch.

Worse, when the coder issues a `Read` or `Grep`, the entire tool result lands in context. A 2,000-line file Read costs ~6k tokens whether the coder needs 50 lines or all 2,000. Across 4 cycles of exploration, the coder can fill 40k tokens with file content it later ignores.

And there's no governor on tool-call thrash: a coder can issue 30 Reads before its first Edit. By then the context is half-spent and the coder hasn't even started the actual work.

Three bounds, one phase:

## Solution

### Phase-only spec injection

`internal/agent/prompt.go` currently injects the parent nebula's full goals + constraints + every phase spec into the volatile suffix. New behavior: inject **only the current phase's spec**, plus the nebula's `[context].goals` and `[context].constraints` (which are short and global).

```go
// renderVolatileSuffix builds the user-prompt portion. For coder/reviewer
// invocations on a specific phase, only that phase's spec is included.
// Sibling phase specs are intentionally elided — the coder cannot touch them.
func renderVolatileSuffix(in VolatileInput) string {
    // before: includes ALL phases via in.Nebula.Phases
    // after:  includes only in.CurrentPhase
}
```

Architect invocations (which DO need to see all phases) keep the full spec — gated by a `RoleNeedsFullNebula(role)` check.

Expected reduction: 25–40% on per-cycle input tokens for coder/reviewer.

### Tool result truncation

`internal/claude/tool_truncate.go` adds a result-stream interceptor. Before the tool result reaches the coder, it's capped:

```go
type TruncationPolicy struct {
    MaxBytesPerResult int       // default 16 * 1024 = 16 KB
    KeepHead          bool      // keep first MaxBytes/2
    KeepTail          bool      // keep last MaxBytes/2
    Marker            string    // "\n... (truncated <N> bytes, <total> total) ...\n"
}

func TruncateResult(result string, p TruncationPolicy) (truncated string, wasTruncated bool)
```

Default: keep 8KB head + 8KB tail with a marker line between. Most file reads stay intact; only very large reads (whole sourcemaps, fixture JSON, transcripts) trim. The coder can request a `range:` from the truncated read if it actually needs the middle.

The policy is configurable per-star via the `[tools.truncation]` TOML block; coder-default is 16KB, reviewer can go higher (32KB) since reviewers do need to see fuller context.

### Soft tool-budget caps

`internal/loop/tool_budget.go` tracks tool call counts per invocation:

```go
type Budget struct {
    MaxReadsBeforeEdit  int      // default 8
    MaxGrepsBeforeEdit  int      // default 6
    MaxTotalReads       int      // default 30 hard cap
    SoftAdvisory        bool     // default true — adds advisory marker instead of hard-blocking
}

// OnToolCall is invoked by the loop before forwarding a tool call.
// When a soft limit is exceeded, the loop injects an advisory message
// into the next assistant response: "You have used 8 Reads without an
// Edit. Please commit to an edit plan."
func (b *Budget) OnToolCall(call ToolCall) (proceed bool, advisory string)
```

Advisory is soft for v1 — the coder is nudged, not forbidden. If real-world data shows the nudge isn't sufficient, a future phase can make the hard cap binding. The soft approach also prevents catastrophic abort on legitimate exploration-heavy phases.

### Wiring

The three pieces share one config block in the star TOML:

```toml
# stars/coder.md frontmatter (excerpt)
[context_budget]
max_reads_before_edit = 8
max_total_reads = 30
tool_result_max_bytes = 16384
include_sibling_phases = false   # ← phase-only injection
```

Per-repo override pattern (Phase 2 of constellation-runtime) lets a repo author bump `tool_result_max_bytes` to 32 KB for code-archaeology-heavy work.

### Tests

- `prompt_test.go` golden: verify volatile suffix is shorter when only one phase is included; ratio of phase content to total content matches expectation
- `tool_truncate_test.go`: 4 KB result passes through unchanged; 32 KB result is truncated to 16 KB with head+tail+marker
- `tool_budget_test.go`: 9 reads with 0 edits triggers advisory on the 9th; 0 reads → 0 advisories; advisory text is deterministic for golden assertions
- Integration: a fixture phase that historically used 90 KB context now uses ≤ 60 KB after wiring

## Files

- `internal/agent/prompt.go` (modify) — phase-only injection for non-architect roles
- `internal/agent/context_budget.go` (new) — central config struct + per-role defaults
- `internal/agent/context_budget_test.go` (new)
- `internal/claude/tool_truncate.go` (new)
- `internal/claude/tool_truncate_test.go` (new)
- `internal/loop/tool_budget.go` (new) — soft cap tracker
- `internal/loop/tool_budget_test.go` (new)
- `internal/artifacts/defaults/skills/coder.md` (modify) — adds `[context_budget]` block to the default coder star (alongside the existing skill mention)

## Acceptance Criteria

- [ ] Non-architect coder invocations include only the current phase's spec in the volatile suffix
- [ ] Architect invocations still receive the full nebula spec
- [ ] `TruncateResult(s, default)` returns `s` unchanged when `len(s) <= MaxBytesPerResult`
- [ ] `TruncateResult` on a 32 KB input returns ≤ 16 KB with the marker between head and tail
- [ ] `Budget.OnToolCall` returns advisory text when `MaxReadsBeforeEdit` is exceeded with zero intervening edits
- [ ] Soft advisory is injected into the next assistant turn as a `<system-reminder>`-style note
- [ ] `MaxTotalReads` hard cap rejects the next tool call when exceeded (`proceed=false`)
- [ ] Configurable per-star via TOML frontmatter; defaults documented
- [ ] A fixture phase that previously logged ≥ 80 KB volatile context now logs ≤ 60 KB
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
