+++
id = "nebula-new-from-source"
title = "Add the `quasar nebula new <source>:<id>` command"
type = "task"
priority = 2
depends_on = ["github-ticket-source", "architect-ticket-adapter", "gitops-and-pre-commit"]
scope = [
    "cmd/nebula.go",
    "cmd/nebula_new.go",
    "cmd/nebula_new_test.go",
    "cmd/nebula_apply.go",
]
+++

## Problem

With the integration layer (phase 1), GitHub adapter (phase 2), and architect ticket path (phase 4) all in place, the user-facing entry point still needs to land. The right shape, decided in design: `quasar nebula new <source>:<id>` produces a draft nebula directory from a ticket reference. No new top-level noun (`quasar ticket …`) — nebulas remain the unit, and the source is just one way to seed one.

This is the only new CLI command in Nebula 1. `nebula apply` and `nebula validate` are unchanged. The freeform-prompt architect path can also be exposed via `nebula new` with a different syntax in a later nebula; this phase covers only the `<source>:<id>` form because that is what the user-facing flow needs.

## Solution

### Command surface

```
quasar nebula new <source>:<id> [--name <override>] [--dir <parent-dir>]

Positional:
  <source>:<id>     A ticket reference (e.g. "github:42", "github:owner/repo#42").
                    The portion before the colon names a registered integration;
                    the portion after is passed verbatim to that integration's
                    Fetch method as the SourceID.

Flags:
  --name <name>     Override the auto-derived nebula directory name.
                    Default: nebula-<number>-<slug-of-title> (see phase 4).
  --dir <path>      Parent directory for the generated nebula. Default: .nebulas/
```

Outputs:
- A new directory at `<dir>/<name>/` containing `nebula.toml` and one `.md` file per phase, written by the architect.
- A `nebulas` row inserted with `source_name`, `source_id`, `path`, `status='draft'`.
- A stderr line: `created draft nebula at .nebulas/<name>/ (source: github, ref: papapumpkin/quasar#42)`.
- Exit 0 on success.

### Cobra wiring

A new `cmd/nebula_new.go` file. Subcommand registered on the existing `nebulaCmd` parent (which lives in `cmd/nebula.go`). Idiomatic with how `nebula apply` and `nebula validate` are already wired.

### Flow

```go
func runNebulaNew(cmd *cobra.Command, args []string) error {
    1. Validate args[0] matches "<source>:<rest>" via a single colon split.
       Reject empty source or empty rest with a clear usage message.

    2. Load config: cfg := config.Load()

    3. Resolve source:
       a. Look up integration config: section, ok := cfg.Integrations[sourceName]
          - Not found: error "no integration named %q configured; add an
            [integrations.%s] block to .quasar.yaml"
       b. Construct adapter via the registry:
          src, err := integrations.Default().BuildTicketSource(
            sourceName, section, integrations.NewSecretResolver())
       c. Adapter errors (gh missing, repo not configured, etc.) propagate
          as-is — they already have actionable messages from phase 2.

    4. Fetch ticket: t, err := src.Fetch(ctx, sourceIDRest)
       - On TicketNotFoundError, surface as "ticket %s not found on %s"
       - On AuthFailedError, surface as "authentication failed for %s; run
         `gh auth login` or configure token_env/token_file"
       - Other errors: wrap with "fetch %s:%s: %w"

    5. Construct architect (existing dependency injection mirrors how
       `nebula apply` wires it; reuse the same pattern):
         claudeInv := claude.NewInvoker(cfg.ClaudePath, cfg.Verbose)
         arch := nebula.NewArchitect(claudeInv, ...)

    6. Determine output directory:
         dir := flagDir (default ".nebulas")
         name := flagName
         if name == "" {
             name = nebula.SlugifyTicket(t)  // helper from phase 4
         }
         outPath := filepath.Join(dir, name)
         Check collision: if outPath exists, append "-2", "-3", … until free.

    7. Generate: info, err := arch.FromTicket(ctx, t, outPath)
       - Architect errors (LLM failure, write failure) propagate.

    8. Insert into SQLite via the existing fabric layer:
         INSERT INTO nebulas (id, source_type, source_name, source_id,
                              path, status, created_at, updated_at)
         VALUES (?, 'ticket', ?, ?, ?, 'draft', NOW, NOW)
       - The id is the nebula directory name.
       - source_type='ticket' distinguishes from existing 'manual' rows.

    9. Print the stderr summary line, return nil.
}
```

