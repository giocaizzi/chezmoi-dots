package detect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
)

// reply is the canned answer to one argv.
type reply struct {
	stdout string
	export string // winget: written to the --output file
	err    error
}

// fakeRunner answers by the space-joined argv (the --output path left out) and
// records the calls.
type fakeRunner struct {
	mu        sync.Mutex
	replies   map[string]reply
	calls     []string
	envs      map[string][]string
	deadlines []time.Duration
	files     []string // winget --output paths seen
}

func (f *fakeRunner) Run(ctx context.Context, c chez.Cmd) error {
	key := strings.Join(c.Args, " ")
	out := ""
	if i := slices.Index(c.Args, "--output"); i >= 0 {
		out = c.Args[i+1]
		key = strings.Join(slices.Concat(c.Args[:i+1], c.Args[i+2:]), " ")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key)
	if f.envs == nil {
		f.envs = map[string][]string{}
	}
	f.envs[key] = c.Env
	if dl, ok := ctx.Deadline(); ok {
		f.deadlines = append(f.deadlines, time.Until(dl))
	}
	r, ok := f.replies[key]
	if !ok {
		return errors.New("exit 1")
	}
	if r.export != "" && out != "" {
		f.files = append(f.files, out)
		if err := os.WriteFile(out, []byte(r.export), 0o600); err != nil {
			return err
		}
	}
	_, _ = c.Stdout.Write([]byte(r.stdout))
	return r.err
}

func lookPath(present ...string) func(string) (string, error) {
	return func(s string) (string, error) {
		if slices.Contains(present, s) {
			return "/bin/" + s, nil
		}
		return "", errors.New("not found")
	}
}

const (
	brewFormulaInstalled = `{"formulae":[{"full_name":"ripgrep","installed":[{"version":"14"}]}],"casks":[]}`
	brewFormulaAvailable = `{"formulae":[{"full_name":"ripgrep","installed":[]}],"casks":[]}`
	brewCaskInstalled    = `{"formulae":[],"casks":[{"token":"iterm2","installed":"3.5"}]}`
	brewCaskAvailable    = `{"formulae":[],"casks":[{"token":"iterm2","installed":null}]}`
	brewEmpty            = `{"formulae":[],"casks":[]}`
	aptInstalled         = "ripgrep:\n  Installed: 14.1\n  Candidate: 14.1\n"
	aptAvailable         = "ripgrep:\n  Installed: (none)\n  Candidate: 14.1\n"
	aptNone              = "ripgrep:\n  Installed: (none)\n  Candidate: (none)\n"
	uvListing            = "ripgrep v1.0.0\n- rg\n"
	npmListing           = `{"dependencies":{"ripgrep":{"version":"1.0.0"}}}`
	wingetOne            = `{"Sources":[{"Packages":[{"PackageIdentifier":"Example.RipGrep","Version":"1"},{"PackageIdentifier":"Other.Thing","Version":"2"}]}]}`
	wingetTwo            = `{"Sources":[{"Packages":[{"PackageIdentifier":"Example.ripgrep","Version":"1"},{"PackageIdentifier":"Other.ripgrep","Version":"2"}]}]}`
	wingetExact          = `{"Sources":[{"Packages":[{"PackageIdentifier":"ripgrep","Version":"1"}]}]}`
)

const (
	brewCmd   = "brew info --json=v2 ripgrep"
	aptCmd    = "apt-cache policy ripgrep"
	wingetCmd = "winget export --output --include-versions --accept-source-agreements --disable-interactivity"
	uvCmd     = "uv tool list"
	npmCmd    = "npm ls -g --depth=0 --json"
)

