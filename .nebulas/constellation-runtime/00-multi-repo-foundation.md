+++
id = "multi-repo-foundation"
title = "Multi-repo registry: SQLite-backed repos table, registration CLI, per-repo config layering"
type = "task"
priority = 1
scope = [
    "internal/repos/repos.go",
    "internal/repos/repos_test.go",
    "internal/repos/registry.go",
    "internal/repos/registry_test.go",
    "internal/repos/resolver.go",
    "internal/repos/resolver_test.go",
    "internal/config/config.go",
    "internal/config/config_test.go",
    "internal/fabric/sqlite.go",
    "internal/fabric/migrations/**",
    "cmd/repo.go",
    "cmd/repo_test.go",
    "cmd/root.go",
]
+++

## Problem

Quasar today assumes a single repo: `quasar nebula apply <path>` operates on whatever's in the current working directory. The deployment model that's actually wanted is a long-running service on a host (EC2 or otherwise) that manages many git repos simultaneously — each with its own `.quasar.yaml`, its own sensors polling for tickets, its own pre-commit rules. Nothing in the current code supports "a registered set of repos" as a first-class concept, so multi-repo support has to be built from the ground up before any of the other constellation/sensor work makes sense.

The foundation has three parts: a SQLite-backed registry of repos Quasar is willing to operate on, a CLI surface for explicit registration (no auto-discovery — opt-in is the right model), and a config resolver that finds the right per-repo config file given a repo path.

This phase ships zero new functionality; everything is plumbing. Subsequent phases depend on it.

## Solution

### Package layout

Create `internal/repos/` with three files:
- `repos.go` — `Repo` DTO, status constants, error types
- `registry.go` — read/write registered repos in SQLite (CRUD against the `repos` table)
- `resolver.go` — given a repo path, load its `.quasar.yaml`, resolve per-repo override files for stars/constellations/skills/sensors

### `Repo` DTO

```go
// Repo is a registered git repository that Quasar may operate on.
type Repo struct {
    Path       string    // absolute filesystem path (PRIMARY KEY)
    Name       string    // display name; defaults to filepath.Base(Path)
    Status     string    // "active" | "paused" | "removed"
    AddedAt    time.Time
    UpdatedAt  time.Time
    LastSeenAt time.Time // updated on every supervisor startup that touched it
}
```

### SQLite schema

New migration: `internal/fabric/migrations/NNN_repos.sql`

```sql
CREATE TABLE repos (
  path         TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'active',
  added_at     INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL
);

CREATE INDEX repos_status ON repos (status);
```

Wire the migration through `internal/fabric/sqlite.go`'s migration runner.

### Registry API

```go
// Registry manages the set of repos Quasar is willing to operate on.
type Registry struct {
    db *sql.DB
}

func New(db *sql.DB) *Registry

// Register adds a repo by absolute path. Validates the path exists, is a
// directory, contains a .git subdirectory, and is readable. Returns
// ErrRepoAlreadyRegistered if already registered. The repo's name defaults
// to filepath.Base(path) if not explicitly set.
func (r *Registry) Register(ctx context.Context, path string, name string) (*Repo, error)

// Unregister removes a repo. Returns ErrRepoActiveNebulas if there are any
// nebulas in non-terminal status for this repo (unless force is true). When
// force is true, in-flight nebulas remain in SQLite but are flagged as
// orphaned — they will surface in the TUI as "needs human attention" until
// the user resolves them manually.
func (r *Registry) Unregister(ctx context.Context, path string, force bool) error

// List returns all registered repos, optionally filtered by status.
func (r *Registry) List(ctx context.Context, statusFilter string) ([]*Repo, error)

// Get returns a single repo by path; ErrRepoNotRegistered if not found.
func (r *Registry) Get(ctx context.Context, path string) (*Repo, error)

// SetStatus updates a repo's status field. Used for pause/resume.
func (r *Registry) SetStatus(ctx context.Context, path string, status string) error

// Touch updates last_seen_at to now. Called by the supervisor on startup
// for every repo it boots a scheduler for.
func (r *Registry) Touch(ctx context.Context, path string) error
```

