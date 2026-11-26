//go:build unix

package chez

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runWithBackgroundSleep runs a shell that starts a background sleep, records its pid
// and waits, under a short timeout. It returns the sleep's pid; a cleanup kills it.
func runWithBackgroundSleep(t *testing.T, group bool) int {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_ = ExecRunner{}.Run(ctx, Cmd{
		Args:  []string{"sh", "-c", `sleep 30 & echo $! > "$1"; wait`, "sh", pidFile},
		Group: group,
	})
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("parse pid %q: %v", b, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

func gone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// TestExecRunnerGroupKillsTree: with Group the background grandchild dies with the child.
func TestExecRunnerGroupKillsTree(t *testing.T) {
	t.Parallel()
	pid := runWithBackgroundSleep(t, true)
	deadline := time.Now().Add(5 * time.Second)
	for !gone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still alive after Run returned", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestExecRunnerNoGroupRuns: without Group the runner behaves as before (only the
// direct child is killed); the stray sleep is killed by the cleanup.
func TestExecRunnerNoGroupRuns(t *testing.T) {
	t.Parallel()
	if pid := runWithBackgroundSleep(t, false); pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
}
