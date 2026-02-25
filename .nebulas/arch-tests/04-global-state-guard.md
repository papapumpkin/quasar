+++
id = "global-state-guard"
title = "Detect mutable global state beyond sentinels and constants"
type = "task"
priority = 2
depends_on = ["arch-foundation"]
scope = ["internal/arch_test/globals_test.go"]
+++

## Problem

Mutable global state makes code hard to test, creates hidden coupling between packages, and is explicitly banned by the project's conventions (with exceptions for error sentinels and constants). Without automated detection, package-level `var` declarations that hold mutable state creep in during development.

## Solution

Create `internal/arch_test/globals_test.go` that scans all internal packages for package-level `var` declarations and flags any that aren't in the allowed categories.

### Allowed patterns

A package-level `var` is acceptable if it matches ANY of these:

1. **Error sentinel**: Type is `error` or the initializer calls `errors.New(...)` or `fmt.Errorf(...)`
2. **Compile-time interface check**: Pattern `var _ SomeInterface = (*SomeType)(nil)` or `var _ SomeInterface = SomeType{}`
3. **Constant-like value**: Initialized with a literal (string, int, bool) and never reassigned (heuristic: if name is `ALL_CAPS` or starts with a lowercase letter and is unexported, allow it if it's a simple literal)
4. **Regex compilation**: `regexp.MustCompile(...)` — common Go pattern for compiled regexes
5. **sync primitives**: `sync.Once`, `sync.Mutex`, `sync.Pool` — these are stateful but are the standard Go concurrency pattern

### Detection logic

Using `go/ast`:

1. Walk each file's top-level declarations
2. For each `*ast.GenDecl` with `token.VAR`:
   - Extract the var spec (name, type, value expression)
   - Check against the allowed patterns above
   - If none match, flag it

### Test function

`TestNoMutableGlobalState(t *testing.T)`:
- For each internal package, scan all non-test `.go` files
- For each package-level `var`, check if it matches an allowed pattern
- If not, fail with: `"mutable global state in %s: var %s (type: %s); use dependency injection or move to a function"`
- Subtests per package

### Allowlist for edge cases

```go
var allowedGlobals = map[string][]string{
    // "package": {"varName1", "varName2"},
    // Add legitimate cases discovered during implementation
}
```

## Files

- `internal/arch_test/globals_test.go` — global state detection tests (new file)

## Acceptance Criteria

- [ ] Error sentinels (`var ErrFoo = errors.New(...)`) are correctly allowed
- [ ] Interface checks (`var _ Foo = (*Bar)(nil)`) are correctly allowed
- [ ] `regexp.MustCompile` vars are correctly allowed
- [ ] Any actual mutable globals in current code are either fixed or explicitly allowlisted
- [ ] Test fails if a new `var myMap = make(map[string]string)` is added at package level
- [ ] `go test ./internal/arch_test/...` passes
