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

func TestPkgUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"pkg"},
		{"pkg", "nosuch"},
		{"pkg", "--offline", "status"},
		{"pkg", "status", "brew"},
		{"pkg", "status", "--offline", "extra"},
		{"pkg", "status", "--nope"},
		{"pkg", "bump", "--nope"},
	} {
		d := cli.Deps{Getenv: func(string) string { return "" }, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
		if err := cli.Run(t.Context(), d, args); !errors.Is(err, cli.ErrUsage) {
			t.Errorf("Run(%v) error = %v, want ErrUsage", args, err)
		}
	}
}

func TestRemovedCommandsAreEditSelectors(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"outdated", "bump"} {
		var out, errw bytes.Buffer
		trap := &helpTrap{}
		_ = cli.Run(t.Context(), trap.deps(&out, &errw), []string{name})
		if strings.Contains(out.String(), "USAGE") || strings.Contains(errw.String(), "unknown") {
			t.Errorf("%s is not an edit selector: stdout %q, stderr %q", name, out.String(), errw.String())
		}
	}
}

// selectionRunner answers `chezmoi execute-template` with a selection that has one
// pinned release whose binary does not exist, and fails any other process.
type selectionRunner struct {
	mu    sync.Mutex
	calls [][]string
}

const missingReleaseSelection = `{"os":"linux","role":"personal","releases":{"example-tool":{"dest":".local/bin/example-tool"}},"release":{"example-tool":{"version":"1.0.0"}}}`

func (r *selectionRunner) Run(_ context.Context, c chez.Cmd) error {
	r.mu.Lock()
	r.calls = append(r.calls, slices.Clone(c.Args))
	r.mu.Unlock()
	if slices.Contains(c.Args, "execute-template") {
		_, err := c.Stdout.Write([]byte(missingReleaseSelection))
		return err
	}
	return errors.New("unexpected process")
}

func (r *selectionRunner) deps(t *testing.T, out *bytes.Buffer) cli.Deps {
	t.Helper()
	src := t.TempDir() // no externals, no packages.toml
	return cli.Deps{
		Getenv: func(key string) string {
			switch key {
			case "NO_COLOR":
				return "1"
			case "CHEZMOI_SOURCE_DIR":
				return src
			}
			return ""
		},
		Stdout: out, Stderr: &bytes.Buffer{}, Runner: r,
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Home:     t.TempDir(), GOOS: "linux",
	}
}

func TestPkgStatusCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want error
	}{
		{"check fails on a missing tool", []string{"pkg", "status", "--offline", "--check"}, cli.ErrReported},
		{"report only", []string{"pkg", "status", "--offline"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			r := &selectionRunner{}
			if err := cli.Run(t.Context(), r.deps(t, &out), tt.args); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Errorf("Run(%v) = %v, want %v", tt.args, err, tt.want)
			}
			if !strings.Contains(out.String(), "example-tool") {
				t.Errorf("report lacks the tool: %q", out.String())
			}
		})
	}
}

func TestPkgStatusCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out bytes.Buffer
	r := &selectionRunner{}
	err := cli.Run(ctx, r.deps(t, &out), []string{"pkg", "status", "--offline"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if out.Len() != 0 {
		t.Errorf("printed a report after cancellation: %q", out.String())
	}
}

type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("unexpected HTTP request: %s", r.URL)
	return nil, errors.New("unexpected network")
}

func TestUpgradeDryRunMakesNoRequests(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	r := &selectionRunner{}
	d := r.deps(t, &out)
	d.HTTP = &http.Client{Transport: failingTransport{t}}
	if err := cli.Run(t.Context(), d, []string{"upgrade", "--dry-run"}); err != nil {
		t.Fatalf("upgrade --dry-run: %v", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("ran processes: %v", r.calls)
	}
	if strings.Contains(out.String(), "releases") {
		t.Errorf("upgrade still mentions releases: %q", out.String())
	}
}

const (
	oldCommit = "0123456789abcdef0123456789abcdef01234567"
	newCommit = "fedcba9876543210fedcba9876543210fedcba98"
	oldSum    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

var archiveBytes = []byte("fake archive bytes")

// archiveRunner answers the chezmoi and git calls the pkg commands make.
type archiveRunner struct {
	mu        sync.Mutex
	head      string
	lsRemotes []string
}

func (r *archiveRunner) Run(_ context.Context, c chez.Cmd) error {
	switch {
	case slices.Contains(c.Args, "execute-template"):
		_, err := c.Stdout.Write([]byte(`{"os":"linux","role":"personal"}`))
		return err
	case slices.Contains(c.Args, "data"):
		_, err := c.Stdout.Write([]byte(`{}`))
		return err
	case slices.Contains(c.Args, "ls-remote"):
		r.mu.Lock()
		r.lsRemotes = append(r.lsRemotes, c.Args[len(c.Args)-2])
		r.mu.Unlock()
		_, err := c.Stdout.Write([]byte(r.head + "\tHEAD\n"))
		return err
	}
	return errors.New("unexpected process")
}

type archiveFetcher struct {
	mu   sync.Mutex
	urls []string
}

func (f *archiveFetcher) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.urls = append(f.urls, r.URL.String())
	f.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(archiveBytes)),
		Header: http.Header{}, Request: r,
	}, nil
}

