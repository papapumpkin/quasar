+++
id = "rename-integrations-to-sensors"
title = "Rename internal/integrations → internal/sensors; shift the interface from Fetch to Poll/Emit"
type = "task"
priority = 1
depends_on = ["multi-repo-foundation"]
scope = [
    "internal/sensors/**",
    "internal/integrations/**",
    "internal/integrations/github/**",
    "internal/nebula/prompt_ticket.go",
    "internal/nebula/prompt_ticket_test.go",
    "internal/nebula/architect.go",
    "cmd/nebula_new.go",
    "cmd/nebula_new_test.go",
    "internal/config/config.go",
    "internal/arch_test/safety_test.go",
]
+++

## Problem

The ticket-ingest nebula shipped `internal/integrations/` with a `TicketSource` interface (`Name()`, `Fetch(ctx, sourceID) (*Ticket, error)`) and a GitHub adapter that pulled a single ticket on demand. The new model is poll-driven: a sensor runs on a persistent service, periodically polls its external system, and emits events when new work appears. The right verbs are `Poll(cursor) → (events, newCursor)` and `EventToPayload(event) → ContextPayload`, not `Fetch(id) → Ticket`.

This is a rename plus an interface shift. The Go code mostly stays the same — the GitHub adapter still shells out to `gh issue list/view` — but the surface that the rest of Quasar talks to changes shape. After this phase, the integration registry becomes the sensor registry, the GitHub ticket source becomes the GitHub sensor, and the on-demand `quasar nebula new github:42` flow gives way to "the sensor polls automatically and produces seed nebulas."

## Solution

### Rename and reshape

1. **Move the directory** — `internal/integrations/` → `internal/sensors/`. All Go imports update accordingly.
2. **Rename types and methods:**
   - `TicketSource` → `Sensor`
   - `Ticket` → keep the name (the Ticket DTO is still useful as the rendered shape that flows into the architect prompt; we're not renaming it to ContextPayload because the user agreed that nebula is the universal artifact, not a separate ContextPayload type — see step 4 below)
3. **Drop `Forge` if it's still there** — the user's direction was that Forge implementation lands in a future nebula. The interface stub with just `Name()` stays defined (in `internal/forge/` from ticket-ingest if it lives there, or here under `internal/sensors/forge.go` if it was co-located). The exact location depends on what ticket-ingest produced; preserve whatever's there.
4. **The interface shifts to poll-driven:**

```go
// Sensor is a poll-driven integration with an external work-tracking system.
// Implementations live in their own subpackage of internal/sensors/ and
// register a constructor with the package-level registry via init().
//
// The sensor's job is to produce seed nebulas. A seed nebula is a row in
// the nebulas table with status='draft', a populated [source] block, basic
// [context] (goals + constraints derived from the source), but NO phases.
// The architect constellation refines seed nebulas into executable plans.
//
// Implementations MUST be safe for concurrent use; the supervisor runs one
// scheduler goroutine per sensor instance.
type Sensor interface {
    // Name returns the sensor type's name (e.g. "github_issues", "jira_issues").
    // Used as the registry key.
    Name() string

    // Configure parses the [config] block from the sensor's TOML instance
    // file. Returns a typed error so `quasar lint` can surface
    // misconfiguration before the supervisor boots.
    Configure(raw map[string]any, secrets SecretResolver) error

    // Poll returns events that occurred since the cursor. The runtime
    // persists newCursor before processing the events, so sensors don't
    // have to manage cursor durability themselves. Cursor is an opaque
    // JSON value the sensor defines for itself.
    Poll(ctx context.Context, cursor json.RawMessage) (events []Event, newCursor json.RawMessage, err error)

    // SeedNebula renders a single event into the seed nebula content that
    // will be inserted into SQLite. The runtime handles the DB write; the
    // sensor just produces the structured content (name, description,
    // source.{name,id,url}, context.{goals,constraints}, labels, assignee).
    SeedNebula(event Event) (*SeedNebulaContent, error)
}

type Event struct {
    ExternalID string                 // sensor-defined, unique per source
    Timestamp  time.Time
    Raw        map[string]any         // adapter-internal payload
}

type SeedNebulaContent struct {
    Name        string
    Description string
    SourceName  string
    SourceID    string
    SourceURL   string
    Goals       []string
    Constraints []string
    Labels      []string
    Assignee    string
}
```

5. **Registry stays the same shape** — name → constructor mapping. Rename `RegisterTicketSource` → `RegisterSensor`. The constructor signature shifts slightly because Configure is now a separate step.

### What stays from ticket-ingest

- The `gh` CLI shell-out machinery in `internal/sensors/github/exec.go` (was `internal/integrations/github/exec.go`)
- The JSON parsers in `internal/sensors/github/parse.go`
- Source-id parsing (`owner/repo#42` etc.) becomes part of how the GitHub sensor produces external IDs for its Event records
- The SecretResolver in `internal/sensors/secret.go` (Docker-friendly token_env/token_file/gh-fallback resolution) — unchanged
- The Forge interface stub — unchanged
- All inline-token-guardrail behavior — unchanged
- All arch tests that forbid direct `exec.Command("gh", ...)` outside `internal/sensors/github/` — paths update but semantics stay

### What gets deleted

- The on-demand `quasar nebula new <source>:<id>` command. In the new model, sensors auto-create nebulas; users approve drafts in the TUI. If a manual "create a nebula from a specific ticket reference right now" surface is wanted later, it can be a thin wrapper that bypasses the polling cycle — but it's not required for this nebula.
- The `Fetch(ctx, sourceID)` method on the old `TicketSource` interface. Replaced by `Poll`.
- `cmd/nebula_new.go` and `cmd/nebula_new_test.go` (since `nebula new` no longer makes sense in the sensor-driven model).
- The architect's `FromTicket(*Ticket)` entry point becomes `FromNebula(nebulaID string)` (the sensor has already written the seed nebula to SQLite; the architect reads it back and refines it).

### Architect adapter changes

The architect's prompt renderer (`internal/nebula/prompt_ticket.go` from ticket-ingest's Phase 4) becomes `prompt_nebula.go`. The template still flattens source-ish context into the architect's prompt, but it reads from the seed nebula's columns (name, description, source_url, context.goals, context.constraints) instead of from a Ticket struct passed in by the caller. The template file stays embedded via `//go:embed`.

