package chez

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerGrandchildHoldingPipe(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	var out bytes.Buffer
	start := time.Now()
	err := ExecRunner{}.Run(t.Context(), Cmd{Args: []string{"sh", "-c", "(sleep 30) & echo ok"}, Stdout: &out})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Run() took %v, want under 5s", d)
	}
	if got := strings.TrimSpace(out.String()); got != "ok" {
		t.Errorf("stdout = %q, want ok", got)
	}
}

func TestExecRunnerTimeoutWithGrandchild(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	start := time.Now()
	err := ExecRunner{}.Run(ctx, Cmd{Args: []string{"sh", "-c", "sleep 30 & wait"}, Stdout: &out})
	if err == nil {
		t.Fatal("Run() = nil, want an error")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("Run() took %v, want well under 30s", d)
	}
}
