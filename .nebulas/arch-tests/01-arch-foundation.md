+++
id = "arch-foundation"
title = "Set up architecture test package and shared helpers"
type = "task"
priority = 1
scope = ["internal/arch_test/helpers_test.go"]
+++

## Problem

There is no dedicated location for architecture-level tests that guard structural invariants. Without shared helpers for parsing Go source, scanning imports, and walking packages, each subsequent architecture test phase would duplicate boilerplate.

## Solution

Create `internal/arch_test/` as a test-only package. This package contains no production code — only `*_test.go` files and a `helpers_test.go` with shared utilities.

### Helpers to implement in `helpers_test.go`

1. **`internalPackages(t *testing.T) []string`** — walks `internal/` and returns all Go package directory paths, excluding `board` (dead code) and `arch_test` itself.

2. **`importsOf(t *testing.T, pkgDir string) []string`** — uses `go/parser` to parse all non-test `.go` files in a package directory and returns deduplicated internal import paths (those matching `github.com/aaronsalm/quasar/internal/`). Strips the module prefix so results are like `agent`, `fabric`, etc.

3. **`goFilesIn(t *testing.T, pkgDir string) []string`** — returns all `.go` files (excluding `_test.go`) in a directory.

4. **`lineCount(t *testing.T, filePath string) int`** — counts lines in a file.

5. **`exportedSymbols(t *testing.T, filePath string) []exportedSymbol`** — parses a Go file and returns all exported type, function, method, var, and const declarations with their doc comment (empty string if missing). Uses `go/ast`.

6. **`interfaceDecls(t *testing.T, filePath string) []interfaceDecl`** — parses a Go file and returns all interface type declarations with name, package, file path, and method list.

All helpers should `t.Helper()` and `t.Fatal()` on errors so test output points to the caller.

### Package setup

- Package declaration: `package arch_test` (external test package, can import internal packages for reflection if needed but the tests themselves use `go/parser` to avoid import side effects)
- All files named `*_test.go` so `go build` never includes them
- No `init()` functions, no global state

## Files

- `internal/arch_test/helpers_test.go` — shared test helpers (new file)

## Acceptance Criteria

- [ ] `go test ./internal/arch_test/...` runs and passes (even if trivially — no test functions yet beyond a placeholder)
- [ ] `go vet ./internal/arch_test/...` passes
- [ ] All helper functions are tested with at least one sanity check (e.g. `internalPackages` returns > 10 packages, `importsOf` on `internal/claude` includes `agent`)
- [ ] `board` and `arch_test` are excluded from `internalPackages` results
