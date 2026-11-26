package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

func (e *env) file(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// isolate moves the chezmoi config out of the directory that holds the destination:
// chezmoi refuses to add files below the directory of its own config file.
func (e *env) isolate(t *testing.T) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(e.src), "cfg")
	cfg := "destDir = " + quote(e.dest) + "\npersistentState = " + quote(filepath.Join(dir, "state.boltdb")) +
		"\n[data]\n    work = false\n    light = false\n    tools = []\n"
	e.vars["CHEZMOI_CONFIG_FILE"] = e.file(t, dir, "chezmoi.toml", cfg)
}

func (e *env) registryText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.src, ".chezmoidata", "edit.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAddUnmanagedFile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.isolate(t)
	target := e.file(t, e.dest, ".gitconfig-extra", "[user]\n")

	stdout, stderr, err := e.run(t, "", "add", target, "--desc", "Extra git config")
	if err != nil {
		t.Fatalf("add = %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "added gitconfig-extra: dot_gitconfig-extra") {
		t.Errorf("stdout = %q", stdout)
	}
	if _, serr := os.Stat(filepath.Join(e.src, "dot_gitconfig-extra")); serr != nil {
		t.Errorf("chezmoi add did not create the source: %v", serr)
	}
	text := e.registryText(t)
	for _, want := range []string{"[edit.gitconfig-extra]", `desc = "Extra git config"`, `source = "dot_gitconfig-extra"`} {
		if !strings.Contains(text, want) {
			t.Errorf("registry lacks %q:\n%s", want, text)
		}
	}

	stdout, stderr, err = e.run(t, "", "add", target, "--desc", "again")
	if err != nil {
		t.Fatalf("second add = %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "is already registered as gitconfig-extra") {
		t.Errorf("stdout = %q", stdout)
	}
	if got := e.registryText(t); got != text {
		t.Errorf("registry changed on a repeated add:\n%s", got)
	}
}

// The registry file needs no entry for itself: the first add on a source whose edit.toml
// has none succeeds.
func TestAddFirstEntryNeedsNoRegistryEntry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.isolate(t)
	reg := filepath.Join(e.src, ".chezmoidata", "edit.toml")
	e.file(t, filepath.Join(e.src, ".chezmoidata"), "edit.toml", "[edit.note]\ndesc = \"A note\"\nsource = \"dot_note.tmpl\"\n")
	target := e.file(t, e.dest, ".first-extra", "x\n")

	if _, stderr, err := e.run(t, "", "add", target, "--desc", "First"); err != nil {
		t.Fatalf("first add = %v\n%s", err, stderr)
	}
	b, err := os.ReadFile(reg)
	if err != nil || !strings.Contains(string(b), "[edit.first-extra]") || strings.Contains(string(b), `source = ".chezmoidata/edit.toml"`) {
		t.Errorf("registry = %q, %v", b, err)
	}
}

func TestAddTemplate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.isolate(t)
	target := e.file(t, e.dest, ".tool.conf", "x = 1\n")

	_, stderr, err := e.run(t, "", "add", target, "--template", "--name", "tool", "--desc", "Tool")
	if err != nil {
		t.Fatalf("add = %v\n%s", err, stderr)
	}
	if !strings.Contains(e.registryText(t), `source = "dot_tool.conf.tmpl"`) {
		t.Errorf("registry:\n%s", e.registryText(t))
	}
	_, _, err = e.run(t, "", "add", target, "--template", "--name", "tool2", "--desc", "Tool")
	if err == nil || !strings.Contains(err.Error(), "already managed") {
		t.Errorf("template on a managed file = %v", err)
	}
}

func TestAddErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.isolate(t)
	missing := filepath.Join(e.dest, ".missing")
	file := func(n string) string { return e.file(t, e.dest, "."+n, "x\n") }

	tests := []struct {
		name  string
		args  func(file string) []string
		usage bool
	}{
		{"no path", func(string) []string { return []string{"add"} }, true},
		{"local with template", func(f string) []string { return []string{"add", f, "--local", "--template"} }, true},
		{"bad name", func(f string) []string { return []string{"add", f, "--name", "a b", "--desc", "d"} }, true},
		{"no desc without a terminal", func(f string) []string { return []string{"add", f} }, true},
		{"missing path", func(string) []string { return []string{"add", missing, "--desc", "d"} }, false},
		{"taken name", func(f string) []string { return []string{"add", f, "--name", "note", "--desc", "d"} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := e.run(t, "", tt.args(file(strings.ReplaceAll(tt.name, " ", "-")))...)
			if err == nil {
				t.Fatal("add succeeded")
			}
			if got := errors.Is(err, cli.ErrUsage); got != tt.usage {
				t.Errorf("usage error = %v, want %v (%v)", got, tt.usage, err)
			}
			// A rejected add leaves the source untouched: chezmoi add runs last.
			if m, _ := filepath.Glob(filepath.Join(e.src, "dot_"+strings.ReplaceAll(tt.name, " ", "-")+"*")); len(m) > 0 {
				t.Errorf("source changed by a rejected add: %v", m)
			}
		})
	}
}

func TestAddDirectory(t *testing.T) {
	t.Parallel()
	t.Run("add <dir> without --local is rejected before anything changes", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.isolate(t)
		e.file(t, e.dest, filepath.Join(".config", "example", "a.conf"), "a\n")
		dir := filepath.Join(e.dest, ".config", "example")
		before := e.registryText(t)

		_, _, err := e.run(t, "", "add", dir, "--desc", "Example folder")
		if !errors.Is(err, cli.ErrUsage) || !strings.Contains(err.Error(), "use --local") {
			t.Errorf("add of a directory = %v, want a usage error suggesting --local", err)
		}
		if got := e.registryText(t); got != before {
			t.Errorf("registry changed:\n%s", got)
		}
		if _, serr := os.Stat(filepath.Join(e.src, "dot_config")); serr == nil {
			t.Error("a rejected add ran chezmoi add on the directory")
		}
	})
	t.Run("a symlink to a folder is a file for chezmoi add, not a folder", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.isolate(t)
		e.file(t, e.dest, filepath.Join("real", "a.conf"), "a\n")
		link := filepath.Join(e.dest, ".linkdir")
		if err := os.Symlink(filepath.Join(e.dest, "real"), link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		if _, stderr, err := e.run(t, "", "add", link, "--desc", "Link"); err != nil {
			t.Fatalf("add of a symlink to a folder = %v\n%s", err, stderr)
		}
		if !strings.Contains(e.registryText(t), `source = "symlink_dot_linkdir"`) {
			t.Errorf("registry:\n%s", e.registryText(t))
		}
	})
}

func TestAddLocal(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.isolate(t)
	file := e.file(t, e.vars["HOME"], filepath.Join(".config", "app", "settings.json"), "{}\n")

	stdout, stderr, err := e.run(t, "", "add", "--local", file, "--desc", "App settings")
	if err != nil {
		t.Fatalf("add = %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "added app: .config/app") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(e.registryText(t), `local = ".config/app"`) {
		t.Errorf("registry:\n%s", e.registryText(t))
	}
	stdout, _, err = e.run(t, "", "add", "--local", filepath.Dir(file), "--desc", "x")
	if err != nil || !strings.Contains(stdout, "already registered as app") {
		t.Errorf("repeat = %v, %q", err, stdout)
	}
}

func TestReadLine(t *testing.T) {
	t.Parallel()
	t.Run("cancelled context returns promptly", func(t *testing.T) {
		t.Parallel()
		pr, pw := io.Pipe()
		t.Cleanup(func() { _ = pw.Close(); _ = pr.Close() })
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := cli.ReadLine(ctx, pr); done <- err }()
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("ReadLine() error = %v, want context.Canceled", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("ReadLine did not return after the context was cancelled")
		}
	})
	t.Run("a written line", func(t *testing.T) {
		t.Parallel()
		got, err := cli.ReadLine(t.Context(), strings.NewReader("my desc\nrest\n"))
		if err != nil || got != "my desc\n" {
			t.Errorf("ReadLine() = %q, %v", got, err)
		}
	})
	t.Run("end of input", func(t *testing.T) {
		t.Parallel()
		if _, err := cli.ReadLine(t.Context(), strings.NewReader("")); !errors.Is(err, io.EOF) {
			t.Errorf("ReadLine() error = %v, want io.EOF", err)
		}
	})
}