func archiveURL(sha string) string {
	return "https://github.com/octocat/example/archive/" + sha + ".tar.gz"
}

func externalsText(sha, sum string) string {
	return "# keep this comment\n{{ if true }}\n[\".vim/pack/example\"]\n    type = \"archive\"\n    url = \"" +
		archiveURL(sha) + "\"\n    checksum.sha256 = \"" + sum + "\"\n{{ end }}\n"
}

// archiveSource writes a source dir holding only an externals file.
func archiveSource(t *testing.T, text string) (src, file string) {
	t.Helper()
	src = t.TempDir()
	file = filepath.Join(src, ".chezmoiexternal.toml.tmpl")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return src, file
}

func archiveDeps(t *testing.T, src, head string, out *bytes.Buffer) (cli.Deps, *archiveRunner, *archiveFetcher) {
	t.Helper()
	r := &archiveRunner{head: head}
	f := &archiveFetcher{}
	return cli.Deps{
		Getenv: func(key string) string {
			switch key {
			case "NO_COLOR":
				return "1"
			case "CHEZMOI_SOURCE_DIR":
				return src
			}
			return ""
		},
		Stdout: out, Stderr: &bytes.Buffer{}, Runner: r, HTTP: &http.Client{Transport: f},
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Home:     t.TempDir(), GOOS: "linux",
	}, r, f
}

func TestPkgStatusArchives(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		head      string
		args      []string
		want      error
		lsRemotes int
	}{
		{"behind head fails check", newCommit, []string{"pkg", "status", "--check"}, cli.ErrReported, 1},
		{"at head passes", oldCommit, []string{"pkg", "status", "--check"}, nil, 1},
		{"offline looks nothing up", newCommit, []string{"pkg", "status", "--offline", "--check"}, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src, _ := archiveSource(t, externalsText(oldCommit, oldSum))
			var out bytes.Buffer
			d, r, _ := archiveDeps(t, src, tt.head, &out)
			if err := cli.Run(t.Context(), d, tt.args); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Errorf("Run(%v) = %v, want %v", tt.args, err, tt.want)
			}
			if !strings.Contains(out.String(), "octocat/example") {
				t.Errorf("report lacks the archive row: %q", out.String())
			}
			if len(r.lsRemotes) != tt.lsRemotes {
				t.Errorf("ls-remote calls = %v, want %d", r.lsRemotes, tt.lsRemotes)
			}
		})
	}
}

