// Package cli is the command line of `chezmoi dots`: it parses arguments, wires the
// adapters into the use cases and prints their results. The logic lives elsewhere.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/edit"
)

// Deps is everything the commands touch outside the process: tests replace it.
type Deps struct {
	Getenv   func(string) string
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Runner   chez.Runner
	LookPath func(string) (string, error)
	HTTP     *http.Client
	Exe      string // this program's path, re-run for the live pane
	Version  string
	GOOS     string
	Home     string
}

// ErrReported is returned for a failure that was already printed.
var ErrReported = edit.ErrReported

// ErrUsage is returned for a command line that makes no sense.
var ErrUsage = edit.ErrUsage

const (
	cmdEdit     = "edit"
	cmdView     = "view"
	cmdList     = "list"
	cmdLint     = "lint"
	cmdAudit    = "audit"
	cmdAliases  = "aliases"
	cmdOutdated = "outdated"
	cmdBump     = "bump"
	cmdUpgrade  = "upgrade"
	cmdLive     = "live"
	cmdVersion  = "version"
	cmdHelp     = "help"
)

type commandError struct {
	command string
	err     error
}

func (e *commandError) Error() string { return e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }

// Run runs one command line (the arguments after `chezmoi dots`).
func Run(ctx context.Context, d Deps, args []string) error {
	cmd, rest := cmdEdit, args
	if len(args) > 0 {
		switch args[0] {
		case cmdEdit, cmdView, cmdList, cmdLint, cmdAudit, cmdOutdated, cmdBump, cmdUpgrade, cmdLive, cmdVersion, cmdHelp:
			cmd, rest = args[0], args[1:]
		case "-h", "--help":
			cmd, rest = "", args
		}
	}
	err := d.run(ctx, cmd, rest)
	if local, ok := errors.AsType[*commandError](err); ok {
		cmd = local.command
	}
	if errors.Is(err, flag.ErrHelp) {
		renderHelp(d.Stdout, helpPage(cmd))
		return nil
	}
	if errors.Is(err, ErrUsage) {
		diagnostic := strings.TrimPrefix(err.Error(), ErrUsage.Error()+": ")
		if diagnostic == ErrUsage.Error() || diagnostic == "" {
			diagnostic = "invalid arguments"
		}
		fmt.Fprintf(d.Stderr, "error: %s\n\n", diagnostic)
		renderHelp(d.Stderr, helpPage(cmd))
	}
	return err
}

func (d Deps) run(ctx context.Context, cmd string, args []string) error {
	switch cmd {
	case cmdEdit:
		return d.edit(ctx, args)
	case cmdView:
		return d.view(ctx, args)
	case cmdList:
		return d.list(ctx, args)
	case cmdLint:
		return d.lint(ctx, args)
	case cmdAudit:
		return d.audit(ctx, args)
	case cmdOutdated:
		return d.outdated(ctx, args)
	case cmdBump:
		return d.bump(ctx, args)
	case cmdUpgrade:
		return d.upgrade(ctx, args)
	case cmdLive:
		return d.live(ctx, args)
	case cmdVersion:
		if _, err := parse(newFlags(cmd), args); errors.Is(err, flag.ErrHelp) {
			return err
		}
		fmt.Fprintln(d.Stdout, "chezmoi-dots", d.Version)
		return nil
	case cmdHelp:
		return d.help(args)
	default:
		_, err := parse(newFlags(""), args)
		return err
	}
}

// parse parses flags that may come before or after the positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if !errors.Is(err, flag.ErrHelp) {
				err = fmt.Errorf("%w: %w", ErrUsage, err)
			}
			return nil, &commandError{command: fs.Name(), err: err}
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		if fs.Name() == cmdAudit && len(pos) == 1 && pos[0] == cmdAliases {
			fs.Init("audit aliases", flag.ContinueOnError)
		}
		args = fs.Args()[1:]
	}
}
