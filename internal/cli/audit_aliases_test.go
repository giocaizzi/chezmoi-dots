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
	t.Run("a missing rendered alias file is skipped", func(t *testing.T) {
		t.Parallel()
		cfg := aliasConfig(t, false)
		out, err := runAuditAliases(t, cfg, aliasShells{}, bothShells)
		if err != nil {
			t.Fatalf("audit aliases = %v, want nil", err)
		}
		want := "aliases: skipped, no rendered alias file " + filepath.Join(cfg, "shell", "aliases.sh") + " (see chezmoi dots help audit)\n"
		if out != want {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	})
}

func TestAuditAliasesOmzNote(t *testing.T) {
	t.Parallel()
	t.Run("a missing omz file is noted", func(t *testing.T) {
		t.Parallel()
		cfg := aliasConfig(t, true)
		out, err := runAuditAliases(t, cfg, aliasShells{"zsh": "ll\n", "bash": "ll\n"}, bothShells)
		if err != nil {
			t.Fatalf("audit aliases = %v", err)
		}
		want := "aliases: note: " + filepath.Join(cfg, "shell", "omz-aliases.zsh") + " not read (missing); Oh My Zsh plugin aliases not included\n"
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	})
	t.Run("an unreadable omz file is noted with the error", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("file modes do not make a file unreadable on Windows")
		}
		cfg := aliasConfig(t, true)
		omz := filepath.Join(cfg, "shell", "omz-aliases.zsh")
		if err := os.WriteFile(omz, []byte("plugins=(git)\n"), 0o000); err != nil {
			t.Fatal(err)
		}
		if f, err := os.Open(omz); err == nil {
			_ = f.Close()
			t.Skip("file modes are not enforced for this user")
		}
		out, err := runAuditAliases(t, cfg, aliasShells{"zsh": "ll\n", "bash": "ll\n"}, bothShells)
		if err != nil {
			t.Fatalf("audit aliases = %v", err)
		}
		if !strings.Contains(out, "aliases: note: "+omz+" not read (") || strings.Contains(out, "(missing)") ||
			!strings.Contains(out, "); Oh My Zsh plugin aliases not included") {
			t.Errorf("stdout lacks the unreadable note:\n%s", out)
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
	sandbox := sandboxEnv(t.TempDir())
	// The sandbox HOME has no ~/.bashrc, but the host's system startup files still run
	// in an interactive bash: skip where they define aliases, so the test stays hermetic.
	probe := exec.CommandContext(t.Context(), "bash", "-ic", "compgen -a || true")
	probe.Env = append(os.Environ(), sandbox...)
	if got, err := probe.Output(); err != nil || len(bytes.TrimSpace(got)) > 0 {
		t.Skipf("host bash defines aliases in its system startup files (%q, %v)", got, err)
	}
	out, err := runAuditAliases(t, aliasConfig(t, true), sandboxRunner{sandbox}, lookBash)
	if err != nil {
		t.Fatalf("audit aliases = %v, want bash without aliases to pass", err)
	}
	if !strings.Contains(out, "bash: all aliases accounted for") {
		t.Errorf("stdout lacks the bash line:\n%s", out)
	}
}

// A missing rendered alias file skips the aliases check without stopping a plain audit.
func TestAuditContinuesToSecretsWithoutAliases(t *testing.T) {
	t.Parallel()
	e := secretsEnv(t, "")
	cfg := t.TempDir()
	e.vars["XDG_CONFIG_HOME"] = cfg
	stdout, stderr, err := e.runWith(t, &secretRunner{}, "audit")
	if err != nil {
		t.Fatalf("audit = %v\n%s", err, stderr)
	}
	for _, want := range []string{"aliases: skipped, no rendered alias file", "secrets: no entries declared"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}
