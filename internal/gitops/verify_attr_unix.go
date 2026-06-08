//go:build !windows

package gitops

import "syscall"

// verifySysProcAttr places the verify subprocess in its own session (and thus
// its own process group). The session leader's negative PID is then a valid
// argument to syscall.Kill to terminate the entire subtree — without this,
// killing only the `sh -c "..."` parent leaves child processes orphaned
// holding the stdout/stderr pipes open, and exec.Cmd.Wait blocks until those
// orphans exit on their own.
func verifySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// killVerifyGroup sends SIGKILL to the process group led by pid. A negative
// pid in syscall.Kill targets the whole group, so any children sh spawned
// (a long-running go-test that itself spawned helpers, a wedged sleep) are
// reaped together with the shell.
func killVerifyGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
