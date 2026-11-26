package vaultcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/finding"
)

// Errors a caller maps to a usage error.
var (
	ErrNotDeclared = errors.New("not declared")       // name not in the registry
	ErrNoInjection = errors.New("no token injection") // the kind has no token to inject
)

// Exec runs a declared secret-manager CLI with its token injected.
type Exec struct {
	Runner   chez.Runner
	LookPath func(string) (string, error)
	Keyring  Keyring
	Home     string
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
}

// exitError carries the exit code of the CLI; the message has the token redacted.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }
func (e *exitError) ExitCode() int { return e.code }

// Run looks up name in data's registry and runs its CLI with args. A non-zero exit
// of the CLI comes back as an error that satisfies interface{ ExitCode() int }.
// A timeout of 0 means none.
func (x Exec) Run(ctx context.Context, data map[string]any, name string, args []string, timeout time.Duration) error {
	entries, found := Parse(data)
	if len(found) > 0 {
		return fmt.Errorf("the secrets registry is invalid (chezmoi dots lint secrets):\n%s", finding.Lines(found))
	}
	var e Entry
	ok := false
	for _, c := range entries {
		if c.Name == name {
			e, ok = c, true
			break
		}
	}
	if !ok {
		return fmt.Errorf("%w: secret %q (declare it in .chezmoidata/secrets.toml)", ErrNotDeclared, name)
	}
	if e.Kind != KindBws {
		return fmt.Errorf("%w: kind %s has no token injection; use the CLI's own session", ErrNoInjection, e.Kind)
	}
	bin, cli, err := resolveCLI(e, x.Home, x.LookPath)
	if ce, ok := errors.AsType[*commandError](err); ok {
		return ce
	}
	if err != nil {
		return fmt.Errorf("%s not found: install %s or set command in .chezmoidata/secrets.toml", cli, e.Kind)
	}
	token, err := x.Keyring.Get(ctx, e.Service, e.User)
	switch {
	case err == nil && token == "", errors.Is(err, ErrNoToken):
		return fmt.Errorf("no access token in the keyring (service %s, user %s): chezmoi secret keyring set --service=%s --user=%s",
			e.Service, e.User, e.Service, e.User)
	case err != nil:
		return fmt.Errorf("read the access token (service %s, user %s): %w", e.Service, e.User, err)
	}
	redact := func(s string) string { return strings.ReplaceAll(s, token, "[redacted]") }

	rctx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		rctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	err = x.Runner.Run(rctx, chez.Cmd{
		Args:  append([]string{bin}, args...),
		Env:   []string{"BWS_ACCESS_TOKEN=" + token},
		Stdin: x.Stdin, Stdout: x.Stdout, Stderr: x.Stderr,
	})
	if err == nil {
		return nil
	}
	if timeout > 0 && errors.Is(rctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out after %s", e.Kind, timeout)
	}
	var ec interface{ ExitCode() int }
	// A child killed by a signal reports -1: that is no exit code to propagate.
	if errors.As(err, &ec) && ec.ExitCode() > 0 {
		return &exitError{code: ec.ExitCode(), msg: redact(err.Error())}
	}
	return errors.New(redact(err.Error()))
}
