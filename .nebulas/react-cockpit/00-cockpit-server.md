+++
id = "cockpit-server"
title = "Go HTTP+WebSocket server: REST endpoints for the fleet model, WS for live deltas, bearer-token auth, embedded static bundle"
type = "task"
priority = 2
scope = [
    "internal/cockpit/**",
    "cmd/serve.go",
]
+++

## Problem

Before any React code exists, the backend that the cockpit consumes needs to exist. It's a small read-mostly API plus a single WS subscription. Living in the same process as the supervisor means there's no separate service to deploy, no cross-process state sync, and no auth boundary other than the bearer token.

The API surface is intentionally narrow: it exposes exactly what the TUI fleet view (constellation-runtime Phase 8) already needs. Anything else is out of scope until v2.

## Solution

### Endpoints

All under `/api/v1`. JSON-only. Auth: `Authorization: Bearer <token>` from `<quasar-data-dir>/cockpit-token`.

```
GET  /api/v1/repos                          # list registered repos
GET  /api/v1/fleet                          # full fleet snapshot (all lanes, all repos)
GET  /api/v1/nebulas?repo=&status=&limit=   # paginated nebula listing
GET  /api/v1/nebulas/:id                    # nebula detail
GET  /api/v1/nebulas/:id/phases             # phase list (read from blob hashes)

POST /api/v1/nebulas/:id/approve            # set status='approved', enqueue architect
POST /api/v1/nebulas/:id/reject             # { reason }
POST /api/v1/nebulas/:id/undelete           # within grace window

GET  /api/v1/runs?state=                    # constellation_runs listing
GET  /api/v1/runs/:id                       # run detail with star_invocations
GET  /api/v1/runs/:id/tail?node=&lines=     # tail recent stdout of an in-flight invocation
POST /api/v1/runs/:id/pause
POST /api/v1/runs/:id/resume
POST /api/v1/runs/:id/kill

WS   /api/v1/subscribe?topics=fleet,runs    # server pushes delta events
```

WS message envelope:

```json
{"topic": "fleet", "type": "nebula_status_changed", "data": {"id": "...", "status": "approved"}}
{"topic": "runs",  "type": "step_started",         "data": {"run_id": "...", "node": "coder"}}
{"topic": "runs",  "type": "step_completed",       "data": {"run_id": "...", "node": "coder", "cost_usd": 0.34}}
```

Delta events are emitted by a `Notifier` that the runtime, scheduler, and TUI all push to. Same source of truth, no double-counting.

### Server structure

`internal/cockpit/server.go`:

```go
type Server struct {
    db           *sql.DB
    runtime      *runtime.Runtime
    notifier     *Notifier
    githubClient *github.Client  // optional, for PR badge lookups
    token        string
    bundle       fs.FS            // embedded React build; empty in dev
    logger       io.Writer
}

func New(opts Opts) (*Server, error)
func (s *Server) Routes() http.Handler
func (s *Server) Run(ctx context.Context, addr string) error
```

### Notifier

`internal/cockpit/notifier.go`:

```go
// Notifier broadcasts delta events to subscribers. The runtime calls
// Publish when state changes; subscribers are WS clients.
//
// Buffered per-subscriber: a slow client doesn't block publishers.
// Subscriber buffer overflow drops oldest events and sends a "resync"
// hint; the client re-fetches /api/v1/fleet to catch up.
type Notifier struct{ /* ... */ }

func (n *Notifier) Publish(event Event)
func (n *Notifier) Subscribe(topics []string) (<-chan Event, func())  // returns unsub
```

The Notifier is hooked into the runtime at construction time. The TUI uses it too — when the cockpit pushes an approve, the TUI lane updates within ~100ms.

### Auth model

- Token at startup: `quasar cockpit token` generates one and writes to `<quasar-data-dir>/cockpit-token` (0600)
- All `/api/v1` routes check `Authorization: Bearer <token>`
- No per-user identity in v1 — the audit log records `actor="cockpit"` for every write
- The bundle (`/`, `/assets/*`) is served *without* auth so the login page can load; the React app prompts for the token and stores it in `localStorage`

### Feature flag

The cockpit is off by default in v1. `.quasar.yaml`:

```yaml
cockpit:
  enabled: false        # default
  addr: "127.0.0.1:7330"
  trusted_proxies: []   # for x-forwarded-for if behind a reverse proxy
```

If `enabled: false`, `cmd/serve.go` doesn't register the routes and the static bundle isn't included in the build (`go:embed` directive guarded by a build tag). This keeps the binary lean and the attack surface zero for ops who don't want a UI.

### Tests

- API handler tests using `httptest.Server` and an in-memory SQLite
- Notifier tests: publish/subscribe, slow-subscriber overflow → drop + resync hint
- Auth tests: missing/wrong/expired token → 401
- WS tests: connect, subscribe to topic, receive delta, unsubscribe — using `nhooyr.io/websocket` client
- Feature flag tests: with `enabled: false`, `/api/v1/*` returns 404

### Out of scope (deferred to v2)

- OIDC/SSO
- Per-user permissions
- Audit log viewer (CLI already exposes it)
- Sensor management UI (CLI suffices)

## Files

- `internal/cockpit/server.go` (new)
- `internal/cockpit/handlers/*.go` (new) — one file per resource (repos, nebulas, runs)
- `internal/cockpit/notifier.go` (new)
- `internal/cockpit/auth.go` (new)
- `internal/cockpit/embed.go` (new) — `go:embed` directive with `cockpit` build tag
- `internal/cockpit/embed_disabled.go` (new) — fallback when build tag absent
- `internal/cockpit/*_test.go` (new)
- `cmd/serve.go` (new) — `quasar serve` registers TUI + cockpit + supervisor; with `--cockpit-only` flag for isolation in tests
- `cmd/cockpit_token.go` (new) — `quasar cockpit token` to (re)generate the bearer token

## Acceptance Criteria

- [ ] All listed REST endpoints respond with documented schemas
- [ ] WS `/api/v1/subscribe` delivers delta events from a synthetic publish
- [ ] Bearer-token auth rejects missing/wrong tokens with 401
- [ ] `quasar cockpit token` writes a new 32-byte hex token to `<data-dir>/cockpit-token` with 0600 mode
- [ ] Slow WS subscriber overflow drops oldest events and emits a `resync` hint event
- [ ] Feature flag `cockpit.enabled=false` removes routes entirely (404)
- [ ] Notifier is the single source of truth for live updates — TUI fleet view also subscribes
- [ ] `go build -tags cockpit ./...` and `go build ./...` (without tag) both succeed
- [ ] `go vet ./...`, `go test ./...` exit 0
