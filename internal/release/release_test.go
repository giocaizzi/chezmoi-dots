package release

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
)

func TestNewestTag(t *testing.T) {
	t.Parallel()
	ls := "a\trefs/tags/v1.2.0\nb\trefs/tags/v1.10.0\nc\trefs/tags/v1.9.0\nd\trefs/tags/v1.10.0-rc1\ne\trefs/tags/other-v9.0.0\nf\trefs/heads/main\n"
	got, err := newestTag(ls, "v")
	if err != nil || got != "1.10.0" {
		t.Errorf("newestTag() = %q, %v, want 1.10.0", got, err)
	}
	if _, err := newestTag(ls, "nope-v"); err == nil {
		t.Error("newestTag() without a matching tag: want an error")
	}
}

// recordRunner answers git ls-remote with one tag and git config with config.
type recordRunner struct {
	config string // what `git config --get core.sshCommand` prints
	lsEnv  []string
}

func (r *recordRunner) Run(_ context.Context, c chez.Cmd) error {
	if slices.Contains(c.Args, "config") {
		_, err := io.WriteString(c.Stdout, r.config)
		return err
	}
	r.lsEnv = c.Env
	_, err := io.WriteString(c.Stdout, "a\trefs/tags/v1.2.0\n")
	return err
}

func TestLatestGitNeverPrompts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		getenv map[string]string
		config string
		want   []string
	}{
		{"default", nil, "", []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"}},
		{"GIT_SSH_COMMAND set", map[string]string{"GIT_SSH_COMMAND": "ssh -i key"}, "", []string{"GIT_TERMINAL_PROMPT=0"}},
		{"core.sshCommand set", nil, "ssh -F cfg\n", []string{"GIT_TERMINAL_PROMPT=0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &recordRunner{config: tt.config}
			f := Finder{Runner: r, Getenv: func(k string) string { return tt.getenv[k] }}
			got, err := f.Latest(t.Context(), pkgreg.Upstream{Kind: "git", Repo: "ssh://git@example.com/octocat/hello.git", TagPrefix: "v"})
			if err != nil || got != "1.2.0" {
				t.Fatalf("Latest() = %q, %v, want 1.2.0", got, err)
			}
			if diff := cmp.Diff(tt.want, r.lsEnv); diff != "" {
				t.Errorf("env (-want +got):\n%s", diff)
			}
		})
	}
}

// headRunner answers git ls-remote with out, or err.
type headRunner struct {
	out  string
	err  error
	args []string
	env  []string
}

func (r *headRunner) Run(_ context.Context, c chez.Cmd) error {
	if slices.Contains(c.Args, "config") {
		return nil
	}
	r.args, r.env = c.Args, c.Env
	if r.err != nil {
		return r.err
	}
	_, err := io.WriteString(c.Stdout, r.out)
	return err
}

func TestHead(t *testing.T) {
	t.Parallel()
	const sha = "0123456789abcdef0123456789abcdef01234567"
	const remote = "https://example.com/octocat/hello.git"
	tests := []struct {
		name    string
		out     string
		err     error
		want    string
		wantErr bool
	}{
		{"ok", sha + "\tHEAD\n", nil, sha, false},
		{"head among others", "bbbb\trefs/heads/main\n" + sha + "\tHEAD\n", nil, sha, false},
		{"no HEAD line", sha + "\trefs/heads/main\n", nil, "", true},
		{"not a commit", "zzz\tHEAD\n", nil, "", true},
		{"empty", "", nil, "", true},
		{"runner error", "", errors.New("boom"), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &headRunner{out: tt.out, err: tt.err}
			f := Finder{Runner: r, Getenv: func(string) string { return "" }}
			got, err := f.Head(t.Context(), remote)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("Head() = %q, %v, want %q (err %v)", got, err, tt.want, tt.wantErr)
			}
			if diff := cmp.Diff([]string{"git", "ls-remote", remote, "HEAD"}, r.args); diff != "" {
				t.Errorf("args (-want +got):\n%s", diff)
			}
			if !slices.Contains(r.env, "GIT_TERMINAL_PROMPT=0") {
				t.Errorf("env %v lacks GIT_TERMINAL_PROMPT=0", r.env)
			}
		})
	}
}
