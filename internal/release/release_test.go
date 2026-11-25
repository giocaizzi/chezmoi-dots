package release

import (
	"context"
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
