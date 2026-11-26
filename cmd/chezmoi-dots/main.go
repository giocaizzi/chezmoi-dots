// Command chezmoi-dots is a chezmoi plugin: `chezmoi dots` edits the dotfiles source
// and shows what the edit changes everywhere, lints the registries and keeps the
// machine's packages up to date. chezmoi runs it for any `chezmoi dots ...` command.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is main without the process around it, so tests can call it.
func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	exe, _ := os.Executable()
	home, _ := os.UserHomeDir()
	deps := cli.Deps{
		Getenv: getenv, Stdin: stdin, Stdout: stdout, Stderr: stderr,
		Runner: chez.ExecRunner{}, LookPath: exec.LookPath,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
		Exe:     exe,
		Version: version(),
		GOOS:    runtime.GOOS,
		Home:    home,
	}
	return exitCode(ctx, cli.Run(ctx, deps, args), stderr)
}

// exitCode maps the result of cli.Run to the process exit code, printing the error when
// nothing else has.
func exitCode(ctx context.Context, err error, stderr io.Writer) int {
	if exit, ok := errors.AsType[*cli.ExitCodeError](err); ok {
		return exit.Code // the declared CLI's own exit code; it already said what it had to
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, cli.ErrReported):
		return 1
	case errors.Is(ctx.Err(), context.Canceled):
		return 130
	case errors.Is(err, cli.ErrUsage):
		return 2 // cli.Run already printed the diagnostic and command-local help.
	default:
		fmt.Fprintf(stderr, "chezmoi-dots: %v\n", err)
		return 1
	}
}

// buildVersion is the release version, set by the release build (-ldflags -X); a
// package variable is the only way to stamp a subdirectory module's tag into the binary.
var buildVersion string //nolint:gochecknoglobals // set at link time

func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, kv := range bi.Settings {
			if kv.Key == "vcs.revision" && len(kv.Value) >= 7 {
				return "devel-" + kv.Value[:7]
			}
		}
	}
	return "devel"
}