Typed errors: `ErrRepoNotRegistered`, `ErrRepoAlreadyRegistered`, `ErrRepoActiveNebulas`, `ErrRepoPathInvalid`.

### Resolver API

```go
// Resolver resolves per-repo configuration. Given a registered repo's path,
// it loads the repo's .quasar.yaml and provides layered lookup for
// constellation/star/skill/sensor files.
type Resolver struct {
    repo Repo
    cfg  RepoConfig
}

func NewResolver(repo *Repo) (*Resolver, error)

// Config returns the parsed .quasar.yaml for this repo.
func (r *Resolver) Config() RepoConfig

// ConstellationPath returns the filesystem path to the constellation TOML
// for the given name. Returns the per-repo override if it exists at
// <repo>/constellations/<name>.toml; otherwise returns the sentinel
// EmbeddedPath value to signal the loader should use the //go:embed default.
func (r *Resolver) ConstellationPath(name string) string

// StarPath, SkillPath, SensorPath mirror ConstellationPath.
// SensorPath has no embedded fallback — returns ErrSensorNotConfigured
// if no per-repo file exists.
func (r *Resolver) StarPath(name string) string
func (r *Resolver) SkillPath(name string) string
func (r *Resolver) SensorPath(name string) (string, error)

// AllSensorPaths returns absolute paths to every <repo>/sensors/*.toml
// file. Used by the supervisor to spin up one scheduler per sensor instance
// at startup.
func (r *Resolver) AllSensorPaths() ([]string, error)

// EmbeddedPath is the sentinel returned when the per-repo override does not
// exist. Callers pass this to the file loader (Phase 2) which knows to
// resolve against the embedded FS instead.
const EmbeddedPath = ":embedded:"
```

### `RepoConfig` shape (extension of existing Config)

Extend `internal/config/config.go` so that the existing `Config` struct can be loaded from a per-repo `.quasar.yaml`. The existing single-file load path stays (used for tests and for backward-compat with the previous single-repo model); the resolver uses a new `LoadFromPath(absolutePath)` entry point.

Per-repo `.quasar.yaml` shape:

```yaml
[github]
base_branch: "main"

[pre_commit]
commands = ["gofmt -w .", "go vet ./...", "go test ./...", "go build ./..."]
fail_on_error = true

[verify]
test  = "go test ./..."
lint  = "go vet ./..."
build = "go build ./..."

# Sensors are NOT declared here — each sensor instance is its own file in
# <repo>/sensors/*.toml. This block is reserved for top-level repo config
# only.
```

Inline-token guardrail from ticket-ingest's Phase 1 stays in force: any TOML/YAML key named `token` (case-insensitive) at load time produces a `ConfigError` with a message pointing at `token_env`/`token_file`.

### CLI commands

New file `cmd/repo.go`:

```
quasar repo register <path> [--name <name>]
quasar repo unregister <path> [--force]
quasar repo list [--status active|paused|removed] [--json]
quasar repo pause <path>
quasar repo resume <path>
quasar repo show <path>
```

`register`:
1. Validate path: must exist, be a directory, contain a `.git/` subdirectory, be readable
2. Resolve to absolute path
3. Insert into `repos` table with status `active`
4. Print confirmation: `registered: <path> (name: <name>)`
5. Errors map to non-zero exit codes (1 for usage, 2 for already-registered, 3 for path invalid)

`unregister`:
1. Check for nebulas in non-terminal status for this repo
2. Without `--force`: error and list the active nebulas
3. With `--force`: update repos.status to `removed` and mark active nebulas as `orphaned`
4. The row is soft-deleted (status flip); a future GC pass purges it
5. Print confirmation

`list`:
1. Query repos table
2. Print one row per repo: `<status>  <path>  <name>  added <relative-time>`
3. With `--json`: structured output for CI / scripting

