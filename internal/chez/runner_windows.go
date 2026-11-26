//go:build windows

package chez

import (
	"os/exec"
	"strconv"
)

// groupCmd makes cancellation kill the child's whole process tree.
func groupCmd(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		// taskkill /T walks the tree; its output and status do not matter, the
		// direct kill below reports the outcome.
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run() //nolint:noctx // fixed program, numeric pid; runs during cancellation
		return cmd.Process.Kill()
	}
}
