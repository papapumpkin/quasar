//go:build windows

package claude

import "syscall"

// sessionAttr returns an empty SysProcAttr on Windows where Setsid is not available.
func sessionAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
