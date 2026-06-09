# Authoring a sensor

This is the how-to for adding a new sensor — a poll-driven adapter that turns an
external work-tracker into seed nebulas. For the conceptual model (what a sensor
is, the cursor and dedup contracts, the scheduler), read
[sensors.md](sensors.md) first; this guide assumes it.

Audience: a Go developer who has never touched the sensor framework. The full
contract is four methods and one `init()`.

> All `file:line` citations were verified against `main` at write time and may
> drift as the code changes.

## Contents

1. [The minimum interface](#1-the-minimum-interface)
2. [Designing a cursor](#2-designing-a-cursor)
3. [Designing an ExternalID](#3-designing-an-externalid)
4. [Testing with an injected I/O seam](#4-testing-with-an-injected-io-seam)
5. [Secrets](#5-secrets)
6. [Backpressure and rate limits](#6-backpressure-and-rate-limits)
7. [Registration](#7-registration)
8. [Worked example — a GitHub Discussions sensor](#8-worked-example--a-github-discussions-sensor)

---

## 1. The minimum interface

Implement `sensors.Sensor` (`internal/sensors/sensors.go:92-112`). That is the
entire contract:

```go
type Sensor interface {
    Name() string
    Configure(raw map[string]any, secrets SecretResolver) error
    Poll(ctx context.Context, cursor json.RawMessage) ([]Event, json.RawMessage, error)
    SeedNebula(event Event) (*SeedNebulaContent, error)
}
```

- **`Name`** returns the type name (`sensors.go:95`). It is the registry key and
  what a repo's `sensors/<name>.toml` `type = "..."` matches. Convention:
  lowercase, snake_case, vendor-prefixed so two vendors' "issues" sensors do not
  collide (e.g. `github_issues`, `linear_issues`).
- **`Configure`** parses the instance's `[config]` block (`raw`) and resolves
  secrets (`sensors.go:97-100`). Validate eagerly and return a typed error — this
  is what `quasar lint` surfaces before the supervisor boots
  (`internal/artifacts/lint.go:132`).
- **`Poll`** returns events since `cursor` plus an advanced cursor
  (`sensors.go:102-106`). An empty cursor means first poll. The runtime persists
  `newCursor` for you (§2).
- **`SeedNebula`** renders one event into `*SeedNebulaContent`
  (`sensors.go:108-111`, the struct at `sensors.go:69-79`). You never touch the
  database — the runtime writes the draft row.

Add a compile-time assertion so a signature drift fails the build, not a poll:

```go
var _ sensors.Sensor = (*Source)(nil)
```

## 2. Designing a cursor

A cursor is **opaque JSON you define for yourself** (`sensors.go:104-105`). The
runtime stores and replays it (`sensor_cursors`,
`internal/fabric/sensor_store.go:15-60`); it never inspects it. The one rule: it
must advance **monotonically** so re-polling never re-emits seeded work. Three
patterns:

- **Numeric counter** — best when the API exposes a dense, monotonic id. The
  GitHub sensor stores `last_issue_number` and emits only issues with a greater
  number (`internal/sensors/github/github.go:114-118`, `116-118`). This is the
  pattern the worked example uses.
- **Timestamp** — best when there is no stable id but you can filter by
  `updated_at >`. Store a `last_updated_at` watermark. (Used by the specced,
  not-yet-shipped Linear sensor; see
  [sensors.md §8](sensors.md#8-status--what-is-wired-today).)
- **Opaque API cursor** — when the API hands you a continuation token to pass
  back, store it verbatim (e.g. a Slack `oldest_ts`). Specced, not shipped.

Anti-pattern: do not compute `max(id)` over one page *and also* follow the API's
"next page" link — you will double-process or skip. Pick one strategy.

## 3. Designing an ExternalID

`Event.ExternalID` (`sensors.go:60-64`) is the dedup identity. The table's
`UNIQUE (repo_path, sensor_name, external_id)`
(`internal/fabric/migrations/005_sensor_state.sql:35`) enforces it, so an
`ExternalID` must be:

- **Stable** — the same event must yield the same id across polls. `INSERT OR
  IGNORE` (`internal/fabric/sensor_store.go:91-118`) treats a re-observed id as a
  no-op, and the scheduler skips it (`scheduler.go:261-263`). An unstable id
  defeats this.
- **Unique** — no two distinct events share an id.
- **Naturally scoped** — the UNIQUE already namespaces by `(repo, sensor)`, so
  the id only needs to be unique *within* one sensor on one repo. A numeric
  `"owner/repo#42"` is ideal.

**Anti-patterns:** an array index into a paginated response (shifts between
polls), or the current timestamp at poll time (different every poll). Both
violate stability and will re-seed or skip work.

## 4. Testing with an injected I/O seam

There is **no shared test harness** in the tree today (the `PollHarness` kit
sketched in the additional-sensors spec is not yet implemented). The shipped
template is the GitHub sensor's pattern: make the I/O call a function field on
your struct and substitute a fake in tests.

The `gh` shell-out is a `runGHFunc` field (`internal/sensors/github/exec.go:13-17`):

```go
type Source struct {
    // ...
    runGH runGHFunc // injectable for tests; defaults to the real gh binary
}
```

Tests build a fake that answers from a fixture and rejects unexpected calls
(`fakeListGH`, `internal/sensors/github/github_test.go:39-49`), wire it through a
`sourceDeps` that never touches the real environment (`testDeps`,
`github_test.go:53-59`), and assert on the returned events and cursor
(`TestPollReturnsEventsAndAdvancesCursor`, `github_test.go:81-90`). Fixtures live
under `testdata/` (`github_test.go:72-79`). Keep the pure logic (cursor
decode/encode, body parsing) in functions you can table-test without any I/O at
all (`deriveGoalsAndConstraints`, `github.go:287-317`).

## 5. Secrets

Resolve every credential through the injected `SecretResolver`
(`internal/sensors/secret.go:39-54`) — never read it from `raw` directly. The
precedence is **file > env > empty** (`ResolveSecret`, `secret.go:56-82`):

```go
token, err := secrets.Resolve(sensors.SecretSpec{
    Env:  asString(raw["token_env"]),
    File: asString(raw["token_file"]),
})
if err != nil {
    return fmt.Errorf("resolve token: %w", err) // a broken token_file is terminal
}
```

1. `token_file` — a path; on Unix it must be mode `0600`/`0400` or you get a
   `*SecretLooseError` (`secret.go:84-105`). A configured-but-broken file is a
   **terminal error**, not a silent fall-through to env (`secret.go:67-71`).
2. `token_env` — an environment variable name.
3. Empty — fall back to your vendor's own auth only if it has one (the GitHub
   sensor lets `gh` resolve its own chain, `github/exec.go:37-46`); otherwise
   treat empty as a Configure error.

Never accept an inline secret in TOML; a literal `token:` is a config-load error.
The injected resolver also makes Configure testable — pass a fake `SecretResolver`
(`captureSecrets`, `github_test.go:17-26`) so tests never touch the real
filesystem or environment.

## 6. Backpressure and rate limits

The scheduler decides what to fire; your `Poll` just returns events. Inside
`Poll`:

- **Cap events per call.** Return a bounded page and let the cursor carry the
  rest to the next tick — the GitHub sensor caps at `pollLimit = 100`
  (`github/github.go:28`, used in `listArgs` at `github.go:210-216`). A typical
  cap is ~50.
- **Do not retry inside `Poll`.** A failed poll is logged and the scheduler's
  next tick re-attempts (`scheduler.go:202-210`); internal retries hide failures
  from the operator. `Poll` already runs under a bounded timeout
  (`scheduler.go:238-243`).

> Note: the additional-sensors spec proposes a `RateLimitError` the scheduler
> would sleep on. That type is **not** in the tree today — until it lands, return
> a plain wrapped error on rate-limit and rely on the next tick.

## 7. Registration

Register your constructor from `init()` so a blank import wires the sensor. The
constructor signature is `sensors.SensorConstructor`, i.e. `func() (Sensor,
error)` (`internal/sensors/registry.go:12`, `RegisterSensor` at
`registry.go:41-51`):

```go
func init() {
    sensors.Default().RegisterSensor("my_sensor_type", func() (sensors.Sensor, error) {
        return New(), nil
    })
}
```

The cmd layer adds a blank import (`_ ".../internal/sensors/mysensor"`) so the
`init()` runs; it never references your concrete type. Duplicate registrations
panic at init — that is a programmer error, not a runtime condition
(`registry.go:46-50`). `quasar lint` validates that a repo's sensor `type`
matches a registered constructor via `HasSensor` (`registry.go:87-92`).

## 8. Worked example — a GitHub Discussions sensor

A complete, compiling sensor that polls a repository's Discussions via `gh` and
seeds one nebula per new discussion. It demonstrates every section above: a
numeric-counter cursor (§2), an `"owner/repo#n"` ExternalID (§3), file/env secret
resolution (§5), a 100-implicit cap from the API page (§6), and `init()`
registration (§7).

The source of truth for this listing is
[`internal/sensors/discussions/discussions.go`](../internal/sensors/discussions/discussions.go),
a committed package, so `go build ./...` verifies it stays compiling.

```go
// Package discussions polls a GitHub repository's Discussions via the gh CLI
// and seeds one nebula per new discussion.
package discussions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/papapumpkin/quasar/internal/sensors"
)

// init registers the sensor under the type a repo's TOML selects with
// `type = "github_discussions"`. (§7)
func init() {
	sensors.Default().RegisterSensor("github_discussions", func() (sensors.Sensor, error) {
		return New(), nil
	})
}

// Source polls GitHub Discussions for one repository.
type Source struct {
	repo  string
	token string
}

// New constructs an unconfigured Source.
func New() *Source { return &Source{} }

// Compile-time check that Source satisfies the sensor contract. (§1)
var _ sensors.Sensor = (*Source)(nil)

// Name returns the registry key.
func (s *Source) Name() string { return "github_discussions" }

// Configure reads repo from [config] and resolves the gh token file > env. (§5)
func (s *Source) Configure(raw map[string]any, secrets sensors.SecretResolver) error {
	repo, _ := raw["repo"].(string)
	if repo == "" {
		return fmt.Errorf("github_discussions: repo is required")
	}
	token, err := secrets.Resolve(sensors.SecretSpec{
		Env:  asString(raw["token_env"]),
		File: asString(raw["token_file"]),
	})
	if err != nil {
		return fmt.Errorf("github_discussions: resolve token: %w", err)
	}
	s.repo, s.token = repo, token
	return nil
}

// cursor is the highest discussion number seen — numeric-counter pattern. (§2)
type cursor struct {
	LastNumber int `json:"last_number"`
}

// discussion is the subset of the gh JSON this sensor reads.
type discussion struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// Poll lists discussions and emits one Event per discussion newer than the
// cursor, advancing newCursor to the highest number seen.
func (s *Source) Poll(ctx context.Context, raw json.RawMessage) ([]sensors.Event, json.RawMessage, error) {
	var cur cursor
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cur); err != nil {
			return nil, raw, fmt.Errorf("github_discussions: decode cursor: %w", err)
		}
	}

	out, err := s.gh(ctx, "api", fmt.Sprintf("repos/%s/discussions", s.repo))
	if err != nil {
		return nil, raw, err
	}
	var list []discussion
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, raw, fmt.Errorf("github_discussions: parse: %w", err)
	}

	highest := cur.LastNumber
	var events []sensors.Event
	for _, d := range list {
		if d.Number <= cur.LastNumber {
			continue // already seeded on an earlier poll
		}
		if d.Number > highest {
			highest = d.Number
		}
		events = append(events, sensors.Event{
			// Stable + unique within (repo, sensor): the dedup key. (§3)
			ExternalID: fmt.Sprintf("%s#%d", s.repo, d.Number),
			Raw:        map[string]any{"title": d.Title, "body": d.Body, "url": d.URL},
		})
	}

	next, err := json.Marshal(cursor{LastNumber: highest})
	if err != nil {
		return nil, raw, fmt.Errorf("github_discussions: encode cursor: %w", err)
	}
	return events, next, nil
}

// SeedNebula renders one discussion Event into seed-nebula content.
func (s *Source) SeedNebula(ev sensors.Event) (*sensors.SeedNebulaContent, error) {
	title := asString(ev.Raw["title"])
	return &sensors.SeedNebulaContent{
		Name:        title,
		Description: asString(ev.Raw["body"]),
		SourceName:  "github",
		SourceID:    ev.ExternalID,
		SourceURL:   asString(ev.Raw["url"]),
		Goals:       []string{title},
	}, nil
}

// gh shells out to the gh binary, injecting the resolved token when present.
func (s *Source) gh(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if s.token != "" {
		cmd.Env = append(cmd.Environ(), "GH_TOKEN="+s.token)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("github_discussions: gh %v: %w", args, err)
	}
	return stdout.Bytes(), nil
}

// asString returns v as a string, or "" when absent or not a string.
func asString(v any) string {
	s, _ := v.(string)
	return s
}
```

To wire it into a repo, drop a `sensors/github-discussions.toml`:

```toml
name          = "github-discussions"
type          = "github_discussions"
poll_interval = "10m"

[config]
repo      = "papapumpkin/quasar"
token_env = "QUASAR_GH_TOKEN"

[[triggers]]
constellation = "architect"
when          = "new_item"
```

See [sensors.md §10](sensors.md#10-per-repo-sensor-toml-format) for the full TOML
schema and [sensors.md](sensors.md) for the conceptual model behind every method
above.
