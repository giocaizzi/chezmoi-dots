//go:build unix

package chez

import (
	"errors"
	"os/exec"
	"syscall"
)

// groupCmd makes the child lead a new process group and, on cancellation, kills the
// whole group.
func groupCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// A negative pid addresses the group whose id is the leader's pid.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == nil || errors.Is(err, syscall.ESRCH) {
			return nil // killed, or already gone
		}
		return cmd.Process.Kill()
	}
}
