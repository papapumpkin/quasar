+++
id = "coordination-preflight"
title = "Pre-flight coordination check: query active entanglements that intersect the phase's scope and inject sibling-aware notes into the coder's prompt"
type = "task"
priority = 2
depends_on = ["entanglement-lifecycle"]
scope = [
    "internal/constellations/coordination.go",
    "internal/constellations/coordination_test.go",
    "internal/constellations/runtime.go",
    "internal/agent/prompt.go",
    "internal/telemetry/coordination_log.go",
]
+++

## Problem

With the entanglement lifecycle from Phase 00, the runtime knows what every in-flight phase is producing, modifying, or deprecating. But that knowledge doesn't yet reach the coder. A coder about to write code calling `Foo()` has no way to learn that another coder is mid-removal of `Foo` — so it writes the call, ships, and breaks the build at merge time.

This phase wires the entanglement state into the coder's prompt as a `## Coordination notes` section: before each coder invocation, query active entanglements that intersect the phase's scope, and render them as advice the coder reads alongside its phase spec.

## Solution

### The check

`internal/constellations/coordination.go`:

```go
// Check inspects the entanglement table for symbols and files that intersect
// the dispatching phase's scope, and returns the set of sibling intents the
// coder needs to know about. Run before each dispatchStar / dispatchConstellation
// for a coder-role star.
type Check struct {
    Store *fabric.EntanglementStore
}

type CoordinationNote struct {
    // What the sibling is doing.
    SiblingRunID    string
    SiblingPhaseID  string
    Kind            string  // "func" | "type" | "interface" | ...
    Name            string
    CurrentSignature string

    // What the sibling's intent is.
    Status   string  // declared | claimed | in_flight | deprecated
    Advice   string  // "use this signature" | "do not reintroduce" | "wait if possible"

    // Provenance for the operator (telemetry + audit).
    Producer       string
    Package        string
    DeclaredAt     time.Time
    UpdatedAt      time.Time
}

func (c *Check) Notes(ctx context.Context, phase PhaseContext) ([]CoordinationNote, error)

type PhaseContext struct {
    RunID        string
    PhaseID      string
    Scope        []string // glob patterns from the phase spec
    Files        []string // resolved file paths (from Scope)
    SelfSymbols  []string // symbols this phase has already declared (so we don't note ourselves)
}
```

