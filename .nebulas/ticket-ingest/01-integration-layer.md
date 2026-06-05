+++
id = "integration-layer"
title = "Define the integration layer: TicketSource, Forge stub, Registry, credential resolution"
type = "task"
priority = 1
scope = [
    "internal/integrations/integrations.go",
    "internal/integrations/integrations_test.go",
    "internal/integrations/registry.go",
    "internal/integrations/registry_test.go",
    "internal/integrations/secret.go",
    "internal/integrations/secret_test.go",
    "internal/config/config.go",
    "internal/config/config_test.go",
    "internal/nebula/types.go",
    "internal/fabric/sqlite.go",
    "internal/fabric/migrations/**",
]
+++

## Problem

Quasar has no concept of "where the work to do came from." Tasks today are either manual nebula files or freeform prompts. To make Quasar a system that pulls work from external trackers (GitHub Issues now; Jira, Linear, ServiceNow later) we need a forge-agnostic abstraction that lets each integration plug in without changes to the architect, the orchestrator, or the CLI surface. The user's stated framing: "treat it as an integration (kinda like Workato)."

This phase establishes the abstraction with no concrete adapters yet — the interface, the DTO, the registry, the credential resolver, and the config schema. Phase 2 adds the first concrete adapter (GitHub).

## Solution

### Package layout

Create `internal/integrations/` as a new package. It owns:
- The `TicketSource` interface (input side — read tickets from a tracker)
- The `Forge` interface stub (output side — currently `Name()` only; full surface lands in Nebula 3)
- The `Ticket` DTO that adapters return
- A registry that maps `name → constructor` for both interface families
- A `SecretSpec` + `ResolveSecret` helper for Docker-friendly credential handling

### `Ticket` DTO

```go
// Ticket is the source-agnostic representation of a unit of work pulled from
// an external tracker. Adapters convert their native shape (GitHub Issue,
// Jira Issue, Linear Issue, etc.) into this DTO at the integration boundary.
type Ticket struct {
    SourceName     string            // adapter name, e.g. "github"
    SourceID       string            // adapter-canonical id, e.g. "papapumpkin/quasar#42"
    Number         int               // human-facing number when applicable; 0 if N/A
    Title          string
    Body           string            // markdown
    State          string            // "open" | "closed" | adapter-specific
    Labels         []string
    Assignee       string
    URL            string            // browser-clickable
    Comments       []Comment         // chronological
    LinkedWork     []string          // PR/MR/cross-ticket refs (URLs or source-ids)
    SourceMetadata map[string]string // adapter-specific extras (sprint, milestone, etc.)
}

type Comment struct {
    Author    string
    Body      string
    CreatedAt time.Time
}
```

`SourceID` is the canonical, unique reference that the registry uses to round-trip a ticket back to its adapter. For GitHub it is `<owner>/<repo>#<number>`. For Jira it would be `<project>-<number>`. Adapters define their own format; the only invariant is uniqueness within `SourceName`.

### `TicketSource` interface

```go
// TicketSource is the read-side integration with an external work-tracking
// system. Implementations are registered in the package-level registry via
// init() so they can be looked up by name at runtime.
//
// Implementations MUST be safe for concurrent use.
type TicketSource interface {
    // Name returns the adapter name (e.g. "github", "jira"). Used as the
    // registry key and the SourceName field on Tickets it returns.
    Name() string

    // Fetch retrieves a single ticket plus its comments and any cross-refs
    // the adapter can resolve cheaply. Implementations should NOT fetch
    // transitively reachable work (e.g. don't follow linked PRs).
    //
    // The sourceID format is adapter-specific (see Ticket.SourceID).
    Fetch(ctx context.Context, sourceID string) (*Ticket, error)
}
```

`List` (multi-ticket browsing) is intentionally omitted in this nebula. Listing is a UI concern that arrives with Nebula 4 — CLI users find tickets via the source's own tooling (`gh issue list`, the Jira board, etc.) and pass the reference to `quasar nebula new`. When the web UI needs to list, the interface can be extended at that time.

### `Forge` interface stub

```go
// Forge is the write-side integration with a Git forge (PR/MR creation,
// comment polling, status sync). This interface is reserved here so the
// .quasar.yaml [forge.*] schema and the registry pattern are uniform across
// integration kinds, but its surface is intentionally minimal in this
// nebula. The full methods land in Nebula 3 (master-review-pr-loop).
type Forge interface {
    Name() string
}
```

