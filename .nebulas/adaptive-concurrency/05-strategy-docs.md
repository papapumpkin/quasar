+++
id = "strategy-docs"
title = "Document strategies in nebula config and CLI help"
type = "task"
priority = 3
depends_on = ["controller-integration"]
scope = ["cmd/nebula.go"]
+++

## Problem

Users need to understand what each strategy does and when to use it. The strategy config is in the manifest TOML but has no documentation surface in the CLI.

## Solution

### CLI help text

Update `quasar nebula apply --help` to describe the `strategy` field:

```
Concurrency Strategies:
  speed      Max parallelism, back off only on conflicts. Best for CI pipelines.
  cost       Conservative start, increase on fast+cheap phases. Best for budget-limited runs.
  quality    Cap at graph width, reduce on low satisfaction. Best for critical refactors.
  balanced   AIMD — increase on clean waves, decrease on conflicts. Default.
```

### Status command integration

Update `quasar nebula status` output to show:
- Active strategy name
- Controller decisions per wave (from `WaveDecision` history)
- Concurrency trajectory: `[3, 3, 2, 3, 3]` showing the per-wave worker count

### Validate command enhancement

When `quasar nebula validate` runs, include strategy info in the success output:

```
✓ nebula "local-parallelism" — 8 phase(s), no errors
  Strategy: balanced (max_workers: 4)
```

## Files to Modify

- `cmd/nebula.go` — Update apply help text, status output, validate output

## Acceptance Criteria

- [ ] `quasar nebula apply --help` shows strategy descriptions
- [ ] `quasar nebula status` shows strategy and per-wave decisions
- [ ] `quasar nebula validate` shows strategy in success output
- [ ] `go build ./...` compiles
