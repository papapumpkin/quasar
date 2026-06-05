# Quasar Integrations

Quasar ingests work from external trackers (GitHub Issues today; Jira, Linear,
and others tomorrow) through a small, forge-agnostic integration layer. This
document explains the pattern and walks through adding a new adapter.

## The integration pattern

Everything lives under `internal/integrations/`:

- **`TicketSource`** (read side) — fetches a unit of work and converts it into
  the source-agnostic `Ticket` DTO:

  ```go
  type TicketSource interface {
      Name() string
      Fetch(ctx context.Context, sourceID string) (*Ticket, error)
  }
  ```

- **`Forge`** (write side) — reserved for PR creation, comment polling, and
  status sync in a later release. In the current release it intentionally has a
  **single** method:

  ```go
  type Forge interface {
      Name() string
  }
  ```

  (`TestForgeStubMinimal` in `internal/arch_test/` keeps it minimal until the
  full surface is rolled out — see [docs/safety.md](safety.md).)

- **`Registry`** — maps an adapter name (e.g. `"github"`) to a constructor.
  Adapters register themselves from their package `init()`, and callers look
  them up by name via `integrations.Default()`. The command layer reaches an
  adapter only through this registry — it never imports the adapter package for
  its types (enforced by `TestIntegrationsLayering`).

- **`SecretResolver` / `ResolveSecret`** — Docker-friendly credential
  resolution. A `SecretSpec{Env, File}` resolves with this precedence:
  1. `File` (a Docker `--secret` mount; must be mode `0600`/`0400`),
  2. `Env` (12-factor),
  3. empty (the adapter may fall back to its own auth, e.g. `gh auth token`).

  A configured-but-broken `token_file` is a **terminal error** — there is no
  silent fallback to the environment, so a misconfigured container surfaces the
  problem loudly.

The `Ticket` DTO is the contract between adapters and the rest of Quasar (the
architect that turns a ticket into a nebula). Adapters translate their native
shape into it at the boundary, so nothing downstream is GitHub- or Jira-aware.

## Adding a new ticket source

Suppose you want to ingest Jira issues. The shape mirrors the GitHub adapter
(`internal/integrations/github/`).

### 1. Package layout

```
internal/integrations/jira/
  jira.go     // the Source adapter + init() registration
  parse.go    // pure functions: source-id parsing, JSON → Ticket
  exec.go     // the thin, swappable shell-out / HTTP client
  *_test.go   // table-driven tests over the pure functions
```

Keeping the pure logic (`parse.go`) separate from the I/O (`exec.go`) is what
makes the bulk of the adapter unit-testable without a live service.

### 2. Register from `init()`

```go
package jira

import "github.com/papapumpkin/quasar/internal/integrations"

func init() {
    integrations.Default().RegisterTicketSource("jira",
        func(cfg map[string]any, secrets integrations.SecretResolver) (integrations.TicketSource, error) {
            return New(cfg, secrets)
        })
}
```

The command layer adds a blank import (`_ ".../internal/integrations/jira"`) so
the `init()` runs and the adapter is wired. It never references `jira` types
directly.

### 3. Parse the source-id

The `nebula new <source>:<id>` command splits on the first colon, so your
adapter receives the raw `<id>`. Define the shapes you accept and reject the
ambiguous ones, e.g.:

```go
// "PROJ-123"            fully qualified
// "123"                 bare number; project taken from [integrations.jira].project
func ParseSourceID(input, defaultProject string) (project string, number int, err error) { … }
```

### 4. Implement `Fetch`

```go
func (s *Source) Fetch(ctx context.Context, sourceID string) (*integrations.Ticket, error) {
    project, number, err := ParseSourceID(sourceID, s.project)
    if err != nil {
        return nil, err
    }
    raw, err := s.client.GetIssue(ctx, project, number) // exec.go seam
    if err != nil {
        return nil, classifyError(err) // map onto integrations.ErrTicketNotFound where possible
    }
    return toTicket(raw), nil // parse.go: native shape → Ticket DTO
}
```

Map "not found" onto the shared sentinel so forge-neutral callers can detect it
without importing your package:

```go
func (e *NotFoundError) Is(target error) bool { return target == integrations.ErrTicketNotFound }
```

### 5. Handle credentials

Resolve the token through the injected resolver — never read it from config
directly:

```go
token, err := secrets.Resolve(integrations.SecretSpec{
    Env:  stringFromCfg(cfg, "token_env"),
    File: stringFromCfg(cfg, "token_file"),
})
```

## `.quasar.yaml` configuration

Each adapter reads an `[integrations.<name>]` block. Sections are stored
opaquely (`map[string]any`), so adding an adapter needs **no** change to the
config parser — strong typing happens inside your constructor.

```yaml
integrations:
  jira:
    project: "PROJ"
    base_url: "https://your-org.atlassian.net"
    token_env: "JIRA_TOKEN"           # or:
    # token_file: "/run/secrets/jira_token"
```

**Never inline a token.** A literal `token:` key is rejected at config-load time
(`config.ErrInlineToken`) and a committed config containing one fails
`TestNoInlineTokens`. Use `token_env` or `token_file` only.

## Testing integrations

Write tests against the pure functions and an injected I/O seam — never the live
service:

- **Parse functions** (`ParseSourceID`, JSON → `Ticket`) are pure and
  table-driven. This is where most of the coverage lives.
- **The exec/HTTP seam** is an injectable function or interface on the `Source`
  struct (see how the GitHub adapter swaps `runGH` in `exec_test.go`). Tests
  feed canned responses and assert the resulting `Ticket`, with no network
  access.
- **Credential resolution** can be exercised with a fake `SecretResolver` so you
  never touch the real filesystem or environment.

See `internal/integrations/github/` for a complete, working reference adapter.
