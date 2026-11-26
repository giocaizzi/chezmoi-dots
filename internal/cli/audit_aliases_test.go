package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

// aliasShells answers the alias probe of each shell with a fixed list.
type aliasShells map[string]string

func (s aliasShells) Run(_ context.Context, c chez.Cmd) error {
	out, ok := s[c.Args[0]]
	if !ok {
		return errNotScripted
	}
	_, err := c.Stdout.Write([]byte(out))
	return err
}

// aliasConfig is a config dir whose rendered aliases.sh defines ll, or an empty one.
func aliasConfig(t *testing.T, rendered bool) string {
	t.Helper()
	cfg := t.TempDir()
	if rendered {
		dir := filepath.Join(cfg, "shell")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "aliases.sh"), []byte("alias ll='ls -l'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func runAuditAliases(t *testing.T, cfg string, runner chez.Runner, lookPath func(string) (string, error)) (string, error) {
	t.Helper()
	var out, errw bytes.Buffer
	d := cli.Deps{
		Getenv: func(k string) string {
			if k == "XDG_CONFIG_HOME" {
				return cfg
			}
			return ""
		},
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errw,
		Runner: runner, LookPath: lookPath,
		GOOS: runtime.GOOS, Home: t.TempDir(), Version: "test",
	}
	err := cli.Run(t.Context(), d, []string{"audit", "aliases"})
	return out.String(), err
}

func bothShells(name string) (string, error) {
	if name == "zsh" || name == "bash" {
		return "/fake/" + name, nil
	}
	return "", errors.New("not found")
}

func TestAuditAliases(t *testing.T) {
	t.Parallel()
	t.Run("every alias is in the registry", func(t *testing.T) {
		t.Parallel()
		out, err := runAuditAliases(t, aliasConfig(t, true), aliasShells{"zsh": "ll\n", "bash": "ll\n"}, bothShells)
		if err != nil {
			t.Fatalf("audit aliases = %v", err)
		}
		for _, want := range []string{"zsh: all aliases accounted for", "bash: all aliases accounted for"} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q:\n%s", want, out)
			}
		}
	})
	t.Run("an alias outside the registry is reported", func(t *testing.T) {
		t.Parallel()
		out, err := runAuditAliases(t, aliasConfig(t, true), aliasShells{"zsh": "ll\nzz\n", "bash": "ll\n"}, bothShells)
		if !errors.Is(err, cli.ErrReported) {
			t.Fatalf("audit aliases = %v, want ErrReported", err)
		}
		if !strings.Contains(out, "aliases not in the registry:\n  zz\n") {
			t.Errorf("stdout does not list zz under the registry heading:\n%s", out)
		}
	})
	t.Run("a missing rendered alias file is an error, not a finding", func(t *testing.T) {
		t.Parallel()
		_, err := runAuditAliases(t, aliasConfig(t, false), aliasShells{}, bothShells)
		if err == nil || errors.Is(err, cli.ErrReported) || !strings.Contains(err.Error(), "chezmoi apply?") {
			t.Fatalf("audit aliases = %v, want a read error suggesting chezmoi apply", err)
		}
	})
}

func TestAuditAliasesBashWithoutAliases(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("bash is not a system shell on Windows")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not on PATH")
	}
	lookBash := func(name string) (string, error) {
		if name == "bash" {
			return exec.LookPath(name)
		}
		return "", errors.New("not found")
	}
	out, err := runAuditAliases(t, aliasConfig(t, true), sandboxRunner{sandboxEnv(t.TempDir())}, lookBash)
	if err != nil {
		t.Fatalf("audit aliases = %v, want bash without aliases to pass", err)
	}
	if !strings.Contains(out, "bash: all aliases accounted for") {
		t.Errorf("stdout lacks the bash line:\n%s", out)
	}
}
