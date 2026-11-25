package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

// statusRunner answers the commands of status from canned output.
type statusRunner struct {
	status    string
	statusErr error
	behind    string // rev-list --count output, "ahead<TAB>behind"
	dirty     string // git status --porcelain output
	gitErr    error
}

func (r statusRunner) Run(_ context.Context, c chez.Cmd) error {
	write := func(s string) error {
		_, err := io.WriteString(c.Stdout, s)
		return err
	}
	line := strings.Join(c.Args, " ")
	switch {
	case c.Args[0] == "git" && r.gitErr != nil:
		return r.gitErr
	case strings.HasSuffix(line, "dump-config --format json"):
		return write(`{"workingTree":"/example/dotfiles"}`)
	case strings.HasSuffix(line, " status"):
		if r.statusErr != nil {
			return r.statusErr
		}
		return write(r.status)
	case strings.HasSuffix(line, "rev-parse --abbrev-ref HEAD"):
		return write("main\n")
	case strings.HasSuffix(line, "@{upstream}") && strings.Contains(line, "rev-parse"):
		return write("origin/main\n")
	case strings.Contains(line, "rev-list"):
		return write(r.behind)
	case strings.HasSuffix(line, "status --porcelain"):
		return write(r.dirty)
	}
	return errors.New("unexpected command: " + line)
}

func statusDeps(r chez.Runner, out io.Writer) cli.Deps {
	return cli.Deps{Getenv: func(string) string { return "" }, Stdout: out, Stderr: io.Discard, Runner: r, GOOS: "linux", Home: "/example/home"}
}

func TestStatusOutputAndCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		r       statusRunner
		args    []string
		want    []string
		wantErr error
	}{
		{
			"clean",
			statusRunner{behind: "0\t0\n"},
			[]string{"status", "--check"},
			[]string{"dotfiles  main · up to date with upstream · not fetched", "apply     up to date"},
			nil,
		},
		{
			"targets differ",
			statusRunner{status: "MM .zshrc\n A .config/example/config.toml\n", behind: "0\t0\n"},
			[]string{"status"},
			[]string{"apply     2 targets differ", "  MM .zshrc", "   A .config/example/config.toml"},
			nil,
		},
		{
			"targets differ with check",
			statusRunner{status: "MM .zshrc\n", behind: "0\t0\n"},
			[]string{"status", "--check"},
			[]string{"apply     1 target differs"},
			cli.ErrReported,
		},
		{
			"behind upstream with check",
			statusRunner{behind: "0\t3\n"},
			[]string{"status", "--check"},
			[]string{"3 behind upstream"},
			cli.ErrReported,
		},
		{
			"behind upstream without check",
			statusRunner{behind: "0\t3\n"},
			[]string{"status"},
			[]string{"3 behind upstream"},
			nil,
		},
		{
			"local changes with check",
			statusRunner{behind: "0\t0\n", dirty: " M a\n?? b\n"},
			[]string{"status", "--check"},
			[]string{"2 local changes"},
			cli.ErrReported,
		},
		{
			"unknown tree is not a failure",
			statusRunner{gitErr: errors.New("not a repo")},
			[]string{"status", "--check"},
			[]string{"dotfiles  not a git checkout", "apply     up to date"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := cli.Run(t.Context(), statusDeps(tt.r, &out), tt.args)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Run(%v) error = %v, want %v", tt.args, err, tt.wantErr)
			}
			for _, w := range tt.want {
				if !strings.Contains(out.String(), w) {
					t.Errorf("output lacks %q:\n%s", w, out.String())
				}
			}
		})
	}
}

func TestStatusFailureIsReturned(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	err := cli.Run(t.Context(), statusDeps(statusRunner{statusErr: boom, gitErr: boom}, io.Discard), []string{"status"})
	if !errors.Is(err, boom) || errors.Is(err, cli.ErrReported) {
		t.Errorf("status error = %v, want the chezmoi failure", err)
	}
}

func TestStatusUsage(t *testing.T) {
	t.Parallel()
	err := cli.Run(t.Context(), statusDeps(statusRunner{}, io.Discard), []string{"status", "extra"})
	if !errors.Is(err, cli.ErrUsage) {
		t.Errorf("status extra error = %v, want ErrUsage", err)
	}
}

func TestStatusOnTheFixture(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	out, stderr, err := e.run(t, "", "status")
	if err != nil {
		t.Fatalf("status = %v\n%s", err, stderr)
	}
	for _, want := range []string{"not a git checkout", ".note"} {
		if !strings.Contains(out, want) {
			t.Errorf("status = %q, want it to mention %q", out, want)
		}
	}
	if _, _, cerr := e.run(t, "", "status", "--check"); !errors.Is(cerr, cli.ErrReported) {
		t.Errorf("status --check before apply = %v, want ErrReported", cerr)
	}

	cz := chez.New(chez.ExecRunner{}, "chezmoi", "--config", e.vars["CHEZMOI_CONFIG_FILE"], "--source", e.src)
	if aerr := cz.Apply(t.Context(), io.Discard, io.Discard); aerr != nil {
		t.Fatalf("apply: %v", aerr)
	}
	out, stderr, err = e.run(t, "", "status", "--check")
	if err != nil {
		t.Errorf("status --check after apply = %v\n%s%s", err, out, stderr)
	}
}
