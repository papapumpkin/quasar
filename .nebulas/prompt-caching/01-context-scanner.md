+++
id = "context-scanner"
title = "Build project context scanner"
type = "feature"
priority = 1
+++

## Problem

Every agent invocation (coder, reviewer, architect) starts from scratch — re-reading the repo structure, CLAUDE.md, and key files to understand the project. In a nebula run with 8 phases and ~4 cycles each, that's ~64 invocations all independently discovering the same context. This wastes tokens and money.

We need a scanner that produces a deterministic, compact "project snapshot" string that can be prepended to agent system prompts. Anthropic's prompt caching gives a 90% discount on cached input tokens when the prefix is stable across calls.

## Solution

Create `internal/context/scanner.go` with a `Scanner` type that:

1. **Walks the repo tree** (respecting .gitignore via `git ls-files`) to produce a compact directory listing
2. **Reads CLAUDE.md** (or similar project instruction files) if present
3. **Reads go.mod** (or package.json, Cargo.toml, etc.) to identify the project language/module
4. **Builds a structured markdown snapshot** with sections:
   - `## Project` — module name, language
   - `## Structure` — directory tree (depth-limited, e.g. 3 levels)
   - `## Conventions` — content of CLAUDE.md if found
5. **Caps total size** at a configurable limit (default ~8K tokens ≈ ~32K chars) with truncation

The snapshot must be **deterministic**: same repo state → identical output. This is critical for cache hits.

### Key design decisions:

- Use `git ls-files` to get the file list (respects .gitignore, fast, works everywhere)
- Build a tree from the flat file list, then render it as indented text
- Depth-limit the tree to keep it compact (default 3 levels)
- Sort everything alphabetically for determinism
- No network calls, no expensive analysis — this runs at nebula-apply time

## Files

- `internal/context/scanner.go` — `Scanner` struct, `Scan(ctx, workDir) (string, error)` method
- `internal/context/tree.go` — directory tree builder from flat file list (helper)

## Acceptance Criteria

- [ ] `Scanner.Scan(ctx, dir)` returns a deterministic snapshot string for a given repo
- [ ] Snapshot includes project identity (module name from go.mod/package.json/etc.)
- [ ] Snapshot includes directory tree (depth-limited, .gitignore-respecting)
- [ ] Snapshot includes CLAUDE.md content if present
- [ ] Snapshot is capped at configurable max size with clean truncation
- [ ] Running `Scan` twice on the same repo produces identical output
- [ ] Works on non-Go repos (detects package.json, Cargo.toml, etc.)
