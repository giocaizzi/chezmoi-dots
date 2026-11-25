package release

import (
	"context"
	"io"
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

type recordRunner struct{ got chez.Cmd }

func (r *recordRunner) Run(_ context.Context, c chez.Cmd) error {
	r.got = c
	_, err := io.WriteString(c.Stdout, "a\trefs/tags/v1.2.0\n")
	return err
}

func TestLatestGitNeverPrompts(t *testing.T) {
	t.Parallel()
	r := &recordRunner{}
	got, err := Finder{Runner: r}.Latest(t.Context(), pkgreg.Upstream{Kind: "git", Repo: "https://example.com/octocat/hello.git", TagPrefix: "v"})
	if err != nil || got != "1.2.0" {
		t.Fatalf("Latest() = %q, %v, want 1.2.0", got, err)
	}
	if diff := cmp.Diff([]string{"GIT_TERMINAL_PROMPT=0"}, r.got.Env); diff != "" {
		t.Errorf("env (-want +got):\n%s", diff)
	}
}
