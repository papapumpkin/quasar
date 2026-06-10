+++
id = "sensors"
title = "Sensor model + the github sensor + authoring guide — the extension surface a developer reads to add a new signal source"
type = "task"
priority = 2
depends_on = ["foundation"]
scope = [
    "docs/sensors.md",
    "docs/sensor-authoring.md",
    "docs/integrations.md",
]
+++

## Problem

`docs/integrations.md` predates the rename: the codebase no longer has a
`TicketSource` interface or an `internal/integrations/` package. The new
`Sensor` model is what's actually running.

There is no good consolidated explanation of what a sensor IS, how the
shipped GitHub sensor works, what the cursor/dedup contract is, or how
someone authors a sensor for Linear or Slack.

## Solution

### `docs/sensors.md` — new (the conceptual doc)

#### TOC

1. **What a sensor is** — a poll-driven adapter. Sensors don't push; they
   are queried periodically by a scheduler. Each Poll returns a batch of
   `Event` values + an updated cursor. The runtime renders each event
   into a seed nebula (a draft row in the nebulas table) that the operator
   approves to trigger the architect constellation.
2. **The Sensor interface** — `internal/sensors/sensors.go`. Four
   methods: `Name`, `Configure`, `Poll`, `SeedNebula`. Walk through each.
3. **Event + SeedNebulaContent** — the typed payloads. `Event` carries
   the sensor-specific raw data; `SeedNebulaContent` is what the runtime
   writes to the nebulas table.
4. **The cursor contract** — opaque JSON; the runtime persists it in
   `sensor_cursors`; the sensor advances it monotonically (typically
   max-issue-number-seen or last-updated-at timestamp). Reference
   `internal/fabric/sensor_store.go`.
5. **The dedup contract** — `sensor_events.UNIQUE(repo_path, sensor_name,
   external_id)`. A re-observed external_id silently fails the INSERT
   and the scheduler skips it. This is why ExternalID must be stable
   across polls (numeric IDs are fine; timestamps at poll-time are
   anti-patterns).
6. **The scheduler** — `internal/sensors/scheduler.go`. One scheduler
   per (repo, sensor) tuple. Poll → INSERT events with dedup → for each
   new event, render seed nebula → fire triggers. The scheduler does
   not enqueue trigger_queue rows directly today — see "Status" below.
7. **The GitHub sensor** — `internal/sensors/github/`. Walk through
   `github.go`: Configure (token resolution: `token_file > token_env >
   gh-fallback`), Poll (the `gh issue list --json ...` shell-out, label
   + assignee filtering, cursor advancement on `last_issue_number`),
   SeedNebula (issue body extraction: first heading section becomes
   goals, "Constraints"/"Requirements"/"Acceptance Criteria" sections
   become constraints). Reference the testdata fixtures used by the
   tests.
8. **Status — what's wired today** — be honest:
   - GitHub sensor exists; Linear / Slack / cron sensors are specced in
     `.nebulas/additional-sensors/` but not yet shipped.
   - The scheduler exists but is not yet started by the fleet view; the
     supervisor consumes `trigger_queue` but does not yet drive the
     scheduler. Document where this wiring should land.
   - Sensor TOML files are loaded by `quasar lint` but not yet by the
     fleet view's auto-discovery.
9. **The token-resolution chain** — file > env > vendor fallback.
   Inline `token:` in TOML is a config error. Cover the SSM resolver
   surface (specced in deployment-cookbook; not yet shipped).
10. **Per-repo sensor TOML format** — `<repo>/sensors/<name>.toml`. Show
    the GitHub sensor TOML as a worked example. Link to docs/sensor-authoring.md
    for the full surface.

### `docs/sensor-authoring.md` — new (the how-to)

#### TOC

1. **The minimum interface** — copy the Sensor interface from
   `internal/sensors/sensors.go` and walk through what each method must
   do.
2. **Designing a cursor** — three patterns with shipped examples:
   - **Numeric counter** (github_issues: `last_issue_number`)
   - **Timestamp** (e.g. linear_issues spec: `last_updated_at`)
   - **Opaque API cursor** (e.g. slack spec: `oldest_ts`)
3. **Designing an ExternalID** — must be stable + unique within the
   sensor's namespace + scoped naturally (the table's
   UNIQUE constraint enforces this). Anti-patterns: array indices,
   poll-time timestamps.
4. **Using the test kit** — `internal/sensors/testing/PollHarness` if
   it exists (verify before claiming); otherwise document the GitHub
   sensor's `fakeRunGH` pattern as the template.
5. **Secrets** — file > env precedence. Show how to validate at
   Configure time. Cover the `SecretResolver` interface.
6. **Backpressure and rate limits** — Poll should cap events per call
   (typical 50). On rate-limit, return `(nil, cursor, &RateLimitError)`
   and the scheduler sleeps before the next tick.
7. **Registration** — `func init() { sensors.Default().RegisterSensor(
   "my_sensor_type", func() sensors.Sensor { return New() }) }`. The
   type name is what the user's TOML `type = "..."` matches.
8. **Worked example — a 60-line GitHub Discussions sensor** — inline
   complete implementation, annotated. Verify it compiles before
   committing.
9. **Cross-link to docs/sensors.md** for the conceptual model.

### `docs/integrations.md` — retire

Replace the file with a single redirect:

```markdown
# Integrations (retired)

The `internal/integrations/` package and the `TicketSource` interface were
renamed to `internal/sensors/` and `Sensor` in the
[rename-integrations-to-sensors](../.nebulas/constellation-runtime/01-rename-integrations-to-sensors.md)
phase of the constellation-runtime nebula. This document is preserved as a
redirect; please read:

- **[Sensor model](sensors.md)** — what a sensor is and how it fits into Quasar
- **[Authoring a sensor](sensor-authoring.md)** — how to add a new one
```

Don't delete the file outright — operators may have stale bookmarks. The
redirect keeps those bookmarks pointing at the right thing.

## Files

- `docs/sensors.md` (new) — the conceptual sensor doc + GitHub sensor walkthrough
- `docs/sensor-authoring.md` (new) — the how-to-add-your-own guide
- `docs/integrations.md` (rewrite as redirect) — preserve as breadcrumb

## Acceptance Criteria

- [ ] `docs/sensors.md` lists all four methods of the Sensor interface
  with file:line citations
- [ ] `docs/sensors.md` walks through the GitHub sensor's Configure,
  Poll, and SeedNebula with file:line for each
- [ ] `docs/sensors.md` is honest about which sensors are shipped vs
  specced (cross-link to `.nebulas/additional-sensors/`)
- [ ] `docs/sensors.md` is honest about the scheduler wiring gap (the
  scheduler exists but the fleet view doesn't start it yet)
- [ ] `docs/sensor-authoring.md` includes a worked example that
  compiles when extracted to a standalone file
- [ ] `docs/integrations.md` is the one-line redirect described above
- [ ] No doc claims a sensor exists that hasn't shipped (Linear, Slack,
  cron)
- [ ] `bash scripts/lint.sh` exits 0
