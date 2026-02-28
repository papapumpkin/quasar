+++
id = "prior-work"
title = "Inject neutron archive summaries as prior work context"
type = "feature"
priority = 2
depends_on = ["context-budget"]
scope = ["internal/nebula/types.go", "internal/nebula/parse.go", "internal/nebula/validate.go", "internal/nebula/prior.go"]
+++

## Problem

Completed work exists in isolation. When starting a new nebula that builds on previous work (e.g., "add rate limiting" after "build auth system"), the coder/reviewer agents have no knowledge of what was already done, what decisions were made, or what constraints emerged. They rediscover context through file reading, or worse, make conflicting decisions.

Neutron archives (from `internal/neutron/`) already capture completed nebula state. We need a way to reference these archives in new nebulas so agents inherit historical context.

## Solution

### 1. Add `prior_work` to manifest context

Extend the `Context` struct in `internal/nebula/types.go`:

```go
type Context struct {
    Repo       string   `toml:"repo"`
    WorkingDir string   `toml:"working_dir"`
    Goals      []string `toml:"goals"`
    Constraints []string `toml:"constraints"`
    PriorWork  []string `toml:"prior_work"` // Paths to neutron archives
}
```

Usage in `nebula.toml`:

```toml
[context]
prior_work = [
    ".neutrons/auth-feature.neutron.json",
    ".neutrons/db-migration.neutron.json",
]
```

### 2. Load and render prior work summaries

Create `internal/nebula/prior.go`:

```go
// LoadPriorWork reads neutron archives and produces a summary for prompt injection.
func LoadPriorWork(paths []string, workDir string) (string, error)
```

The function:
1. Reads each neutron JSON file
2. Extracts the nebula name, completion date, and per-phase summaries
3. Renders a compact markdown block:

```markdown
## Prior Work

### auth-feature (completed 2026-02-16)
- setup-models: Created User and Session models with validation
- auth-middleware: JWT middleware with role-based access control
- integration-tests: Full test coverage for auth flows

### db-migration (completed 2026-02-14)
- schema-v2: Migrated from flat tables to normalized schema
- data-backfill: Backfilled 2M rows with zero downtime
```

Each phase is one line: its report summary from the neutron archive. If no report summary exists, fall back to the phase title.

### 3. Validation

Add validation rules in `internal/nebula/validate.go`:
- Warn if a `prior_work` path does not exist
- Warn if a neutron file is from a different repo
- Error if a neutron file is malformed

### 4. Injection path

Prior work feeds into the context budget system from phase 4. In `buildPhasePrompt`, the flow becomes:

```
PROJECT CONTEXT:
Goals: ...
Constraints: ...

[PRIOR WORK]  ← New section, injected when prior_work is non-empty
...

PHASE:
[phase body]
```

### 5. Convention: `.neutrons/` directory

Suggest (but don't enforce) a `.neutrons/` directory alongside `.nebulas/` for storing archives:

```
project/
  .nebulas/
    rate-limiting/
  .neutrons/
    auth-feature.neutron.json
    db-migration.neutron.json
```

## Files

- `internal/nebula/types.go` — Add `PriorWork []string` to `Context`
- `internal/nebula/parse.go` — Parse `prior_work` from TOML
- `internal/nebula/prior.go` — `LoadPriorWork` function
- `internal/nebula/prior_test.go` — Tests with mock neutron files
- `internal/nebula/validate.go` — Add prior_work path validation
- `internal/nebula/worker.go` — Inject prior work into `buildPhasePrompt`

## Acceptance Criteria

- [ ] `prior_work` field in `[context]` is parsed from `nebula.toml`
- [ ] `LoadPriorWork` reads neutron archives and produces a compact markdown summary
- [ ] Prior work summary is injected into coder/reviewer prompts via the context budget
- [ ] Missing neutron files produce a validation warning (not a hard error)
- [ ] Malformed neutron files produce a validation error
- [ ] When `prior_work` is empty or omitted, behavior is identical to current (backward compatible)
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` clean
