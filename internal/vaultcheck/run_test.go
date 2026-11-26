package vaultcheck_test

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/vaultcheck"
)

// childRunner stands for the CLI: it records the Cmd and acts out its reply.
type childRunner struct {
	calls  []chez.Cmd
	stdout string
	err    error
	block  bool
}

func (c *childRunner) Run(ctx context.Context, cmd chez.Cmd) error {
	cmd.Args = slices.Clone(cmd.Args)
	c.calls = append(c.calls, cmd)
	if c.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if cmd.Stdout != nil {
		_, _ = cmd.Stdout.Write([]byte(c.stdout))
	}
	return c.err
}

type exitCodedError struct{ code int }

func (e exitCodedError) Error() string { return "exit status" }
func (e exitCodedError) ExitCode() int { return e.code }

func registry(kind string, extra map[string]any) m {
	entry := m{"kind": kind}
	if kind == "bws" {
		entry["project"] = "proj-1"
		entry["keyring"] = m{"service": "bws", "user": "access-token"}
	}
	maps.Copy(entry, extra)
	return m{"secret": m{"example-vault": entry}}
}

type execRig struct {
	runner  *childRunner
	keyring *fakeKeyring
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	stdin   *strings.Reader
	exec    vaultcheck.Exec
}

func newRig(have ...string) *execRig {
	r := &execRig{runner: &childRunner{}, keyring: &fakeKeyring{value: token}, stdin: strings.NewReader("in")}
	r.exec = vaultcheck.Exec{
		Runner: r.runner, LookPath: lookPath(have...), Keyring: r.keyring, Home: "/home/u",
		Stdin: r.stdin, Stdout: &r.stdout, Stderr: &r.stderr,
	}
	return r
}

func TestExecRunOK(t *testing.T) {
	t.Parallel()
	r := newRig("bws")
	r.runner.stdout = `[{"key":"K","value":"V"}]`
	args := []string{"secret", "list", "proj-1", "--output", "json", "-h"}
	if err := r.exec.Run(t.Context(), registry("bws", nil), "example-vault", args, 0); err != nil {
		t.Fatal(err)
	}
	if len(r.runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(r.runner.calls))
	}
	c := r.runner.calls[0]
	if diff := cmp.Diff(append([]string{"/fake/bws"}, args...), c.Args); diff != "" {
		t.Errorf("argv (-want +got):\n%s", diff)
	}
	for _, sh := range []string{"sh", "bash", "cmd", "powershell", "pwsh"} {
		if filepath.Base(c.Args[0]) == sh {
			t.Errorf("argv[0] is a shell: %s", c.Args[0])
		}
	}
	if diff := cmp.Diff([]string{"BWS_ACCESS_TOKEN=" + token}, c.Env); diff != "" {
		t.Errorf("env (-want +got):\n%s", diff)
	}
	if slices.ContainsFunc(c.Args, func(a string) bool { return strings.Contains(a, token) }) {
		t.Error("the token is in argv")
	}
	if c.Stdin != r.stdin || c.Stdout != &r.stdout || c.Stderr != &r.stderr {
		t.Error("the child did not get the given Stdin, Stdout and Stderr")
	}
	if r.stdout.String() != r.runner.stdout {
		t.Errorf("stdout = %q, want the child's", r.stdout.String())
	}
	if strings.Contains(r.stdout.String()+r.stderr.String(), token) {
		t.Error("the token reached stdout or stderr")
	}
	if diff := cmp.Diff([][2]string{{"bws", "access-token"}}, r.keyring.asked); diff != "" {
		t.Errorf("keyring asked (-want +got):\n%s", diff)
	}
}

func TestExecRunErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    m
		have    []string
		keyring *fakeKeyring
		cmdName string
		want    error  // errors.Is target
		text    string // substring of the message
		keyAsk  int
	}{
		{name: "not declared", data: registry("bws", nil), have: []string{"bws"}, cmdName: "other", want: vaultcheck.ErrNotDeclared, text: `secret "other"`},
		{name: "kind bw", data: registry("bw", nil), have: []string{"bw"}, cmdName: "example-vault", want: vaultcheck.ErrNoInjection, text: "kind bw"},
		{name: "kind rbw", data: registry("rbw", nil), have: []string{"rbw"}, cmdName: "example-vault", want: vaultcheck.ErrNoInjection, text: "kind rbw"},
		{name: "invalid registry", data: m{"secret": m{"example-vault": m{}}}, have: []string{"bws"}, cmdName: "example-vault", text: "the secrets registry is invalid (chezmoi dots lint secrets)"},
		{name: "cli missing", data: registry("bws", nil), cmdName: "example-vault", text: "bws not found: install bws or set command in .chezmoidata/secrets.toml"},
		{name: "keyring error", data: registry("bws", nil), have: []string{"bws"}, keyring: &fakeKeyring{err: errors.New("boom")}, cmdName: "example-vault", text: "(service bws, user access-token): chezmoi secret keyring set --service=bws --user=access-token", keyAsk: 1},
		{name: "keyring empty", data: registry("bws", nil), have: []string{"bws"}, keyring: &fakeKeyring{}, cmdName: "example-vault", text: "no access token in the keyring", keyAsk: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(tt.have...)
			if tt.keyring != nil {
				r.keyring = tt.keyring
				r.exec.Keyring = tt.keyring
			}
			err := r.exec.Run(t.Context(), tt.data, tt.cmdName, []string{"x"}, 0)
			if err == nil {
				t.Fatal("want an error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
			if !strings.Contains(err.Error(), tt.text) {
				t.Errorf("error %q lacks %q", err, tt.text)
			}
			if len(r.runner.calls) != 0 {
				t.Errorf("runner calls = %d, want 0", len(r.runner.calls))
			}
			if len(r.keyring.asked) != tt.keyAsk {
				t.Errorf("keyring calls = %d, want %d", len(r.keyring.asked), tt.keyAsk)
			}
		})
	}
}

func TestExecRunChildFailure(t *testing.T) {
	t.Parallel()
	t.Run("exit code", func(t *testing.T) {
		t.Parallel()
		r := newRig("bws")
		r.runner.err = exitCodedError{3}
		err := r.exec.Run(t.Context(), registry("bws", nil), "example-vault", nil, 0)
		var ec interface{ ExitCode() int }
		if !errors.As(err, &ec) || ec.ExitCode() != 3 {
			t.Errorf("error = %v, want ExitCode 3", err)
		}
	})
	t.Run("token redacted", func(t *testing.T) {
		t.Parallel()
		r := newRig("bws")
		r.runner.err = errors.New("bad token " + token)
		err := r.exec.Run(t.Context(), registry("bws", nil), "example-vault", nil, 0)
		if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[redacted]") {
			t.Errorf("error = %v, want the token redacted", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		r := newRig("bws")
		r.runner.block = true
		err := r.exec.Run(t.Context(), registry("bws", nil), "example-vault", nil, 20*time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "timed out after 20ms") {
			t.Errorf("error = %v, want a timeout", err)
		}
	})
}

func TestExecRunHomeCommand(t *testing.T) {
	t.Parallel()
	r := newRig()
	var looked string
	r.exec.LookPath = func(n string) (string, error) { looked = n; return n, nil }
	data := registry("bws", map[string]any{"command": "~/bin/bws"})
	if err := r.exec.Run(t.Context(), data, "example-vault", nil, 0); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/u", "bin/bws"); looked != want || r.runner.calls[0].Args[0] != want {
		t.Errorf("looked up %q, ran %q, want %q", looked, r.runner.calls[0].Args[0], want)
	}
}

func TestExecRunRealLookPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := "bws"
	if runtime.GOOS == "windows" {
		file = "bws.exe"
	}
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig()
	r.exec.LookPath = exec.LookPath
	data := registry("bws", map[string]any{"command": filepath.ToSlash(filepath.Join(dir, "bws"))})
	if err := r.exec.Run(t.Context(), data, "example-vault", []string{"x"}, 0); err != nil {
		t.Fatal(err)
	}
	got := r.runner.calls[0].Args[0]
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(got), ".exe") {
		t.Errorf("resolved %q, want a .exe", got)
	}
	if filepath.Base(got) != file {
		t.Errorf("resolved %q, want %s", got, file)
	}
}
