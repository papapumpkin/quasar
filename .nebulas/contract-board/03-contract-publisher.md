+++
id = "contract-publisher"
title = "Post-phase hook that extracts and publishes contracts"
type = "feature"
priority = 2
depends_on = ["board-store", "contract-model"]
scope = ["internal/board/publisher.go", "internal/board/publisher_test.go"]
+++

## Problem

After a phase completes its coder-reviewer loop and commits, its outputs need to be captured as contracts on the board. This extraction must be automated — we can't rely on the phase's LLM agent to explicitly post contracts, because that would require modifying the agent prompt and coupling the loop to the board.

## Solution

Create a `Publisher` that runs as a post-phase hook. It examines the git diff of the completed phase and extracts contracts from the changed files.

### Extraction Strategy

1. **Get changed files**: Run `git diff --name-only <before-sha>..<after-sha>` to find files the phase touched.
2. **Parse Go files**: For each `.go` file changed, use `go/parser` and `go/ast` to extract exported symbols:
   - Exported type definitions → `ContractType`
   - Exported function declarations → `ContractFunction`
   - Exported interface types → `ContractInterface`
   - Exported methods → `ContractMethod`
3. **File contracts**: Every modified file gets a `ContractFile` entry.
4. **Publish**: Batch-insert all extracted contracts into the board via `Board.PublishContracts`.

```go
// Publisher extracts and publishes contracts from completed phases.
type Publisher struct {
    Board   Board
    WorkDir string
}

// PublishPhase extracts contracts from the git diff of a completed phase
// and writes them to the board.
func (p *Publisher) PublishPhase(ctx context.Context, phaseID, beforeSHA, afterSHA string) error
```

The publisher uses `go/parser.ParseFile` with `parser.ParseComments` to get the AST, then walks exported declarations. It does NOT need full type-checking — signatures are extracted from source text (the declaration line), not resolved types. This keeps it fast and dependency-free.

For non-Go files (TOML, markdown, etc.), only `ContractFile` entries are created.

### File Claims

The publisher also claims all files the phase modified via `Board.ClaimFile`. If a file is already claimed by another phase, the publisher logs a warning but does not fail — the claim represents the most recent owner.

## Files

- `internal/board/publisher.go` — Publisher struct, PublishPhase method, AST extraction
- `internal/board/publisher_test.go` — Tests with fixture Go files and mock Board

## Acceptance Criteria

- [ ] Publisher extracts exported types, functions, interfaces, and methods from changed `.go` files
- [ ] Non-Go files produce ContractFile entries only
- [ ] Contracts are batch-published to the board
- [ ] File claims are updated for all modified files
- [ ] Extraction uses `go/parser` and `go/ast` — no external dependencies
- [ ] Handles parse errors gracefully (logs warning, skips file, continues)
- [ ] `go test ./internal/board/...` passes
