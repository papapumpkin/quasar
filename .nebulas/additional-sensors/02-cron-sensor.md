+++
id = "cron-sensor"
title = "Cron sensor: synthetic schedule-driven events (nightly dependency-bump nebulas, weekly stale-PR sweeps)"
type = "task"
priority = 2
depends_on = ["linear-sensor"]
scope = [
    "internal/sensors/cron/**",
]
+++

## Problem

Not every nebula starts from a ticket. The recurring ones — nightly `go get -u && go mod tidy && open PR`, weekly stale-issue sweeps, monthly security audits — need a sensor that fires on a clock, not an external event. The cron sensor is the simplest possible sensor: its `Poll` returns at most one event per tick, the event's content is the configured template, and its `external_id` is the cron tick timestamp so it dedupes naturally if the scheduler restarts mid-tick.

## Solution

### Sensor

`internal/sensors/cron/cron.go`:

```go
type Sensor struct {
    name     string  // "cron"
    schedule cron.Schedule  // parsed from config
    template SeedTemplate   // the seed nebula content
    clock    func() time.Time  // injectable for tests
}

func (s *Sensor) Poll(ctx context.Context, cursor json.RawMessage) (events []sensors.Event, newCursor json.RawMessage, err error) {
    // cursor: { "last_fire": "2026-06-04T22:00:00Z" }
    // 1. now := s.clock()
    // 2. next := s.schedule.Next(cursor.last_fire)
    // 3. if next <= now: emit one event with external_id = next.Format(RFC3339), advance cursor to next
    //    else: emit nothing, cursor unchanged
}

func (s *Sensor) SeedNebula(event sensors.Event) (*sensors.SeedNebulaContent, error) {
    // Render s.template with event.Timestamp as ${tick_at}
}
```

Cron parsing uses `github.com/robfig/cron/v3` (already commonly used in Go cron tools; vendored). Standard 5-field crontab format plus the `@daily`, `@weekly`, `@monthly` shortcuts.

### Seed template

```toml
name = "nightly-dep-bump"
type = "cron"
poll_interval = "1m"   # how often to wake up and check; NOT the cron schedule itself

[config]
schedule = "0 3 * * *"   # 3am daily, repo-local time

[config.template]
title = "Nightly dependency bump"
goals = [
    "Run `go get -u ./...` and `go mod tidy`",
    "Run the test suite; if all green, open a PR",
    "If any test fails after the bump, leave a failure note on the nebula and do not open a PR",
]
constraints = [
    "Do not bump beyond the minor version of any direct dependency",
    "Do not introduce new direct dependencies",
]
labels = ["automation", "deps"]

[[triggers]]
constellation = "architect"
when = "new_item"
```

The `${tick_at}` placeholder is interpolated into goals/constraints/title at SeedNebula time so the nebula name disambiguates per-day: `nebula-nightly-dep-bump-2026-06-05T03-00-00Z`.

### Time zones

Cron schedules are interpreted in the *runtime's local time*, not UTC, because operators reason about "3am" as a wall-clock concept. The cursor stores UTC for determinism. If the EC2 is set to UTC (default for our deployment cookbook) this distinction is moot.

### Catch-up vs. drop-old

If the supervisor was down for 36 hours and the schedule is `@daily`, the sensor faces a choice: fire all 36 missed ticks, or skip ahead to the next tick? The cron sensor defaults to **skip ahead** (`catchup_mode = "skip"`); operators can opt into `catchup_mode = "all"` per sensor instance for jobs that genuinely need every tick (rare).

### Tests

- Parser: invalid cron string → Configure error with the field name
- Tick advancement: with a fixed clock, fire on schedule boundary, skip between
- Catch-up: with `skip` mode, 36-hour gap fires only the next tick; with `all` mode, fires all missed ticks (the unique constraint dedupes if any were already processed)
- Template render: `${tick_at}` interpolation into title, goals, constraints

## Files

- `internal/sensors/cron/cron.go` (new)
- `internal/sensors/cron/template.go` (new)
- `internal/sensors/cron/cron_test.go` (new)

## Acceptance Criteria

- [ ] `cron.Sensor` implements `sensors.Sensor`
- [ ] `Configure` requires `schedule` (cron string) and `template`
- [ ] Invalid cron string → Configure error citing the field
- [ ] `Poll` emits exactly one event per crossed schedule boundary; cursor advances atomically
- [ ] `external_id` = `tick_at.Format(time.RFC3339)` — stable, sortable, idempotent on restart
- [ ] `catchup_mode = "skip"` skips missed ticks after a downtime gap; `all` fires them all
- [ ] `${tick_at}` placeholder is interpolated in title/goals/constraints
- [ ] Clock is injectable for tests; no `time.Now()` outside the `clock` field
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
