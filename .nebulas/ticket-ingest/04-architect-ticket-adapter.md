+++
id = "architect-ticket-adapter"
title = "Extend the architect to accept a Ticket as an alternative input"
type = "task"
priority = 2
depends_on = ["integration-layer"]
scope = [
    "internal/nebula/architect.go",
    "internal/nebula/architect_test.go",
    "internal/nebula/prompt_ticket.tmpl",
    "internal/nebula/prompt_ticket.go",
    "internal/nebula/prompt_ticket_test.go",
]
+++

## Problem

The architect today (`internal/nebula/architect.go`, 409 LOC) takes a freeform prompt and emits a nebula directory. To make tickets first-class without forking the architect, it needs a second input path: given a `*integrations.Ticket`, render that ticket into the architect's existing prompt format and reuse the rest of the pipeline unchanged.

This phase only touches the architect's input boundary. The downstream behavior (LLM call, nebula directory write, manifest emission) stays exactly as it is.

## Solution

### New input path

Add a new exported function on the architect (call it `Architect`, `Generator`, or whatever the existing type is named — discover via the current architect.go):

```go
// FromTicket runs the architect against a ticket context. It renders the
// ticket into the architect's standard prompt format (see prompt_ticket.tmpl),
// invokes the LLM, and writes the resulting nebula directory to outDir.
//
// The returned NebulaInfo contains the generated name and path. The Nebula
// struct it produces has SourceName and SourceID populated from the ticket
// (added in phase 1).
func (a *Architect) FromTicket(ctx context.Context, t *integrations.Ticket, outDir string) (*NebulaInfo, error)
```

The existing freeform-prompt path (`FromPrompt` or whatever its current name is) is unchanged. The two entry points share the back half of the architect.

### Prompt template

Create `internal/nebula/prompt_ticket.tmpl`:

```text
You are designing a multi-phase plan to address a ticket pulled from an
external work-tracking system.

Source: {{.SourceName}}
Reference: {{.SourceID}}
URL: {{.URL}}

Title: {{.Title}}
{{if .Labels}}Labels: {{join .Labels ", "}}{{end}}
{{if .Assignee}}Assigned to: {{.Assignee}}{{end}}
State: {{.State}}

--- Ticket Body ---
{{.Body}}

{{if .Comments}}
--- Comments ({{len .Comments}}) ---
{{range .Comments}}
[{{.Author}} @ {{.CreatedAt.Format "2006-01-02"}}]
{{.Body}}

{{end}}
{{end}}

{{if .LinkedWork}}
--- Linked Work (for context, do not reopen) ---
{{range .LinkedWork}}{{.}}
{{end}}
{{end}}

Now produce the nebula manifest and phase files per the conventions you
already know.
```

Helpers: a `join` template function bound to `strings.Join` (define inline at template parse time). The template is parsed once at architect construction time, not on every call.

### Render layer

`internal/nebula/prompt_ticket.go`:

```go
// RenderTicketPrompt is the pure function that produces the architect's
// user prompt for a ticket-driven nebula. Kept separate from the architect
// type so it can be unit-tested without any LLM dependency.
func RenderTicketPrompt(t *integrations.Ticket) (string, error)
```

Internally this loads the embedded template via `embed.FS` (`//go:embed prompt_ticket.tmpl`), executes it with the ticket as data, returns the string.

### Nebula name derivation

The generated nebula's directory name follows the spec convention:
- `nebula-<number>-<slug-of-title>` for tickets with a numeric ID (GitHub Issues, Jira)
- `nebula-<slugified-source-id>-<slug-of-title>` for tickets without a number (slugify the colon → hyphen, etc.)

Slug rules: lowercase, ASCII-only, hyphenated, max 40 chars total. Implemented in a small `slugifyTicket(t *Ticket) string` helper. Collisions (same ticket re-pulled) append `-2`, `-3`, etc. — the caller (phase 5 CLI command) handles the on-disk collision check; this phase just produces a candidate name.

### Architect plumbing

Internal to `architect.go`:
1. Both `FromTicket` and the existing `FromPrompt` build a common `architectInput` struct (system prompt, user prompt, target directory, source attribution).
2. The existing back half — LLM call, parse, write nebula files, populate state.toml — moves into a private method that consumes `architectInput`.
3. `FromTicket` populates the resulting `Nebula.SourceName` and `Nebula.SourceID` so the generated `nebula.toml` records the provenance.

Reflect provenance in the generated `nebula.toml`:
```toml
[nebula]
name = "..."
description = "..."
source_name = "github"
source_id   = "papapumpkin/quasar#42"
```

The TOML parser (phase 1's type additions in `internal/nebula/types.go`) already understands these fields.

### What this phase does NOT do

- No CLI command yet (phase 5)
- No SQLite write of source attribution (the cmd-level glue does that when it invokes the architect)
- No changes to the existing freeform prompt path

### Testing

`architect_test.go`: ensure both paths still work. Use a fake LLM invoker (one already exists in the package — reuse it) and feed a canned ticket → expect a deterministic prompt to be sent to the invoker. Don't assert on the LLM output; that's a stub in tests.

`prompt_ticket_test.go`: table-driven cases for `RenderTicketPrompt` — minimal ticket, full ticket with comments + linked work, ticket with empty body (architect prompt must still render coherently), ticket with multi-paragraph body containing backticks.

## Files

- `internal/nebula/architect.go` — add `FromTicket` method; refactor common back-half into a private method; populate `Nebula.SourceName`/`SourceID` on the returned struct
- `internal/nebula/architect_test.go` — adapt existing tests to the refactored back-half; new test for `FromTicket` happy path
- `internal/nebula/prompt_ticket.go` (new) — `RenderTicketPrompt`, template parsing, slug helper
- `internal/nebula/prompt_ticket.tmpl` (new) — the template above, embedded via `//go:embed`
- `internal/nebula/prompt_ticket_test.go` (new) — table-driven render tests

## Acceptance Criteria

- [ ] `internal/nebula/architect.go` exposes a public method `FromTicket(ctx, *integrations.Ticket, outDir string) (*NebulaInfo, error)`
- [ ] `FromTicket` and the existing freeform path share the same back half (verify by grepping for duplicated LLM-call code; there should be one call site)
- [ ] `RenderTicketPrompt(t)` returns a prompt that contains, in order: title, source ref, URL, labels (if any), body, comments (chronologically), linked work (if any)
- [ ] The generated `nebula.toml` includes `source_name` and `source_id` fields populated from the ticket
- [ ] The generated directory name follows the `nebula-<number>-<slug>` convention; slug is lowercase ASCII, hyphenated, ≤40 chars
- [ ] Existing freeform-prompt architect tests still pass without modification beyond the back-half refactor
- [ ] `prompt_ticket_test.go` covers: empty body, multi-paragraph body, comments present, linked work present, all-fields-set, minimal fields
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
