+++
id = "scope-tests"
title = "Add comprehensive tests for scope validation"
type = "task"
priority = 2
depends_on = ["scope-validation"]
scope = ["internal/nebula/nebula_test.go"]
+++

## Problem

The new scope validation logic needs thorough test coverage.

## Solution

Add table-driven tests to `internal/nebula/nebula_test.go`.

### Tests for `scopesOverlap` helper

| A | B | Expected |
|---|---|----------|
| `["internal/api/"]` | `["internal/api/middleware/"]` | overlap (prefix containment) |
| `["internal/api/"]` | `["internal/config/"]` | no overlap |
| `["cmd/*.go"]` | `["cmd/root.go"]` | overlap |
| `["**/*.proto"]` | `["api/v1/service.proto"]` | overlap |
| `["internal/"]` | `["internal/"]` | overlap (exact) |
| `[]` | `["internal/"]` | no overlap (empty = unscoped) |

### Tests for `Validate` scope pass

| Scenario | Expected |
|----------|----------|
| Two parallel phases, non-overlapping scopes | no error |
| Two parallel phases, overlapping scopes | ErrScopeOverlap |
| Overlapping scopes + direct dependency | no error |
| Overlapping scopes + transitive dependency | no error |
| Overlapping scopes + allow_scope_overlap on one | no error |
| One scoped + one unscoped phase | no error |
| Three phases: A↔B overlap, B↔C overlap, A↔C no overlap | two errors |

### Tests for `Graph.HasPath` / `Graph.Connected`

| Graph | Query | Expected |
|-------|-------|----------|
| a→b→c | HasPath(a, b) | true |
| a→b→c | HasPath(a, c) | true (transitive) |
| a→b→c | HasPath(c, a) | false |
| a→b, c→d | Connected(a, c) | false |
| a→b | Connected(b, a) | true |

## Files to Modify

- `internal/nebula/nebula_test.go` — Add scope validation and graph tests

## Acceptance Criteria

- [ ] All table-driven tests pass
- [ ] Edge cases covered (empty scope, self-overlap, three-way)
- [ ] `go test ./internal/nebula/... -v` shows clear test names
- [ ] Tests use `t.Parallel()` where safe
