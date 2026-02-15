+++
id = "add-godoc"
title = "Add GoDoc comments to all exported symbols"
type = "task"
priority = 3
depends_on = ["extract-ui-interface", "refactor-runloop"]
+++

## Problem

Many exported types, functions, and methods lack GoDoc comments. Go convention requires every exported symbol to have a doc comment starting with the symbol name. Examples of missing docs:

- `ui.CycleSummaryData` and its fields
- `loop.CycleState`, `loop.Phase`, `loop.ReviewFinding`
- `agent.Role`, `agent.RoleCoder`, `agent.RoleReviewer`
- `config.Config` fields
- `nebula.Manifest`, `nebula.TaskSpec`, `nebula.State`, `nebula.WorkerGroup`
- Several `Printer` methods

## Solution

Add concise GoDoc comments following the `// SymbolName does X.` convention. Do not over-document obvious fields — focus on types, interfaces, and public methods where the purpose isn't self-evident.

## Files to Modify

- `internal/ui/ui.go` — Document `CycleSummaryData`, undocumented methods
- `internal/loop/state.go` — Document `Phase`, `CycleState`, `ReviewFinding`
- `internal/loop/loop.go` — Document `Loop`, `TaskResult`
- `internal/agent/agent.go` — Document `Role`, `Agent`, `InvocationResult`
- `internal/config/config.go` — Document `Config`
- `internal/nebula/types.go` — Document all exported types

## Acceptance Criteria

- [ ] Every exported type, interface, function, and method has a GoDoc comment
- [ ] Comments follow `// Name does/is/holds ...` convention
- [ ] `go vet ./...` passes
- [ ] No comments on unexported symbols unless the logic is non-obvious
