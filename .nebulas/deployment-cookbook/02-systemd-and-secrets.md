+++
id = "systemd-and-secrets"
title = "Systemd unit with sd_notify heartbeat, hardening flags, SSM secret resolution at supervisor start"
type = "task"
priority = 2
depends_on = ["packer-ami", "terraform-module"]
scope = [
    "deploy/packer/files/quasar.service",
    "internal/supervisor/sdnotify.go",
    "internal/supervisor/secrets_ssm.go",
]
+++

## Problem

The supervisor (Phase 5 of constellation-runtime) runs forever; if it deadlocks it should be restarted, not just killed when monitoring catches it later. systemd's `WatchdogSec` mechanism is the right primitive: the supervisor sends a heartbeat via `sd_notify(WATCHDOG=1)`, and if 120s pass without one systemd restarts the process. We just need to wire it in.

Secrets pulled from SSM at start (not from env vars in the unit file) keep them out of systemd dumps and `ps` output. The supervisor reads each secret on demand via the existing `SecretResolver` interface; this phase adds an SSM-backed implementation alongside the file/env ones.

## Solution

### systemd unit

`deploy/packer/files/quasar.service`:

```ini
[Unit]
Description=Quasar autonomous coding supervisor
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
NotifyAccess=main
ExecStart=/usr/local/bin/quasar supervise --config /etc/quasar/quasar.yaml
Restart=on-failure
RestartSec=10s

# Watchdog: supervisor heartbeats every 30s; systemd restarts if 120s without one
WatchdogSec=120s

# User
User=quasar
Group=quasar

# Filesystem hardening
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/quasar /srv/repos
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true

# Process hardening
NoNewPrivileges=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=false  # claude CLI uses JIT; leave allowed
RestrictNamespaces=true
RestrictRealtime=true
SystemCallArchitectures=native

# Resource limits
LimitNOFILE=65536
TasksMax=2048

# Logging — systemd journal forwarded to CloudWatch via vector
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

`MemoryDenyWriteExecute=false` is intentional — the claude CLI uses a JIT and the supervisor shells out to it. Setting it `true` breaks runs immediately.

### sd_notify integration

`internal/supervisor/sdnotify.go`:

```go
// SDNotifier wraps systemd's sd_notify socket.
// If NOTIFY_SOCKET is unset (not under systemd), all methods are no-ops.
type SDNotifier struct {
    socketAddr string
    conn       net.Conn
}

func New() (*SDNotifier, error)

// Ready signals the supervisor finished its startup phase.
func (n *SDNotifier) Ready() error

// Heartbeat signals the supervisor is alive. Called every 30s by the
// supervisor's main loop.
func (n *SDNotifier) Heartbeat() error

// Stopping signals graceful shutdown is starting.
func (n *SDNotifier) Stopping() error
```

The supervisor's main loop is updated to call `Heartbeat()` at the top of each tick. If the loop is stuck (deadlock on a SQL transaction, blocked on a stuck subprocess), heartbeats stop and systemd restarts the process within 120s.

### SSM secret resolver

`internal/supervisor/secrets_ssm.go`:

```go
// SSMResolver looks up secrets in AWS SSM Parameter Store.
// Configured via the SecretResolver interface from internal/sensors.
// Lookup key syntax in TOML: `secret_ssm = "/quasar/gh-pat"`
type SSMResolver struct {
    client SSMClient   // injected, can be aws-sdk-go-v2 ssm.Client or a fake
    cache  *secretCache  // 5min TTL; secret rotation reload is on next cache miss
}

func (r *SSMResolver) Resolve(key string) (string, error)
```

This adds a fourth precedence rule to the existing chain (from Phase 1 of additional-sensors authoring guide):

1. `<key>_file`
2. `<key>_env`
3. `<key>_ssm`  ← NEW
4. Vendor CLI fallback

A sensor TOML on EC2 can therefore say `token_ssm = "/quasar/gh-pat"` and the supervisor resolves it via the EC2 instance profile's `ssm:GetParameter` permission (granted by Phase 1's IAM module). No tokens cross systemd or env vars.

### Cache + rotation

SSM responses are cached for 5 minutes per key. To rotate a secret without restarting the supervisor: update the SSM value, wait 5 minutes (or call `quasar secret flush`). The cache is in-memory only — no on-disk secret persistence.

### Health endpoint (for ALB / monitoring)

`internal/supervisor/health.go` exposes `GET /health` on `127.0.0.1:7331` returning:

```json
{
  "status": "ok",
  "version": "v0.2.0",
  "uptime_s": 123456,
  "supervisor_tick_ago_s": 12,
  "active_runs": 3,
  "last_gc_run_ago_s": 2400
}
```

Status is `degraded` if `supervisor_tick_ago_s > 90` (supervisor is slow), `unhealthy` if `> 200` (systemd is about to restart anyway).

### Tests

- `sdnotify_test.go` — start a fake notify-socket listener; verify Ready/Heartbeat/Stopping write the expected wire format
- `secrets_ssm_test.go` — fake SSM client; verify cache hits/misses, rotation behavior, error mapping (`ParameterNotFound` → typed error)
- `health_test.go` — start the server with a fake supervisor; verify each status branch

### Doc

A short addition to `docs/deployment.md` (from constellation-runtime Phase 9) covering:
- How to rotate the bot PAT (update SSM, optionally `quasar secret flush`)
- How to read the watchdog log when systemd restarts the supervisor (`journalctl -u quasar --since '1 hour ago' | grep -i watchdog`)
- How to disable the watchdog for debugging (`systemctl edit quasar` → `WatchdogSec=0`)

## Files

- `deploy/packer/files/quasar.service` (modify — final form)
- `internal/supervisor/sdnotify.go` (new)
- `internal/supervisor/sdnotify_test.go` (new)
- `internal/supervisor/secrets_ssm.go` (new)
- `internal/supervisor/secrets_ssm_test.go` (new)
- `internal/supervisor/health.go` (new)
- `internal/supervisor/health_test.go` (new)
- `internal/supervisor/supervise.go` (modify) — call Ready() after init, Heartbeat() per tick, Stopping() on ctx.Done
- `docs/deployment.md` (modify) — add SSM/rotation + watchdog sections

## Acceptance Criteria

- [ ] `quasar supervise` calls `sd_notify(READY=1)` after startup and `WATCHDOG=1` at least every 30s
- [ ] When run outside systemd (NOTIFY_SOCKET unset), all sd_notify calls are no-ops; supervisor runs normally
- [ ] `SSMResolver` resolves `<key>_ssm = "/quasar/foo"` via the AWS SDK, caches for 5 minutes
- [ ] Cache rotation: updating SSM and waiting > 5 minutes (or calling `quasar secret flush`) picks up the new value
- [ ] Secret resolution precedence: file > env > ssm > vendor fallback
- [ ] `GET 127.0.0.1:7331/health` returns the documented JSON with correct status branches
- [ ] Hardened systemd unit passes `systemd-analyze security quasar.service` with score < 4.0 (best is 0)
- [ ] No PAT or other secret in `journalctl -u quasar` output (verified by an integration test that lifts secrets from SSM and greps the journal)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
