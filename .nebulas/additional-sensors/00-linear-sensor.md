+++
id = "linear-sensor"
title = "Linear sensor: poll a workspace's project for open issues; produce seed nebulas; dedup by issue key"
type = "task"
priority = 2
scope = [
    "internal/sensors/linear/**",
    "internal/sensors/testing/**",
]
+++

## Problem

Linear is the most-requested second sensor after GitHub issues. The shape mirrors `github_issues` from Phase 4 of constellation-runtime: poll a workspace's project for issues, render each new one into a seed nebula, persist cursor, dedup by external ID. What differs is the API (GraphQL not REST) and the auth (Linear API key in a header, no `gh`-style CLI fallback).

This phase also extracts the sensor-test-helper kit out of `internal/sensors/github/` so the linear sensor (and future ones) reuse it.

## Solution

### Shared test kit

`internal/sensors/testing/` lifts the test fixtures + fake-HTTP-client pattern from the GitHub sensor:

```go
// FakeTransport returns an http.RoundTripper that serves pre-baked responses
// from a slice of (request-matcher, response) pairs. Used by every sensor
// test to avoid hitting real APIs.
func FakeTransport(routes []Route) http.RoundTripper

// PollHarness runs a sensor through one or more cursor advances using
// a fixture set of events; asserts cursor progression and seed-nebula
// content via golden files.
type PollHarness struct {
    Sensor sensors.Sensor
    Steps  []Step
}

func (h *PollHarness) Run(t *testing.T)
```

The GitHub sensor's existing test file is migrated to use `PollHarness` so the pattern is real before linear adopts it.

### Linear sensor

`internal/sensors/linear/linear.go`:

```go
type Sensor struct {
    name          string  // "linear_issues"
    workspaceID   string  // required
    projectID     string  // required
    apiKey        string  // resolved at Configure
    teamFilter    []string
    stateFilter   []string  // e.g. ["Todo", "In Progress"]
    httpClient    *http.Client
}

func New() *Sensor

func (s *Sensor) Name() string { return "linear_issues" }

func (s *Sensor) Configure(raw map[string]any, secrets sensors.SecretResolver) error {
    // Required: workspace_id, project_id
    // Optional: team_filter, state_filter
    // API key resolution: api_key_file > api_key_env (no CLI fallback)
}

func (s *Sensor) Poll(ctx context.Context, cursor json.RawMessage) (events []sensors.Event, newCursor json.RawMessage, err error) {
    // cursor format: { "last_updated_at": "2026-06-04T22:00:00Z" }
    // 1. GraphQL query: issues in project, updated after cursor.last_updated_at, paginated
    // 2. Filter by team/state if configured
    // 3. Return events; newCursor = max(updatedAt of seen issues)
}

func (s *Sensor) SeedNebula(event sensors.Event) (*sensors.SeedNebulaContent, error) {
    // - Name: nebula-<issue-key-slugified>
    // - SourceName: "linear"
    // - SourceID: "<workspaceID>:<issueKey>"  (e.g. "ws_abc:ENG-123")
    // - SourceURL: issue web URL
    // - Goals: derived from issue description markdown (first ## section or first paragraph)
    // - Labels: pass through Linear labels
    // - Assignee: primary assignee email if any
}

func init() {
    sensors.Default().RegisterSensor("linear_issues", func() sensors.Sensor { return New() })
}
```

### Cursor migration safety

Linear issues' `updatedAt` is the cursor (not numeric ID — GraphQL doesn't expose dense monotonic IDs). This means:
- If the same issue is re-edited, it re-appears in the poll
- The `sensor_events.UNIQUE(repo_path, sensor_name, external_id)` constraint dedupes — the insert silently fails on the second sighting and the scheduler skips it

`external_id` for linear = `<workspaceID>:<issueKey>` (stable across re-edits).

A re-edit will still cause one DB lookup per poll cycle (the UNIQUE conflict), which is fine — Linear projects rarely have >1000 active issues.

### Per-repo sensor TOML

```toml
name = "linear-eng-queue"
type = "linear_issues"
poll_interval = "10m"
max_inflight = 4

[config]
workspace_id = "ws_acme"
project_id   = "proj_engineering"
api_key_env  = "LINEAR_API_KEY"
team_filter  = ["ENG", "INFRA"]
state_filter = ["Todo"]

[[triggers]]
constellation = "architect"
when = "new_item"
```

### Tests

- Configure: required field validation, secret resolution precedence, default state_filter handling
- Poll: cursor advances correctly across paginated GraphQL responses (using `FakeTransport`)
- Poll: re-edited issue surfaces again but is deduped at the scheduler layer (integration with a fake `SensorEventStore`)
- SeedNebula: golden file comparison for a fixture Linear issue → expected SeedNebulaContent

## Files

- `internal/sensors/testing/harness.go` (new) — shared test helpers
- `internal/sensors/testing/fake_transport.go` (new)
- `internal/sensors/github/github_test.go` (modify) — migrate to PollHarness pattern
- `internal/sensors/linear/linear.go` (new)
- `internal/sensors/linear/graphql.go` (new) — minimal GraphQL request/response types
- `internal/sensors/linear/seed_nebula.go` (new) — body → SeedNebulaContent renderer
- `internal/sensors/linear/linear_test.go` (new)
- `internal/sensors/linear/testdata/*.json` (new) — fixture GraphQL responses

## Acceptance Criteria

- [ ] `linear.Sensor` implements `sensors.Sensor`
- [ ] `Configure` requires `workspace_id` + `project_id`; errors with field path otherwise
- [ ] API key resolution: api_key_file > api_key_env; missing key → Configure error
- [ ] `Poll` issues a paginated GraphQL query against Linear's API (mockable via injected HTTP transport)
- [ ] `Poll` returns events for issues with `updatedAt > cursor.last_updated_at` after team/state filters
- [ ] `external_id` is `<workspaceID>:<issueKey>` — stable across edits
- [ ] `SeedNebula` produces a SeedNebulaContent with goals from the issue description's first section
- [ ] `internal/sensors/testing/PollHarness` is consumed by both `github` and `linear` sensor tests
- [ ] No `exec.Command(...)` calls in `internal/sensors/linear/` (Linear uses HTTP only)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
