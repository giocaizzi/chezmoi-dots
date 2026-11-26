package cli_test

import (
	"bytes"
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

// These tests drive the real command line against a real chezmoi and a small fixture
// source; they skip when chezmoi is not installed.

const configTemplate = `{{- $user := promptStringOnce . "gitUser" "Name" "" -}}
{{- $tools := promptMultichoiceOnce . "tools" "Tools" (list "alpha" "beta") (get . "tools" | default list) -}}
{{- $featureX := promptBoolOnce . "featureX" "Feature X?" false -}}
[data]
    gitUser = {{ $user | quote }}
    featureX = {{ $featureX }}
    tools = {{ $tools | toJson }}
{{- if eq .chezmoi.os "windows" }}
    winOnly = true
{{- end }}
`

const matrixDeclaration = "[matrix.profile.home]\nfeatureX = true\n[matrix.profile.minimal]\n"

const registry = `[edit.note]
desc = "A note"
source = "dot_note.tmpl"
check = ["chezmoi", "execute-template", "--file", "{file}"]
note = "reload the note"

[edit.registry]
desc = "The registry"
source = ".chezmoidata/edit.toml"
`

type env struct {
	src, dest string
	vars      map[string]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if _, err := exec.LookPath("chezmoi"); err != nil {
		t.Skip("chezmoi is not installed")
	}
	root := t.TempDir()
	e := &env{src: filepath.Join(root, "src"), dest: filepath.Join(root, "dest")}
	write := func(path, text string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The config template names the destination: a per-configuration config replaces the
	// machine config, as chezmoi init would store the template's output.
	write(filepath.Join(e.src, ".chezmoi.toml.tmpl"), "destDir = "+quote(e.dest)+"\n{{ \"\" }}"+configTemplate)
	write(filepath.Join(e.src, ".chezmoidata", "edit.toml"), registry)
	write(filepath.Join(e.src, "dot_note.tmpl"), "hello {{ if get . \"featureX\" }}feature{{ else }}home{{ end }}\n")
	cfg := filepath.Join(root, "chezmoi.toml")
	write(cfg, "destDir = "+quote(e.dest)+"\npersistentState = "+quote(filepath.Join(root, "state.boltdb"))+"\n[data]\n    featureX = false\n    tools = []\n")
	if err := os.MkdirAll(e.dest, 0o755); err != nil {
		t.Fatal(err)
	}
	e.vars = map[string]string{
		"CHEZMOI_EXECUTABLE":  "chezmoi",
		"CHEZMOI_SOURCE_DIR":  e.src,
		"CHEZMOI_CONFIG_FILE": cfg,
		"HOME":                root,
	}
	return e
}

func quote(p string) string { return `"` + filepath.ToSlash(p) + `"` }

func (e *env) run(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errw bytes.Buffer
	// A file, as on a terminal: the editor subprocess then shares it instead of
	// draining a reader that the confirmation prompt needs afterwards.
	pr, pw, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	t.Cleanup(func() { _ = pr.Close() })
	if _, perr = pw.WriteString(stdin); perr != nil {
		t.Fatal(perr)
	}
	_ = pw.Close()
	d := cli.Deps{
		Getenv: func(k string) string { return e.vars[k] },
		Stdin:  pr, Stdout: &out, Stderr: &errw,
		Runner: chez.ExecRunner{}, LookPath: exec.LookPath,
		GOOS: runtime.GOOS, Home: e.vars["HOME"], Version: "test",
	}
	err = cli.Run(t.Context(), d, args)
	return out.String(), errw.String(), err
}

// editor makes the test binary the editor: it appends text to the file it is given.
func (e *env) editor(t *testing.T, text string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.vars["EDITOR"] = "'" + exe + "' -test.run=TestHelperEditor -- e2e-helper '" + text + "'"
}

// TestHelperEditor is the editor of the edit tests, run as a subprocess.
func TestHelperEditor(t *testing.T) {
	t.Parallel()
	n := len(os.Args)
	if n < 3 || os.Args[n-3] != "e2e-helper" {
		t.Skip("helper process")
	}
	path := os.Args[n-1]
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(os.Args[n-2]); err != nil {
		t.Fatal(err)
	}
}

func TestListShowsTheRegistry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	out, _, err := e.run(t, "", "list")
	if err != nil || !strings.Contains(out, "note") || !strings.Contains(out, "A note") {
		t.Errorf("list = %q, %v, want the note entry", out, err)
	}
}

func TestViewRendersAConfiguration(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration)
	out, _, err := e.run(t, "", "view", filepath.Join(e.dest, ".note"), "--as", "linux,home")
	if err != nil || !strings.Contains(out, "hello feature") {
		t.Errorf("view --as linux,home = %q, %v, want the home render", out, err)
	}
	if out, _, err = e.run(t, "", "view", filepath.Join(e.dest, ".note"), "--as", "linux,minimal"); err != nil || !strings.Contains(out, "hello home") {
		t.Errorf("view --as linux,minimal = %q, %v, want the minimal render", out, err)
	}
}