`pause`/`resume`:
1. Set status to `paused` / `active`
2. Paused repos: sensors stop polling, no new nebulas, in-flight nebulas continue

`show`:
1. Print full Repo row plus a summary: active nebula count, last sensor poll time per sensor

### Wiring

`cmd/root.go`: add `repoCmd` to the root command. Subcommands register via `init()`.

The supervisor loop (which doesn't exist yet — comes in Phase 5) will:
1. Call `Registry.List(ctx, "active")` at startup
2. For each repo, call `Registry.Touch()`
3. For each repo, call `NewResolver(repo)` and walk its sensors

This phase does NOT build the supervisor. It only ships the registry, the resolver, and the CLI.

## Files

- `internal/repos/repos.go` (new) — Repo DTO, error types, status constants
- `internal/repos/repos_test.go` (new) — DTO + error assertions
- `internal/repos/registry.go` (new) — Registry struct with Register/Unregister/List/Get/SetStatus/Touch
- `internal/repos/registry_test.go` (new) — SQLite-backed tests with t.TempDir() for the db
- `internal/repos/resolver.go` (new) — Resolver struct with per-repo path resolution
- `internal/repos/resolver_test.go` (new) — table-driven tests covering override-exists and embedded-fallback cases
- `internal/config/config.go` — add LoadFromPath(absolutePath) entry point; existing Load() stays for backward compat
- `internal/config/config_test.go` — LoadFromPath round-trip + inline-token guardrail tests
- `internal/fabric/sqlite.go` — wire NNN_repos.sql migration
- `internal/fabric/migrations/NNN_repos.sql` (new) — repos table + indexes
- `cmd/repo.go` (new) — repoCmd with register/unregister/list/pause/resume/show subcommands
- `cmd/repo_test.go` (new) — cobra command tests with fake registry
- `cmd/root.go` — register repoCmd

## Acceptance Criteria

- [ ] `internal/repos/` package compiles and tests pass
- [ ] `repos` table exists in SQLite after migration with PRIMARY KEY on `path` and indexes on `status`
- [ ] `Registry.Register("/tmp/test-repo", "")` succeeds when /tmp/test-repo is a valid git repo; errors with `ErrRepoPathInvalid` when path doesn't exist or lacks `.git/`
- [ ] `Registry.Register` errors with `ErrRepoAlreadyRegistered` when called twice for the same path
- [ ] `Registry.Unregister(path, false)` errors with `ErrRepoActiveNebulas` when nebulas table has non-terminal rows for that repo
- [ ] `Registry.Unregister(path, true)` flips status to `removed` and marks affected nebulas as `orphaned`
- [ ] `Registry.List(ctx, "active")` returns only active rows
- [ ] `Resolver.ConstellationPath("coder-reviewer")` returns the per-repo override path if `<repo>/constellations/coder-reviewer.toml` exists, otherwise returns `EmbeddedPath`
- [ ] `Resolver.AllSensorPaths()` returns absolute paths for every `<repo>/sensors/*.toml` file
- [ ] `Resolver.SensorPath("nonexistent")` returns `ErrSensorNotConfigured`
- [ ] `Config.LoadFromPath(<repo>/.quasar.yaml)` parses `[pre_commit]`, `[verify]`, `[github]`, etc. and returns the same typed Config struct used today
- [ ] Loading a `.quasar.yaml` containing an inline `token: "ghp_xxx"` returns a `ConfigError` pointing at `token_env`/`token_file`
- [ ] `quasar repo register /path/to/valid-repo --name myrepo` adds the repo and prints confirmation; exit 0
- [ ] `quasar repo list` prints registered repos with status, path, name, and added-at time
- [ ] `quasar repo list --json` outputs valid JSON consumable by scripts
- [ ] `quasar repo pause <path>` flips status to `paused`; `quasar repo resume <path>` flips back to `active`
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
