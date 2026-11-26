package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

const auditToken = "tok-s3cr3t-value"

const secretsRegistry = `[secret.example-vault]
kind = "bws"
projectFrom = "secretsProjectId"
keyring = { service = "bws", user = "access-token" }

[secret.passwords]
kind = "bw"

[secret.rbw]
kind = "rbw"
`

// secretRunner answers the secret-manager CLIs and the keyring read with scripts and
// runs everything else (chezmoi) for real.
type secretRunner struct {
	mu    sync.Mutex
	calls [][]string
	bws   error  // result of `bws project get`
	bw    string // stdout of `bw status`
	rbw   error  // result of `rbw unlocked`
}

func (r *secretRunner) Run(ctx context.Context, c chez.Cmd) error {
	base := filepath.Base(c.Args[0])
	isKeyring := slices.Contains(c.Args, "keyring") && slices.Contains(c.Args, "get") && slices.Contains(c.Args, "secret")
	if !isKeyring && base != "bws" && base != "bw" && base != "rbw" {
		return chez.ExecRunner{}.Run(ctx, c)
	}
	r.mu.Lock()
	r.calls = append(r.calls, slices.Clone(c.Args))
	r.mu.Unlock()
	switch {
	case isKeyring:
		_, _ = c.Stdout.Write([]byte(auditToken + "\n"))
	case base == "bws":
		if r.bws != nil {
			_, _ = c.Stderr.Write([]byte("Error: invalid access token " + auditToken + "\n"))
		}
		return r.bws
	case base == "bw":
		_, _ = c.Stdout.Write([]byte(r.bw))
	default:
		return r.rbw
	}
	return nil
}

func (r *secretRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func fakeLookPath(name string) (string, error) {
	switch name {
	case "bws", "bw", "rbw":
		return "/fake/" + name, nil
	}
	return exec.LookPath(name)
}

func (e *env) writeSource(t *testing.T, rel, text string) {
	t.Helper()
	path := filepath.Join(e.src, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *env) runWith(t *testing.T, runner chez.Runner, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errw bytes.Buffer
	d := cli.Deps{
		Getenv: func(k string) string { return e.vars[k] },
		Stdin:  strings.NewReader(""), Stdout: &out, Stderr: &errw,
		Runner: runner, LookPath: fakeLookPath,
		GOOS: runtime.GOOS, Home: e.vars["HOME"], Version: "test",
	}
	err = cli.Run(t.Context(), d, args)
	return out.String(), errw.String(), err
}

// secretsEnv is a sandbox whose source declares the registry and whose config answers secretsProjectId.
func secretsEnv(t *testing.T, registry string) *env {
	t.Helper()
	e := newEnv(t)
	if registry != "" {
		e.writeSource(t, ".chezmoidata/secrets.toml", registry)
	}
	cfg := e.vars["CHEZMOI_CONFIG_FILE"]
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(b, []byte("    secretsProjectId = \"proj-1\"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAuditSecrets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		registry   string
		runner     *secretRunner
		args       []string
		wantErr    error
		wantOut    []string
		wantErrOut []string
		wantCalls  bool
	}{
		{
			name: "all ok", registry: secretsRegistry, runner: &secretRunner{bw: `{"status":"unlocked"}`}, args: []string{"audit", "secrets"},
			wantOut:   []string{"NAME", "KIND", "STATE", "DETAIL", "example-vault", "project proj-1 reachable", "passwords", "vault unlocked", "agent unlocked"},
			wantCalls: true,
		},
		{
			name: "one fail", registry: secretsRegistry, runner: &secretRunner{bws: errors.New("exit status 1"), bw: `{"status":"locked"}`}, args: []string{"audit", "secrets"},
			wantErr:   cli.ErrReported,
			wantOut:   []string{"fail", "bws project get failed: Error: invalid access token [redacted]", "  example-vault: check the token", "warn", "  passwords: bw unlock, then export BW_SESSION"},
			wantCalls: true,
		},
		{
			name: "invalid registry", registry: "[secret.a]\n", runner: &secretRunner{}, args: []string{"audit", "secrets"},
			wantErr: cli.ErrReported, wantErrOut: []string{"✗ secrets", "secret a: missing kind", "fix the registry (chezmoi dots lint secrets)"},
		},
		{
			name: "no entries", registry: "", runner: &secretRunner{}, args: []string{"audit", "secrets"},
			wantOut: []string{"secrets: no entries declared (.chezmoidata/secrets.toml)"},
		},
		{
			name: "unknown check", registry: secretsRegistry, runner: &secretRunner{}, args: []string{"audit", "unknown"},
			wantErr: cli.ErrUsage,
		},
		{
			name: "lint secrets reports the finding", registry: "[secret.a]\nkind = \"bw\"\nproject = \"p\"\n", runner: &secretRunner{}, args: []string{"lint", "secrets"},
			wantErr: cli.ErrReported, wantErrOut: []string{"✗ secrets", "secret a: project is only for kind bws"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := secretsEnv(t, tt.registry)
			stdout, stderr, err := e.runWith(t, tt.runner, tt.args...)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("%v = %v, want %v\nstdout:\n%s\nstderr:\n%s", tt.args, err, tt.wantErr, stdout, stderr)
			}
			assertContains(t, "stdout", stdout, tt.wantOut)
			assertContains(t, "stderr", stderr, tt.wantErrOut)
			if tt.wantCalls != (tt.runner.count() > 0) {
				t.Errorf("probe calls = %d, want calls: %v", tt.runner.count(), tt.wantCalls)
			}
			if strings.Contains(stdout+stderr, auditToken) {
				t.Errorf("the token leaked:\n%s\n%s", stdout, stderr)
			}
		})
	}
}

func assertContains(t *testing.T, stream, got string, want []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q:\n%s", stream, w, got)
		}
	}
}