### Subpackage renames

- `internal/integrations/github/` → `internal/sensors/github/`
- The init() function now calls `sensors.Default().RegisterSensor("github_issues", ...)` instead of `integrations.Default().RegisterTicketSource("github", ...)`
- The Go-implemented sensor type's name becomes `github_issues` (the `_issues` suffix leaves room for a future `github_prs` sensor type)

### Backward compatibility

Manifests like `[integrations.github]` in old `.quasar.yaml` files: parsing emits a warning and silently treats them as deprecated. Users migrate to `<repo>/sensors/github-issues.toml` (the new per-repo sensor instance file format — see Phase 2 for the file shape). No code paths read the `[integrations.*]` block in the new model.

## Files

- `internal/sensors/sensors.go` (moved from internal/integrations/integrations.go, renamed) — Sensor interface, Event, SeedNebulaContent
- `internal/sensors/registry.go` (moved) — Registry with RegisterSensor/BuildSensor
- `internal/sensors/secret.go` (moved) — SecretResolver, SecretSpec, OSSecretResolver
- `internal/sensors/github/github.go` (moved + reshaped) — github_issues Sensor with Configure, Poll, SeedNebula
- `internal/sensors/github/parse.go` (moved) — JSON parsing helpers unchanged
- `internal/sensors/github/exec.go` (moved) — runGH shell-out wrapper unchanged
- `internal/sensors/github/testdata/**` (moved) — gh JSON fixtures unchanged
- `internal/integrations/` (delete) — the entire directory is removed; arch tests catch any residual import path
- `internal/nebula/prompt_nebula.go` (renamed from prompt_ticket.go) — renderer that takes a nebula row, not a Ticket
- `internal/nebula/prompt_nebula.tmpl` (renamed embedded template)
- `internal/nebula/architect.go` — replace FromTicket with FromNebula(nebulaID)
- `cmd/nebula_new.go` (delete)
- `cmd/nebula_new_test.go` (delete)
- `internal/config/config.go` — emit deprecation warning when parsing legacy `[integrations.*]` blocks
- `internal/arch_test/safety_test.go` — update allowlist paths from `internal/integrations/github/` to `internal/sensors/github/`

## Acceptance Criteria

- [ ] `grep -rn "internal/integrations" --include="*.go" .` returns no matches
- [ ] `internal/sensors/` package compiles
- [ ] `Sensor` interface has exactly four methods: Name, Configure, Poll, SeedNebula
- [ ] `github_issues` sensor is registered under that name in the default registry
- [ ] Calling a fake `runGH` from the GitHub sensor's `Poll` returns parsed events; cursor is correctly advanced (last-issue-number-seen format)
- [ ] `Sensor.SeedNebula(event)` returns a populated SeedNebulaContent with source_name="github", source_id matching `<owner>/<repo>#<number>`, and goals/constraints derived from the issue body
- [ ] `cmd/nebula_new.go` and `cmd/nebula_new_test.go` no longer exist
- [ ] `quasar nebula new ...` is no longer a registered command (`quasar --help` does not list it)
- [ ] `internal/nebula/architect.go` exposes `FromNebula(nebulaID string)` and not `FromTicket(*Ticket)`
- [ ] Arch test `TestNoDirectGHExecOutsideAllowedPackages` passes with `internal/sensors/github/` in the allowlist (and not the old path)
- [ ] Loading a `.quasar.yaml` with a legacy `[integrations.github]` block emits a deprecation warning to stderr but does not error
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
