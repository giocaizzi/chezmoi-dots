package cli_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

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
	return cli.Deps{
		Getenv: func(key string) string {
			if key == "NO_COLOR" {
				return "1"
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