func TestViewMatrixOverview(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	out, _, err := e.run(t, "", "view", "--matrix")
	if err != nil || !strings.Contains(strings.ToLower(out), "matrix overview") || !strings.Contains(out, "note") {
		t.Errorf("view --matrix overview = %q, %v, want matrix overview and note entry", out, err)
	}
}

func TestViewEntryMatrix(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	out, _, err := e.run(t, "", "view", "note", "--matrix")
	if err != nil || !strings.Contains(out, "darwin") || !strings.Contains(out, "linux") || !strings.Contains(out, "windows") {
		t.Errorf("view note --matrix = %q, %v, want grid rows (darwin, linux, windows)", out, err)
	}
}

func TestEditAppliesWhenConfirmed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.editor(t, "more")
	_, stderr, err := e.run(t, "y\n", "edit", "note")
	if err != nil {
		t.Fatalf("edit = %v\n%s", err, stderr)
	}
	got, _ := os.ReadFile(filepath.Join(e.dest, ".note"))
	if string(got) != "hello home\nmore" {
		t.Errorf("applied note = %q, want the edited render", got)
	}
	src, _ := os.ReadFile(filepath.Join(e.src, "dot_note.tmpl"))
	if !strings.HasSuffix(string(src), "more") {
		t.Errorf("source = %q, want the edit", src)
	}
	for _, want := range []string{"linux/default", "reload the note"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to mention %q", stderr, want)
		}
	}
}