// gitEnv turns the source dir into a repository with one seed commit and switches
// chezmoi's autoCommit on. It skips when git is missing.
func (e *env) gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	e.git(t, "init", "-q")
	for _, kv := range [][2]string{{"user.name", "Example User"}, {"user.email", "user@example.com"}, {"commit.gpgsign", "false"}} {
		e.git(t, "config", kv[0], kv[1])
	}
	e.git(t, "add", "-A")
	e.git(t, "commit", "-q", "-m", "chore: seed")
	cfg := e.vars["CHEZMOI_CONFIG_FILE"]
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg, append(b, "\n[git]\n    autoCommit = true\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *env) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", e.src}, args...)...)
	cmd.Env = append(os.Environ(), gitEnv(e.sandbox)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// wantCommitted checks that the source is clean and HEAD, the one commit after the
// seed, holds files.
func (e *env) wantCommitted(t *testing.T, files ...string) {
	t.Helper()
	if st := e.git(t, "status", "--porcelain"); st != "" {
		t.Errorf("source is not clean:\n%s", st)
	}
	if n := strings.TrimSpace(e.git(t, "rev-list", "--count", "HEAD")); n != "2" {
		t.Errorf("commits = %s, want the seed and one more", n)
	}
	head := e.git(t, "show", "--name-only", "--format=", "HEAD")
	for _, f := range files {
		if !strings.Contains(head, f) {
			t.Errorf("HEAD lacks %s:\n%s", f, head)
		}
	}
}

func TestAddGitAutoCommit(t *testing.T) {
	t.Parallel()
	t.Run("unmanaged file: source and entry in one commit", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.isolate(t)
		e.gitEnv(t)
		target := e.file(t, e.dest, ".gitconfig-extra", "[user]\n")

		if _, stderr, err := e.run(t, "", "add", target, "--desc", "Extra"); err != nil {
			t.Fatalf("add = %v\n%s", err, stderr)
		}
		e.wantCommitted(t, "dot_gitconfig-extra", ".chezmoidata/edit.toml")
	})
	t.Run("managed file: only the registry is committed", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.isolate(t)
		target := e.file(t, e.dest, ".managed", "x\n")
		if _, stderr, err := e.run(t, "", "add", target, "--desc", "Managed"); err != nil {
			t.Fatalf("seed add = %v\n%s", err, stderr)
		}
		// Drop the entry, so the file is managed but unregistered; the seed commit holds both.
		reg := filepath.Join(e.src, ".chezmoidata", "edit.toml")
		kept, _, _ := strings.Cut(e.registryText(t), "[edit.managed]")
		if err := os.WriteFile(reg, []byte(kept), 0o644); err != nil {
			t.Fatal(err)
		}
		e.gitEnv(t)

		if _, stderr, err := e.run(t, "", "add", target, "--desc", "Managed"); err != nil {
			t.Fatalf("add = %v\n%s", err, stderr)
		}
		e.wantCommitted(t, ".chezmoidata/edit.toml")
		if head := e.git(t, "show", "--name-only", "--format=", "HEAD"); strings.Contains(head, "dot_managed") {
			t.Errorf("HEAD holds the source file too:\n%s", head)
		}
	})
	t.Run("encrypt without encryption fails and leaves the registry untouched", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.isolate(t)
		target := e.file(t, e.dest, ".secret", "x\n")
		before := e.registryText(t)

		_, _, err := e.run(t, "", "add", target, "--encrypt", "--desc", "Secret")
		if err == nil {
			t.Fatal("add --encrypt without encryption succeeded")
		}
		if got := e.registryText(t); got != before {
			t.Errorf("registry changed by a failed add:\n%s", got)
		}
	})
}

