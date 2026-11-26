//go:build windows

package chez

import (
	"context"
	"testing"
	"time"
)

// TestExecRunnerGroupReturnsPromptly: with Group and a short timeout, Run kills the tree
// and returns well before the child's 30 s. Checking the grandchild's pid is not
// reliable through cmd's start /b, so this asserts the prompt return only.
func TestExecRunnerGroupReturnsPromptly(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := ExecRunner{}.Run(ctx, Cmd{
		Args:  []string{"cmd", "/c", "ping -n 30 127.0.0.1 >nul"},
		Group: true,
	})
	if err == nil {
		t.Error("Run() = nil, want a timeout error")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("Run took %v, want it killed promptly", d)
	}
}