func TestEditAppliesAndPushesUsingChezmoiConfig(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	remote := t.TempDir()
	git(t, remote, "-c", "init.templateDir=", "init", "--bare")
	git(t, e.src, "-c", "init.templateDir=", "init", "--initial-branch=main")
	git(t, e.src, "config", "user.name", "Example")
	git(t, e.src, "config", "user.email", "example@example.com")
	git(t, e.src, "config", "commit.gpgSign", "false")
	git(t, e.src, "config", "core.hooksPath", t.TempDir())
	git(t, e.src, "config", "push.default", "simple")
	git(t, e.src, "remote", "add", "origin", filepath.ToSlash(remote))
	git(t, e.src, "add", ".")
	git(t, e.src, "commit", "-m", "initial")
	git(t, e.src, "push", "--set-upstream", "origin", "main")

	const staged = "separately-staged.txt"
	if err := os.WriteFile(filepath.Join(e.src, staged), []byte("separate"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, e.src, "add", "--", staged)
	configureEditAutomation(t, e.vars["CHEZMOI_CONFIG_FILE"])
	e.editor(t, "more")

	_, stderr, err := e.run(t, "", "edit", "note")
	if err != nil {
		t.Fatalf("edit = %v\n%s", err, stderr)
	}
	if strings.Contains(stderr, "apply now?") {
		t.Errorf("stderr = %q, want automatic apply without confirmation", stderr)
	}
	if got, err := os.ReadFile(filepath.Join(e.dest, ".note")); err != nil || string(got) != "hello home\nmore" {
		t.Errorf("applied note = %q, %v, want the edited render", got, err)
	}
	if got := git(t, remote, "diff-tree", "--no-commit-id", "--name-only", "-r", "refs/heads/main"); got != "dot_note.tmpl" {
		t.Errorf("pushed paths = %q, want dot_note.tmpl", got)
	}
	if got, want := git(t, remote, "rev-parse", "refs/heads/main"), git(t, e.src, "rev-parse", "HEAD"); got != want {
		t.Errorf("remote HEAD = %q, want local HEAD %q", got, want)
	}
	if got := git(t, e.src, "diff", "--cached", "--name-only"); got != staged {
		t.Errorf("remaining staged paths = %q, want %q", got, staged)
	}
}

func configureEditAutomation(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteString("\n[edit]\napply = true\n[git]\nautoPush = true\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := chez.Cmd{Dir: dir, Args: append([]string{"git"}, args...)}
	out, err := chez.Output(t.Context(), chez.ExecRunner{}, cmd)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestEditDeclinedLeavesTheTarget(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.editor(t, "more")
	if _, stderr, err := e.run(t, "n\n", "edit", "note"); err != nil {
		t.Fatalf("edit = %v\n%s", err, stderr)
	}
	if _, err := os.Stat(filepath.Join(e.dest, ".note")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target exists after declining (stat error %v)", err)
	}
}

func TestEditWithABrokenTemplateAppliesNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.editor(t, "{{ if }}")
	_, stderr, err := e.run(t, "y\n", "edit", "note")
	if !errors.Is(err, cli.ErrReported) {
		t.Fatalf("edit = %v, want ErrReported\n%s", err, stderr)
	}
	if _, err := os.Stat(filepath.Join(e.dest, ".note")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target exists after a failed check (stat error %v)", err)
	}
	if !strings.Contains(stderr, "nothing applied") {
		t.Errorf("stderr = %q, want the nothing-applied notice", stderr)
	}
}

func TestLintRegistryOnTheFixture(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, stderr, err := e.run(t, "", "lint", "registry"); err != nil {
		t.Errorf("lint registry = %v\n%s", err, stderr)
	}
}

// The matrix declaration is optional: without one the matrix builds from the defaults.
func TestLintWithoutAMatrixDeclaration(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, stderr, err := e.run(t, "", "lint", "matrix", "templates"); err != nil {
		t.Errorf("lint matrix templates = %v\n%s", err, stderr)
	}
}

func (e *env) writeFile(t *testing.T, rel, text string) {
	t.Helper()
	path := filepath.Join(e.src, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLintMatrixInvalidDeclaration(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.writeFile(t, ".chezmoidata/matrix.toml", "[matrix.profile.Boss]\nfeatureX = true\n")
	_, stderr, err := e.run(t, "", "lint", "matrix")
	if err == nil || !strings.Contains(stderr, "matrix.profile.Boss") || !strings.Contains(stderr, "invalid profile name") {
		t.Errorf("lint matrix = %v, stderr %q, want a finding naming the invalid profile", err, stderr)
	}
}

// A profile answer is seeded into the config template, so a script gated on it breaks
// only for that profile.
func TestLintTemplatesSeedsProfileAnswers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration)
	e.writeFile(t, "dot_probe.json.tmpl", "{{ if .featureX }}{ broken{{ end }}")
	_, stderr, err := e.run(t, "", "lint", "templates")
	if err == nil || !strings.Contains(stderr, "(darwin/home)") || strings.Contains(stderr, "/minimal)") {
		t.Errorf("lint templates = %v, stderr %q, want findings for the home profile only", err, stderr)
	}
}

// The config template sees the OS it is rendered for, so an answer it derives from the
// OS reaches only that OS's configurations.
func TestLintTemplatesRendersConfigPerOS(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.writeFile(t, "dot_probe.json.tmpl", "{{ if get . \"winOnly\" }}{ broken{{ end }}")
	_, stderr, err := e.run(t, "", "lint", "templates")
	if err == nil || !strings.Contains(stderr, "(windows/default)") || strings.Contains(stderr, "(darwin/") || strings.Contains(stderr, "(linux/") {
		t.Errorf("lint templates = %v, stderr %q, want findings for the windows configuration only", err, stderr)
	}
}

func TestLintTemplatesVariant(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration+"[[matrix.variant]]\nname = \"one tool\"\nprofiles = [\"home\"]\nset = { tools = [\"alpha\"] }\n")
	e.writeFile(t, "dot_probe.json.tmpl", `{{ if has "alpha" (get . "tools" | default list) }}{ broken{{ end }}`)
	_, stderr, err := e.run(t, "", "lint", "templates")
	if err == nil || !strings.Contains(stderr, "(darwin/home [one tool])") || strings.Contains(stderr, "/minimal") || strings.Contains(stderr, "(darwin/home)") {
		t.Errorf("lint templates = %v, stderr %q, want findings for the home variant only", err, stderr)
	}
}

// A template .chezmoiignore leaves out of a configuration is not rendered there, so
// an answer declared empty keeps gated templates (secrets, network) out of the lint.
func TestLintTemplatesHonoursChezmoiignore(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration)
	e.writeFile(t, ".chezmoiignore", "{{ if not .featureX }}.probe.json\n{{ end }}")
	e.writeFile(t, "dot_probe.json.tmpl", "{ broken")
	_, stderr, err := e.run(t, "", "lint", "templates")
	if err == nil || !strings.Contains(stderr, "dot_probe.json.tmpl (darwin/home)") || strings.Contains(stderr, "/minimal)") {
		t.Errorf("lint templates = %v, stderr %q, want findings for the home profile only", err, stderr)
	}
}

