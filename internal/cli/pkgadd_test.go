package cli_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
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
	// probes answers a detection command by its program ("brew", "uv", ...); probed records them.
	probes map[string]string
	probed [][]string
}

func (r *pkgAddRunner) Run(_ context.Context, c chez.Cmd) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case len(c.Args) > 0 && r.probes[c.Args[0]] != "":
		r.probed = append(r.probed, slices.Clone(c.Args))
		_, err := c.Stdout.Write([]byte(r.probes[c.Args[0]]))
		return err
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
	goos      string   // default: the host's
	managers  []string // package managers on PATH
	stdin     string
	tty       bool
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

func (e *pkgAddEnv) hostOS() string {
	if e.goos != "" {
		return e.goos
	}
	return runtime.GOOS
}

// withoutNotes drops the "not set for <os>" lines, which depend on the host OS.
func withoutNotes(out string) string {
	var kept []string
	for l := range strings.SplitAfterSeq(out, "\n") {
		if !strings.HasPrefix(l, "not set for ") {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "")
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
		Stdin:  strings.NewReader(e.stdin),
		Stdout: &e.out, Stderr: &e.errw, Runner: e.runner, HTTP: &http.Client{Transport: e.http},
		LookPath: func(n string) (string, error) {
			if slices.Contains(e.managers, n) {
				return n, nil
			}
			return "", errors.New("not found")
		},
		Home: t.TempDir(), GOOS: e.hostOS(), Interactive: func() bool { return e.tty },
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
			if got := withoutNotes(e.out.String()); got != "added package wget\n" {
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
			if diff := cmp.Diff(tt.want, withoutNotes(e.out.String())); diff != "" {
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

func TestPkgAddRolesAgainstDeclaredProfiles(t *testing.T) {
	t.Parallel()
	const declared = `{"matrix":{"profile":{"home":{},"minimal":{}}}}`
	t.Run("undeclared role", func(t *testing.T) {
		t.Parallel()
		e := newPkgAddEnv(t, pkgAddOldText, declared)
		err := e.run(t, "wget", "--brew", "--roles", "home,admin")
		if !errors.Is(err, cli.ErrUsage) || !strings.Contains(err.Error(), "home, minimal") || !strings.Contains(err.Error(), `"admin"`) {
			t.Fatalf("pkg add = %v, want a usage error naming the declared profiles", err)
		}
		if got := e.text(t); got != pkgAddOldText {
			t.Errorf("a rejected command changed packages.toml:\n%s", got)
		}
	})
	t.Run("declared role", func(t *testing.T) {
		t.Parallel()
		e := newPkgAddEnv(t, pkgAddOldText, declared)
		if err := e.run(t, "wget", "--brew", "--roles", "minimal"); err != nil {
			t.Fatalf("pkg add = %v, want it to pass", err)
		}
	})
	t.Run("no declaration accepts any name", func(t *testing.T) {
		t.Parallel()
		e := newPkgAddEnv(t, pkgAddOldText, `{}`)
		if err := e.run(t, "wget", "--brew", "--roles", "admin"); err != nil {
			t.Fatalf("pkg add = %v, want it to pass", err)
		}
	})
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
		{
			"package in release mode", `{"package":{"wget":{"brew":"wget"}}}`, "package wget already exists",
			[]string{"wget", "--release", "--version", "1", "--url", "https://example.com/{version}/x", "--target", "linux/amd64=x"},
		},
		{"package in github mode", `{"package":{"wget":{"brew":"wget"}}}`, "package wget already exists", []string{"wget", "--github", "octocat/wget"}},
		{"package with nothing new", `{"package":{"wget":{"brew":"wget"}}}`, "package wget already has these keys; change it with chezmoi dots edit", []string{"wget", "--brew"}},
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

const brewWgetJSON = `{"formulae":[{"full_name":"wget","installed":[]}],"casks":[]}`

// detectEnv is a darwin machine with brew and uv, where brew knows wget.
func detectEnv(t *testing.T, text string, datas ...string) *pkgAddEnv {
	t.Helper()
	e := newPkgAddEnv(t, text, datas...)
	e.goos, e.managers = "darwin", []string{"brew", "uv"}
	e.runner.probes = map[string]string{"brew": brewWgetJSON}
	return e
}

func TestPkgAddFlagsDisableDetection(t *testing.T) {
	t.Parallel()
	e := detectEnv(t, pkgAddOldText, `{}`)
	if err := e.run(t, "wget", "--apt", "--dry-run"); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	if len(e.runner.probed) != 0 {
		t.Errorf("detection ran despite a source flag: %v", e.runner.probed)
	}
	if got := withoutNotes(e.out.String()); got != "[package.wget]\napt = \"wget\"\n" {
		t.Errorf("entry = %q", got)
	}
}

func TestPkgAddDetect(t *testing.T) { //nolint:gocognit // table test with two outcome checks
	t.Parallel()
	const wantEntry = "[package.wget]\nbrew = \"wget\"\n"
	tests := []struct {
		name      string
		args      []string
		stdin     string
		tty       bool
		managers  []string
		probes    map[string]string
		wantEntry string // the entry written or printed; "" for a failure
		wantUsage string // a message of the ErrUsage failure
	}{
		{name: "tty accepts with enter", stdin: "\n", tty: true, wantEntry: wantEntry},
		{name: "tty accepts with y", stdin: "y\n", tty: true, wantEntry: wantEntry},
		{name: "tty asks again on a stray answer", stdin: "what\nY\n", tty: true, wantEntry: wantEntry},
		{name: "tty edits the id", stdin: "e\nwget2\n", tty: true, wantEntry: "[package.wget]\nbrew = \"wget2\"\n"},
		{name: "tty edit with an empty id keeps it", stdin: "e\n\n", tty: true, wantEntry: wantEntry},
		{name: "tty declines everything", stdin: "n\n", tty: true, wantUsage: "no detected source was confirmed"},
		{name: "no tty without --yes", wantUsage: "detected sources need confirmation; pass --yes or set them with flags"},
		{name: "no tty with --yes", args: []string{"--yes"}, wantEntry: wantEntry},
		{name: "tty with --yes does not ask", args: []string{"--yes"}, tty: true, wantEntry: wantEntry},
		{name: "dry run needs --yes off a tty", args: []string{"--dry-run"}, wantUsage: "detected sources need confirmation"},
		{name: "dry run with --yes prints", args: []string{"--dry-run", "--yes"}, wantEntry: wantEntry},
		{
			name: "roles with a detected source", args: []string{"--yes", "--roles", "work"},
			wantEntry: "[package.wget]\nbrew = \"wget\"\nroles = [\"work\"]\n",
		},
		{name: "nothing found", probes: map[string]string{"brew": `{"formulae":[],"casks":[]}`}, wantUsage: "nothing found for wget by brew, uv"},
		{name: "no manager on this machine", managers: []string{"none"}, wantUsage: "nothing found for wget by any package manager on this machine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := detectEnv(t, pkgAddOldText, `{}`, `{"package":{"wget":{"brew":"wget"}}}`)
			e.stdin, e.tty = tt.stdin, tt.tty
			if tt.managers != nil {
				e.managers = tt.managers
			}
			if tt.probes != nil {
				e.runner.probes = tt.probes
			}
			dry := slices.Contains(tt.args, "--dry-run")
			err := e.run(t, append([]string{"wget"}, tt.args...)...)
			if tt.wantUsage != "" {
				if !errors.Is(err, cli.ErrUsage) || !strings.Contains(err.Error(), tt.wantUsage) {
					t.Fatalf("pkg add = %v, want a usage error containing %q", err, tt.wantUsage)
				}
				if got := e.text(t); got != pkgAddOldText {
					t.Errorf("packages.toml changed:\n%s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
			}
			if !strings.Contains(e.errw.String(), "  brew = \"wget\"  (available)\n") || !strings.Contains(e.errw.String(), "  nothing found by uv\n") {
				t.Errorf("stderr lacks the proposal:\n%s", e.errw.String())
			}
			if dry {
				if got := withoutNotes(e.out.String()); got != tt.wantEntry {
					t.Errorf("printed entry = %q, want %q", got, tt.wantEntry)
				}
				if got := e.text(t); got != pkgAddOldText {
					t.Errorf("a dry run changed packages.toml:\n%s", got)
				}
				return
			}
			if diff := cmp.Diff(pkgAddOldText+"\n"+tt.wantEntry, e.text(t)); diff != "" {
				t.Errorf("packages.toml (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPkgAddOSNotes(t *testing.T) {
	t.Parallel()
	const (
		darwin  = "not set for darwin: brew, cask; pass --brew/--cask or run chezmoi dots pkg add x on darwin\n"
		linux   = "not set for linux: apt; pass --apt or run chezmoi dots pkg add x on linux\n"
		windows = "not set for windows: winget; pass --winget or run chezmoi dots pkg add x on windows\n"
	)
	tests := []struct {
		name string
		flag []string
		want string
	}{
		{"brew set", []string{"--brew"}, linux + windows},
		{"cask set", []string{"--cask", "c"}, linux + windows},
		{"apt set", []string{"--apt"}, darwin + windows},
		{"winget set", []string{"--winget", "W"}, darwin + linux},
		{"everywhere only", []string{"--uv", "--npm"}, darwin + linux + windows},
		{"all set", []string{"--brew", "--apt", "--winget", "W"}, ""},
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, tt := range tests {
			t.Run(goos+" "+tt.name, func(t *testing.T) {
				t.Parallel()
				e := newPkgAddEnv(t, pkgAddOldText, `{}`)
				e.goos = goos
				if err := e.run(t, append([]string{"x", "--dry-run"}, tt.flag...)...); err != nil {
					t.Fatalf("pkg add = %v", err)
				}
				var notes strings.Builder
				for l := range strings.SplitAfterSeq(e.out.String(), "\n") {
					if strings.HasPrefix(l, "not set for ") {
						notes.WriteString(l)
					}
				}
				if diff := cmp.Diff(tt.want, notes.String()); diff != "" {
					t.Errorf("notes (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestPkgAddNoteAfterWrite(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, pkgAddOldText, `{}`, pkgAddWgetData)
	if err := e.run(t, "wget", "--apt"); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	want := "added package wget\nnot set for darwin: brew, cask; pass --brew/--cask or run chezmoi dots pkg add wget on darwin\n" +
		"not set for windows: winget; pass --winget or run chezmoi dots pkg add wget on windows\n"
	if got := e.out.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

const mergeText = "# top\n[package.wget]\nbrew = \"wget\" # keep\n\n# about other\n[package.other]\nbrew = \"o\"\n"

func TestPkgAddMergesKeys(t *testing.T) {
	t.Parallel()
	const have = `{"package":{"wget":{"brew":"wget"},"other":{"brew":"o"}}}`
	after := `{"package":{"wget":{"brew":"wget","apt":"wget"},"other":{"brew":"o"}}}`
	tests := []struct {
		name, text, want, stdout string
		args                     []string
	}{
		{
			"adds a missing key", mergeText,
			"# top\n[package.wget]\nbrew = \"wget\" # keep\napt = \"wget\"\n\n# about other\n[package.other]\nbrew = \"o\"\n",
			"updated package wget: apt\n",
			[]string{"--apt"},
		},
		{
			"crlf", strings.ReplaceAll(mergeText, "\n", "\r\n"),
			strings.ReplaceAll("# top\n[package.wget]\nbrew = \"wget\" # keep\napt = \"wget\"\n\n# about other\n[package.other]\nbrew = \"o\"\n", "\n", "\r\n"),
			"updated package wget: apt\n",
			[]string{"--apt"},
		},
		{
			"keys and roles in registry order", mergeText,
			"# top\n[package.wget]\nbrew = \"wget\" # keep\napt = \"wget2\"\nroles = [\"work\"]\n\n# about other\n[package.other]\nbrew = \"o\"\n",
			"updated package wget: apt, roles\n",
			[]string{"--apt=wget2", "--roles", "work"},
		},
		{
			"a key already set is left alone", mergeText,
			"# top\n[package.wget]\nbrew = \"wget\" # keep\napt = \"wget\"\n\n# about other\n[package.other]\nbrew = \"o\"\n",
			"updated package wget: apt\n",
			[]string{"--brew=changed", "--apt"},
		},
		{
			"last package in the file without a newline", "[package.wget]\nbrew = \"wget\"",
			"[package.wget]\nbrew = \"wget\"\napt = \"wget\"\n", "updated package wget: apt\n",
			[]string{"--apt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPkgAddEnv(t, tt.text, have, after)
			if err := e.run(t, append([]string{"wget"}, tt.args...)...); err != nil {
				t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
			}
			if diff := cmp.Diff(tt.want, e.text(t)); diff != "" {
				t.Errorf("packages.toml (-want +got):\n%s", diff)
			}
			if got := withoutNotes(e.out.String()); got != tt.stdout {
				t.Errorf("stdout = %q, want %q", got, tt.stdout)
			}
			if strings.Contains(tt.name, "already set") && !strings.Contains(e.errw.String(), "brew already set; change it with chezmoi dots edit\n") {
				t.Errorf("stderr lacks the already-set key:\n%s", e.errw.String())
			}
		})
	}
}

func TestPkgAddMergeDryRunPrintsNewKeys(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, mergeText, `{"package":{"wget":{"brew":"wget"}}}`)
	if err := e.run(t, "wget", "--apt", "--dry-run"); err != nil {
		t.Fatalf("pkg add = %v", err)
	}
	if got := withoutNotes(e.out.String()); got != "apt = \"wget\"\n" {
		t.Errorf("stdout = %q", got)
	}
	if got := e.text(t); got != mergeText {
		t.Errorf("a dry run changed packages.toml:\n%s", got)
	}
}

func TestPkgAddMergeMergesDetectedKeys(t *testing.T) {
	t.Parallel()
	e := detectEnv(t, "[package.wget]\napt = \"wget\"\n", `{"package":{"wget":{"apt":"wget"}}}`, `{"package":{"wget":{"apt":"wget","brew":"wget"}}}`)
	if err := e.run(t, "wget", "--yes"); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	if got := e.text(t); got != "[package.wget]\napt = \"wget\"\nbrew = \"wget\"\n" {
		t.Errorf("packages.toml:\n%s", got)
	}
	if got := withoutNotes(e.out.String()); got != "updated package wget: brew\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestPkgAddMergeSkipsDetectedKeysAlreadySet(t *testing.T) {
	t.Parallel()
	e := detectEnv(t, mergeText, `{"package":{"wget":{"brew":"wget"}}}`)
	err := e.run(t, "wget", "--yes")
	if err == nil || errors.Is(err, cli.ErrUsage) || !strings.Contains(err.Error(), "package wget already has these keys; change it with chezmoi dots edit") {
		t.Fatalf("pkg add = %v, want the nothing-to-add error", err)
	}
	if !strings.Contains(e.errw.String(), "brew already set; change it with chezmoi dots edit\n") {
		t.Errorf("stderr lacks the already-set key:\n%s", e.errw.String())
	}
	if got := e.text(t); got != mergeText {
		t.Errorf("packages.toml changed:\n%s", got)
	}
}

func TestPkgAddMergeNothingToAddLeavesFile(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, mergeText, `{"package":{"wget":{"brew":"wget","apt":"wget","roles":["work"]}}}`)
	err := e.run(t, "wget", "--apt", "--roles", "personal")
	if err == nil || errors.Is(err, cli.ErrUsage) || !strings.Contains(err.Error(), "already has these keys") {
		t.Fatalf("pkg add = %v, want the nothing-to-add error", err)
	}
	for _, key := range []string{"apt", "roles"} {
		if !strings.Contains(e.errw.String(), key+" already set; change it with chezmoi dots edit\n") {
			t.Errorf("stderr lacks %q:\n%s", key, e.errw.String())
		}
	}
	if got := e.text(t); got != mergeText {
		t.Errorf("packages.toml changed:\n%s", got)
	}
}

func TestPkgAddMergeTableNotInFile(t *testing.T) {
	t.Parallel()
	const inline = "[package]\nwget = { brew = \"wget\" }\n"
	e := newPkgAddEnv(t, inline, `{"package":{"wget":{"brew":"wget"}}}`)
	err := e.run(t, "wget", "--apt")
	if err == nil || errors.Is(err, cli.ErrUsage) || !strings.Contains(err.Error(), "not a [package.wget] table") {
		t.Fatalf("pkg add = %v, want a plain error", err)
	}
	if got := e.text(t); got != inline {
		t.Errorf("packages.toml changed:\n%s", got)
	}
}

// ghTransport serves the GitHub API and counts the archive downloads.
type ghTransport struct {
	mu        sync.Mutex
	api       []string
	downloads []string
	json      string
}

func (g *ghTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	body := archiveBytes
	if r.URL.Host == "api.github.com" {
		g.api = append(g.api, r.URL.Path)
		body = []byte(g.json)
	} else {
		g.downloads = append(g.downloads, r.URL.String())
	}
	return &http.Response{
		StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body)),
		Header: http.Header{}, Request: r,
	}, nil
}

const ghDownload = "https://github.com/octocat/example-tool/releases/download/v1.2.3/"

func ghReleaseJSON() string {
	names := []string{
		"example-tool_1.2.3_linux_amd64.tar.gz", "example-tool_1.2.3_darwin_arm64.tar.gz", "example-tool_1.2.3_windows_amd64.zip",
		"checksums.txt", "example-tool_1.2.3_linux_arm64.tar.gz", "example-tool_1.2.3_Linux_aarch64.tar.gz",
	}
	assets := make([]string, len(names))
	for i, n := range names {
		assets[i] = fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, n, ghDownload+n)
	}
	return `{"tag_name":"v1.2.3","assets":[` + strings.Join(assets, ",") + `]}`
}

func ghEnv(t *testing.T, text string, datas ...string) (*pkgAddEnv, *ghTransport) {
	t.Helper()
	gh := &ghTransport{json: ghReleaseJSON()}
	e := newPkgAddEnv(t, text, datas...)
	e.http = gh
	return e, gh
}

func TestPkgAddGitHub(t *testing.T) { //nolint:gocognit // table test with many output checks
	t.Parallel()
	sum := fmt.Sprintf("%x", sha256.Sum256(archiveBytes))
	valid := fmt.Sprintf(`{"release":{"example-tool":{"version":"1.2.3","url":%q,"type":"archive-file","path":"example-tool","exe":true,
		"target":{"linux/amd64":"linux_amd64.tar.gz","darwin/arm64":"darwin_arm64.tar.gz","windows/amd64":"windows_amd64.zip"},
		"sha256":{"linux/amd64":%q,"darwin/arm64":%q,"windows/amd64":%q}}}}`,
		ghDownload[:len(ghDownload)-len("v1.2.3/")]+"v{version}/example-tool_{version}_{target}", sum, sum, sum)
	tests := []struct {
		name  string
		args  []string
		stdin string
		tty   bool
	}{
		{"yes", []string{"--yes"}, "", false},
		{"tty confirms", nil, "y\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, gh := ghEnv(t, pkgAddOldText, `{}`, valid)
			e.stdin, e.tty = tt.stdin, tt.tty
			if err := e.run(t, append([]string{"example-tool", "--github", "octocat/example-tool", "--roles", "work"}, tt.args...)...); err != nil {
				t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
			}
			want := pkgAddOldText + "\n" +
				"[release.example-tool]\nversion = \"1.2.3\"\nurl = \"" + ghDownload[:len(ghDownload)-len("v1.2.3/")] + "v{version}/example-tool_{version}_{target}\"\n" +
				"type = \"archive-file\"\npath = \"example-tool\"\nexe = true\nroles = [\"work\"]\n\n" +
				"[release.example-tool.target]\n\"darwin/arm64\" = \"darwin_arm64.tar.gz\"\n\"linux/amd64\" = \"linux_amd64.tar.gz\"\n\"windows/amd64\" = \"windows_amd64.zip\"\n\n" +
				"[release.example-tool.sha256]\n\"darwin/arm64\" = \"" + sum + "\"\n\"linux/amd64\" = \"" + sum + "\"\n\"windows/amd64\" = \"" + sum + "\"\n"
			if diff := cmp.Diff(want, e.text(t)); diff != "" {
				t.Errorf("packages.toml (-want +got):\n%s", diff)
			}
			if got := e.out.String(); got != "added release example-tool\n" {
				t.Errorf("stdout = %q", got)
			}
			if diff := cmp.Diff([]string{"/repos/octocat/example-tool/releases/latest"}, gh.api); diff != "" {
				t.Errorf("api requests (-want +got):\n%s", diff)
			}
			if len(gh.downloads) != 3 {
				t.Errorf("downloads = %v, want one per target", gh.downloads)
			}
			for _, w := range []string{
				`  version = "1.2.3"`, `  type = "archive-file"`, `  path = "example-tool"`, "  exe = true",
				`  linux/amd64 = "linux_amd64.tar.gz"`, "  left out: checksums.txt, ", "  ambiguous: linux/arm64: ",
			} {
				if !strings.Contains(e.errw.String(), w) {
					t.Errorf("stderr lacks %q:\n%s", w, e.errw.String())
				}
			}
		})
	}
}

func TestPkgAddGitHubDryRunDownloadsNothing(t *testing.T) {
	t.Parallel()
	e, gh := ghEnv(t, pkgAddOldText, `{}`)
	if err := e.run(t, "example-tool", "--github", "octocat/example-tool", "--yes", "--dry-run"); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	if len(gh.downloads) != 0 || len(gh.api) != 1 {
		t.Errorf("api = %v, downloads = %v, want one API call and no download", gh.api, gh.downloads)
	}
	for _, w := range []string{"[release.example-tool]\n", "type = \"archive-file\"\n", "exe = true\n", "# sha256 computed when written\n"} {
		if !strings.Contains(e.out.String(), w) {
			t.Errorf("stdout lacks %q:\n%s", w, e.out.String())
		}
	}
	if got := e.text(t); got != pkgAddOldText {
		t.Errorf("a dry run changed packages.toml:\n%s", got)
	}
}

func TestPkgAddGitHubNeedsConfirmation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, stdin string
		tty         bool
		args        []string
		usage       bool
	}{
		{"no tty without --yes", "", false, nil, true},
		{"dry run no tty without --yes", "", false, []string{"--dry-run"}, true},
		{"tty declines", "n\n", true, nil, false},
		{"tty empty answer declines", "\n", true, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, gh := ghEnv(t, pkgAddOldText, `{}`)
			e.stdin, e.tty = tt.stdin, tt.tty
			err := e.run(t, append([]string{"example-tool", "--github", "octocat/example-tool"}, tt.args...)...)
			if err == nil || errors.Is(err, cli.ErrUsage) != tt.usage {
				t.Fatalf("pkg add = %v, want usage error %v", err, tt.usage)
			}
			if len(gh.downloads) != 0 {
				t.Errorf("downloads = %v", gh.downloads)
			}
			if got := e.text(t); got != pkgAddOldText {
				t.Errorf("packages.toml changed:\n%s", got)
			}
		})
	}
}

func TestPkgAddGitHubUsageErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{"with --version", []string{"--version", "1"}},
		{"with --url", []string{"--url", releaseURL}},
		{"with --target", []string{"--target", "linux/amd64=x"}},
		{"with --brew", []string{"--brew"}},
		{"with --cask", []string{"--cask", "c"}},
		{"with --uv", []string{"--uv"}},
		{"empty repository", []string{"--github", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newPkgAddEnv(t, pkgAddOldText, `{}`)
			e.http = failingTransport{t}
			args := append([]string{"example-tool", "--github", "octocat/example-tool"}, tt.args...)
			if err := e.run(t, args...); !errors.Is(err, cli.ErrUsage) {
				t.Fatalf("pkg add %v = %v, want ErrUsage", args, err)
			}
			if e.runner.calls != 0 {
				t.Errorf("a rejected command read the chezmoi data")
			}
		})
	}
}

func TestPkgAddHelpExplainsDetectionAndGitHub(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, "")
	if err := e.run(t, "--help"); err != nil {
		t.Fatalf("pkg add --help = %v", err)
	}
	for _, want := range []string{"never a public index", "turns detection off", "--github", "--yes", "GH_TOKEN"} {
		if !strings.Contains(e.out.String(), want) {
			t.Errorf("help lacks %q", want)
		}
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

func TestPkgAddBrewFalseIsNotAFormula(t *testing.T) {
	t.Parallel()
	e := newPkgAddEnv(t, pkgAddOldText, `{}`)
	if err := e.run(t, "wget", "--brew=false", "--apt", "--dry-run"); err != nil {
		return // a usage error is fine
	}
	if out := e.out.String(); strings.Contains(out, `brew = "false"`) {
		t.Errorf("--brew=false wrote a formula named false:\n%s", out)
	}
}
