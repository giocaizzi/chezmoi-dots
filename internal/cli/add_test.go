package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		"\n[data]\n    work = false\n    light = false\n    harnesses = []\n"
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
