+++
id = "config-opt-in"
title = "Add config and CLI flags for context caching"
type = "task"
priority = 2
depends_on = ["nebula-wiring"]
+++

## Problem

Context caching should be enabled by default but configurable. Users may want to disable it (e.g., if their CLAUDE.md contains sensitive info they don't want in system prompts, or for debugging). The nebula manifest should also be able to control it.

## Solution

### 1. CLI flags

Add `--no-context` flag to both `quasar run` and `quasar nebula apply`:

```go
cmd.Flags().Bool("no-context", false, "Disable project context caching for agent prompts")
```

Bind to Viper as `QUASAR_NO_CONTEXT`.

### 2. Manifest support

Add an optional `context_caching` field to `[execution]` in the nebula manifest:

```toml
[execution]
context_caching = true  # default: true
```

When set to `false`, skip context generation for that nebula run.

### 3. Config precedence

Follow existing pattern: CLI flag > env > manifest > default (true).

### 4. Scanner config from manifest context

The `[context]` section already has `working_dir`. We can also add optional fields for scanner tuning:

```toml
[context]
context_max_depth = 3     # directory tree depth (default 3)
context_max_size = 32000  # max snapshot chars (default 32000)
```

These are optional and have sensible defaults.

## Files

- `cmd/run.go` — add `--no-context` flag
- `cmd/nebula_apply.go` — add `--no-context` flag, read manifest setting
- `internal/nebula/types.go` — add `ContextCaching *bool` to `Execution`
- `internal/nebula/parse.go` — parse new field (if not auto-handled by TOML unmarshaler)

## Acceptance Criteria

- [ ] `--no-context` flag works on both `run` and `nebula apply`
- [ ] `QUASAR_NO_CONTEXT=true` env var disables context
- [ ] Manifest `context_caching = false` disables context for that nebula
- [ ] Default behavior is context enabled (opt-out, not opt-in)
- [ ] Config precedence follows existing Viper pattern