### Error surface (typed at the cmd layer for clean UI later)

The web UI in Nebula 4 will need to surface these in chips/toasts. Each branch above already produces a stable error message; do NOT add prefix decoration in the cmd layer (Cobra prints them as-is). The exit code is 1 for all errors except `TicketNotFoundError` which exits 2 (so scripts can distinguish "ticket doesn't exist" from "everything else").

### Idempotency

If a user runs `quasar nebula new github:42` twice:
- First run: creates `.nebulas/nebula-42-add-oauth/`, inserts SQLite row
- Second run: `.nebulas/nebula-42-add-oauth/` exists → architect writes to `.nebulas/nebula-42-add-oauth-2/`, inserts a new SQLite row

The user is responsible for deleting the old draft if they want to start over. (`quasar nebula reject <id>` lands later as part of the web-UI review surface in Nebula 4 — out of scope here.)

This behavior matches what the design spec described for "already-pulled issue."

### Testing

`nebula_new_test.go` uses table-driven cases against a fake `Architect` and fake `TicketSource`:
- Happy path: github:42 → calls fetch → calls architect → writes row, prints summary, exit 0
- Bad source format (`github` with no colon): error message, exit 1
- Unknown source name (`linear:42` when no [integrations.linear]): error mentions the missing config block, exit 1
- TicketNotFoundError from Fetch: exit 2
- Collision on output dir: appends "-2"; assert the architect was called with the suffixed path

Use the existing test infrastructure for cobra command testing (see how `nebula apply` is tested — mirror that style).

### What this phase does NOT do

- The freeform-prompt path (e.g. `quasar nebula new "build me a thing"`) is NOT added here. That can land later if it proves useful.
- No browse/list functionality (out of scope for the CLI; web UI in Nebula 4 will handle browsing).
- No `quasar nebula reject` (web UI concern).
- No automatic approval / launch — `nebula new` produces a draft. The user reviews it (manually editing the .md files if desired) and then runs `quasar nebula apply .nebulas/<name>/` as today.

## Files

- `cmd/nebula_new.go` (new) — subcommand definition, flag wiring, runNebulaNew function
- `cmd/nebula_new_test.go` (new) — table-driven tests with fakes
- `cmd/nebula.go` — register the new subcommand on the existing `nebulaCmd` parent
- `cmd/nebula_apply.go` — no behavior changes; this file is in scope only so any minor adjustments (e.g. shared helper extraction) are tracked

## Acceptance Criteria

- [ ] `quasar nebula new --help` prints the documented usage and flag list
- [ ] `quasar nebula new github:42` with a configured `[integrations.github]` block and a valid ticket writes a nebula directory and inserts an SQLite row with `source_name='github'` and `source_id='owner/repo#42'`
- [ ] `quasar nebula new github` (no colon) returns a usage error and exits 1
- [ ] `quasar nebula new linear:42` with no `[integrations.linear]` block returns an error mentioning the missing config and exits 1
- [ ] `quasar nebula new github:999999` (nonexistent ticket) exits 2 with a "ticket not found" message
- [ ] Running the command twice for the same ticket produces a second nebula directory with a `-2` suffix (or higher) and a second SQLite row
- [ ] The generated `nebula.toml` contains `source_name` and `source_id` matching the ticket
- [ ] No new tracking-system-specific code lives in `cmd/` — the command dispatches through the integration registry only
- [ ] The existing `quasar nebula apply <path>` flow is unchanged and continues to execute the generated draft after the user (or the Nebula 4 UI) approves it
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
