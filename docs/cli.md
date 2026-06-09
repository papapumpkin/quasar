# CLI Reference

Every Quasar subcommand, its flags, what it does, and where it lives in the
code. Quasar is a Cobra application; **each file in `cmd/` defines one command**,
so the "Implementation" pointer at the end of each section is a one-hop jump from
this reference into the source.

Conventions used here:

- Human-readable output goes to **stderr**; **stdout** is reserved for
  structured/machine-readable data (e.g. `version`, `--json` flags). See
  [CLAUDE.md](../CLAUDE.md).
- Config precedence is CLI flags > `QUASAR_*` env > `.quasar.yaml` > built-in
  defaults.
- `<arg>` is required; `[arg]` is optional.

## Command → feature doc

For the *concept* behind a command, follow these into the feature docs:

| Command | Feature doc |
|---|---|
| `run` | [runtime.md](runtime.md) |
| `nebula *` | [constellations.md](constellations.md), [CLAUDE.md](../CLAUDE.md) (authoring) |
| `repo *`, `fleet` | [runtime.md](runtime.md) (the supervisor); `docs/multi-repo.md` (planned) |
| `cockpit` | [runtime.md](runtime.md) |
| `gc *`, `nebula undelete` | [gc.md](gc.md) |
| `cache report` | [runtime.md](runtime.md) (prompt-cache telemetry) |
| `coordination report`, `conflicts report` | [safety.md](safety.md#audit-trail) |
| `coder report` | [runtime.md](runtime.md) (dead-coder healthcheck) |
| `fabric *`, `pulse *`, `discovery` | [fabric.md](fabric.md) |
| `sensor poll` | [integrations.md](integrations.md) (superseded), `docs/sensors.md` (planned) |
| `doctor`, `init`, `lint` | [per-repo-config.md](per-repo-config.md) |

## `quasar` (root)

```
quasar
```

With no subcommand, the root auto-launches the interactive TUI when the current
directory contains a `.nebulas/` directory; otherwise it prints help
(`runRootDefault`, `cmd/root.go:81`).

**Persistent flags** (apply to every subcommand):

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--config` | string | `.quasar.yaml` | config file path |
| `--verbose`, `-v` | bool | false | verbose stderr output |

**Implementation:** `cmd/root.go`.

## Setup & diagnostics

### `quasar init`

```
quasar init [--force] [--yes]
```

Scaffolds a `.quasar.yaml` in the current directory, auto-detecting language and
the GitHub remote.

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--force` | bool | false | overwrite an existing `.quasar.yaml` |
| `--yes` | bool | false | skip the overwrite confirmation (requires `--force`) |

**Implementation:** `cmd/init.go` (`runInit`, `cmd/init.go:39`).

### `quasar doctor`

```
quasar doctor [--json]
```

Diagnoses config, integrations, credentials, git, and pre-commit. Exits non-zero
when any check fails, so it is usable in CI/pre-flight scripts.

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--json` | bool | false | emit the report as JSON on stdout |

**Implementation:** `cmd/doctor.go` (`runDoctor`, `cmd/doctor.go:96`).

### `quasar validate` (deprecated)

```
quasar validate [--json]
```

Deprecated alias for `quasar doctor`; prints a deprecation notice and runs the
same `runDoctor` (`cmd/validate.go:16`).

**Implementation:** `cmd/validate.go`.

### `quasar version`

```
quasar version
```

Prints the version string to **stdout** (the one command whose primary output is
machine-readable). No flags.

**Implementation:** `cmd/version.go`.

### `quasar lint`

```
quasar lint [--repo <path>] [--strict] [--json]
```

Validates a repo's artifact files (constellations, stars, skills, sensors).
Exits non-zero when issues are found.

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--repo` | string | cwd | repo path to lint |
| `--strict` | bool | false | treat unknown fields and warnings as errors |
| `--json` | bool | false | emit findings as JSON on stdout |

**Implementation:** `cmd/lint.go` (`runLint`, `cmd/lint.go:32`).

## Repositories (the fleet)

### `quasar repo`

Command group for the repositories Quasar operates on; the registry lives in the
shared fabric DB. The group carries a `--db` persistent flag (default
`.quasar/fabric.db`).

| Subcommand | Synopsis | Notes | Run function |
|---|---|---|---|
| `register <path>` | Register a git repository | `--name` display name (defaults to dir name) | `cmd/repo.go:115` |
| `unregister <path>` | Soft-delete a registered repo | `--force` orphans in-flight nebulas | `cmd/repo.go:144` |
| `list` | List registered repos | `--status active\|paused\|removed`, `--json` | `cmd/repo.go:162` |
| `pause <path>` | Pause a repo (sensors stop; in-flight work continues) | — | `cmd/repo.go:196` |
| `resume <path>` | Resume a paused repo | — | `cmd/repo.go:200` |
| `show <path>` | Show a repo's details and summary | — | `cmd/repo.go:223` |

**Implementation:** `cmd/repo.go`.

### `quasar fleet` (alias `tui`)

```
quasar fleet [--db <path>]
```

Launches the multi-repo fleet dashboard — the operator view over every
registered repo, backed by the supervisor that polls sensors and advances
constellation runs. Requires a TTY. The supervisor routes its diagnostics to
`.quasar/supervisor.log` (`cmd/fleet.go:176`) so they never corrupt the TUI.

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--db` | string | `.quasar/fabric.db` | fabric database path |

**Implementation:** `cmd/fleet.go` (`runFleet`, `cmd/fleet.go:59`).

### `quasar cockpit` (legacy)

```
quasar cockpit [--dir <path>] [--no-splash] [--max-workers <n>]
```

The legacy single-repo cockpit browser, superseded by `fleet` for multi-repo
operation. Requires a TTY.

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--dir` | string | cwd | directory to scan for `.nebulas/` |
| `--no-splash` | bool | false | skip the startup splash animation |
| `--max-workers` | int | 1 | maximum concurrent workers |

**Implementation:** `cmd/tui.go` (`runTUI`, `cmd/tui.go:45`).

## Nebulas

### `quasar nebula`

Command group for nebula blueprints. See [CLAUDE.md](../CLAUDE.md) for the
manifest/phase-file format and [constellations.md](constellations.md) for what
runs underneath.

| Subcommand | Synopsis | Key flags | Run function |
|---|---|---|---|
| `validate <path>` | Validate a nebula's structure and dependencies | — | `cmd/nebula_validate.go:12` |
| `plan <path>` | Preview the execution plan | `--json`, `--save`, `--diff`, `--no-color` | `cmd/nebula_plan.go:25` |
| `apply <path>` | Import into SQLite and execute its phases | `--auto`, `--watch`, `--max-workers`, `--no-tui`, `--no-splash`, `--max-context-tokens`, `--resume` | `cmd/nebula_apply.go:43` |
| `show <path>` | Display current nebula state | — | `cmd/nebula_show.go:10` |
| `status <path>` | Metrics summary for a nebula run | `--json` | `cmd/nebula_status.go:20` |
| `generate <prompt>` | Generate a nebula from a natural-language description | `--name`, `--output`, `--model`, `--budget`, `--force`, `--dry-run` | `cmd/nebula_generate.go:41` |
| `import <path>` | Import a blueprint without executing it | `--approve` (mark approved so the supervisor fires it) | `cmd/nebula_import.go:106` |
| `undelete <id>` | Restore a GC soft-deleted nebula within its grace window | — | `cmd/nebula_undelete.go:18` |

`apply` is the workhorse: `--auto` starts workers for ready phases; `--resume`
continues from checkpoints. `undelete` is the recovery path documented in
[gc.md](gc.md#cli-surface).

**Implementation:** `cmd/nebula.go` (group), plus `cmd/nebula_*.go` per
subcommand as cited above.

### `quasar run`

```
quasar run [flags]
```

Starts the interactive single-repo coder-reviewer REPL (the legacy in-process
loop; see [runtime.md](runtime.md)). `--auto` runs one task from stdin and exits.

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--max-cycles` | int | 0 | override max review cycles |
| `--max-budget` | float | 0 | override max budget (USD) |
| `--budget-usd` | float | 0 | override run budget cap (takes precedence over `--max-budget`) |
| `--coder-prompt-file` | string | "" | custom coder system prompt file |
| `--reviewer-prompt-file` | string | "" | custom reviewer system prompt file |
| `--auto` | bool | false | run one task from stdin and exit (non-interactive) |
| `--no-tui` | bool | false | disable TUI even on a TTY |
| `--no-splash` | bool | false | skip the startup splash |
| `--cache-optimization` | bool | true | stable system-prompt prefix for prompt caching |
| `--cache-verbose` | bool | false | log cache hit/miss to stderr |
| `--project-context-path` | string | "" | static project-context file (overrides scanner) |
| `--max-context-tokens` | int | 0 | context-injection token budget (0 = default 10000) |

**Implementation:** `cmd/run.go` (`runRun`, `cmd/run.go:54`).

## Garbage collection

### `quasar gc`

Group that drives the garbage collector as one-shot operations. Full behavior in
[gc.md](gc.md).

| Subcommand | Synopsis | Flags | Run function |
|---|---|---|---|
| `run` | One GC pass: row categories, then blobs + worktrees | `--dry-run`, `--category <name>` | `cmd/gc.go:118` |
| `blobs` | The blob mark-and-sweep only | `--dry-run` | `cmd/gc.go:140` |
| `audit` | Tail the JSONL audit log and summarize `gc_runs` | `--since <dur>` (default 24h) | `cmd/gc.go:156` |

**Implementation:** `cmd/gc.go`.

## Telemetry & observability reports

Each of these summarizes one of the JSONL telemetry logs catalogued in
[safety.md](safety.md#audit-trail). All write their tables to stderr.

### `quasar cache report`

```
quasar cache report --nebula <id> [--router]
```

Reports prompt-cache hit rates for a nebula; `--router` reports model-routing
token savings instead.

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--nebula` | string | required | nebula ID to report on |
| `--router` | bool | false | report routing savings instead of cache hit rates |

**Implementation:** `cmd/cache.go` (`runCacheReport`, `cmd/cache.go:45`).

### `quasar coordination report`

```
quasar coordination report [--since <dur>]
```

Reports cross-phase coordination-note volume and override usage. `--since`
default 24h.

**Implementation:** `cmd/coordination.go` (`runCoordinationReport`,
`cmd/coordination.go:40`).

### `quasar conflicts report`

```
quasar conflicts report [--since <dur>]
```

Reports cross-phase merge-conflict resolution rate, cost, and latency. `--since`
default 168h.

**Implementation:** `cmd/conflicts.go` (`runConflictsReport`,
`cmd/conflicts.go:40`).

### `quasar coder report`

```
quasar coder report [--since <dur>]
```

Reports dead-coder termination causes over a window. `--since` default 24h.

**Implementation:** `cmd/coder.go` (`runCoderReport`, `cmd/coder.go:37`).

### `quasar telemetry`

```
quasar telemetry [--epoch <id>] [--follow]
```

Views the JSONL telemetry event stream for a nebula epoch (most recent by
default); `--follow`/`-f` tails it like `tail -f`.

**Implementation:** `cmd/telemetry.go` (`runTelemetry`, `cmd/telemetry.go:38`).

## Fabric coordination

The `fabric` group and its siblings drive the coordination substrate
([fabric.md](fabric.md)) directly. These are primarily used by agents and for
operator inspection.

### `quasar fabric`

Group with `--db` and `--task` persistent flags. Subcommands:

| Subcommand | Synopsis | Implementation |
|---|---|---|
| `post` | Post a coordination message | `cmd/fabric_post.go:22` |
| `read` | Read coordination state | `cmd/fabric_read.go:15` |
| `diff` | Diff fabric state | `cmd/fabric_diff.go:15` |
| `claim` / `release` | Claim or release a symbol/file | `cmd/fabric_claim.go:14`, `cmd/fabric_claim.go:25` |
| `entanglements` | Inspect cross-phase entanglements | `cmd/fabric_entanglements.go:15` |
| `archive` | Snapshot and purge fabric state for an epoch (`--epoch`, `--output`, `--force`) | `cmd/fabric_archive.go:62` |
| `purge` | Discard all fabric state for an abandoned epoch (`--force`) | `cmd/fabric_archive.go:94` |

`quasar archive` is also registered as a top-level alias of `fabric archive`
(`cmd/fabric_archive.go:15`).

**Implementation:** `cmd/fabric.go` (group) and `cmd/fabric_*.go`.

### `quasar pulse`

```
quasar pulse emit [content] --kind <kind>
quasar pulse list [--task <id>]
```

Manages shared execution-context "pulses" in the fabric. `emit` writes a pulse
(`--kind note|decision|failure|reviewer_feedback`) and prints its ID to stdout;
`list` lists pulses, optionally filtered by `--task`.

**Implementation:** `cmd/pulse.go` (`runPulseEmit`, `cmd/pulse.go:59`;
`runPulseList`, `cmd/pulse.go:97`).

### `quasar discovery`

```
quasar discovery --kind <kind> --detail <text> [--affects <task>]
```

Posts an agent discovery (e.g. `missing_dependency`, `file_conflict`,
`budget_alert`) to the fabric and prints its ID to stdout.

**Implementation:** `cmd/discovery.go` (`runDiscovery`, `cmd/discovery.go:34`).

## Hidden / debug commands

These are not part of the operator surface; they exist for debugging or as
internal subprocess hooks (`Hidden: true`).

### `quasar sensor poll` (hidden)

```
quasar sensor poll <repo-path> <sensor-name>
```

Forces a single poll cycle for one sensor, bypassing the supervisor's
`poll_interval`. Useful when testing a sensor adapter.

**Implementation:** `cmd/sensor.go` (`runSensorPoll`, `cmd/sensor.go:48`).

### `quasar __budget-hook` (hidden, internal)

A Claude CLI `PreToolUse` hook that enforces per-invocation read/grep/edit caps.
Operators never invoke it directly; the runtime wires it into agent subprocesses.

**Implementation:** `cmd/budgethook.go`.

---

For the concepts behind these commands, start at [architecture.md](architecture.md)
and follow the [Command → feature doc](#command--feature-doc) table above.
