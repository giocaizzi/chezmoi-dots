//go:build unix

package vaultcheck_test

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
)

// A child killed by SIGTERM exits 143 (128+15), as a shell reports it.
func TestExecRunChildKilledBySignal(t *testing.T) {
	t.Parallel()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	r := newRig()
	r.exec.Runner = chez.ExecRunner{}
	r.exec.LookPath = func(string) (string, error) { return sh, nil }
	err = r.exec.Run(t.Context(), registry("bws", nil), "example-vault", []string{"-c", "kill -TERM $$"}, 0)
	var ec interface{ ExitCode() int }
	if !errors.As(err, &ec) || ec.ExitCode() != 143 {
		t.Errorf("error = %v, want ExitCode 143", err)
	}
}
