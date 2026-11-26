//go:build unix

package vaultcheck

import (
	"errors"
	"os/exec"
	"syscall"
)

// signalExit is the shell convention 128+N for a child that was killed by signal N.
func signalExit(err error) (int, bool) {
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return 0, false
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return 0, false
	}
	return 128 + int(ws.Signal()), true
}