### Registry

```go
// Registry holds the runtime mapping of integration name to constructor.
// There are two parallel namespaces — ticket sources and forges — because
// the same name (e.g. "github") legitimately appears in both with distinct
// roles. The registry is goroutine-safe.
type Registry struct { /* sync.RWMutex, two maps */ }

// RegisterTicketSource registers a TicketSource constructor under the given
// name. Adapters call this from their package init() so the binary picks
// them up automatically. Duplicate registrations panic — this is an init-
// time programmer error, not a runtime condition.
func (r *Registry) RegisterTicketSource(name string, ctor TicketSourceConstructor)

// RegisterForge mirrors RegisterTicketSource for forges.
func (r *Registry) RegisterForge(name string, ctor ForgeConstructor)

// BuildTicketSource resolves the named source from config, returning the
// constructed adapter or an error if the name is not registered, the config
// section is missing, or the adapter's constructor errors (e.g. credential
// resolution failed).
func (r *Registry) BuildTicketSource(name string, cfg map[string]any, secrets SecretResolver) (TicketSource, error)

// Constructors are functions over the parsed config section + secret resolver.
type TicketSourceConstructor func(cfg map[string]any, secrets SecretResolver) (TicketSource, error)
type ForgeConstructor        func(cfg map[string]any, secrets SecretResolver) (Forge, error)
```

A package-level `Default()` returns the process registry; tests can construct their own `Registry` for isolation.

### Credential resolution

```go
// SecretSpec describes how a secret should be loaded. Both fields are
// optional; an adapter that needs a secret should call ResolveSecret and
// decide whether an empty result is an error or a fallback (e.g. GitHub
// can fall back to `gh auth token`).
type SecretSpec struct {
    Env  string // environment variable name, e.g. "GITHUB_TOKEN"
    File string // filesystem path, e.g. "/run/secrets/github_token"
}

// ResolveSecret returns the secret value with this precedence:
//   1. File (takes precedence — supports Docker --secret mounts)
//   2. Env (12-factor pattern)
//   3. Empty string
// File reads are restricted: the file MUST have mode 0600 or 0400 on Unix;
// looser permissions are a SecretLooseError so misconfigured containers
// surface the issue immediately. Returned secrets are trimmed of trailing
// newlines (common in Docker secret files).
func ResolveSecret(spec SecretSpec) (string, error)
```

A `SecretResolver` interface wraps `ResolveSecret` so tests can inject a fake without filesystem access.

### Config schema additions

Extend `internal/config/config.go` to parse:

```yaml
[integrations.github]
repo: ""                       # empty = auto-detect via `git remote get-url origin`
token_env: "GITHUB_TOKEN"
token_file: ""                 # takes precedence if set

[forge.github]
repo: ""
base_branch: "main"
token_env: "GITHUB_TOKEN"
# (methods land in Nebula 3; this nebula reserves the schema only)
```

The config layer stores integration sections as opaque `map[string]any` so adding `[integrations.jira]` tomorrow doesn't require a parser change. Strong-typing happens inside each adapter's constructor.

**Inline-token guardrail.** If a parsed `[integrations.*]` or `[forge.*]` map contains a key named `token` (case-insensitive), config load MUST return an error pointing the user to `token_env` / `token_file`. Tokens never live in the YAML.

### Nebula schema additions

In `internal/nebula/types.go`, add to `Nebula`:
```go
SourceName string  // empty when the nebula was manually authored
SourceID   string  // empty when the nebula was manually authored
```

> **DEFERRED — precondition missing in current codebase (see review note below).**
> The original intent was to extend a `nebulas` table in the SQLite layer:
> ```sql
> ALTER TABLE nebulas ADD COLUMN source_name TEXT NOT NULL DEFAULT '';
> ALTER TABLE nebulas ADD COLUMN source_id   TEXT NOT NULL DEFAULT '';
> CREATE INDEX nebulas_source ON nebulas (source_name, source_id);
> ```
> However, `internal/fabric/sqlite.go` has **no `nebulas` table** (only `fabric`,
> `entanglements`, `file_claims`, `discoveries`, `pulses`) and **no migration
> framework** — its schema is a single inline `CREATE TABLE IF NOT EXISTS` DDL
> string. Nebula state today is persisted as TOML (`nebula.state.toml`), not DB
> rows. Building a `nebulas` table plus a migration runner now would be
> speculative schema with no caller, contradicting this nebula's own
> "don't add on speculation" philosophy. The consumable part — the
> `SourceName`/`SourceID` fields on the in-memory `Nebula` struct — is done.
> Persistence is deferred to the phase that actually writes nebulas to the DB
> (phase 5, `nebula new`), where the table shape is known.