The query walks active entanglements (Phase 00's `Active`) and filters by:
- Symbol name appears as text in any of the phase's scope-resolved files (cheap content match)
- OR the entanglement's package overlaps the phase's scope globs
- AND the producer run is not the current run (exclude self)

Recency-ordered so the most recently-updated intent floats to the top — that's the signature the sibling is likely to ship with.

### Prompt injection

`internal/agent/prompt.go` adds a renderer for `[]CoordinationNote` that produces a `## Coordination notes` block in the volatile suffix:

```markdown
## Coordination notes
Other phases are currently in flight on symbols that overlap your scope.
Both their work and yours are valid — treat these as constraints, not
optional guidance.

- **Sensor.Poll** (in_flight, phase `rename-integrations-to-sensors`, cycle 3)
  Current signature: `Poll(ctx, cursor) ([]Event, json.RawMessage, error)`
  Advice: If you call this method, use this signature; do not assume the
  prior shape.

- **FromTicket** (deprecated, phase `rename-integrations-to-sensors`, cycle 3)
  Advice: Do not introduce new uses of this symbol. It is being removed.
  Use `FromNebula` instead — see the producer's spec for migration shape.

- **Scheduler** (declared, phase `github-sensor-produces-nebula`, cycle 0)
  Advice: A sibling phase intends to create this type. If your work
  could land first and inadvertently use the name, coordinate with that
  phase or rename to avoid the collision.
```

The Advice line is generated from the status:

| Status | Advice template |
|---|---|
| `declared` | "A sibling phase intends to produce this symbol. Avoid the name collision." |
| `claimed` | "A sibling coder has picked up work on this symbol. Coordinate or wait." |
| `in_flight` | "Use the current signature shown above." |
| `deprecated` | "Do not introduce new uses. Use the replacement noted in the producer's spec." |

### Where it hooks in

In `dispatchStar` (and the existing `dispatchConstellation` for nested coder-reviewer invocations), before the LLM call:

```go
// Existing budget check first…
if err := r.budget.CheckBefore(ctx, run.ID); err != nil { ... }

// NEW: coordination notes.
notes, err := r.coordination.Notes(ctx, PhaseContext{...})
if err != nil {
    fmt.Fprintf(os.Stderr, "coordination check: %v\n", err)  // advisory only
    notes = nil
}
// Existing star load…
star, err := r.loader.LoadStar(node.Star)
…
// Render the prompt with notes attached.
prompt := userPrompt(args, st)
if len(notes) > 0 {
    prompt = appendCoordinationNotes(prompt, notes)
}
```

The check is **advisory only** — a failure to read entanglements never fails the run. The coder might miss a note but the merge gate (Phase 02) still catches the conflict.

### Override mechanism

A phase's spec may explicitly require ignoring coordination notes — e.g. a phase whose entire purpose is to reintroduce a deprecated symbol that was wrongly removed. Phase spec frontmatter:

```toml
[coordination]
ignore_deprecations = ["FromTicket"]   # explicit allowlist
ignore_signatures   = ["Sensor.Poll"]  # use the prior, not the in-flight, signature
```

When set, the renderer omits the matching notes and writes a single line to telemetry: `coordination_overridden{phase, symbol, reason}`. That's the audit trail.

### Telemetry

Every coordination check appends one JSONL row to `coordination_log.jsonl`:

```json
{"ts":"...","run_id":"...","phase_id":"...","notes_count":3,"by_status":{"in_flight":1,"deprecated":1,"declared":1}}
```

A separate `quasar coordination report --since 24h` walks the log and surfaces:
- How often coordination notes fire per phase
- Which symbols cause the most cross-phase contention
- How often overrides are used

That data informs whether the architecture is paying for itself.

### Configurability

A star's TOML frontmatter opts in:

```toml
# stars/coder.md frontmatter (excerpt)
coordination_aware = true   # default true for coder-class stars
```

When false (e.g. for the architect, which works at a different abstraction layer), the check is skipped entirely.

### Tests

- `coordination_test.go` — symbol-name match against scope files; package-glob match; self-exclusion
- `coordination_test.go` — recency ordering: an `in_flight` updated 30s ago ranks above a `declared` 5min ago
- `prompt_test.go` golden — the rendered `## Coordination notes` block for each status type
- Integration: two fixture phases with overlapping scope; phase B's coder prompt contains a note about phase A's in-flight symbol

## Files

- `internal/constellations/coordination.go` (new)
- `internal/constellations/coordination_test.go` (new)
- `internal/constellations/runtime.go` (modify) — wire Check.Notes into dispatchStar pre-flight
- `internal/agent/prompt.go` (modify) — renderer for `[]CoordinationNote`
- `internal/agent/prompt_test.go` (modify) — golden tests for the renderer
- `internal/telemetry/coordination_log.go` (new) — JSONL writer
- `internal/telemetry/coordination_log_test.go` (new)
- `cmd/coordination.go` (new) — `quasar coordination report --since 24h`

## Acceptance Criteria

- [ ] `Check.Notes(ctx, phase)` returns coordination notes for active entanglements that intersect the phase's scope by symbol or package
- [ ] Notes exclude entanglements owned by the dispatching run (self-exclusion)
- [ ] Notes are recency-ordered (most-recent `updated_at` first)
- [ ] Each note carries an advice string matching its status (declared | claimed | in_flight | deprecated)
- [ ] `appendCoordinationNotes` produces a deterministic `## Coordination notes` block for golden assertion
- [ ] The check is skipped when the dispatching star has `coordination_aware = false`
- [ ] Per-phase override (`[coordination].ignore_deprecations`, `[coordination].ignore_signatures`) suppresses matching notes and writes an override telemetry row
- [ ] Failure to read entanglements does NOT fail the run — the coder simply gets no notes
- [ ] `coordination_log.jsonl` receives one row per check
- [ ] `quasar coordination report --since 24h` prints per-phase note counts and override counts
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
