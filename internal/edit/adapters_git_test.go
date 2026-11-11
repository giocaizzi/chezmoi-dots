package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
)

func TestExecGitCommitOnlyLiteralPath(t *testing.T) {
	t.Parallel()
	g := ExecGit{Runner: chez.ExecRunner{}}
	dir := gitWorktree(t, g)
	const edited, other = "[x].txt", "x.txt"
	writeGitFile(t, dir, edited, "original")
	writeGitFile(t, dir, other, "original")
	gitCommand(t, g, dir, "add", "--", edited, other)
	gitCommand(t, g, dir, "commit", "-m", "initial")

	writeGitFile(t, dir, other, "staged separately")
	gitCommand(t, g, dir, "add", "--", other)
	if status, err := g.FileStatus(t.Context(), dir, edited); err != nil || status != "" {
		t.Fatalf("FileStatus() = %q, %v, want clean edited path", status, err)
	}
	writeGitFile(t, dir, edited, "edited")
	if err := g.Stage(t.Context(), dir, edited); err != nil {
		t.Fatal(err)
	}
	if got := gitCommand(t, g, dir, "diff", "--cached", "--name-only", "--", edited); got != edited {
		t.Fatalf("staged edited path = %q, want %q", got, edited)
	}
	if err := g.Commit(t.Context(), dir, edited); err != nil {
		t.Fatal(err)
	}
	if got := gitCommand(t, g, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"); got != edited {
		t.Errorf("committed paths = %q, want %q", got, edited)
	}
	if got := gitCommand(t, g, dir, "diff", "--cached", "--name-only"); got != other {
		t.Errorf("remaining staged paths = %q, want %q", got, other)
	}
}

func TestExecGitPushRejectionKeepsLocalCommit(t *testing.T) {
	t.Parallel()
	g := ExecGit{Runner: chez.ExecRunner{}}
	dir := gitWorktree(t, g)
	remote := t.TempDir()
	gitCommand(t, g, remote, "-c", "init.templateDir=", "init", "--bare")
	gitCommand(t, g, dir, "remote", "add", "origin", filepath.ToSlash(remote))
	gitCommand(t, g, dir, "config", "branch.main.remote", "origin")
	gitCommand(t, g, dir, "config", "branch.main.merge", "refs/heads/main")

	const path = "example.txt"
	writeGitFile(t, dir, path, "first")
	if status, err := g.FileStatus(t.Context(), dir, path); err != nil || !strings.HasPrefix(status, "?? ") {
		t.Fatalf("FileStatus() = %q, %v, want untracked path", status, err)
	}
	if err := g.Stage(t.Context(), dir, path); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit(t.Context(), dir, path); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	pushed := gitCommand(t, g, dir, "rev-parse", "HEAD")
	gitCommand(t, g, remote, "config", "receive.hideRefs", "refs/heads/main")

	writeGitFile(t, dir, path, "second")
	if err := g.Commit(t.Context(), dir, path); err != nil {
		t.Fatal(err)
	}
	local := gitCommand(t, g, dir, "rev-parse", "HEAD")
	if err := g.Push(t.Context(), dir); err == nil || !strings.Contains(err.Error(), "deny updating a hidden ref") {
		t.Fatalf("Push() = %v, want Git's remote rejection", err)
	}
	if got := gitCommand(t, g, dir, "rev-parse", "HEAD"); got != local || got == pushed {
		t.Errorf("local HEAD = %q, want retained commit %q after %q", got, local, pushed)
	}
	if got := gitCommand(t, g, remote, "rev-parse", "refs/heads/main"); got != pushed {
		t.Errorf("remote main = %q, want %q", got, pushed)
	}
}

func gitWorktree(t *testing.T, g ExecGit) string {
	t.Helper()
	dir := t.TempDir()
	gitCommand(t, g, dir, "-c", "init.templateDir=", "init", "--initial-branch=main")
	gitCommand(t, g, dir, "config", "user.name", "Example")
	gitCommand(t, g, dir, "config", "user.email", "example@example.com")
	gitCommand(t, g, dir, "config", "commit.gpgSign", "false")
	gitCommand(t, g, dir, "config", "push.default", "simple")
	gitCommand(t, g, dir, "config", "core.hooksPath", t.TempDir())
	return dir
}

func gitCommand(t *testing.T, g ExecGit, dir string, args ...string) string {
	t.Helper()
	out, err := g.run(t.Context(), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func writeGitFile(t *testing.T, dir, path, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
