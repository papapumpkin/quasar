+++
id = "watcher-remove-dead-check"
title = "Remove unreachable .toml guard in watcher.isPhaseFile"
type = "task"
priority = 3
scope = ["internal/nebula/watcher.go"]
+++

## Problem

In `internal/nebula/watcher.go`, the `isPhaseFile` method has dead code:

```go
func (w *Watcher) isPhaseFile(name string) bool {
    base := filepath.Base(name)
    if !strings.HasSuffix(base, ".md") {
        return false
    }
    // These checks are unreachable — .toml files already fail the .md suffix check
    if base == "nebula.toml" || base == "nebula.state.toml" {
        return false
    }
    return true
}
```

The `.toml` guard is unreachable because the `.md` suffix check already filters them out. This dead code is confusing to readers.

## Solution

Remove the unreachable `.toml` check. The function becomes a simple one-liner:

```go
func (w *Watcher) isPhaseFile(name string) bool {
    return strings.HasSuffix(filepath.Base(name), ".md")
}
```

## Files

- `internal/nebula/watcher.go` — simplify `isPhaseFile`

## Acceptance Criteria

- [ ] Unreachable `.toml` guard is removed
- [ ] `isPhaseFile` still correctly identifies `.md` files
- [ ] `go test ./...` passes
