//go:build !windows

package claude

import "syscall"

// sessionAttr returns SysProcAttr that places the subprocess in its own session,
// preventing it from accessing the parent's controlling terminal.
func sessionAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
