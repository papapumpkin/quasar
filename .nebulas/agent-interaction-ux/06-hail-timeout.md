+++
id = "hail-timeout"
title = "Add configurable hail timeout with fallback behavior"
type = "feature"
priority = 3
depends_on = ["hail-relay"]
+++

## Problem

If the human is away or doesn't notice a hail, agents shouldn't be stuck waiting indefinitely. Hails are designed to be non-blocking (unlike gates), but the agent still benefits from knowing whether the human responded. There needs to be a timeout after which the agent proceeds with its best judgment.

## Solution

Add a `hail_timeout` configuration option (default: 5 minutes) to the execution config:

```toml
[execution]
hail_timeout = "5m"   # Duration string
```

When a hail times out without human resolution:
1. Auto-resolve it with a standard message: "No human response within timeout. Agent proceeded with best judgment."
2. Mark it as `auto_resolved` (distinct from human resolution)
3. The agent's next cycle prompt includes: "[HAIL TIMEOUT] No response to your question about X. Proceed with your best judgment."

This keeps hails as soft nudges rather than hard blocks, matching the design intent of hails vs. gates.

## Files

- `internal/loop/hail.go` — Add `AutoResolved` field, timeout logic in HailQueue
- `internal/nebula/types.go` — Add `HailTimeout` to execution config
- `internal/nebula/parse.go` — Parse hail_timeout duration from manifest
- `internal/loop/loop.go` — Wire timeout config into HailQueue

## Acceptance Criteria

- [ ] Hails auto-resolve after configurable timeout
- [ ] Auto-resolved hails are distinguishable from human-resolved ones
- [ ] Timeout prompt is injected on next cycle
- [ ] Default timeout is 5 minutes
- [ ] Timeout of 0 disables auto-resolution (wait indefinitely)
- [ ] Tests cover timeout, auto-resolution, and disabled-timeout cases