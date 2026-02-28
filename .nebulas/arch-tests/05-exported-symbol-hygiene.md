+++
id = "exported-symbol-hygiene"
title = "Ensure all exported symbols have GoDoc comments"
type = "task"
priority = 2
depends_on = ["arch-foundation"]
scope = ["internal/arch_test/godoc_test.go"]
+++

## Problem

The project convention requires every exported type, interface, function, and method to have a GoDoc comment following the `// Name does X.` pattern. Missing documentation makes the codebase harder to navigate and violates Go community standards. Without enforcement, documentation gaps widen during rapid development.

## Solution

Create `internal/arch_test/godoc_test.go` that scans all internal packages for exported symbols missing GoDoc comments.

### What to check

1. **Exported types** (`type Foo struct`, `type Bar interface`, `type Baz int`): Must have a comment starting with `// Foo` or `// Bar` etc.
2. **Exported functions** (`func DoThing(...)`): Must have a comment starting with `// DoThing`.
3. **Exported methods** (`func (f *Foo) DoThing(...)`): Must have a comment starting with `// DoThing`.
4. **Exported constants and vars** in standalone declarations: Must have a comment. Grouped `const` or `var` blocks need a block comment or individual comments.

### What to skip

- Test files (`_test.go`)
- Generated files (check for `// Code generated` header)
- `board` package (dead code)
- `arch_test` package itself

### Detection logic

Using `go/ast`:

1. Parse each file with `parser.ParseComments`
2. Walk top-level declarations
3. For each exported symbol, check `ast.CommentGroup` associated with the declaration
4. Verify the comment text starts with `// SymbolName` (the Go convention)

### Test function

`TestExportedSymbolsHaveGoDoc(t *testing.T)`:
- For each internal package, scan all non-test `.go` files
- For each exported symbol, verify it has a GoDoc comment
- Fail with: `"%s:%d: exported %s %s has no GoDoc comment"`
- Subtests per package for clear output

### Allowlist

Some exported symbols may intentionally lack docs (e.g., trivially self-documenting). Maintain a small allowlist:

```go
var docExemptions = map[string][]string{
    // "package": {"SymbolName1", "SymbolName2"},
}
```

Keep this list as small as possible. The goal is near-complete coverage.

## Files

- `internal/arch_test/godoc_test.go` — GoDoc comment enforcement tests (new file)

## Acceptance Criteria

- [ ] All current exported symbols either have GoDoc or are explicitly exempted
- [ ] Adding an exported function without a GoDoc comment fails the test
- [ ] Comment content is verified to start with the symbol name (not just any comment)
- [ ] Generated files are correctly skipped
- [ ] `go test ./internal/arch_test/...` passes
