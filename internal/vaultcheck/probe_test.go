package vaultcheck_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/vaultcheck"
)

const token = "tok-s3cr3t-value"

// script is what the fake runner answers for one command.
type script struct {
	stdout, stderr string
	err            error
	block          bool // wait for the context instead of answering
}

type recorded struct {
	Args []string
	Env  []string
}

// fakeRunner records every Cmd and answers from script.
type fakeRunner struct {
	mu    sync.Mutex
	calls []recorded
	reply script
}

func (f *fakeRunner) Run(ctx context.Context, c chez.Cmd) error {
	f.mu.Lock()
	f.calls = append(f.calls, recorded{Args: slices.Clone(c.Args), Env: slices.Clone(c.Env)})
	r := f.reply
	f.mu.Unlock()
	if r.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if c.Stdout != nil {
		_, _ = c.Stdout.Write([]byte(r.stdout))
	}
	if c.Stderr != nil {
		_, _ = c.Stderr.Write([]byte(r.stderr))
	}
	return r.err
}

type fakeKeyring struct {
	value string
	err   error
	asked [][2]string
}

func (k *fakeKeyring) Get(_ context.Context, service, user string) (string, error) {
	k.asked = append(k.asked, [2]string{service, user})
	return k.value, k.err
}

func lookPath(have ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(have, name) {
			return "/fake/" + name, nil
		}
		return "", errors.New("not found")
	}
}

var bwsEntry = vaultcheck.Entry{Name: "example-vault", Kind: "bws", ProjectFrom: "secretsProjectId", Service: "bws", User: "access-token"}

