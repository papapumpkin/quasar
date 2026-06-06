+++
id = "github-sensor-produces-nebula"
title = "GitHub sensor: poll-driven; writes seed nebulas directly to SQLite; triggers architect constellation"
type = "task"
priority = 2
depends_on = ["rename-integrations-to-sensors", "nebula-to-sqlite-migration"]
scope = [
    "internal/sensors/github/github.go",
    "internal/sensors/github/github_test.go",
    "internal/sensors/scheduler.go",
    "internal/sensors/scheduler_test.go",
    "internal/fabric/sensor_store.go",
    "internal/fabric/sensor_store_test.go",
    "internal/fabric/migrations/**",
]
+++

## Problem

With the rename done (Phase 1) and SQLite as canonical state (Phase 3), the GitHub sensor needs to actually do its job in the new model: poll for new issues on its configured repo, render each new issue into a seed nebula, write it to SQLite, and emit a trigger event that the constellation runtime (Phase 5) will pick up to fire the architect.

The sensor itself stays close to what ticket-ingest shipped — same `gh` shell-out, same JSON parsing, same auth chain. What changes is the integration point: instead of being called on-demand via `quasar nebula new`, it's driven by a scheduler goroutine that ticks on its configured `poll_interval` and persists its cursor to SQLite for crash safety.

This phase also adds the sensor scheduler (the loop that drives polling) and the sensor cursor / event tables that make the whole thing durable.

## Solution

### SQLite additions

New migration `internal/fabric/migrations/NNN_sensor_state.sql`:

```sql
CREATE TABLE sensor_cursors (
  repo_path     TEXT NOT NULL REFERENCES repos(path) ON DELETE CASCADE,
  sensor_name   TEXT NOT NULL,
  cursor        BLOB NOT NULL,           -- JSON-encoded sensor-specific cursor
  updated_at    INTEGER NOT NULL,
  PRIMARY KEY (repo_path, sensor_name)
);

CREATE TABLE sensor_events (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_path       TEXT NOT NULL REFERENCES repos(path) ON DELETE CASCADE,
  sensor_name     TEXT NOT NULL,
  external_id     TEXT NOT NULL,
  received_at     INTEGER NOT NULL,
  processed_at    INTEGER,                -- NULL until processed; non-NULL after seed nebula written
  nebula_id       TEXT REFERENCES nebulas(id) ON DELETE SET NULL,
  UNIQUE (repo_path, sensor_name, external_id)
);

CREATE INDEX sensor_events_unprocessed ON sensor_events (repo_path, sensor_name, processed_at) WHERE processed_at IS NULL;
```

The UNIQUE constraint on `(repo_path, sensor_name, external_id)` is the deduplication mechanism. If the sensor's `Poll` returns the same event ID twice (e.g. on restart after a crash mid-processing), the INSERT silently fails and the runtime skips it.

### GitHub sensor implementation

`internal/sensors/github/github.go`:

```go
type Sensor struct {
    name       string  // typically "github_issues"
    repo       string  // "owner/repo" — required
    token      string  // resolved at Configure time
    labelFilter []string
    assigneeFilter string

    runGH      func(ctx context.Context, args ...string) ([]byte, error)
}

func New() *Sensor

func (s *Sensor) Name() string { return "github_issues" }

func (s *Sensor) Configure(raw map[string]any, secrets sensors.SecretResolver) error {
    // Validate required: repo. Optional: token_env, token_file, labels, assignee.
    // Token resolution: token_file > token_env > gh auth fallback (token stays empty).
    // Returns errors with explicit field references.
}

func (s *Sensor) Poll(ctx context.Context, cursor json.RawMessage) (events []sensors.Event, newCursor json.RawMessage, err error) {
    // cursor format: { "last_issue_number": N }
    // 1. gh issue list --repo <repo> --state open --json number,title,body,labels,assignees,url,comments --search "is:issue is:open -label:wip"
    // 2. Filter by labels/assignee if configured
    // 3. Filter to issues with number > cursor.last_issue_number
    // 4. Return one Event per new issue
    // 5. newCursor = max(seen issue numbers)
}

func (s *Sensor) SeedNebula(event sensors.Event) (*sensors.SeedNebulaContent, error) {
    // Render the issue's raw fields into a SeedNebulaContent.
    // - Name: nebula-<num>-<slug-of-title>
    // - Description: brief; references the issue URL
    // - SourceName: "github", SourceID: "<owner>/<repo>#<num>", SourceURL: issue URL
    // - Goals: derived from issue body (first heading-section if present, else first paragraph)
    // - Constraints: derived from issue body sections labeled "Constraints" / "Requirements" / "Acceptance Criteria"
    // - Labels: pass through from the issue
    // - Assignee: primary assignee if any
}

func init() {
    sensors.Default().RegisterSensor("github_issues", func() sensors.Sensor { return New() })
}
```

