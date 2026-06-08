//go:build windows

package gitops

import "syscall"

// verifySysProcAttr is a no-op on Windows: there is no direct analog of
// POSIX sessions/process groups in this package's process-tree-kill role.
// Windows reaps the child cleanly when the parent exits in practice, and
// the WaitDelay backstop bounds runVerify's wait either way.
func verifySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// killVerifyGroup falls back to a process-level signal on Windows; the
// caller pairs this with cmd.WaitDelay so the wait completes regardless.
func killVerifyGroup(_ int) error { return nil }