func TestAddGitAutoPushFailure(t *testing.T) {
	t.Parallel()
	t.Run("a failed autoPush keeps the committed entry", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.isolate(t)
		e.gitEnv(t)
		cfg := e.vars["CHEZMOI_CONFIG_FILE"]
		b, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		// No remote, so chezmoi commits the source file and its entry, then the push fails.
		if err = os.WriteFile(cfg, append(b, "    autoPush = true\n"...), 0o644); err != nil {
			t.Fatal(err)
		}
		target := e.file(t, e.dest, ".pushed", "x\n")

		if _, _, err = e.run(t, "", "add", target, "--desc", "Pushed"); err == nil {
			t.Fatal("add succeeded although the push failed")
		}
		// The commit holds the file and the entry; the restore must not undo the entry.
		e.wantCommitted(t, "dot_pushed", ".chezmoidata/edit.toml")
		if !strings.Contains(e.registryText(t), "[edit.pushed]") {
			t.Errorf("entry removed although chezmoi added and committed the file:\n%s", e.registryText(t))
		}
	})
}

// failAdd runs everything through chezmoi except the real add, which fails after
// the dry run planned it.
type failAdd struct{ chez.ExecRunner }

func (f failAdd) Run(ctx context.Context, c chez.Cmd) error {
	if slices.Contains(c.Args, "add") && !slices.Contains(c.Args, "--dry-run") {
		return errors.New("chezmoi add: boom")
	}
	return f.ExecRunner.Run(ctx, c)
}

func TestAddRestoresRegistryWhenChezmoiAddFails(t *testing.T) {
	t.Parallel()
	for _, existed := range []bool{true, false} {
		t.Run(fmt.Sprintf("registry existed %v", existed), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.isolate(t)
			reg := filepath.Join(e.src, ".chezmoidata", "edit.toml")
			before := e.registryText(t)
			if !existed {
				// Registry data lives in another file, so edit.toml is created by add.
				other := filepath.Join(e.src, ".chezmoidata", "other.toml")
				if err := os.Rename(reg, other); err != nil {
					t.Fatal(err)
				}
			}
			target := e.file(t, e.dest, ".fails", "x\n")
			var out, errw bytes.Buffer
			d := cli.Deps{
				Getenv: func(k string) string { return e.vars[k] },
				Stdin:  strings.NewReader(""), Stdout: &out, Stderr: &errw,
				Runner: failAdd{}, LookPath: exec.LookPath,
				GOOS: runtime.GOOS, Home: e.vars["HOME"], Version: "test",
			}
			err := cli.Run(t.Context(), d, []string{"add", target, "--desc", "Fails"})
			if err == nil || !strings.Contains(err.Error(), "boom") {
				t.Fatalf("add = %v, want the chezmoi add failure", err)
			}
			b, rerr := os.ReadFile(reg)
			switch {
			case existed && (rerr != nil || string(b) != before):
				t.Errorf("registry = %q, %v, want it restored", b, rerr)
			case !existed && rerr == nil:
				t.Errorf("registry created by add was not removed:\n%s", b)
			}
		})
	}
}

func TestAddWithoutDescOffATerminalIsAUsageError(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.isolate(t)
	target := e.file(t, e.dest, ".nodesc", "x\n")
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = null.Close() })
	var out, errw bytes.Buffer
	d := cli.Deps{
		Getenv: func(k string) string { return e.vars[k] },
		Stdin:  null, Stdout: &out, Stderr: &errw,
		Runner: sandboxRunner{sandboxEnv(e.sandbox)}, LookPath: exec.LookPath,
		GOOS: runtime.GOOS, Home: e.vars["HOME"], Version: "test",
	}
	err = cli.Run(t.Context(), d, []string{"add", target})
	const want = "--desc is required when stdin is not a terminal"
	if !errors.Is(err, cli.ErrUsage) || !strings.Contains(err.Error(), want) {
		t.Errorf("add = %v, want a usage error containing %q", err, want)
	}
	if strings.Contains(errw.String(), "Description for") {
		t.Errorf("a prompt was written:\n%s", errw.String())
	}
}