The token-resolution chain stays unchanged from ticket-ingest. If neither `token_file` nor `token_env` is set, the sensor leaves `token` empty and `gh` falls back to its own auth chain.

### Scheduler

`internal/sensors/scheduler.go` is the runtime that drives polling. One scheduler instance per `(repo_path, sensor_name)` tuple.

```go
type Scheduler struct {
    repoPath   string
    instance   *artifacts.SensorInstance
    sensor     sensors.Sensor
    cursorStore *fabric.SensorCursorStore
    eventStore  *fabric.SensorEventStore
    nebulaStore *fabric.NebulaStore
    logger     io.Writer

    // Triggers
    trigger    func(ctx context.Context, repoPath, nebulaID, constellationName string) error
}

func NewScheduler(opts SchedulerOpts) (*Scheduler, error)

// Run starts the scheduler loop. It blocks until ctx is canceled.
// Each tick:
//   1. Load cursor from SQLite
//   2. Call sensor.Poll(ctx, cursor) — bounded by configurable timeout
//   3. For each event, INSERT into sensor_events (UNIQUE dedup; silently skip on conflict)
//   4. Update sensor_cursors with newCursor
//   5. For each newly-inserted event, call sensor.SeedNebula(event)
//   6. INSERT into nebulas with the seed content (status='awaiting_approval'
//      since these are sensor-generated and need user approval before
//      execution; the architect constellation will run when the user
//      approves)
//   7. Mark sensor_events.processed_at = now, nebula_id = the new nebula id
//   8. For each instance.Triggers matching this event, call s.trigger(...)
//   9. Sleep until next poll_interval tick
func (s *Scheduler) Run(ctx context.Context) error
```

The triggers semantics: in Phase 5 the constellation runtime is what `trigger` actually fires. In this phase, `trigger` is an injectable callback so the scheduler can be tested without the runtime. The default implementation stores the trigger as a queued event in a `trigger_queue` table (added in Phase 5) for the runtime to consume.

### Cursor and event stores

`internal/fabric/sensor_store.go`:

```go
type SensorCursorStore struct{ db *sql.DB }

func (s *SensorCursorStore) Get(ctx context.Context, repoPath, sensorName string) (json.RawMessage, error)
func (s *SensorCursorStore) Set(ctx context.Context, repoPath, sensorName string, cursor json.RawMessage) error

type SensorEventStore struct{ db *sql.DB }

// Insert returns (id, isNew). isNew is false when the (repo, sensor, external_id)
// already exists — the caller skips processing duplicates.
func (s *SensorEventStore) Insert(ctx context.Context, repoPath, sensorName, externalID string, ts time.Time) (id int64, isNew bool, err error)
func (s *SensorEventStore) MarkProcessed(ctx context.Context, id int64, nebulaID string) error
func (s *SensorEventStore) Unprocessed(ctx context.Context, repoPath, sensorName string) ([]SensorEventRow, error)
```

### Backpressure