// A configuration whose managed list does not render is a finding of that
// configuration; the others are still linted.
func TestLintTemplatesManagedFailureIsAFinding(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.writeFile(t, ".chezmoiignore", `{{ if eq .chezmoi.os "windows" }}{{ fail "no ignore for windows" }}{{ end }}`)
	e.writeFile(t, "dot_probe.json.tmpl", "{ broken")
	_, stderr, err := e.run(t, "", "lint", "templates")
	if err == nil || !strings.Contains(stderr, "windows/default: cannot list the managed files") || !strings.Contains(stderr, "dot_probe.json.tmpl (darwin/default)") {
		t.Errorf("lint templates = %v, stderr %q, want a finding for windows and the others linted", err, stderr)
	}
}

// --as without a profile is ambiguous once several are declared.
func TestViewAsNeedsAProfile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration)
	_, _, err := e.run(t, "", "view", filepath.Join(e.dest, ".note"), "--as", "linux")
	if err == nil || !strings.Contains(err.Error(), "home, minimal") {
		t.Errorf("view --as linux = %v, want an error listing the profiles", err)
	}
	if out, _, err := newEnv(t).run(t, "", "view", "--matrix"); err != nil || !strings.Contains(out, "D=default") {
		t.Errorf("view --matrix without a declaration = %q, %v, want the default profile", out, err)
	}
}

// Review findings on the generic matrix, reproduced against real chezmoi.
func TestMatrixReview(t *testing.T) {
	t.Parallel()
	// A [data] key this machine's config holds but no profile's render writes (a feature
	// sub-answer, a key an older template wrote) reaches every configuration of view
	// and edit, which render over this machine's config.
	t.Run("a key only this machine's config holds does not leak into view --as", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration)
		e.appendConfig(t, "    machineOnly = \"from-this-machine\"\n")
		e.writeFile(t, "dot_probe.tmpl", "probe={{ get . \"machineOnly\" | default \"none\" }}\n")
		out, _, err := e.run(t, "", "view", filepath.Join(e.dest, ".probe"), "--as", "linux,home")
		if err != nil || strings.Contains(out, "from-this-machine") {
			t.Errorf("view --as linux,home = %q, %v, want no value from this machine's config", out, err)
		}
	})
	// --override-data merges tables recursively, so the {} a table key is zeroed to
	// keeps every nested key of this machine's config.
	t.Run("a table only this machine's config holds does not leak into view --as", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration)
		e.appendConfig(t, "[data.machineTable]\n    secret = \"from-this-machine\"\n")
		e.writeFile(t, "dot_probe.tmpl", "probe={{ dig \"machineTable\" \"secret\" \"none\" . }}\n")
		out, _, err := e.run(t, "", "view", filepath.Join(e.dest, ".probe"), "--as", "linux,home")
		if err != nil || strings.Contains(out, "from-this-machine") {
			t.Errorf("view --as linux,home = %q, %v, want no value from this machine's config", out, err)
		}
	})
	// Config [data] overrides .chezmoidata, so a key this machine's config overrides is
	// zeroed in every configuration instead of falling back to the source's value,
	// which is what chezmoi init with that profile would give.
	t.Run("a machine override of a .chezmoidata key renders the source's value", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration)
		e.writeFile(t, ".chezmoidata/shared.toml", "shared = \"from-source\"\n")
		e.appendConfig(t, "    shared = \"from-this-machine\"\n")
		e.writeFile(t, "dot_probe.tmpl", "probe={{ .shared }}\n")
		out, _, err := e.run(t, "", "view", filepath.Join(e.dest, ".probe"), "--as", "linux,home")
		if err != nil || !strings.Contains(out, "probe=from-source") {
			t.Errorf("view --as linux,home = %q, %v, want the .chezmoidata value", out, err)
		}
	})
}

