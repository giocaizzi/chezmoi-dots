package cli_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

// pkgAddRunner answers the chezmoi calls of `pkg add` from fixed answers and records
// the git commands: it never starts a process. datas are the answers of successive
// `chezmoi data` calls (the last one repeats).
type pkgAddRunner struct {
	mu    sync.Mutex
	datas []string
	calls int
	cfg   string
	git   [][]string
}

func (r *pkgAddRunner) Run(_ context.Context, c chez.Cmd) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case len(c.Args) > 0 && c.Args[0] == "git":
		r.git = append(r.git, slices.Clone(c.Args))
		return nil
	case slices.Contains(c.Args, "dump-config"):
		_, err := c.Stdout.Write([]byte(r.cfg))
		return err
	case slices.Contains(c.Args, "data"):
		data := r.datas[min(r.calls, len(r.datas)-1)]
		r.calls++
		_, err := c.Stdout.Write([]byte(data))
		return err
	}
	return errors.New("unexpected process")
}

type pkgAddEnv struct {
	src, file string
	runner    *pkgAddRunner
	out, errw bytes.Buffer
	fetch     *archiveFetcher
	http      http.RoundTripper
}

const (
	pkgAddWgetData = `{"package":{"wget":{"brew":"wget"}}}`
	pkgAddOldText  = "# my packages\n[package.old]\nbrew = \"old\"\n"
)

// newPkgAddEnv has a source dir whose packages.toml holds text (absent when empty)
// and a runner that answers data with datas.
func newPkgAddEnv(t *testing.T, text string, datas ...string) *pkgAddEnv {
	t.Helper()
	src := t.TempDir()
	e := &pkgAddEnv{src: src, file: filepath.Join(src, ".chezmoidata", "packages.toml"), fetch: &archiveFetcher{}}
	e.http = e.fetch
	if text != "" {
		if err := os.MkdirAll(filepath.Dir(e.file), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(e.file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e.runner = &pkgAddRunner{datas: datas, cfg: `{}`}
	return e
}

func (e *pkgAddEnv) run(t *testing.T, args ...string) error {
	t.Helper()
	d := cli.Deps{
		Getenv: func(key string) string {
			switch key {
			case "NO_COLOR":
				return "1"
			case "CHEZMOI_SOURCE_DIR":
				return e.src
			}
			return ""
		},
		Stdout: &e.out, Stderr: &e.errw, Runner: e.runner, HTTP: &http.Client{Transport: e.http},
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Home:     t.TempDir(), GOOS: runtime.GOOS,
	}
	return cli.Run(t.Context(), d, append([]string{"pkg", "add"}, args...))
}

func (e *pkgAddEnv) text(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPkgAddWritesPackage(t *testing.T) {
	t.Parallel()
	entry := "[package.wget]\nbrew = \"wget\"\napt = \"wget2\"\nroles = [\"personal\", \"work\"]\nrequires = \"gui\"\n"
	tests := []struct {
		name, old, want string
	}{
		{"after existing content", pkgAddOldText, pkgAddOldText + "\n" + entry},
		{"after a blank line", pkgAddOldText + "\n", pkgAddOldText + "\n" + entry},
		{"no trailing newline", "[package.old]\nbrew = \"old\"", "[package.old]\nbrew = \"old\"\n\n" + entry},
		{
			"crlf file", strings.ReplaceAll(pkgAddOldText, "\n", "\r\n"),
			strings.ReplaceAll(pkgAddOldText+"\n"+entry, "\n", "\r\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPkgAddEnv(t, tt.old, `{}`, pkgAddWgetData)
			err := e.run(t, "wget", "--brew", "--apt=wget2", "--roles", "personal,work", "--requires", "gui")
			if err != nil {
				t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
			}
			if diff := cmp.Diff(tt.want, e.text(t)); diff != "" {
				t.Errorf("packages.toml (-want +got):\n%s", diff)
			}
			if got := e.out.String(); got != "added package wget\n" {
				t.Errorf("stdout = %q", got)
			}
			if e.runner.calls != 2 {
				t.Errorf("chezmoi data calls = %d, want 2 (before and after the write)", e.runner.calls)
			}
			if len(e.runner.git) != 0 {
				t.Errorf("git ran without git settings: %v", e.runner.git)
			}
		})
	}
}

func TestPkgAddCreatesMissingFile(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, "", `{}`, pkgAddWgetData)
	if err := e.run(t, "wget", "--brew"); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	if diff := cmp.Diff("[package.wget]\nbrew = \"wget\"\n", e.text(t)); diff != "" {
		t.Errorf("packages.toml (-want +got):\n%s", diff)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(e.file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("mode = %v, want 0644", info.Mode().Perm())
		}
	}
}

func TestPkgAddFlagForms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bare brew", []string{"wget", "--brew"}, "[package.wget]\nbrew = \"wget\"\n"},
		{"flag before the name", []string{"--brew", "wget"}, "[package.wget]\nbrew = \"wget\"\n"},
		{"brew with a value", []string{"wget", "--brew=wget2"}, "[package.wget]\nbrew = \"wget2\"\n"},
		{"bare apt, uv and npm", []string{"example-tool", "--apt", "--uv", "--npm"}, "[package.example-tool]\napt = \"example-tool\"\nuv = \"example-tool\"\nnpm = \"example-tool\"\n"},
		{"npm with a spec", []string{"tsc", "--npm=typescript@5"}, "[package.tsc]\nnpm = \"typescript@5\"\n"},
		{"uv with a spec", []string{"example-tool", "--uv=example-tool[extra]"}, "[package.example-tool]\nuv = \"example-tool[extra]\"\n"},
		{"cask and winget", []string{"app", "--cask", "example-app", "--winget", "Example.App"}, "[package.app]\ncask = \"example-app\"\nwinget = \"Example.App\"\n"},
		{"all sources in order", []string{"x", "--npm", "--uv", "--winget", "W", "--apt", "--cask", "C", "--brew"}, "[package.x]\nbrew = \"x\"\ncask = \"C\"\napt = \"x\"\nwinget = \"W\"\nuv = \"x\"\nnpm = \"x\"\n"},
		{"light role only", []string{"x", "--brew", "--roles", "light"}, "[package.x]\nbrew = \"x\"\nroles = [\"light\"]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPkgAddEnv(t, pkgAddOldText, `{}`)
			if err := e.run(t, append(tt.args, "--dry-run")...); err != nil {
				t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
			}
			if diff := cmp.Diff(tt.want, e.out.String()); diff != "" {
				t.Errorf("entry (-want +got):\n%s", diff)
			}
			if got := e.text(t); got != pkgAddOldText {
				t.Errorf("a dry run changed packages.toml:\n%s", got)
			}
			if len(e.runner.git) != 0 {
				t.Errorf("a dry run ran git: %v", e.runner.git)
			}
		})
	}
}

