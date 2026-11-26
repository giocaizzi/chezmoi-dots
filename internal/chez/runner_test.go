package chez

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestExecRunnerEnvOverridesInherited: an entry in Cmd.Env wins over the same variable
// inherited from the parent (a stale exported BWS_ACCESS_TOKEN must not reach the child).
// Serial: t.Setenv. The child is this test binary, so it runs on every OS.
func TestExecRunnerEnvOverridesInherited(t *testing.T) {
	if os.Getenv("CHEZ_TEST_PRINT_ENV") == "1" {
		_, _ = os.Stdout.WriteString(os.Getenv("BWS_ACCESS_TOKEN"))
		os.Exit(0) // helper process: end before the test framework prints
	}
	t.Setenv("BWS_ACCESS_TOKEN", "inherited")
	var out bytes.Buffer
	err := ExecRunner{}.Run(t.Context(), Cmd{
		Args:   []string{os.Args[0], "-test.run=^TestExecRunnerEnvOverridesInherited$"},
		Env:    []string{"CHEZ_TEST_PRINT_ENV=1", "BWS_ACCESS_TOKEN=injected"},
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if got := out.String(); got != "injected" {
		t.Errorf("child BWS_ACCESS_TOKEN = %q, want %q", got, "injected")
	}
}

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
