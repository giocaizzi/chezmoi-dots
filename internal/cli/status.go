package cli

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/inventory"
	"github.com/giocaizzi/chezmoi-dots/internal/survey"
	"github.com/giocaizzi/chezmoi-dots/internal/term"
)

// status prints the dotfiles repository state and what chezmoi apply would change.
func (d Deps) status(ctx context.Context, args []string) error {
	fs := newFlags(cmdStatus)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return &commandError{command: cmdStatus, err: fmt.Errorf("%w: status takes no arguments (got %q)", ErrUsage, pos[0])}
	}
	cz, _ := d.chezmoi()
	wt := d.Getenv("CHEZMOI_WORKING_TREE")
	if wt == "" {
		wt, _ = cz.WorkingTree(ctx) // an unknown tree is shown as such
	}
	var (
		wg      sync.WaitGroup
		tree    inventory.Tree
		out     string
		statErr error
	)
	wg.Go(func() {
		tree = survey.Survey{
			Runner: d.Runner, Stat: os.Stat, LookPath: d.LookPath, Home: d.Home, GOOS: d.GOOS,
			WorkingTree: wt, Fetch: boolValue(fs, "fetch"), Getenv: d.Getenv,
		}.Tree(ctx)
	})
	wg.Go(func() { out, statErr = cz.Status(ctx) })
	wg.Wait()
	if ctx.Err() != nil {
		return fmt.Errorf("status: %w", ctx.Err())
	}
	if statErr != nil {
		return statErr
	}
	entries := chez.ParseStatus(out)
	term.NewStyle(d.Stdout, d.Getenv).Status(d.Stdout, tree, entries, time.Now())
	if boolValue(fs, "check") && (len(entries) > 0 || tree.Known && (tree.Behind > 0 || tree.Ahead > 0 || tree.Dirty > 0)) {
		return ErrReported
	}
	return nil
}
