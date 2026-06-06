+++
id = "sensor-authoring-guide"
title = "docs/authoring-a-sensor.md: minimum interface, test kit usage, cursor design, dedup contract"
type = "task"
priority = 3
depends_on = ["linear-sensor", "slack-mention-sensor", "cron-sensor"]
scope = [
    "docs/authoring-a-sensor.md",
]
+++

## Problem

After this nebula lands, we have four reference implementations: github_issues, linear_issues, slack_mention, cron. That's enough surface to extract a reusable authoring guide for the fifth sensor — internal team or external contributor — without them having to read all four packages.

## Solution

Single markdown doc, `docs/authoring-a-sensor.md`. Audience: a Go developer who has never touched the sensor framework.

Sections:

### 1. The `Sensor` interface (the only contract)

```go
type Sensor interface {
    Name() string
    Configure(raw map[string]any, secrets SecretResolver) error
    Poll(ctx context.Context, cursor json.RawMessage) ([]Event, json.RawMessage, error)
    SeedNebula(event Event) (*SeedNebulaContent, error)
}
```

That's it. No registration boilerplate beyond a single `init()`. Walked through line-by-line.

### 2. Designing a cursor

Three patterns, each with a real example from the existing four:

- **Numeric counter** (github_issues `last_issue_number`) — best when the API exposes a dense, monotonic ID
- **Timestamp** (linear_issues `last_updated_at`) — best when the API doesn't have a stable ID but you can filter by `updated_at >`
- **Opaque cursor** (slack `oldest_ts`) — when the API hands you a cursor token to pass back

Anti-pattern: don't compute the cursor as `max(ID)` over a paginated response and then *also* call the next page — you'll double-process or skip. Pick one strategy and stick to it.

### 3. The dedup contract

External IDs MUST be:
- **Stable** — the same event must produce the same ID across polls
- **Unique** — no two distinct events share an ID
- **Scoped naturally** to the workspace (the table's UNIQUE is `(repo_path, sensor_name, external_id)`)

Examples of bad IDs: array index in a paginated response, current timestamp at poll time. Both violate stability.

### 4. Using the test kit

```go
func TestMySensor_Poll(t *testing.T) {
    h := sensortesting.PollHarness{
        Sensor: New(),
        Steps: []sensortesting.Step{
            {
                Cursor:  `{"last_id": 0}`,
                Routes:  []sensortesting.Route{ /* fake HTTP responses */ },
                Expect:  sensortesting.Expect{
                    NewCursor: `{"last_id": 42}`,
                    EventCount: 3,
                    SeedNebulaGolden: "testdata/seed-issue-42.json",
                },
            },
        },
    }
    h.Run(t)
}
```

The harness handles cursor parsing, HTTP client injection, and golden-file assertion. Most tests are < 30 LOC.

### 5. Secrets

Three resolution strategies, in this precedence:
1. `<key>_file` — read from a file path (preferred for production; the path can be a systemd `LoadCredential` mount)
2. `<key>_env` — read from an env var (preferred for development)
3. Vendor CLI fallback — only if the vendor has its own auth (e.g. `gh auth status`); otherwise omit

Never accept inline secrets in TOML. Configure validation must reject `<key>` (without suffix) with a field-path error.

### 6. Backpressure and rate limits

Sensors return events; the scheduler decides what to fire. Inside `Poll`:
- Cap the number of events returned per call (typical: 50). If your API has more, leave them for the next tick.
- If the API returns a rate-limit response, return `(nil, cursor, &RateLimitError{RetryAfter: dur})` and the scheduler will sleep before the next tick.
- Don't retry inside `Poll` — let the scheduler's tick handle it. Internal retries hide failures from the operator.

### 7. Registration

```go
func init() {
    sensors.Default().RegisterSensor("my_sensor_type", func() sensors.Sensor { return New() })
}
```

The `type` field in the user's sensor TOML maps to this string. Convention: lowercase, snake_case, includes the vendor (so two sensors from different vendors don't collide on `issues`).

### 8. Worked example: a 60-line GitHub Discussions sensor

A complete sensor implementation as an inline appendix, with annotations pointing back to the relevant section. Demonstrates: cursor = `last_id`, secret = GH token via gh-fallback, dedup via numeric discussion ID, ~60 LOC total.

### Linking

The guide is linked from:
- `docs/per-repo-config.md` (from constellation-runtime Phase 9) — "to add a custom sensor"
- `CLAUDE.md` — `internal/sensors/` blurb adds "see docs/authoring-a-sensor.md"
- `README.md` — top-level "Extending Quasar" section

## Files

- `docs/authoring-a-sensor.md` (new)
- `docs/per-repo-config.md` (modify) — add cross-link
- `CLAUDE.md` (modify) — add cross-link in sensor blurb
- `README.md` (modify) — Extending Quasar section

## Acceptance Criteria

- [ ] `docs/authoring-a-sensor.md` exists with all 8 sections
- [ ] The worked example compiles when extracted to a standalone file (verified by a small `docs/examples_test.go` extractor)
- [ ] Linkcheck (from constellation-runtime Phase 9) passes on the new doc
- [ ] Cross-links from per-repo-config.md, CLAUDE.md, README.md all resolve
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
