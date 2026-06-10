# Quasar Cockpit — Live Fleet Dashboard (Design Spec)

**Date:** 2026-06-10
**Status:** Approved (brainstorm) — ready for implementation plan
**Visual reference:** [`2026-06-10-quasar-cockpit-mockup.html`](./2026-06-10-quasar-cockpit-mockup.html) (open in a browser)
**Supersedes the stack choice in:** `.nebulas/react-cockpit/` (React/Vite) — see "Divergence from the nebula" below.

## Context

Quasar today is a Go CLI + Bubble Tea TUI. The TUI fleet view is the canonical
headless interface and stays. This adds a **browser-based cockpit** — the screen
an operator opens on a real machine to watch the fleet and approve work — without
introducing a separate service, a Node toolchain, or a JSON API.

The cockpit is purely additive, off by default, and shares the supervisor's
process and SQLite data. The `react-cockpit` nebula scoped this feature but
assumed a React SPA + JSON API; we deliberately diverge to a go-native
hypermedia stack (decision below).

## Decisions (locked in brainstorm)

1. **Visual direction: "Mission Control"** — dark, dense, monospace data, neon
   status accents (cyan/green/amber/red/violet), live-pulsing in-flight runs. An
   always-on ops console. The committed mockup is the visual north star.
2. **Stack: Go + `templ` + Tailwind + Datastar over SSE.** Fully go-native,
   real-time-first, single binary, no Node/npm/Vite.
   - `templ` — type-safe Go templates compiled into the binary; renders full
     pages and small fragments.
   - Tailwind — via the **standalone CLI binary** (no npm); produces one
     `cockpit.css` embedded via `go:embed`.
   - **Datastar** (~15KB, embedded) — holds the SSE connection, patches the DOM
     with server-pushed fragments, and wires actions (`data-on-click` → POST).
   - The server renders HTML. There is **no JSON API and no JS bundle build.**
3. **First slice: the live fleet dashboard, end-to-end.** Server + auth + embed +
   feature flag + the Mission Control fleet view with real live updates from the
   runtime `Notifier` and one-click approve/reject. Run-detail, deep live-tail,
   and nebula-detail are deferred follow-ups.

### Divergence from the `react-cockpit` nebula

| Nebula assumption | This design |
|---|---|
| React + Vite + TypeScript SPA under `cockpit/` | No SPA. `templ` server-rendered views in `internal/cockpit/views/`. |
| Backend serves JSON only; "no HTML rendering on the server" | Server renders HTML (pages + SSE fragments). No JSON API. |
| WebSocket subscription | SSE (one-way server→client is all the cockpit needs; actions are plain POSTs). Datastar is SSE-native. |
| pnpm bundle embedded via `go:embed all:../../cockpit/dist` | Tailwind standalone CLI → one `cockpit.css`; Datastar JS vendored; both embedded. No Node. |

Everything else from the nebula holds: same data model as the TUI fleet view,
bearer-token auth from a config file, `go:embed` + build-tag feature flag, the
`Notifier` as the single source of truth, TUI remains canonical.

## Architecture

A new package `internal/cockpit/` owns the web surface. One `Server` struct,
constructor-injected, no global state:

```go
type Server struct {
    db       *sql.DB             // read views over fabric
    runtime  *constellations.Runtime // approve/reject/pause/resume/kill
    fleet    *fleet.Store        // reuse the TUI's fleet query shape
    notifier *Notifier           // live event fan-out
    github   GitHubBadger        // optional PR/issue status (interface; nil-safe)
    token    string              // bearer token
    assets   fs.FS               // embedded css + datastar js (empty w/o build tag)
    logf     func(string, ...any)
}

func New(opts Opts) (*Server, error)
func (s *Server) Routes() http.Handler          // stdlib net/http + http.ServeMux
func (s *Server) Run(ctx context.Context, addr string) error
```

- **Routing:** stdlib `net/http` + `http.ServeMux` (Go 1.22+ pattern routing).
  No web framework — keeps deps minimal and matches the project's stdlib-first
  convention.
- **Rendering:** `templ` components in `internal/cockpit/views/`. A page
  component renders the whole board; lane and card components render the
  fragments pushed over SSE.

### Notifier (single source of truth)

```go
// Notifier broadcasts runtime/scheduler state-change events to SSE subscribers.
// Buffered per-subscriber; a slow client drops oldest events and receives a
// `resync` event so it re-renders the whole board. The TUI may subscribe too.
type Notifier struct{ /* mu, map[subID]chan Event */ }

func (n *Notifier) Publish(e Event)
func (n *Notifier) Subscribe(topics []string) (id string, ch <-chan Event, cancel func())
```

`Event{Topic, Type, Data}` mirrors the nebula envelope (`fleet`/`runs` topics;
`nebula_status_changed`, `step_started`, `step_completed`, `resync`). The
runtime and scheduler call `Publish` where they already persist state; the
Notifier is injected at construction so there is one emit path, no double-count.

### Auth