Existing rows get empty strings (which means "manual"). No cache table for tickets — that is explicitly out of scope.

### Testing

- `integrations_test.go`: assert the interface compiles, sanity-check the DTO zero value
- `registry_test.go`: register a fake adapter, build it, verify duplicate-register panics
- `secret_test.go`: env path, file path with mode 0600 + 0644 (latter errors), env-and-file precedence, missing file, trim trailing newline
- `config_test.go`: round-trip a yaml with `[integrations.github]`, assert `token` inline triggers an error

### What this phase does NOT do

- No GitHub adapter (phase 2)
- No git wrapper or pre-commit runner (phase 3)
- No architect changes (phase 4)
- No CLI command (phase 5)
- No `init` / `doctor` / docs (phase 6)

## Files

- `internal/integrations/integrations.go` (new) — TicketSource, Forge, Ticket, Comment types
- `internal/integrations/integrations_test.go` (new) — interface + DTO sanity tests
- `internal/integrations/registry.go` (new) — Registry struct + Default() singleton + constructor types
- `internal/integrations/registry_test.go` (new) — registration, lookup, dispatch, duplicate-register panic
- `internal/integrations/secret.go` (new) — SecretSpec, ResolveSecret, SecretResolver, file-mode check
- `internal/integrations/secret_test.go` (new) — precedence, mode-check, trimming, env-only, file-only, neither
- `internal/config/config.go` — add IntegrationSections + ForgeSections fields (each is `map[string]map[string]any`), parse `[pre_commit]` placeholder (no consumption yet), inline-token guardrail
- `internal/config/config_test.go` — yaml round-trips, inline-token error
- `internal/nebula/types.go` — add SourceName + SourceID to the Nebula struct
- ~~`internal/fabric/sqlite.go` — wire the new migration into the migrations list~~ — **DEFERRED to phase 5** (no `nebulas` table or migration framework exists; see SQLite note above)
- ~~`internal/fabric/migrations/NNN_nebulas_source.sql` (new) — ALTER TABLE statements above~~ — **DEFERRED to phase 5** (precondition missing)

## Acceptance Criteria

- [ ] `internal/integrations/` package compiles and `go test ./internal/integrations/...` passes
- [ ] `TicketSource` interface has `Name()` and `Fetch(ctx, sourceID)` methods; no other methods
- [ ] `Forge` interface has only `Name()` (full surface deferred to Nebula 3)
- [ ] `Registry` exposes RegisterTicketSource, RegisterForge, BuildTicketSource, BuildForge, and a package-level Default()
- [ ] Duplicate registration of the same name panics with a descriptive message
- [ ] `ResolveSecret` returns the file content (trimmed) when `File` is set with mode 0600 or 0400; returns an error for mode 0644 with a message naming the file path
- [ ] `ResolveSecret` returns the env value when only `Env` is set and the env var exists; returns an empty string with no error when neither is set
- [ ] Loading a `.quasar.yaml` with `[integrations.github] token: "ghp_xxx"` returns an error mentioning `token_env`/`token_file`
- [x] `Nebula` struct has `SourceName` and `SourceID` string fields
- [~] ~~SQLite migration adds `source_name`, `source_id` columns to `nebulas` with NOT NULL DEFAULT '' and creates an index on the pair~~ — **DEFERRED to phase 5**: there is no `nebulas` table and no migration framework in this codebase (nebula state is TOML-persisted). Fabricating both on speculation contradicts this nebula's "no cache table / don't add on speculation" philosophy. The Go struct fields — the part consumable now — are present; persistence lands with the phase that writes nebulas to the DB.
- [x] Existing nebulas in `.nebulas/*/nebula.toml` continue to load (no schema change to the TOML manifest in this phase — only the Go struct gains fields, which are populated by phase 5 when the source is known)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
