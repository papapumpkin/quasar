//go:build !windows

package claude

import (
	"os/exec"
	"syscall"
)

// signalSubprocess sends sig to the subprocess. Because the coder runs in its
// own session (see sessionAttr / Setsid), we signal the whole process group
// (negative PID) so child processes the coder spawned — shells, go test, etc. —
// are torn down too rather than orphaned. Falls back to signaling just the
// process if the group send fails.
func signalSubprocess(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, sig); err != nil {
		return cmd.Process.Signal(sig)
	}
	return nil
}

// termSignal returns the graceful-termination signal for this platform.
func termSignal() syscall.Signal { return syscall.SIGTERM }

// killSignal returns the force-kill signal for this platform.
func killSignal() syscall.Signal { return syscall.SIGKILL }