func TestPkgAddUsageErrors(t *testing.T) {
	t.Parallel()
	const url = "https://example.com/{version}/tool-{target}.tar.gz"
	tests := []struct {
		name string
		args []string
	}{
		{"no name", []string{"--brew"}},
		{"two names", []string{"a", "b", "--brew"}},
		{"bad name", []string{"a b", "--brew"}},
		{"name with a dot", []string{"a.b", "--brew"}},
		{"no source", []string{"wget"}},
		{"bad role", []string{"wget", "--brew", "--roles", "personal,admin"}},
		{"empty role", []string{"wget", "--brew", "--roles", ""}},
		{"empty requires", []string{"wget", "--brew", "--requires", ""}},
		{"empty brew value", []string{"wget", "--brew="}},
		{"empty cask", []string{"wget", "--cask", ""}},
		{"unknown flag", []string{"wget", "--brew", "--nope"}},
		{"release only flag without --release", []string{"wget", "--brew", "--version", "1.0.0"}},
		{"target without --release", []string{"wget", "--brew", "--target", "linux/amd64=x"}},
		{"package flag with --release", []string{"x", "--release", "--brew", "--version", "1", "--url", url, "--target", "linux/amd64=x"}},
		{"release without version", []string{"x", "--release", "--url", url, "--target", "linux/amd64=x"}},
		{"release without url", []string{"x", "--release", "--version", "1", "--target", "linux/amd64=x"}},
		{"release without target", []string{"x", "--release", "--version", "1", "--url", url}},
		{"url without {version}", []string{"x", "--release", "--version", "1", "--url", "https://example.com/tool.tar.gz", "--target", "linux/amd64=x"}},
		{"bad platform", []string{"x", "--release", "--version", "1", "--url", url, "--target", "freebsd/amd64=x"}},
		{"target without a name", []string{"x", "--release", "--version", "1", "--url", url, "--target", "linux/amd64"}},
		{"target with an empty name", []string{"x", "--release", "--version", "1", "--url", url, "--target", "linux/amd64="}},
		{"duplicate target", []string{"x", "--release", "--version", "1", "--url", url, "--target", "linux/amd64=x", "--target", "linux/amd64=y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPkgAddEnv(t, pkgAddOldText, `{}`)
			e.http = failingTransport{t}
			err := e.run(t, tt.args...)
			if !errors.Is(err, cli.ErrUsage) {
				t.Fatalf("pkg add %v = %v, want ErrUsage", tt.args, err)
			}
			if got := e.text(t); got != pkgAddOldText {
				t.Errorf("a rejected command changed packages.toml:\n%s", got)
			}
			if e.runner.calls != 0 {
				t.Errorf("a rejected command read the chezmoi data")
			}
			if !strings.Contains(e.errw.String(), "chezmoi dots pkg add") {
				t.Errorf("stderr lacks the pkg add help:\n%s", e.errw.String())
			}
		})
	}
}

