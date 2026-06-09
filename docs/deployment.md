# Deploying Quasar

Audience: an operator standing up Quasar as an always-on service on EC2 (or any
Linux host). Quasar runs as a single persistent supervisor process that manages
N registered repositories from one SQLite database.

For the security boundary the service runs inside, read
[safety.md](safety.md) first. For per-repository setup, see
[per-repo-config.md](per-repo-config.md).

## System requirements

- **Go 1.25+** — to build the binary (`go build -o quasar .`).
- **SQLite** — vendored via the Go driver; no system package required.
- **`git` 2.30+** — the output safety perimeter shells vanilla git.
- **`gh` CLI** — authenticated as the bot user; used by the GitHub sensor/adapter
  for ticket reading.
- **`claude` CLI** — installed and authenticated for the coder/reviewer agents.
- **`zstd`** — optional; blob compression has a vendored fallback, so a system
  `zstd` is not required.

## Directory layout

A conventional single-host install:

```
/opt/quasar/                       # binary + embedded defaults
/var/lib/quasar/state.sqlite       # canonical state (WAL mode)
/var/lib/quasar/blobs/             # content-addressed blob store
/var/lib/quasar/gc-audit.log       # JSONL GC audit log
/var/lib/quasar/runs/<run-id>/     # ephemeral per-run logs
/etc/quasar/quasar.yaml            # global config
/srv/repos/<owner>/<name>/         # checked-out repositories
```

For every key the global `quasar.yaml` and the per-repo `.quasar.yaml` accept,
see [configuration.md](configuration.md); for per-repo authoring see
[per-repo-config.md](per-repo-config.md).

SQLite runs in WAL mode so concurrent sensor schedulers can read while a writer
commits. The blob store is content-addressed (SHA-256, git-style two-char
fanout), so it is safe to back up with a plain file copy.

## Registering a repo

```bash
quasar repo register /srv/repos/papapumpkin/quasar
```

This inserts a row in the `repos` table keyed by the repository's absolute path
and makes the repo visible to the supervisor and the TUI. The per-repo resolver
then expects, relative to the repo root:

- `.quasar.yaml` — required; the per-repo config (see
  [per-repo-config.md](per-repo-config.md)).
- `sensors/*.toml` — optional; one file per sensor instance.
- `constellations/`, `stars/`, `skills/` — optional; per-repo overrides of the
  embedded built-ins.

## systemd unit

```ini
# /etc/systemd/system/quasar.service
[Unit]
Description=Quasar supervisor
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=/opt/quasar/quasar supervise
Restart=on-failure
WatchdogSec=120s
ProtectSystem=strict
ReadWritePaths=/var/lib/quasar /srv/repos
NoNewPrivileges=true
User=quasar

[Install]
WantedBy=multi-user.target
```

`Type=notify` with `WatchdogSec=120s` lets the supervisor heartbeat via
`sd_notify`; if the loop wedges, systemd restarts it. `ProtectSystem=strict`
plus a narrow `ReadWritePaths` keeps the process from writing anywhere except
its state directory and the checked-out repos.

## Upgrading

1. `systemctl stop quasar`
2. Swap the binary at `/opt/quasar/quasar`.
3. `systemctl start quasar`

The supervisor holds a single-instance guard (an advisory lock on the SQLite
database), so an accidental double-start refuses rather than corrupting state.
Sensors resume from their persisted cursors automatically — no poll is lost or
double-counted across the restart.

## Backup

Snapshot `/var/lib/quasar/` to back up everything:

- `state.sqlite` — use SQLite's online `.backup` API (or copy while the service
  is stopped) for an atomic, consistent snapshot.
- `blobs/` — content-addressed, so `rsync` is safe even on a live store: a blob
  file never changes once written.

Restoring is the reverse: drop the files back in place and start the unit.
