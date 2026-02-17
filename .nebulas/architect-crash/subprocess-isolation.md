+++
id = "subprocess-isolation"
title = "Add subprocess terminal isolation via Setsid"
type = "bug"
priority = 2
+++

## Bug

The `claude` subprocess spawned by `Invoke()` inherits the parent's session. While stdout/stderr are captured via pipes, the subprocess could access the controlling terminal via `/dev/tty`, potentially interfering with BubbleTea's terminal management.

## Root Cause

`exec.CommandContext` in `internal/claude/claude.go` creates the subprocess without setting `SysProcAttr`. The child process shares the parent's session and controlling terminal.

## Fix

### 1. Create `internal/claude/setsid_unix.go`

```go
//go:build !windows

package claude

import "syscall"

// sessionAttr returns SysProcAttr that places the subprocess in its own session,
// preventing it from accessing the parent's controlling terminal.
func sessionAttr() *syscall.SysProcAttr {
    return &syscall.SysProcAttr{Setsid: true}
}
```

### 2. Create `internal/claude/setsid_windows.go`

```go
//go:build windows

package claude

import "syscall"

// sessionAttr returns an empty SysProcAttr on Windows where Setsid is not available.
func sessionAttr() *syscall.SysProcAttr {
    return &syscall.SysProcAttr{}
}
```

### 3. Set `SysProcAttr` in `Invoke()` in `internal/claude/claude.go`

After creating the `cmd` via `exec.CommandContext`, add:
```go
cmd.SysProcAttr = sessionAttr()
```

This goes right after `cmd.Dir = workDir` (line ~69).

## Files

- `internal/claude/claude.go` — add `cmd.SysProcAttr = sessionAttr()` to `Invoke()`
- `internal/claude/setsid_unix.go` — new file, build-tagged `!windows`
- `internal/claude/setsid_windows.go` — new file, build-tagged `windows`
