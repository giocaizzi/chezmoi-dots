package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

func testGetenv(key string) string {
	switch key {
	case "NO_COLOR":
		return "1"
	case "CHEZMOI_SOURCE_DIR", "XDG_CONFIG_HOME":
		return filepath.Join("testdata", "missing-source")
	case "CHEZMOI_EXECUTABLE":
		return filepath.Join("testdata", "missing-chezmoi")
	}
	return ""
}

func TestRunHelp(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}, {"view", "--help"}, {"audit", "aliases", "-h"}, {"help", "live"}, {"help", "help"}, {"version", "-h"}, {"version", "--help"}, {"version", "extra", "--help"}, {"audit", "--help", "aliases"}, {"audit", "aliases", "--help"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			if code := run(t.Context(), args, testGetenv, strings.NewReader(""), &out, &errw); code != 0 {
				t.Errorf("exit = %d, want 0", code)
			}
			if !strings.Contains(out.String(), "USAGE") || errw.Len() != 0 {
				t.Errorf("stdout %q, stderr %q", out.String(), errw.String())
			}
		})
	}
}

func TestRunVersionIgnoredArguments(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"version", "--unknown"}, {"version", "--as=linux"}, {"version", "extra"}, {"version", "--unknown", "--help"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			if code := run(t.Context(), args, testGetenv, strings.NewReader(""), &out, &errw); code != 0 {
				t.Errorf("exit = %d, want 0", code)
			}
			if !strings.HasPrefix(out.String(), "chezmoi-dots ") || strings.Contains(out.String(), "USAGE") || errw.Len() != 0 {
				t.Errorf("stdout %q, stderr %q; want version only", out.String(), errw.String())
			}
		})
	}
}

func TestRunUsageNoDuplicateDiagnostic(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"list", "pattern"}, {"view", "--unknown"}, {"audit"}, {"help", "audit", "unknown"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw, cliOut, cliErr bytes.Buffer
			d := cli.Deps{Getenv: testGetenv, Stdout: &cliOut, Stderr: &cliErr}
			_ = cli.Run(t.Context(), d, args)
			if code := run(t.Context(), args, testGetenv, strings.NewReader(""), &out, &errw); code != 2 {
				t.Errorf("exit = %d, want 2", code)
			}
			if out.Len() != 0 || strings.Count(errw.String(), "USAGE") != 1 {
				t.Errorf("stdout %q, stderr %q; want one stderr help", out.String(), errw.String())
			}
			if errw.String() != cliErr.String() {
				t.Errorf("main adds output to CLI usage report:\nCLI: %q\nmain: %q", cliErr.String(), errw.String())
			}
		})
	}
}

func TestRunOperationalError(t *testing.T) {
	t.Parallel()
	var out, errw bytes.Buffer
	if code := run(t.Context(), []string{"list"}, testGetenv, strings.NewReader(""), &out, &errw); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if out.Len() != 0 || errw.Len() == 0 || strings.Contains(errw.String(), "USAGE") {
		t.Errorf("stdout %q, stderr %q", out.String(), errw.String())
	}
}

func TestRunCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, errw bytes.Buffer
	if code := run(ctx, []string{"list"}, testGetenv, strings.NewReader(""), &out, &errw); code != 130 {
		t.Errorf("exit = %d, want 130", code)
	}
}