func TestPkgAddExistingName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, data, want string
		args             []string
	}{
		{"package", `{"package":{"wget":{"brew":"wget"}}}`, "package wget already exists", []string{"wget", "--brew"}},
		{"release", `{"release":{"wget":{"version":"1"}}}`, "release wget already exists", []string{"wget", "--brew"}},
		{
			"release in release mode", `{"release":{"wget":{"version":"1"}}}`, "release wget already exists; change it with chezmoi dots edit or move a release with chezmoi dots pkg bump",
			[]string{"wget", "--release", "--version", "1", "--url", "https://example.com/{version}/x", "--target", "linux/amd64=x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPkgAddEnv(t, pkgAddOldText, tt.data)
			e.http = failingTransport{t}
			err := e.run(t, tt.args...)
			if err == nil || errors.Is(err, cli.ErrUsage) || errors.Is(err, cli.ErrReported) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("pkg add = %v, want a plain error containing %q", err, tt.want)
			}
			if got := e.text(t); got != pkgAddOldText {
				t.Errorf("packages.toml changed:\n%s", got)
			}
		})
	}
}

const releaseURL = "https://example.com/{version}/tool-{target}.tar.gz"

func releaseArgs(extra ...string) []string {
	return append([]string{
		"example-tool", "--release", "--version", "1.2.3", "--url", releaseURL,
		"--target", "linux/amd64=linux-x86_64", "--target", "darwin/arm64=macos-arm",
	}, extra...)
}

func TestPkgAddRelease(t *testing.T) {
	t.Parallel()
	sum := fmt.Sprintf("%x", sha256.Sum256(archiveBytes))
	valid := fmt.Sprintf(`{"release":{"example-tool":{"version":"1.2.3","url":%q,
		"target":{"linux/amd64":"linux-x86_64","darwin/arm64":"macos-arm"},"sha256":{"linux/amd64":%q,"darwin/arm64":%q}}}}`, releaseURL, sum, sum)
	e := newPkgAddEnv(t, pkgAddOldText, `{}`, valid)
	if err := e.run(t, releaseArgs("--roles", "work", "--requires", "gui")...); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	want := pkgAddOldText + "\n" +
		"[release.example-tool]\nversion = \"1.2.3\"\nurl = \"" + releaseURL + "\"\nroles = [\"work\"]\nrequires = \"gui\"\n\n" +
		"[release.example-tool.target]\n\"darwin/arm64\" = \"macos-arm\"\n\"linux/amd64\" = \"linux-x86_64\"\n\n" +
		"[release.example-tool.sha256]\n\"darwin/arm64\" = \"" + sum + "\"\n\"linux/amd64\" = \"" + sum + "\"\n"
	if diff := cmp.Diff(want, e.text(t)); diff != "" {
		t.Errorf("packages.toml (-want +got):\n%s", diff)
	}
	if got := e.out.String(); got != "added release example-tool\n" {
		t.Errorf("stdout = %q", got)
	}
	slices.Sort(e.fetch.urls)
	wantURLs := []string{"https://example.com/1.2.3/tool-linux-x86_64.tar.gz", "https://example.com/1.2.3/tool-macos-arm.tar.gz"}
	if diff := cmp.Diff(wantURLs, e.fetch.urls); diff != "" {
		t.Errorf("downloads (-want +got):\n%s", diff)
	}
}

