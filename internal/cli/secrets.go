package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/vaultcheck"
)

// ExitCodeError is a declared CLI's own non-zero exit; the command line exits with Code
// and prints nothing more.
type ExitCodeError struct{ Code int }

func (e *ExitCodeError) Error() string { return "exit status " + strconv.Itoa(e.Code) }

// secrets dispatches the secret-manager subcommands; the subcommand is the first token.
func (d Deps) secrets(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return &commandError{command: cmdSecrets, err: flag.ErrHelp}
	}
	switch args[0] {
	case "run":
		return d.secretsRun(ctx, args[1:])
	case "-h", helpFlag:
		return &commandError{command: cmdSecrets, err: flag.ErrHelp}
	default:
		return &commandError{command: cmdSecrets, err: fmt.Errorf("%w: unknown secrets command %q (commands: run)", ErrUsage, args[0])}
	}
}

// secretsRun runs a declared secret-manager CLI with its token injected. Everything after
// the first -- belongs to the CLI and is never parsed here.
func (d Deps) secretsRun(ctx context.Context, args []string) error {
	var passthrough []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, passthrough = args[:i], args[i+1:]
	}
	fs := newFlags(cmdSecretsRun)
	pos, err := parse(fs, args)
	if err != nil {
		if errors.Is(err, ErrUsage) {
			err = &commandError{command: cmdSecretsRun, err: fmt.Errorf("%w; put the CLI's arguments after --", err)}
		}
		return err
	}
	if len(pos) == 0 {
		return &commandError{command: cmdSecretsRun, err: fmt.Errorf("%w: secrets run needs a name", ErrUsage)}
	}
	getter, _ := fs.Lookup("timeout").Value.(flag.Getter) // registered in newFlags
	timeout, _ := getter.Get().(time.Duration)
	if timeout < 0 {
		return &commandError{command: cmdSecretsRun, err: fmt.Errorf("%w: --timeout must not be negative", ErrUsage)}
	}
	cz, global := d.chezmoi()
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return fmt.Errorf("read chezmoi data: %w", err)
	}
	x := vaultcheck.Exec{
		Runner: d.Runner, LookPath: d.LookPath, Home: d.Home,
		Keyring: vaultcheck.ChezmoiKeyring{Runner: d.Runner, Chezmoi: global},
		Stdin:   d.Stdin, Stdout: d.Stdout, Stderr: d.Stderr,
	}
	err = x.Run(ctx, data, pos[0], append(slices.Clone(pos[1:]), passthrough...), timeout)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, vaultcheck.ErrNotDeclared), errors.Is(err, vaultcheck.ErrNoInjection):
		return &commandError{command: cmdSecretsRun, err: fmt.Errorf("%w: %w", ErrUsage, err)}
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return &ExitCodeError{Code: coded.ExitCode()}
	}
	return err
}