func TestCheck(t *testing.T) {
	t.Parallel()
	exit1 := errors.New("exit status 1")
	withProject := m{"secretsProjectId": "proj-1"}
	tests := []struct {
		name    string
		entry   vaultcheck.Entry
		data    m
		have    []string
		keyring *fakeKeyring
		reply   script
		timeout time.Duration
		want    vaultcheck.Result
		argv    []string // the one CLI call expected; nil means none
	}{
		{
			name: "bws projectFrom missing", entry: bwsEntry, data: m{}, have: []string{"bws"}, keyring: &fakeKeyring{value: token},
			want: vaultcheck.Result{Name: "example-vault", Kind: "bws", State: vaultcheck.OK, Detail: "not configured (answer secretsProjectId is empty)"},
		},
		{
			name: "bws projectFrom empty", entry: bwsEntry, data: m{"secretsProjectId": ""}, have: []string{"bws"}, keyring: &fakeKeyring{value: token},
			want: vaultcheck.Result{Name: "example-vault", Kind: "bws", State: vaultcheck.OK, Detail: "not configured (answer secretsProjectId is empty)"},
		},
		{
			name: "bws answer not a string", entry: bwsEntry, data: m{"secretsProjectId": 4}, have: []string{"bws"}, keyring: &fakeKeyring{value: token},
			want: vaultcheck.Result{Name: "example-vault", Kind: "bws", State: vaultcheck.Fail, Detail: "answer secretsProjectId is not a string"},
		},
		{
			name: "cli missing", entry: bwsEntry, data: withProject, keyring: &fakeKeyring{value: token},
			want: vaultcheck.Result{Name: "example-vault", Kind: "bws", State: vaultcheck.Fail, Detail: "bws not found", Hint: "install bws or set command in .chezmoidata/secrets.toml"},
		},
		{
			name: "keyring missing", entry: bwsEntry, data: withProject, have: []string{"bws"}, keyring: &fakeKeyring{err: errors.New("boom")},
			want: vaultcheck.Result{
				Name: "example-vault", Kind: "bws", State: vaultcheck.Fail, Detail: "no access token in the keyring (service bws, user access-token)",
				Hint: "chezmoi secret keyring set --service=bws --user=access-token",
			},
		},
		{
			name: "keyring empty", entry: bwsEntry, data: withProject, have: []string{"bws"}, keyring: &fakeKeyring{},
			want: vaultcheck.Result{
				Name: "example-vault", Kind: "bws", State: vaultcheck.Fail, Detail: "no access token in the keyring (service bws, user access-token)",
				Hint: "chezmoi secret keyring set --service=bws --user=access-token",
			},
		},
		{
			name: "bws ok", entry: bwsEntry, data: withProject, have: []string{"bws"}, keyring: &fakeKeyring{value: token},
			reply: script{stdout: "value-that-must-not-leak"},
			want:  vaultcheck.Result{Name: "example-vault", Kind: "bws", State: vaultcheck.OK, Detail: "project proj-1 reachable"},
			argv:  []string{"/fake/bws", "project", "get", "proj-1", "--output", "none", "--color", "no"},
		},
		{
			name: "bws literal project", entry: vaultcheck.Entry{Name: "ci", Kind: "bws", Project: "lit", Service: "bws", User: "t"}, have: []string{"bws"}, keyring: &fakeKeyring{value: token},
			want: vaultcheck.Result{Name: "ci", Kind: "bws", State: vaultcheck.OK, Detail: "project lit reachable"},
			argv: []string{"/fake/bws", "project", "get", "lit", "--output", "none", "--color", "no"},
		},
		{
			name: "bws network error", entry: bwsEntry, data: withProject, have: []string{"bws"}, keyring: &fakeKeyring{value: token},
			reply: script{stderr: "\nError: error sending request for url\nsecond line", err: exit1},
			want: vaultcheck.Result{
				Name: "example-vault", Kind: "bws", State: vaultcheck.Warn, Detail: "unreachable: Error: error sending request for url", Hint: "check the network and rerun",
			},
			argv: []string{"/fake/bws", "project", "get", "proj-1", "--output", "none", "--color", "no"},
		},
		{
			name: "bws auth error echoing the token", entry: bwsEntry, data: withProject, have: []string{"bws"}, keyring: &fakeKeyring{value: token},
			reply: script{stderr: "Error: invalid access token " + token + "\n", err: exit1},
			want: vaultcheck.Result{
				Name: "example-vault", Kind: "bws", State: vaultcheck.Fail, Detail: "bws project get failed: Error: invalid access token [redacted]",
				Hint: "check the token and that its machine account can read project proj-1",
			},
			argv: []string{"/fake/bws", "project", "get", "proj-1", "--output", "none", "--color", "no"},
		},
		{
			name: "bws timeout", entry: bwsEntry, data: withProject, have: []string{"bws"}, keyring: &fakeKeyring{value: token},
			reply: script{block: true}, timeout: 5 * time.Millisecond,
			want: vaultcheck.Result{
				Name: "example-vault", Kind: "bws", State: vaultcheck.Warn, Detail: "unreachable: timed out after 5ms", Hint: "check the network and rerun",
			},
			argv: []string{"/fake/bws", "project", "get", "proj-1", "--output", "none", "--color", "no"},
		},
		{
			name: "command override with home", entry: vaultcheck.Entry{Name: "x", Kind: "rbw", Command: "~/bin/rbw"}, have: []string{"/home/octocat/bin/rbw"},
			want: vaultcheck.Result{Name: "x", Kind: "rbw", State: vaultcheck.OK, Detail: "agent unlocked"},
			argv: []string{"/fake//home/octocat/bin/rbw", "unlocked"},
		},
		{
			name: "bw unlocked", entry: vaultcheck.Entry{Name: "pw", Kind: "bw"}, have: []string{"bw"},
			reply: script{stdout: `{"userEmail":"octocat@example.com","status":"unlocked"}`},
			want:  vaultcheck.Result{Name: "pw", Kind: "bw", State: vaultcheck.OK, Detail: "vault unlocked"},
			argv:  []string{"/fake/bw", "status"},
		},
		{
			name: "bw locked", entry: vaultcheck.Entry{Name: "pw", Kind: "bw"}, have: []string{"bw"},
			reply: script{stdout: `{"userEmail":"octocat@example.com","status":"locked"}`},
			want:  vaultcheck.Result{Name: "pw", Kind: "bw", State: vaultcheck.Warn, Detail: "vault locked", Hint: "bw unlock, then export BW_SESSION"},
			argv:  []string{"/fake/bw", "status"},
		},
		{
			name: "bw unauthenticated", entry: vaultcheck.Entry{Name: "pw", Kind: "bw"}, have: []string{"bw"},
			reply: script{stdout: `{"status":"unauthenticated"}`},
			want:  vaultcheck.Result{Name: "pw", Kind: "bw", State: vaultcheck.Fail, Detail: "not logged in", Hint: "bw login"},
			argv:  []string{"/fake/bw", "status"},
		},
		{
			name: "bw garbage", entry: vaultcheck.Entry{Name: "pw", Kind: "bw"}, have: []string{"bw"},
			reply: script{stdout: "octocat@example.com is not json"},
			want:  vaultcheck.Result{Name: "pw", Kind: "bw", State: vaultcheck.Fail, Detail: "bw status failed: unexpected output"},
			argv:  []string{"/fake/bw", "status"},
		},
		{
			name: "bw unknown status", entry: vaultcheck.Entry{Name: "pw", Kind: "bw"}, have: []string{"bw"},
			reply: script{stdout: `{"userEmail":"octocat@example.com","status":"weird"}`},
			want:  vaultcheck.Result{Name: "pw", Kind: "bw", State: vaultcheck.Fail, Detail: "bw status failed: unexpected output"},
			argv:  []string{"/fake/bw", "status"},
		},
		{
			name: "bw run error", entry: vaultcheck.Entry{Name: "pw", Kind: "bw"}, have: []string{"bw"},
			reply: script{stdout: "octocat@example.com", stderr: "boom\n", err: exit1},
			want:  vaultcheck.Result{Name: "pw", Kind: "bw", State: vaultcheck.Fail, Detail: "bw status failed: boom"},
			argv:  []string{"/fake/bw", "status"},
		},
		{
			name: "bw timeout", entry: vaultcheck.Entry{Name: "pw", Kind: "bw"}, have: []string{"bw"}, reply: script{block: true}, timeout: 5 * time.Millisecond,
			want: vaultcheck.Result{Name: "pw", Kind: "bw", State: vaultcheck.Warn, Detail: "unreachable: timed out after 5ms", Hint: "check the network and rerun"},
			argv: []string{"/fake/bw", "status"},
		},
		{
			name: "rbw ok", entry: vaultcheck.Entry{Name: "r", Kind: "rbw"}, have: []string{"rbw"},
			want: vaultcheck.Result{Name: "r", Kind: "rbw", State: vaultcheck.OK, Detail: "agent unlocked"},
			argv: []string{"/fake/rbw", "unlocked"},
		},
		{
			name: "rbw locked", entry: vaultcheck.Entry{Name: "r", Kind: "rbw"}, have: []string{"rbw"}, reply: script{err: exit1},
			want: vaultcheck.Result{Name: "r", Kind: "rbw", State: vaultcheck.Warn, Detail: "locked or agent not running", Hint: "rbw unlock"},
			argv: []string{"/fake/rbw", "unlocked"},
		},
		{
			name: "rbw timeout", entry: vaultcheck.Entry{Name: "r", Kind: "rbw"}, have: []string{"rbw"}, reply: script{block: true}, timeout: 5 * time.Millisecond,
			want: vaultcheck.Result{Name: "r", Kind: "rbw", State: vaultcheck.Warn, Detail: "unreachable: timed out after 5ms", Hint: "check the network and rerun"},
			argv: []string{"/fake/rbw", "unlocked"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fr := &fakeRunner{reply: tt.reply}
			kr := tt.keyring
			if kr == nil {
				kr = &fakeKeyring{}
			}
			p := vaultcheck.Prober{Runner: fr, LookPath: lookPath(tt.have...), Keyring: kr, Home: "/home/octocat", Timeout: tt.timeout}
			got := p.Check(t.Context(), tt.entry, tt.data)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Check() mismatch (-want +got):\n%s", diff)
			}
			assertCall(t, fr, tt.entry.Kind, tt.argv, got)
		})
	}
}