func TestPkgBumpArchives(t *testing.T) {
	t.Parallel()
	wantSum := fmt.Sprintf("%x", sha256.Sum256(archiveBytes))
	line := "octocat/example " + oldCommit[:7] + " → " + newCommit[:7] + "\n"
	tests := []struct {
		name      string
		head      string
		args      []string
		wantOut   string
		wantText  string // the externals file afterwards
		wantFetch int
	}{
		{
			"rewrites commit and checksum", newCommit,
			[]string{"pkg", "bump"},
			line + "pinned in %s; run chezmoi apply to install\n", externalsText(newCommit, wantSum), 1,
		},
		{
			"select by repository path", newCommit,
			[]string{"pkg", "bump", "octocat/example"},
			line + "pinned in %s; run chezmoi apply to install\n", externalsText(newCommit, wantSum), 1,
		},
		{
			"dry run writes nothing", newCommit,
			[]string{"pkg", "bump", "--dry-run"},
			line + "dry run: nothing written\n", externalsText(oldCommit, oldSum), 1,
		},
		{
			"up to date", oldCommit,
			[]string{"pkg", "bump"},
			"all pins are up to date\n", externalsText(oldCommit, oldSum), 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src, file := archiveSource(t, externalsText(oldCommit, oldSum))
			var out bytes.Buffer
			d, _, f := archiveDeps(t, src, tt.head, &out)
			if err := cli.Run(t.Context(), d, tt.args); err != nil {
				t.Fatalf("Run(%v): %v", tt.args, err)
			}
			want := tt.wantOut
			if strings.Contains(want, "%s") {
				want = fmt.Sprintf(want, file)
			}
			if diff := cmp.Diff(want, out.String()); diff != "" {
				t.Errorf("output (-want +got):\n%s", diff)
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.wantText, string(got)); diff != "" {
				t.Errorf("externals file (-want +got):\n%s", diff)
			}
			wantURLs := slices.Repeat([]string{archiveURL(newCommit)}, tt.wantFetch)
			if diff := cmp.Diff(wantURLs, f.urls, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("downloads (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPkgBumpUnknownPin(t *testing.T) {
	t.Parallel()
	src, _ := archiveSource(t, externalsText(oldCommit, oldSum))
	var out bytes.Buffer
	d, _, _ := archiveDeps(t, src, newCommit, &out)
	err := cli.Run(t.Context(), d, []string{"pkg", "bump", "octocat/example", "octocat/nosuch"})
	if !errors.Is(err, cli.ErrUsage) {
		t.Errorf("error = %v, want ErrUsage", err)
	}
	if out.Len() != 0 {
		t.Errorf("printed %q before the usage error", out.String())
	}
}

const sha512Entry = "[\".vim/pack/other\"]\n    url = \"https://github.com/octocat/other/archive/" + oldCommit +
	".tar.gz\"\n    checksum.sha512 = \"x\"\n"

// withSkippedEntry adds an externals file whose entry bump cannot handle and returns its path.
func withSkippedEntry(t *testing.T, src string) string {
	t.Helper()
	dir := filepath.Join(src, ".chezmoiexternals")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "other.toml")
	if err := os.WriteFile(file, []byte(sha512Entry), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestPkgBumpPrintsSkips(t *testing.T) {
	t.Parallel()
	src, file := archiveSource(t, externalsText(oldCommit, oldSum))
	other := withSkippedEntry(t, src)
	var out, errOut bytes.Buffer
	d, _, _ := archiveDeps(t, src, newCommit, &out)
	d.Stderr = &errOut
	if err := cli.Run(t.Context(), d, []string{"pkg", "bump"}); err != nil {
		t.Fatalf("Run(pkg bump) with a skipped entry: %v", err)
	}
	want := "skip " + other + " \".vim/pack/other\": checksum sha512 is not supported; bump it by hand\n"
	if diff := cmp.Diff(want, errOut.String()); diff != "" {
		t.Errorf("stderr (-want +got):\n%s", diff)
	}
	got, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sha512Entry {
		t.Errorf("skipped file changed:\n%s", got)
	}
	bumped, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bumped), newCommit) {
		t.Errorf("the pinned entry was not bumped:\n%s", bumped)
	}
}

func TestPkgStatusReportsSkips(t *testing.T) {
	t.Parallel()
	src, _ := archiveSource(t, externalsText(oldCommit, oldSum))
	withSkippedEntry(t, src)
	var out bytes.Buffer
	d, _, _ := archiveDeps(t, src, oldCommit, &out)
	if err := cli.Run(t.Context(), d, []string{"pkg", "status", "--offline"}); err != nil {
		t.Fatalf("Run(pkg status): %v", err)
	}
	if want := "skipped 1: \".vim/pack/other\": checksum sha512";!strings.Contains(out.String(), want) {
		t.Errorf("report lacks %q:\n%s", want, out.String())
	}
}

func TestPkgStatusSurvivesUnreadableDir(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("Unix permissions do not apply")
	}
	src, _ := archiveSource(t, externalsText(oldCommit, oldSum))
	locked := filepath.Join(src, "locked")
	if err := os.Mkdir(locked, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	var out bytes.Buffer
	d, _, _ := archiveDeps(t, src, oldCommit, &out)
	if err := cli.Run(t.Context(), d, []string{"pkg", "status", "--offline"}); err != nil {
		t.Fatalf("Run(pkg status) with an unreadable dir: %v", err)
	}
	for _, want := range []string{"octocat/example", "skipped 1: " + locked} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
}

// brokenDownload answers every archive download with a server error.
type brokenDownload struct{}

func (brokenDownload) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusBadGateway, Status: "502 Bad Gateway", Body: io.NopCloser(strings.NewReader("")),
		Header: http.Header{}, Request: r,
	}, nil
}

func TestPkgBumpArchiveFailureWritesNothing(t *testing.T) {
	t.Parallel()
	text := externalsText(oldCommit, oldSum)
	src, file := archiveSource(t, text)
	var out bytes.Buffer
	d, _, _ := archiveDeps(t, src, newCommit, &out)
	d.HTTP = &http.Client{Transport: brokenDownload{}}
	if err := cli.Run(t.Context(), d, []string{"pkg", "bump"}); err == nil {
		t.Fatal("Run(pkg bump) with a failing download: want an error")
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(text, string(got)); diff != "" {
		t.Errorf("externals file changed after a failed bump (-want +got):\n%s", diff)
	}
}