The scheduler enforces a per-(repo, sensor) cap on in-flight constellation triggers. If `instance.MaxInflight` (default 4) is exceeded, the scheduler queues the trigger but does NOT fire it; once a slot frees the queued triggers fire in FIFO order. This prevents a sudden burst of new issues from spawning N concurrent architect constellations.

### Per-repo sensor TOML format

User-facing config example, written to `<repo>/sensors/github-prod-issues.toml`:

```toml
name = "github-prod-issues"
type = "github_issues"
poll_interval = "5m"
max_inflight = 4

[config]
repo = "papapumpkin/quasar"
token_env = "GITHUB_TOKEN"
labels = ["needs-quasar"]
assignee = "@me"

[[triggers]]
constellation = "architect"
when = "new_item"
```

The supervisor (Phase 5) iterates over registered repos, calls `Resolver.AllSensorPaths()` for each, loads each instance via the file loader from Phase 2, and spawns a Scheduler goroutine per instance.

### Manual smoke test

A `quasar sensor poll <repo-path> <sensor-name>` CLI command (admin-only — undocumented but exists) lets developers force a single poll cycle for debugging. Useful for testing without waiting for the 5-minute tick.

## Files

- `internal/sensors/github/github.go` — Sensor struct with Configure/Poll/SeedNebula; init() registers the type
- `internal/sensors/github/github_test.go` — fake-runGH tests for Poll cursor advancement, SeedNebula rendering, label/assignee filtering
- `internal/sensors/scheduler.go` (new) — Scheduler with Run loop
- `internal/sensors/scheduler_test.go` (new) — table tests with fake sensor + fake stores
- `internal/fabric/sensor_store.go` (new) — SensorCursorStore + SensorEventStore typed APIs
- `internal/fabric/sensor_store_test.go` (new) — uniqueness, mark-processed, unprocessed-listing tests
- `internal/fabric/migrations/NNN_sensor_state.sql` (new) — sensor_cursors + sensor_events tables
- `cmd/sensor.go` (new) — `quasar sensor poll <repo-path> <sensor-name>` admin command
- `cmd/sensor_test.go` (new)

## Acceptance Criteria

- [ ] `internal/sensors/github/Sensor` implements the `sensors.Sensor` interface
- [ ] `sensors.Default().BuildSensor("github_issues", cfg, secrets)` returns a configured sensor
- [ ] `Sensor.Configure` requires `repo` and errors if missing; optionally accepts `token_env`, `token_file`, `labels`, `assignee`
- [ ] Token resolution honors precedence: token_file > token_env > gh-fallback (sensor leaves token empty)
- [ ] `Sensor.Poll(ctx, cursor)` with cursor `{"last_issue_number": 41}` returns events for any issue with number > 41
- [ ] `Sensor.Poll` filters out issues that don't match the configured labels (when set) or assignee (when set)
- [ ] `Sensor.SeedNebula(event)` returns a SeedNebulaContent with `source_id` == `<owner>/<repo>#<number>` and goals extracted from the issue body
- [ ] `Scheduler.Run` ticks at the configured poll_interval, polls the sensor, persists cursor, inserts events with dedup, writes nebulas with status `awaiting_approval`, fires triggers
- [ ] `Scheduler` respects `max_inflight` per (repo, sensor) and queues triggers when at capacity
- [ ] `sensor_cursors` row is updated atomically after each successful poll
- [ ] `sensor_events.UNIQUE(repo_path, sensor_name, external_id)` prevents duplicate processing across restarts
- [ ] After a scheduler tick, the nebulas table contains one row per new issue with `source_name='github'`, `source_url=<issue url>`, `status='awaiting_approval'`
- [ ] `quasar sensor poll <repo-path> <sensor-name>` forces a single poll for debugging and prints results
- [ ] No production code in `internal/sensors/github/` calls `exec.Command("git", ...)` (arch test from gitops phase enforces this)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