// assertCall checks the one CLI call the probe made: argv, the token only in Env, nothing leaked.
func assertCall(t *testing.T, fr *fakeRunner, kind string, argv []string, got vaultcheck.Result) {
	t.Helper()
	if argv == nil {
		if len(fr.calls) != 0 {
			t.Errorf("runner calls = %v, want none", fr.calls)
		}
		return
	}
	if len(fr.calls) != 1 || !slices.Equal(fr.calls[0].Args, argv) {
		t.Fatalf("runner calls = %v, want one with argv %v", fr.calls, argv)
	}
	call := fr.calls[0]
	var wantEnv []string
	if kind == "bws" {
		wantEnv = []string{"BWS_ACCESS_TOKEN=" + token}
	}
	if diff := cmp.Diff(wantEnv, call.Env); diff != "" {
		t.Errorf("Env mismatch (-want +got):\n%s", diff)
	}
	for _, a := range call.Args {
		if strings.Contains(a, token) {
			t.Errorf("token in argv element %q", a)
		}
	}
	all := got.Name + got.Kind + got.Detail + got.Hint
	for _, leak := range []string{token, "octocat@example.com", "value-that-must-not-leak"} {
		if strings.Contains(all, leak) {
			t.Errorf("Result leaks %q: %+v", leak, got)
		}
	}
}