- `quasar cockpit token` generates a 32-byte hex token → `<data-dir>/cockpit-token` (0600).
- A minimal login page sets an `HttpOnly` cookie holding the token (so SSE `GET`
  and fragment `GET`s authenticate without JS header plumbing).
- All cockpit routes except the login page + static assets require the token;
  mismatch → redirect to login (or 401 for fragment/SSE requests).
- No per-user identity in v1; writes audit as `actor="cockpit"`.

### Embed + feature flag

- `embed.go` (`//go:build cockpit`) embeds `assets/` (the built `cockpit.css`
  and vendored `datastar.js`). `embed_disabled.go` (`//go:build !cockpit`)
  provides an empty `fs.FS`.
- `.quasar.yaml`: `cockpit.enabled` (default `false`), `cockpit.addr`
  (`127.0.0.1:7330`). When disabled, `cmd/serve.go` does not mount the routes.
- `go build ./...` (no tag) and `go build -tags cockpit ./...` both compile.

## Data flow

1. **Initial load** — `GET /` (authed) → `fleet.Store` snapshot → templ renders
   the full board → the page's Datastar attributes open an SSE stream to
   `GET /sse`.
2. **Live update** — runtime persists a transition and calls
   `notifier.Publish(step_completed{run_id, node, cost_usd})` → SSE handler
   formats a Datastar fragment-patch of just that run card → every connected
   operator's DOM updates in <1s. No polling.
3. **Action** — Approve → Datastar `POST /nebulas/{id}/approve` →
   `runtime` enqueues the architect + sets status → `Publish(nebula_status_changed)`
   → the card animates from the awaiting lane to in-flight on all screens. The
   POST response also returns the immediate fragment for the actor (optimistic).

## Components / file structure

```
internal/cockpit/
  server.go            # Server struct, New, Run
  routes.go            # ServeMux wiring (pages, actions, /sse, /assets)
  notifier.go          # Notifier + Event
  notifier_test.go
  auth.go              # token check, login handler, cookie
  auth_test.go
  embed.go             # //go:build cockpit  — embeds assets/
  embed_disabled.go    # //go:build !cockpit — empty FS
  sse.go               # SSE handler: subscribe, format Datastar patches, heartbeat
  handlers/
    fleet.go           # GET / (full board), GET /sse
    nebulas.go         # POST approve/reject
    runs.go            # POST pause/resume/kill
    *_test.go          # httptest + in-memory sqlite
  views/               # templ
    page.templ         # shell: <head>, datastar, topbar, board
    rack.templ         # one repo rack (header + 3 lanes)
    lane.templ         # a lane (awaiting | in-flight | recent)
    card.templ         # nebula card (+ approve/reject)
    run.templ          # live in-flight run card (flow, bar, cost, tail)
    badges.templ       # status pills, PR/issue links
  assets/
    cockpit.css        # Tailwind CLI output (generated; committed)
    datastar.js        # vendored (committed)
cmd/
  serve.go             # `quasar serve [--cockpit]` — TUI + supervisor + cockpit
  cockpit_token.go     # `quasar cockpit token`
scripts/
  build-cockpit.sh     # tailwind build + go build -tags cockpit
```

Reuse, not reinvent: the fleet query reuses `internal/tui/fleet`'s data shape;
GitHub badge lookups reuse the existing `internal/sensors/github` client behind a
small `GitHubBadger` interface defined in cockpit (nil-safe when unavailable).

## Testing

- **Handlers** — `httptest.Server` + in-memory SQLite seeded with repos/nebulas/
  runs; assert rendered fragments contain the expected lane/card/state.
- **Notifier** — publish/subscribe delivery; slow-subscriber overflow → drop
  oldest + `resync`; cancel removes the subscriber (no leak).
- **Auth** — missing/wrong token → login redirect / 401; valid cookie → 200.
- **Feature flag** — routes absent when disabled (404).
- **templ** — components render without error for representative states
  (empty lane, failed run, open-PR card).
- Gates: `go vet ./...`, `go test ./...`, and `scripts/lint.sh` green; both
  `go build ./...` and `go build -tags cockpit ./...` compile.

## Deferred (clean follow-up slices)

Run-detail page · full scrollable constellation step trace · dedicated live-tail
viewer · nebula-detail page · multi-operator cursors/presence avatars · sensor
management UI · OIDC/SSO. This slice ships the live board + approve/reject.

## Risks / notes

- **templ + Tailwind toolchain** are new to the repo. Both are single static
  binaries (`templ generate`, `tailwindcss`); no Node. `build-cockpit.sh` and a
  short `docs/` note document the flow. The committed `cockpit.css`/`datastar.js`
  mean `go build -tags cockpit` works without running the tools.
- **Datastar maturity** — newer than HTMX. Mitigation: it's a single vendored
  file with a small, stable SSE/attribute API; the server contract (SSE emits
  fragment patches) is framework-agnostic enough to swap to HTMX if ever needed.
- **`Notifier` emit sites** — the runtime/scheduler must call `Publish` at each
  state transition. The plan enumerates the exact call sites so none are missed
  (the same discipline that the budget/heartbeat writes already follow).
