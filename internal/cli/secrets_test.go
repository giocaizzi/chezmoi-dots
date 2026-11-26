package cli_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

type codedError struct{ code int }

func (e codedError) Error() string { return "exit status" }
func (e codedError) ExitCode() int { return e.code }

// cliRunner scripts the secret-manager CLI and the keyring read, and runs chezmoi for real.
type cliRunner struct {
	mu     sync.Mutex
	cliRun []chez.Cmd
	stdout string
	err    error
}

func (r *cliRunner) Run(ctx context.Context, c chez.Cmd) error {
	base := filepath.Base(c.Args[0])
	isKeyring := slices.Contains(c.Args, "keyring") && slices.Contains(c.Args, "get") && slices.Contains(c.Args, "secret")
	switch {
	case isKeyring:
		_, _ = c.Stdout.Write([]byte(auditToken + "\n"))
		return nil
	case base == "bws" || base == "bw" || base == "rbw":
		r.mu.Lock()
		c.Args = slices.Clone(c.Args)
		r.cliRun = append(r.cliRun, c)
		r.mu.Unlock()
		_, _ = c.Stdout.Write([]byte(r.stdout))
		return r.err
	}
	return chez.ExecRunner{}.Run(ctx, c)
}

func TestSecretsRun(t *testing.T) {
	t.Parallel()
	passthrough := []string{"secret", "list", "proj-1", "--output", "json", "-h", "--color", "no"}
	tests := []struct {
		name       string
		runner     *cliRunner
		args       []string
		wantErr    error
		wantCode   int // >0: the error must be a *cli.ExitCodeError with this code
		wantArgv   []string
		wantOut    string
		wantOutHas []string
		wantErrOut []string
	}{
		{
			name: "passthrough", runner: &cliRunner{stdout: `[{"key":"K"}]`},
			args:     append([]string{"secrets", "run", "example-vault", "--"}, passthrough...),
			wantArgv: append([]string{"/fake/bws"}, passthrough...), wantOut: `[{"key":"K"}]`,
		},
		{
			name: "flags before the separator are ours", runner: &cliRunner{},
			args:     []string{"secrets", "run", "--timeout", "1m", "example-vault", "--", "--timeout", "5"},
			wantArgv: []string{"/fake/bws", "--timeout", "5"},
		},
		{
			name: "exit code", runner: &cliRunner{err: codedError{3}}, wantCode: 3,
			args: []string{"secrets", "run", "example-vault", "--", "project", "list"}, wantArgv: []string{"/fake/bws", "project", "list"},
		},
		{name: "no subcommand shows the group", runner: &cliRunner{}, args: []string{"secrets"}, wantOutHas: []string{"COMMANDS", "run"}},
		{name: "unknown subcommand", runner: &cliRunner{}, args: []string{"secrets", "nosuch"}, wantErr: cli.ErrUsage, wantErrOut: []string{"unknown secrets command", "run"}},
		{name: "no name", runner: &cliRunner{}, args: []string{"secrets", "run"}, wantErr: cli.ErrUsage, wantErrOut: []string{"secrets run needs a name"}},
		{name: "no name before separator", runner: &cliRunner{}, args: []string{"secrets", "run", "--", "project"}, wantErr: cli.ErrUsage, wantErrOut: []string{"secrets run needs a name"}},
		{name: "undeclared", runner: &cliRunner{}, args: []string{"secrets", "run", "other", "--", "x"}, wantErr: cli.ErrUsage, wantErrOut: []string{`secret "other"`, "secrets.toml"}},
		{name: "kind bw", runner: &cliRunner{}, args: []string{"secrets", "run", "passwords", "--", "x"}, wantErr: cli.ErrUsage, wantErrOut: []string{"kind bw has no token injection"}},
		{name: "unknown flag", runner: &cliRunner{}, args: []string{"secrets", "run", "example-vault", "--output", "json"}, wantErr: cli.ErrUsage, wantErrOut: []string{"put the CLI's arguments after --"}},
		{name: "negative timeout", runner: &cliRunner{}, args: []string{"secrets", "run", "example-vault", "--timeout=-1s"}, wantErr: cli.ErrUsage, wantErrOut: []string{"--timeout"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := secretsEnv(t, secretsRegistry)
			stdout, stderr, err := e.runWith(t, tt.runner, tt.args...)
			switch {
			case tt.wantCode > 0:
				exit, ok := errors.AsType[*cli.ExitCodeError](err)
				if !ok || exit.Code != tt.wantCode {
					t.Fatalf("error = %v, want ExitCodeError %d", err, tt.wantCode)
				}
			case !errors.Is(err, tt.wantErr):
				t.Fatalf("error = %v, want %v\nstderr:\n%s", err, tt.wantErr, stderr)
			}
			checkCLIRun(t, tt.runner, tt.wantArgv)
			if tt.wantOut != "" && stdout != tt.wantOut {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantOut)
			}
			assertContains(t, "stdout", stdout, tt.wantOutHas)
			assertContains(t, "stderr", stderr, tt.wantErrOut)
			if strings.Contains(stdout+stderr, auditToken) {
				t.Errorf("the token leaked:\n%s\n%s", stdout, stderr)
			}
		})
	}
}

// checkCLIRun asserts the one run of the CLI (nil argv: none) and its injected token.
func checkCLIRun(t *testing.T, r *cliRunner, wantArgv []string) {
	t.Helper()
	if wantArgv == nil {
		if len(r.cliRun) != 0 {
			t.Errorf("CLI runs = %d, want none", len(r.cliRun))
		}
		return
	}
	if len(r.cliRun) != 1 {
		t.Fatalf("CLI runs = %d, want 1", len(r.cliRun))
	}
	c := r.cliRun[0]
	if diff := cmp.Diff(wantArgv, c.Args); diff != "" {
		t.Errorf("argv (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"BWS_ACCESS_TOKEN=" + auditToken}, c.Env); diff != "" {
		t.Errorf("env (-want +got):\n%s", diff)
	}
}