func TestCheckAll(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{}
	p := vaultcheck.Prober{Runner: fr, LookPath: lookPath("rbw", "bw"), Keyring: &fakeKeyring{}}
	data := m{"secret": m{"z": m{"kind": "rbw"}, "a": m{"kind": "bws", "projectFrom": "id", "keyring": ring("s", "u")}}}
	got, fs := p.CheckAll(t.Context(), data)
	if len(fs) != 0 {
		t.Fatalf("findings = %v", fs)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "z" {
		t.Fatalf("results = %+v, want a then z", got)
	}
	if got[0].State != vaultcheck.OK || len(fr.calls) != 1 {
		t.Errorf("unconfigured bws must not probe; results %+v, calls %v", got, fr.calls)
	}

	got, fs = p.CheckAll(t.Context(), m{"secret": m{"a": m{}}})
	if got != nil || len(fs) == 0 {
		t.Errorf("CheckAll(invalid) = %v, %v; want findings only", got, fs)
	}
}

func TestState_String(t *testing.T) {
	t.Parallel()
	for s, want := range map[vaultcheck.State]string{vaultcheck.OK: "ok", vaultcheck.Warn: "warn", vaultcheck.Fail: "fail"} {
		if got := s.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestChezmoiKeyring_Get(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		reply   script
		want    string
		wantErr string // substring; empty means success
	}{
		{name: "trimmed value", reply: script{stdout: token + "\n"}, want: token},
		{name: "error carries stderr not stdout", reply: script{stdout: "stdout-secret", stderr: "keyring: not found\n", err: errors.New("exit status 1")}, wantErr: "keyring: not found"},
		{name: "empty", reply: script{stdout: "\n"}, wantErr: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fr := &fakeRunner{reply: tt.reply}
			k := vaultcheck.ChezmoiKeyring{Runner: fr, Chezmoi: []string{"chezmoi", "--config", "c.toml"}}
			got, err := k.Get(t.Context(), "bws", "access-token")
			wantArgs := []string{"chezmoi", "--config", "c.toml", "secret", "keyring", "get", "--service", "bws", "--user", "access-token"}
			if len(fr.calls) != 1 || !slices.Equal(fr.calls[0].Args, wantArgs) {
				t.Errorf("calls = %v, want argv %v", fr.calls, wantArgs)
			}
			if (err == nil) != (tt.wantErr == "") || got != tt.want {
				t.Fatalf("Get() = %q, %v; want %q, error containing %q", got, err, tt.want, tt.wantErr)
			}
			if err != nil && (!strings.Contains(err.Error(), tt.wantErr) || strings.Contains(err.Error(), "stdout-secret")) {
				t.Errorf("error %q lacks stderr or carries stdout", err)
			}
		})
	}
}
