+++
id = "project-scanner"
title = "Build deterministic project context scanner"
type = "feature"
priority = 1
labels = ["quasar", "context", "cost-optimization"]
scope = ["internal/snapshot/**"]
+++

## Problem

Every agent invocation (coder, reviewer, lint-fix) starts from scratch — re-reading the repo structure, CLAUDE.md, and key files to understand the project. In a nebula run with 8 phases and ~4 cycles each, that's ~64 invocations all independently discovering the same context. This wastes tokens and money.

Anthropic's prompt caching gives a 90% discount on cached input tokens when the system prompt prefix is stable across calls. If we front-load a deterministic project snapshot into the system prompt, every invocation after the first gets the context nearly free.

The deprecated `internal/context/` package was a placeholder for this work but was never implemented and shadows Go's stdlib `context` package. We need a new `internal/snapshot/` package.

## Solution

### 1. Scanner type

Create `internal/snapshot/scanner.go` with:

```go
// Scanner produces a deterministic project snapshot for prompt injection.
type Scanner struct {
    MaxSize   int    // max snapshot size in bytes (default 32000 ≈ 8K tokens)
    MaxDepth  int    // max directory tree depth (default 3)
    WorkDir   string // repo root
}

// Scan produces a deterministic markdown snapshot of the project.
func (s *Scanner) Scan(ctx context.Context) (string, error)
```

### 2. Snapshot content (in order)

The snapshot is a markdown document with these sections:

```markdown
## Project
- Module: github.com/user/repo
- Language: Go

## Structure
```
cmd/
  root.go
  run.go
internal/
  loop/
    loop.go
    ...
  ...
```

## Conventions
[Contents of CLAUDE.md, truncated to fit budget]
```

### 3. Implementation details

**File listing**: Use `git ls-files` (respects .gitignore, fast, deterministic when sorted). Fall back to `os.ReadDir` walk if not in a git repo.

**Tree builder** (`internal/snapshot/tree.go`): Convert the flat file list into a depth-limited indented tree. Sort alphabetically at every level. Collapse directories with more than N files to `dir/ (N files)`.

**Project detection**: Check for these files in order:
- `go.mod` → extract module path, language = "Go"
- `package.json` → extract name, language = "JavaScript/TypeScript"
- `Cargo.toml` → extract package name, language = "Rust"
- `pyproject.toml` / `setup.py` → language = "Python"

**CLAUDE.md detection**: Read `CLAUDE.md` (or `.claude.md`, `claude.md`) if present. Include up to a configurable portion of the budget.

**Determinism**: Everything must be sorted. No timestamps, no randomness, no host-specific paths. Same repo state = same output byte-for-byte.

**Size capping**: Track byte count as we build. If the tree section exceeds budget, reduce depth. If CLAUDE.md exceeds remaining budget, truncate with a `[truncated]` marker.

### 4. Budget allocation

Default 32K bytes (~8K tokens). Split across sections:
- Project header: ~200 bytes (fixed)
- Structure: up to 40% of budget
- Conventions: up to 60% of budget

## Files

- `internal/snapshot/scanner.go` — `Scanner` struct, `Scan` method, project detection
- `internal/snapshot/tree.go` — tree builder from flat file list, depth limiting, collapse logic
- `internal/snapshot/scanner_test.go` — determinism tests, size cap tests, project detection tests
- `internal/snapshot/tree_test.go` — tree rendering tests, depth limiting tests

## Acceptance Criteria

- [ ] `Scanner.Scan(ctx)` returns a deterministic markdown snapshot string
- [ ] Running `Scan` twice on the same repo produces identical output (byte-for-byte)
- [ ] Snapshot includes project identity (module name extracted from go.mod/package.json/Cargo.toml)
- [ ] Snapshot includes depth-limited directory tree (respecting .gitignore via `git ls-files`)
- [ ] Snapshot includes CLAUDE.md content if present
- [ ] Total snapshot size stays within configurable `MaxSize` (default 32K bytes)
- [ ] Works on non-Go repos (detects package.json, Cargo.toml, pyproject.toml)
- [ ] Falls back to `os.ReadDir` walk when not in a git repo
- [ ] `go test ./internal/snapshot/...` passes
- [ ] `go vet ./...` clean