func TestDetect(t *testing.T) {
	t.Parallel()
	home := filepath.Join(string(filepath.Separator), "home", "u")
	npmLinux := npmCmd + " --prefix " + filepath.Join(home, ".local")
	errExit := errors.New("exit 1")
	tests := []struct {
		name     string
		goos     string
		present  []string
		replies  map[string]reply
		wantFind []Found
		wantNone []string
		wantCmds []string
	}{
		{
			name: "brew formula installed", goos: "darwin", present: []string{"brew"},
			replies:  map[string]reply{brewCmd: {stdout: brewFormulaInstalled}},
			wantFind: []Found{{"brew", "ripgrep", "installed"}},
			wantCmds: []string{brewCmd},
		},
		{
			name: "brew formula available", goos: "darwin", present: []string{"brew"},
			replies:  map[string]reply{brewCmd: {stdout: brewFormulaAvailable}},
			wantFind: []Found{{"brew", "ripgrep", "available"}},
			wantCmds: []string{brewCmd},
		},
		{
			name: "brew cask installed", goos: "darwin", present: []string{"brew"},
			replies:  map[string]reply{brewCmd: {stdout: brewCaskInstalled}},
			wantFind: []Found{{"cask", "iterm2", "installed"}},
			wantCmds: []string{brewCmd},
		},
		{
			name: "brew cask available", goos: "darwin", present: []string{"brew"},
			replies:  map[string]reply{brewCmd: {stdout: brewCaskAvailable}},
			wantFind: []Found{{"cask", "iterm2", "available"}},
			wantCmds: []string{brewCmd},
		},
		{
			name: "brew nothing", goos: "darwin", present: []string{"brew"},
			replies:  map[string]reply{brewCmd: {stdout: brewEmpty}},
			wantNone: []string{"brew"},
			wantCmds: []string{brewCmd},
		},
		{
			name: "brew command fails", goos: "darwin", present: []string{"brew"},
			replies:  map[string]reply{brewCmd: {err: errExit}},
			wantNone: []string{"brew"},
			wantCmds: []string{brewCmd},
		},
		{
			name: "brew bad json", goos: "darwin", present: []string{"brew"},
			replies:  map[string]reply{brewCmd: {stdout: "not json"}},
			wantNone: []string{"brew"},
			wantCmds: []string{brewCmd},
		},
		{
			name: "apt installed", goos: "linux", present: []string{"apt-cache"},
			replies:  map[string]reply{aptCmd: {stdout: aptInstalled}},
			wantFind: []Found{{"apt", "ripgrep", "installed"}},
			wantCmds: []string{aptCmd},
		},
		{
			name: "apt available", goos: "linux", present: []string{"apt-cache"},
			replies:  map[string]reply{aptCmd: {stdout: aptAvailable}},
			wantFind: []Found{{"apt", "ripgrep", "available"}},
			wantCmds: []string{aptCmd},
		},
		{
			name: "apt none", goos: "linux", present: []string{"apt-cache"},
			replies:  map[string]reply{aptCmd: {stdout: aptNone}},
			wantNone: []string{"apt"},
			wantCmds: []string{aptCmd},
		},
		{
			name: "apt unknown package", goos: "linux", present: []string{"apt-cache"},
			replies:  map[string]reply{aptCmd: {stdout: ""}},
			wantNone: []string{"apt"},
			wantCmds: []string{aptCmd},
		},
		{
			name: "apt command fails", goos: "linux", present: []string{"apt-cache"},
			replies:  map[string]reply{aptCmd: {stdout: aptInstalled, err: errExit}},
			wantNone: []string{"apt"},
			wantCmds: []string{aptCmd},
		},
		{
			name: "winget by last segment", goos: "windows", present: []string{"winget"},
			replies:  map[string]reply{wingetCmd: {export: wingetOne}},
			wantFind: []Found{{"winget", "example.ripgrep", "installed"}},
			wantCmds: []string{wingetCmd},
		},
		{
			name: "winget exact id", goos: "windows", present: []string{"winget"},
			replies:  map[string]reply{wingetCmd: {export: wingetExact}},
			wantFind: []Found{{"winget", "ripgrep", "installed"}},
			wantCmds: []string{wingetCmd},
		},
		{
			name: "winget two matches", goos: "windows", present: []string{"winget"},
			replies:  map[string]reply{wingetCmd: {export: wingetTwo}},
			wantNone: []string{"winget"},
			wantCmds: []string{wingetCmd},
		},
		{
			name: "winget non-zero exit with export", goos: "windows", present: []string{"winget"},
			replies:  map[string]reply{wingetCmd: {export: wingetOne, err: errExit}},
			wantFind: []Found{{"winget", "example.ripgrep", "installed"}},
			wantCmds: []string{wingetCmd},
		},
		{
			name: "winget fails without export", goos: "windows", present: []string{"winget"},
			replies:  map[string]reply{wingetCmd: {err: errExit}},
			wantNone: []string{"winget"},
			wantCmds: []string{wingetCmd},
		},
		{
			name: "winget no match", goos: "windows", present: []string{"winget"},
			replies:  map[string]reply{wingetCmd: {export: `{"Sources":[]}`}},
			wantNone: []string{"winget"},
			wantCmds: []string{wingetCmd},
		},
		{
			name: "uv installed", goos: "darwin", present: []string{"uv"},
			replies:  map[string]reply{uvCmd: {stdout: uvListing}},
			wantFind: []Found{{"uv", "ripgrep", "installed"}},
			wantCmds: []string{uvCmd},
		},
		{
			name: "uv absent tool", goos: "darwin", present: []string{"uv"},
			replies:  map[string]reply{uvCmd: {stdout: "other v1\n"}},
			wantNone: []string{"uv"},
			wantCmds: []string{uvCmd},
		},
		{
			name: "uv fails", goos: "darwin", present: []string{"uv"},
			replies:  map[string]reply{uvCmd: {err: errExit}},
			wantNone: []string{"uv"},
			wantCmds: []string{uvCmd},
		},
		{
			name: "npm installed", goos: "darwin", present: []string{"npm"},
			replies:  map[string]reply{npmCmd: {stdout: npmListing}},
			wantFind: []Found{{"npm", "ripgrep", "installed"}},
			wantCmds: []string{npmCmd},
		},
		{
			name: "npm installed despite exit 1", goos: "windows", present: []string{"npm"},
			replies:  map[string]reply{npmCmd: {stdout: npmListing, err: errExit}},
			wantFind: []Found{{"npm", "ripgrep", "installed"}},
			wantCmds: []string{npmCmd},
		},
		{
			name: "npm uses the local prefix on linux", goos: "linux", present: []string{"npm"},
			replies:  map[string]reply{npmLinux: {stdout: npmListing}},
			wantFind: []Found{{"npm", "ripgrep", "installed"}},
			wantCmds: []string{npmLinux},
		},
		{
			name: "npm absent package", goos: "darwin", present: []string{"npm"},
			replies:  map[string]reply{npmCmd: {stdout: `{"dependencies":{}}`}},
			wantNone: []string{"npm"},
			wantCmds: []string{npmCmd},
		},
		{
			name: "npm fails", goos: "darwin", present: []string{"npm"},
			replies:  map[string]reply{npmCmd: {err: errExit}},
			wantNone: []string{"npm"},
			wantCmds: []string{npmCmd},
		},
		{
			name: "managers absent are not probed nor listed", goos: "darwin", present: nil,
			replies: map[string]reply{brewCmd: {stdout: brewFormulaInstalled}},
		},
		{
			name: "key order across managers", goos: "darwin", present: []string{"brew", "uv", "npm"},
			replies: map[string]reply{
				brewCmd: {stdout: brewFormulaInstalled},
				uvCmd:   {stdout: uvListing},
				npmCmd:  {stdout: npmListing},
			},
			wantFind: []Found{{"brew", "ripgrep", "installed"}, {"uv", "ripgrep", "installed"}, {"npm", "ripgrep", "installed"}},
			wantCmds: []string{brewCmd, uvCmd, npmCmd},
		},
		{
			name: "GOOS darwin ignores apt and winget", goos: "darwin",
			present: []string{"apt-cache", "winget"},
			replies: map[string]reply{aptCmd: {stdout: aptInstalled}},
		},
		{
			name: "GOOS linux ignores brew", goos: "linux",
			present:  []string{"brew", "apt-cache"},
			replies:  map[string]reply{aptCmd: {stdout: aptAvailable}, brewCmd: {stdout: brewFormulaInstalled}},
			wantFind: []Found{{"apt", "ripgrep", "available"}},
			wantCmds: []string{aptCmd},
		},
		{
			name: "mixed found and none", goos: "darwin", present: []string{"brew", "uv"},
			replies:  map[string]reply{brewCmd: {stdout: brewCaskAvailable}, uvCmd: {stdout: ""}},
			wantFind: []Found{{"cask", "iterm2", "available"}},
			wantNone: []string{"uv"},
			wantCmds: []string{brewCmd, uvCmd},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &fakeRunner{replies: tt.replies}
			d := Detector{Runner: r, LookPath: lookPath(tt.present...), GOOS: tt.goos, Home: home}
			found, none := d.Detect(t.Context(), "ripgrep")
			if diff := cmp.Diff(tt.wantFind, found); diff != "" {
				t.Errorf("found (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantNone, none); diff != "" {
				t.Errorf("none (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantCmds, r.calls); diff != "" {
				t.Errorf("commands (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDetectRejectsBadNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "a b", "a;rm", "$(x)", "a\nb", "-rf", "--json", "a`b`", "\u540d\u524d"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := &fakeRunner{}
			d := Detector{Runner: r, LookPath: lookPath("brew", "uv", "npm"), GOOS: "darwin"}
			found, none := d.Detect(t.Context(), name)
			if len(found) != 0 || len(none) != 0 || len(r.calls) != 0 {
				t.Errorf("Detect(%q) = %v, %v, ran %v; want nothing", name, found, none, r.calls)
			}
		})
	}
}

func TestDetectPassesNameAsOneArgument(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{replies: map[string]reply{"brew info --json=v2 owner/tap/pkg@1": {stdout: brewEmpty}}}
	d := Detector{Runner: r, LookPath: lookPath("brew"), GOOS: "darwin"}
	_, none := d.Detect(t.Context(), "owner/tap/pkg@1")
	if diff := cmp.Diff([]string{"brew"}, none); diff != "" || len(r.calls) != 1 {
		t.Errorf("none (-want +got):\n%s\ncalls %v", diff, r.calls)
	}
}

func TestDetectEnvironment(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	Detector{Runner: r, LookPath: lookPath("brew"), GOOS: "darwin"}.Detect(t.Context(), "ripgrep")
	if diff := cmp.Diff([]string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1"}, r.envs[brewCmd]); diff != "" {
		t.Errorf("brew env (-want +got):\n%s", diff)
	}
	r = &fakeRunner{}
	Detector{Runner: r, LookPath: lookPath("apt-cache"), GOOS: "linux"}.Detect(t.Context(), "ripgrep")
	if diff := cmp.Diff([]string{"LC_ALL=C"}, r.envs[aptCmd]); diff != "" {
		t.Errorf("apt env (-want +got):\n%s", diff)
	}
}

func TestDetectTimeouts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		goos    string
		present string
		timeout time.Duration
		want    time.Duration
	}{
		{"default", "darwin", "uv", 0, 10 * time.Second},
		{"custom", "darwin", "uv", 3 * time.Second, 3 * time.Second},
		{"winget default", "windows", "winget", 0, time.Minute},
		{"winget custom", "windows", "winget", 3 * time.Second, 3 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &fakeRunner{}
			d := Detector{Runner: r, LookPath: lookPath(tt.present), GOOS: tt.goos, Timeout: tt.timeout}
			d.Detect(t.Context(), "ripgrep")
			if len(r.deadlines) != 1 {
				t.Fatalf("deadlines = %v, want one", r.deadlines)
			}
			if got := r.deadlines[0]; got > tt.want || got < tt.want-5*time.Second {
				t.Errorf("deadline in %v, want about %v", got, tt.want)
			}
		})
	}
}

func TestWingetRemovesTempDir(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{replies: map[string]reply{wingetCmd: {export: wingetOne}}}
	d := Detector{Runner: r, LookPath: lookPath("winget"), GOOS: "windows"}
	d.Detect(t.Context(), "ripgrep")
	if len(r.files) != 1 {
		t.Fatalf("export files = %v, want one", r.files)
	}
	if _, err := os.Stat(r.files[0]); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("export file %s still exists (err %v)", r.files[0], err)
	}
}

func TestOSKeys(t *testing.T) {
	t.Parallel()
	want := map[string][]string{
		"darwin":  {"brew", "cask"},
		"linux":   {"apt"},
		"windows": {"winget"},
	}
	if diff := cmp.Diff(want, OSKeys()); diff != "" {
		t.Errorf("OSKeys() (-want +got):\n%s", diff)
	}
}
