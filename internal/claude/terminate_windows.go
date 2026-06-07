//go:build windows

package claude

import (
	"os/exec"
	"syscall"
)

// signalSubprocess terminates the subprocess. Windows has no SIGTERM/SIGKILL
// distinction nor process groups via Setsid, so both the graceful and forced
// paths call Process.Kill — the OS-level terminate.
func signalSubprocess(cmd *exec.Cmd, _ syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// termSignal returns a placeholder signal; on Windows signalSubprocess ignores
// it and calls Kill regardless.
func termSignal() syscall.Signal { return syscall.Signal(0) }

// killSignal returns a placeholder signal; on Windows signalSubprocess ignores
// it and calls Kill regardless.
func killSignal() syscall.Signal { return syscall.Signal(0) }
