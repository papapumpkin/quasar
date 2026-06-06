+++
id = "runbooks"
title = "Operational runbooks: upgrade, rollback, log inspection, GC manual run, sensor cursor reset, incident response"
type = "task"
priority = 3
depends_on = ["packer-ami", "terraform-module", "systemd-and-secrets"]
scope = [
    "docs/runbooks/**",
]
+++

## Problem

Once Quasar is in production on EC2, day-2 ops needs documented procedures. These are not architecture docs (those live in `docs/safety.md` etc) — these are the "what do I run when X happens" checklists. They're short, prescriptive, and dated so it's obvious when they go stale.

## Solution

Six runbooks, each its own markdown file under `docs/runbooks/`. Each follows the same template:

```markdown
# <title>

**When to use:** <one-line trigger>
**Severity:** routine | high | incident
**Estimated time:** <minutes>
**Last verified:** YYYY-MM-DD against quasar <version>

## Prerequisites
- [ ] ...

## Procedure
1. ...
2. ...

## Verification
- [ ] ...

## Rollback
...

## Related
- <links>
```

### The six runbooks

#### `docs/runbooks/01-upgrade.md`

Upgrading the supervisor to a new version.

- Trigger: a new tagged release is available
- Time: ~10 minutes
- Steps:
  1. Verify no constellation_runs are in `running` state (`quasar runs list --state running`)
  2. Confirm new AMI ID in SSM `/quasar/latest-ami-id`
  3. `terraform apply` with the new AMI (replaces the instance)
  4. SSH in, run `quasar version` and `quasar doctor`
  5. Verify supervisor came back online: `curl 127.0.0.1:7331/health`
- Rollback: revert the AMI ID in SSM and re-apply

#### `docs/runbooks/02-rollback.md`

Rolling back after a bad upgrade.

- Trigger: post-upgrade `quasar doctor` fails OR a new bug appears
- Time: ~5 minutes
- Steps:
  1. Find the previous AMI ID (`aws ssm get-parameter-history --name /quasar/latest-ami-id`)
  2. Set SSM to the previous AMI ID
  3. `terraform apply`
  4. Verify `quasar version` reports the prior version
- Note: SQLite migrations are forward-only; downgrading the binary without restoring the SQLite snapshot may break. If a migration ran, restore the snapshot too (next runbook).

#### `docs/runbooks/03-restore-sqlite.md`

Restoring SQLite from snapshot.

- Trigger: corruption, accidental hard-delete, post-rollback after a migration
- Time: ~5 minutes per GB
- Prerequisites: a recent snapshot in S3 (snapshotting is a cron job documented in `docs/deployment.md`)
- Steps:
  1. `systemctl stop quasar`
  2. `mv /var/lib/quasar/state.sqlite{,.bad-$(date +%s)}`
  3. `aws s3 cp s3://<bucket>/quasar-snapshots/<latest>.sqlite /var/lib/quasar/state.sqlite`
  4. `chown quasar:quasar /var/lib/quasar/state.sqlite`
  5. `systemctl start quasar`
  6. Verify with `quasar doctor` and `curl 127.0.0.1:7331/health`

#### `docs/runbooks/04-gc-manual-run.md`

Forcing a GC sweep.

- Trigger: disk space alert; or after a backfill that left many soft-deleted rows
- Time: ~1-30 minutes depending on backlog
- Steps:
  1. `quasar gc run --dry-run` — preview what will be reaped
  2. `quasar gc run --category completed_nebulas`
  3. `quasar gc blobs --dry-run`
  4. `quasar gc blobs`
  5. Verify disk freed: `df -h /var/lib/quasar`
- Caveat: GC skips categories whose primary table has active rows; if `running` runs exist, sweep them first or wait.

#### `docs/runbooks/05-sensor-cursor-reset.md`

Resetting a sensor cursor (re-process from scratch).

- Trigger: sensor produced bad seed nebulas (e.g. wrong template, broken parser) and you want to re-ingest
- Time: ~5 minutes
- Steps:
  1. Identify the (repo, sensor) pair: `quasar sensor list`
  2. Optionally back up the existing cursor: `quasar sensor cursor get <repo> <sensor>`
  3. `quasar sensor cursor reset <repo> <sensor>` — sets cursor to empty
  4. Sensor will re-process on its next poll tick; dedup via `sensor_events.UNIQUE` prevents creating duplicate seed nebulas for events already processed
- Warning: if you also `DELETE FROM sensor_events WHERE ...` you bypass dedup. Only do this if you understand the consequences.

#### `docs/runbooks/06-incident-runaway-runs.md`

Stopping a runaway: too many constellation_runs spawning, budget burning fast.

- Trigger: budget alarm OR fleet view shows >N concurrent runs unexpectedly
- Severity: incident
- Time: <2 minutes
- Steps:
  1. **Pause everything**: `quasar runs pause --all` (sets `state='paused'` on every running run)
  2. **Identify the source**: `quasar runs list --state paused --since 10m` — what sensor produced these nebulas?
  3. **Stop the sensor**: edit the offending sensor's TOML, set `enabled = false`, save (the supervisor reloads on next tick)
  4. **Triage**: inspect a few of the paused runs to determine if they should resume, abandon, or be killed
  5. **Resume the good ones**: `quasar runs resume <id>` for each
  6. **Kill the bad ones**: `quasar runs kill <id>` for each
  7. **Re-enable the sensor** only after the root cause is fixed

This runbook references the cycle limit + budget enforcement from `master-reviewer-loop-hardening` — those are the primary backstops; this runbook is for the case where they aren't enough.

### Verification

A small CI test, `docs/runbooks_test.go`, walks the runbooks directory and verifies:
- Every runbook has the required headers (`When to use`, `Severity`, `Estimated time`, `Last verified`)
- `Last verified` is parseable as a date
- Any `quasar <subcommand>` mentioned in steps exists in the CLI (grep `cobra.Command{Use:` in `cmd/`)

### Linking

- `README.md` "Operations" section links to the index
- `docs/deployment.md` "What to do if X" section gets prescriptive links into the runbooks
- `docs/runbooks/README.md` is the index, ordered by frequency-of-use

## Files

- `docs/runbooks/README.md` (new)
- `docs/runbooks/01-upgrade.md` (new)
- `docs/runbooks/02-rollback.md` (new)
- `docs/runbooks/03-restore-sqlite.md` (new)
- `docs/runbooks/04-gc-manual-run.md` (new)
- `docs/runbooks/05-sensor-cursor-reset.md` (new)
- `docs/runbooks/06-incident-runaway-runs.md` (new)
- `docs/runbooks_test.go` (new) — runbook header + CLI-existence check
- `README.md` (modify) — link to runbook index
- `docs/deployment.md` (modify) — prescriptive cross-links

## Acceptance Criteria

- [ ] All six runbooks exist and pass `runbooks_test.go` header check
- [ ] Every `quasar` subcommand mentioned in a runbook step is present in `cmd/`
- [ ] `docs/runbooks/README.md` indexes them in frequency order with one-line summaries
- [ ] `README.md` and `docs/deployment.md` link in
- [ ] Linkcheck (from constellation-runtime Phase 9) passes on all new files
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
