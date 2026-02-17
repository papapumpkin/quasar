+++
id = "neutron-as-context"
title = "Use neutron archives as context input for new nursery nebulas"
type = "feature"
priority = 2
depends_on = ["neutron-compression"]
+++

## Problem

Completed work shouldn't exist in isolation. When starting a new nebula that builds on previous work, the user should be able to reference neutron archives so the coder/reviewer agents understand what was already done, what decisions were made, and what constraints apply.

## Solution

### 1. Prior Work in Manifest

Add a `prior_work` field to `[context]` in `nebula.toml`:

```toml
[context]
prior_work = [
    ".neutrons/auth-feature.neutron.json",
    ".neutrons/db-migration.neutron.json",
]
```

### 2. Context Injection

When building coder/reviewer prompts, inject prior work summaries:

```
[PRIOR WORK]
The following nebulas were previously completed and are relevant context:

## auth-feature (completed 2026-02-16)
- setup-models: Created User and Session models with proper validation
- auth-middleware: JWT middleware with role-based access control
- integration-tests: Full test coverage for auth flows

## db-migration
- schema-v2: Migrated from flat tables to normalized schema
- data-backfill: Backfilled 2M rows with zero downtime
```

Each phase is one line: its summary from the neutron archive.

### 3. Neutron Storage Convention

Suggest a `.neutrons/` directory alongside `.nebulas/` for archived neutrons:

```
project/
├── .nebulas/
│   ├── auth-feature/     (nursery or constellation)
│   └── rate-limiting/    (nursery)
├── .neutrons/
│   ├── setup-infra.neutron.json
│   └── db-migration.neutron.json
```

### 4. Auto-Discovery

When a nebula's `prior_work` references a neutron file, validate that:
- The file exists and is a valid neutron archive
- It's version-compatible
- Warn if the referenced neutron is very old or from a different repo

## Files to Modify

- `internal/nebula/types.go` — Add `PriorWork []string` to context
- `internal/nebula/parse.go` — Parse `prior_work` field
- `internal/nebula/neutron.go` — `LoadSummaries(paths []string)` for prompt injection
- `internal/loop/prompt.go` — Add `[PRIOR WORK]` section when prior_work is provided
- `internal/nebula/validate.go` — Validate prior_work paths

## Acceptance Criteria

- [ ] `prior_work` field in `[context]` references neutron archives
- [ ] Coder/reviewer prompts include phase summaries from referenced neutrons
- [ ] Invalid neutron paths produce validation warnings
- [ ] `go build` and `go test ./...` pass
