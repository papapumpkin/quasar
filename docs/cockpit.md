# The Cockpit — browser fleet dashboard

The **cockpit** is Quasar's browser-based, real-time fleet dashboard: a
"Mission Control" view of every registered repo's lanes (awaiting-approval,
in-flight, recent), with live in-flight run updates and one-click approve/reject.
It is **additive** to the TUI — the TUI (`quasar fleet`) remains the canonical
headless/SSH interface; the cockpit is what you open on a real machine.

The cockpit is **off by default** and its assets are only compiled into the
binary under a build tag, so a default build carries none of its code paths.

- Design: [`superpowers/specs/2026-06-10-quasar-cockpit-design.md`](superpowers/specs/2026-06-10-quasar-cockpit-design.md)
- Visual reference: [`superpowers/specs/2026-06-10-quasar-cockpit-mockup.html`](superpowers/specs/2026-06-10-quasar-cockpit-mockup.html)

## How it's built (go-native, no Node)

The cockpit is **server-rendered Go**, not a JavaScript SPA. This deliberately
diverges from the original `react-cockpit` nebula (React/Vite + JSON API) in
favour of a hypermedia stack that stays in one language and one binary:

| Concern | Choice |
|---------|--------|
| HTML | [`templ`](https://templ.guide) components compiled into the binary (`internal/cockpit/views/`) |
| CSS | Tailwind via its **standalone CLI** (no npm) → one embedded `cockpit.css` |
| Live updates | **Server-Sent Events** + [Datastar](https://data-star.dev) (~15 KB, vendored) — the server pushes DOM-fragment patches |
| Transport | The server renders HTML; there is **no JSON API and no JS bundle build** |

The runtime's `EventSink` (an interface in `internal/constellations`) feeds a
`Notifier` that fans state-change events out to every connected operator's SSE
stream, so an approve by one operator updates everyone's board within ~1s.

## Running it

```bash
# 1. Generate the bearer token (written to ~/.quasar/cockpit-token, mode 0600).
quasar cockpit token

# 2. Enable the cockpit — either flag or config:
#    .quasar.yaml:
#      cockpit:
#        enabled: true
#        addr: "127.0.0.1:7330"
#    ...or pass --cockpit on the command line.

# 3. Serve (cockpit + supervisor + step driver in one process):
quasar serve --cockpit
#    --cockpit-only runs just the cockpit (no supervisor), for testing.

# 4. Open http://127.0.0.1:7330, paste the token, watch the fleet.
```

The cockpit must be built with the `cockpit` build tag for its assets to be
embedded. Use the build script (it runs `templ generate`, compiles Tailwind,
then `go build -tags cockpit`):

```bash
TAILWIND_BIN=/path/to/tailwindcss scripts/build-cockpit.sh   # produces ./quasar
```

`templ` (the generator) and the Tailwind standalone CLI are single static
binaries — no Node. The generated `*_templ.go` and `cockpit.css` are committed,
so a plain `go build -tags cockpit ./...` works without re-running the tools.

## Auth model

- A 32-byte hex bearer token in `~/.quasar/cockpit-token` (0600), created by
  `quasar cockpit token`.
- The login page sets an `HttpOnly` cookie carrying the token; all routes except
  `/login` and `/assets/*` require it. Event-stream requests (`/sse`) get a 401
  rather than a redirect.
- No per-user identity in v1; writes are attributed to the cockpit.

## What it does (and doesn't, yet)

**Ships now:** the live fleet board (per-repo lanes), live in-flight run cards
(constellation step-flow, progress, cost, cycle) updated over SSE, and
approve/reject of awaiting-approval nebulas (reusing the same approve path as the
TUI: status → `approved` + an architect trigger).

**Deferred (clean follow-ups):** a run-detail page, a dedicated live stdout-tail
viewer (the tail element is a placeholder today), a nebula-detail page, GitHub PR
status badges (the `GitHubBadger` seam exists but is wired `nil` pending a
read-only `gh pr` adapter), and multi-operator presence.

## Safety

The cockpit's only writes are approve/reject and the runtime actions the TUI
already exposes — it never bypasses the [output safety perimeter](safety.md). All
git writes still flow through `internal/gitops`.
