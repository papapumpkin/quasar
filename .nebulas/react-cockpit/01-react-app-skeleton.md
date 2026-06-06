+++
id = "react-app-skeleton"
title = "React + Vite + TypeScript app skeleton: routing, auth flow, WS client, design system, embedded into the Go binary"
type = "task"
priority = 2
depends_on = ["cockpit-server"]
scope = [
    "cockpit/**",
    "internal/cockpit/embed.go",
]
+++

## Problem

The frontend lives under `cockpit/` at the repo root (peer to `cmd/`, `internal/`, `docs/`). It's a Vite + React + TypeScript app that builds to a static bundle which the Go server embeds. This phase establishes the skeleton: dependencies, routing, auth flow, design system, the WS client, and a build pipeline that produces a deterministic bundle Go can `go:embed`.

No fleet-specific UI yet — that's the next phase. This is purely scaffolding plus the "log in with a token" flow.

## Solution

### Stack

- Vite + React 18 + TypeScript
- React Router (file-based routing via Vite plugin)
- TanStack Query for REST cache (handles refetch-on-WS-event nicely)
- Tailwind CSS + shadcn/ui component primitives
- `nhooyr.io/websocket` browser client (matches the Go server side)
- Vitest + React Testing Library for unit tests
- Playwright for one smoke test (login → dashboard renders)

### Directory layout

```
cockpit/
  src/
    main.tsx                  # entry
    routes/
      _layout.tsx             # global shell
      login.tsx               # token entry
      index.tsx               # fleet overview (next phase)
      nebulas/$id.tsx         # placeholder until next phase
      runs/$id.tsx            # placeholder until next phase
    lib/
      api.ts                  # typed REST client
      ws.ts                   # WS subscription hook
      auth.ts                 # token storage + login state
      schema.ts               # zod schemas for API responses
    components/
      ui/                     # shadcn primitives
      Topbar.tsx
      RepoBadge.tsx
    styles/
      globals.css
  public/
    favicon.svg
  index.html
  vite.config.ts
  tsconfig.json
  package.json
  pnpm-lock.yaml              # pnpm because it's deterministic
```

### Build pipeline

`pnpm build` produces `cockpit/dist/`. The Go side embeds it via:

```go
//go:build cockpit
// +build cockpit

package cockpit

import "embed"

//go:embed all:../../cockpit/dist
var bundleFS embed.FS
```

CI step before `go build -tags cockpit ./...`:

```
cd cockpit && pnpm install --frozen-lockfile && pnpm build
```

Without the `cockpit` build tag, `embed_disabled.go` provides an empty `bundleFS` so dev builds don't need Node installed.

### Auth flow

1. App loads at `/`. If no token in `localStorage["quasar.token"]`, redirect to `/login`.
2. `/login` shows a single token input. Submitting calls `GET /api/v1/repos` with the token; if 200, store and redirect; if 401, show error.
3. All API calls (REST + WS upgrade) attach `Authorization: Bearer <token>` from storage.
4. On any 401 response, clear storage and redirect to `/login`.

### WS client

`cockpit/src/lib/ws.ts`:

```ts
export function useSubscription(topics: string[]) {
  // Open WS once per topic-set; reconnect with exponential backoff on close.
  // On message, invalidate the matching TanStack Query keys so the cache refetches.
  // On 'resync' hint, invalidate ALL queries.
}
```

Reconnect strategy: 1s → 2s → 5s → 10s → 30s cap, with jitter. Each reconnect, the client fetches `/api/v1/fleet` to resync.

### Tests

- Vitest: `api.ts` URL building, auth header attachment, 401 handling
- Vitest: `ws.ts` reconnect backoff sequence with a mocked WebSocket
- Playwright: launch `quasar serve --cockpit`, navigate to `http://127.0.0.1:7330`, paste token, see dashboard skeleton
- The Playwright test is gated behind a `pnpm test:e2e` script; it's not part of `pnpm test`

### Why not Next.js / Remix / etc.

We want a static bundle the Go binary owns end-to-end. SSR adds operational complexity (a separate runtime needs Node). React Router + Vite is the smallest stack that hits the goals and produces a folder of static files we can embed.

## Files

- `cockpit/package.json` (new)
- `cockpit/pnpm-lock.yaml` (new — committed for reproducibility)
- `cockpit/vite.config.ts` (new)
- `cockpit/tsconfig.json` (new)
- `cockpit/tailwind.config.ts` (new)
- `cockpit/index.html` (new)
- `cockpit/src/main.tsx` (new)
- `cockpit/src/routes/_layout.tsx` (new)
- `cockpit/src/routes/login.tsx` (new)
- `cockpit/src/routes/index.tsx` (new) — placeholder dashboard
- `cockpit/src/lib/api.ts` (new)
- `cockpit/src/lib/ws.ts` (new)
- `cockpit/src/lib/auth.ts` (new)
- `cockpit/src/lib/schema.ts` (new)
- `cockpit/src/components/Topbar.tsx` (new)
- `cockpit/src/components/RepoBadge.tsx` (new)
- `cockpit/src/styles/globals.css` (new)
- `cockpit/src/**/*.test.ts(x)` (new)
- `cockpit/e2e/login.spec.ts` (new)
- `cockpit/.gitignore` (new) — `dist/`, `node_modules/`
- `internal/cockpit/embed.go` (modify) — point at `../../cockpit/dist`
- `Makefile` or `scripts/build-cockpit.sh` (new) — convenience for `pnpm build` → `go build -tags cockpit`

## Acceptance Criteria

- [ ] `cd cockpit && pnpm install --frozen-lockfile && pnpm build` produces `cockpit/dist/`
- [ ] `go build -tags cockpit ./...` succeeds and embeds the bundle
- [ ] `quasar serve --cockpit` serves the React app at the configured addr
- [ ] Visiting `/` without a token redirects to `/login`
- [ ] Submitting a valid token at `/login` redirects to `/` and shows the dashboard skeleton
- [ ] Submitting an invalid token shows an error and stays on `/login`
- [ ] WS auto-reconnects with exponential backoff on disconnect
- [ ] `cockpit` directory has its own `.gitignore` and `README.md`
- [ ] Vitest suite passes
- [ ] Playwright e2e smoke test passes locally (CI runs it as a separate job)
- [ ] `go vet ./...`, `go test ./...` exit 0