// chezmoi's other documented config template formats are rendered too, and a source
// with none gets a config holding the declared answers. A template that breaks for
// one configuration shows which configurations got the data.
func TestConfigTemplateFormats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, file, text, probe, want, notWant string
	}{
		{
			name: "yaml", file: ".chezmoi.yaml.tmpl",
			text:  "data:\n  fromTemplate: yaml-{{ .chezmoi.os }}\n",
			probe: `{{ if eq (get . "fromTemplate" | default "") "yaml-linux" }}{ broken{{ end }}`,
			want:  "(linux/default)", notWant: "(darwin/",
		},
		{
			name: "json", file: ".chezmoi.json.tmpl",
			text:  `{"data": {"fromTemplate": "json-{{ .chezmoi.os }}"}}`,
			probe: `{{ if eq (get . "fromTemplate" | default "") "json-windows" }}{ broken{{ end }}`,
			want:  "(windows/default)", notWant: "(linux/",
		},
		{
			name: "none", probe: `{{ if get . "featureX" }}{ broken{{ end }}`,
			want: "(darwin/home)", notWant: "/minimal)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			if tt.name == "none" {
				e.writeFile(t, ".chezmoidata/matrix.toml", matrixDeclaration)
			}
			if err := os.Remove(filepath.Join(e.src, ".chezmoi.toml.tmpl")); err != nil {
				t.Fatal(err)
			}
			if tt.file != "" {
				e.writeFile(t, tt.file, tt.text)
			}
			e.writeFile(t, "dot_probe.json.tmpl", tt.probe)
			_, stderr, err := e.run(t, "", "lint", "templates")
			if err == nil || !strings.Contains(stderr, tt.want) || strings.Contains(stderr, tt.notWant) {
				t.Errorf("lint templates = %v, stderr %q, want a finding for %s only", err, stderr, tt.want)
			}
		})
	}
}

// A seed value with control characters reaches the render intact: chezmoi parses the
// TOML the tool writes.
func TestSeedWithControlCharacters(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if err := os.Remove(filepath.Join(e.src, ".chezmoi.toml.tmpl")); err != nil {
		t.Fatal(err)
	}
	e.writeFile(t, ".chezmoidata/matrix.toml", "[matrix.seed]\nodd = \"a\\u0001b\\tc\\\"d\\\\e\"\n")
	e.writeFile(t, "dot_probe.json.tmpl", "{{ if eq (len .odd) 9 }}{ broken{{ end }}")
	_, stderr, err := e.run(t, "", "lint", "templates")
	if err == nil || !strings.Contains(stderr, "dot_probe.json.tmpl (darwin/default)") {
		t.Errorf("lint templates = %v, stderr %q, want the nine-character value rendered", err, stderr)
	}
}

// appendConfig appends text to this machine's chezmoi config, whose last table is [data].
func (e *env) appendConfig(t *testing.T, text string) {
	t.Helper()
	cfg := e.vars["CHEZMOI_CONFIG_FILE"]
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg, append(b, text...), 0o644); err != nil {
		t.Fatal(err)
	}
}