func TestPkgAddDownloadFailureWritesNothing(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, pkgAddOldText, `{}`)
	e.http = brokenDownload{}
	if err := e.run(t, releaseArgs()...); err == nil {
		t.Fatal("pkg add succeeded although the download failed")
	}
	if got := e.text(t); got != pkgAddOldText {
		t.Errorf("packages.toml changed:\n%s", got)
	}
}

func TestPkgAddReleaseDryRun(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, "", `{}`)
	e.http = failingTransport{t}
	if err := e.run(t, releaseArgs("--dry-run")...); err != nil {
		t.Fatalf("pkg add --dry-run = %v\n%s", err, e.errw.String())
	}
	want := "[release.example-tool]\nversion = \"1.2.3\"\nurl = \"" + releaseURL + "\"\n\n" +
		"[release.example-tool.target]\n\"darwin/arm64\" = \"macos-arm\"\n\"linux/amd64\" = \"linux-x86_64\"\n\n" +
		"# sha256 computed when written\n"
	if diff := cmp.Diff(want, e.out.String()); diff != "" {
		t.Errorf("entry (-want +got):\n%s", diff)
	}
	if _, err := os.Stat(e.file); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a dry run created packages.toml: %v", err)
	}
}

func TestPkgAddFindingsKeepTheEntry(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, pkgAddOldText, `{}`, `{"package":{"wget":{"brew":"wget","bogus":1}}}`)
	e.runner.cfg = fmt.Sprintf(`{"workingTree":%q,"git":{"autoadd":true,"autocommit":true,"autopush":true}}`, filepath.ToSlash(e.src))
	err := e.run(t, "wget", "--brew")
	if !errors.Is(err, cli.ErrReported) {
		t.Fatalf("pkg add = %v, want ErrReported", err)
	}
	for _, want := range []string{"✗ packages", "unknown key bogus", "wget was written to " + e.file, "chezmoi dots lint packages"} {
		if !strings.Contains(e.errw.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, e.errw.String())
		}
	}
	if !strings.HasSuffix(e.text(t), "[package.wget]\nbrew = \"wget\"\n") {
		t.Errorf("the entry was not kept:\n%s", e.text(t))
	}
	if e.out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", e.out.String())
	}
	if len(e.runner.git) != 0 {
		t.Errorf("git ran after findings: %v", e.runner.git)
	}
}

// checkGitOnlyPackages fails unless argv runs in the working tree and, but for push,
// names packages.toml alone.
func checkGitOnlyPackages(t *testing.T, argv []string, tree string) {
	t.Helper()
	if argv[3] != tree && argv[3] != filepath.ToSlash(tree) {
		t.Errorf("git ran in %q, want the working tree %q: %v", argv[3], tree, argv)
	}
	if argv[4] != "push" && argv[len(argv)-1] != ".chezmoidata/packages.toml" {
		t.Errorf("git %v touches more than packages.toml", argv)
	}
}

func TestPkgAddGit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, git string
		want      []string // the git subcommands after -C <dir>
	}{
		{"autoPush", `{"autoadd":true,"autocommit":true,"autopush":true}`, []string{"add", "commit", "push"}},
		{"autoCommit", `{"autocommit":true}`, []string{"add", "commit"}},
		{"autoAdd", `{"autoadd":true}`, []string{"add"}},
		{"none", `{}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPkgAddEnv(t, pkgAddOldText, `{}`, pkgAddWgetData)
			e.runner.cfg = fmt.Sprintf(`{"workingTree":%q,"git":%s}`, filepath.ToSlash(e.src), tt.git)
			if err := e.run(t, "wget", "--brew"); err != nil {
				t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
			}
			got := make([]string, 0, len(e.runner.git))
			for _, argv := range e.runner.git {
				got = append(got, argv[4]) // git --literal-pathspecs -C <dir> <subcommand>
				checkGitOnlyPackages(t, argv, e.src)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("git subcommands (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPkgAddPrintsHelpPage(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, "")
	if err := e.run(t, "--help"); err != nil {
		t.Fatalf("pkg add --help = %v", err)
	}
	for _, want := range []string{"chezmoi dots pkg add — ", "--brew", "--release", "--dry-run", "--target"} {
		if !strings.Contains(e.out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, e.out.String())
		}
	}
}
