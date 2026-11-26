package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/audit"
	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/term"
	"github.com/giocaizzi/chezmoi-dots/internal/vaultcheck"
)

// audit runs the live checks in the order aliases, secrets; with no argument all run.
func (d Deps) audit(ctx context.Context, args []string) error {
	checks, err := parse(newFlags(cmdAudit), args)
	if err != nil {
		return err
	}
	for _, c := range checks {
		if c != cmdAliases && c != cmdSecrets {
			return &commandError{command: cmdAudit, err: fmt.Errorf("%w: unknown audit check %q (checks: %s, %s)", ErrUsage, c, cmdAliases, cmdSecrets)}
		}
	}
	if len(checks) == 0 {
		checks = []string{cmdAliases, cmdSecrets}
	}
	failed := false
	for _, c := range checks {
		run := d.auditAliases
		if c == cmdSecrets {
			run = d.auditSecrets
		}
		if err := run(ctx); errors.Is(err, ErrReported) {
			failed = true
		} else if err != nil {
			return err
		}
	}
	if failed {
		return ErrReported
	}
	return nil
}

// auditSecrets probes the secret managers the registry declares; it never prints a secret.
func (d Deps) auditSecrets(ctx context.Context) error {
	cz, global := d.chezmoi()
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return fmt.Errorf("read chezmoi data: %w", err)
	}
	p := vaultcheck.Prober{
		Runner: d.Runner, LookPath: d.LookPath, Home: d.Home,
		Keyring: vaultcheck.ChezmoiKeyring{Runner: d.Runner, Chezmoi: global},
	}
	results, found := p.CheckAll(ctx, data)
	if len(found) > 0 {
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%s✗%s secrets\n%s\n  fix the registry (chezmoi dots lint secrets)\n", st.R, st.O, joinLines(found))
		return ErrReported
	}
	if ignored := vaultcheck.Ignored(data); len(ignored) > 0 {
		defer func() {
			fmt.Fprintf(d.Stdout, "secrets: ignored data.secret entries without a kind: %s (see docs/secrets.md)\n", strings.Join(ignored, ", "))
		}()
	}
	if len(results) == 0 {
		fmt.Fprintln(d.Stdout, "secrets: no entries declared (.chezmoidata/secrets.toml)")
		return nil
	}
	st := term.NewStyle(d.Stdout, d.Getenv)
	colour := map[vaultcheck.State]string{vaultcheck.OK: st.G, vaultcheck.Warn: st.Y, vaultcheck.Fail: st.R}
	rows := make([][]string, 0, len(results))
	failed := false
	for _, r := range results {
		failed = failed || r.State == vaultcheck.Fail
		rows = append(rows, []string{r.Name, r.Kind, colour[r.State] + r.State.String() + st.O, r.Detail})
	}
	st.Table(d.Stdout, []string{"NAME", "KIND", "STATE", "DETAIL"}, rows)
	for _, r := range results {
		if r.State != vaultcheck.OK && r.Hint != "" {
			fmt.Fprintf(d.Stdout, "  %s: %s\n", r.Name, r.Hint)
		}
	}
	if failed {
		return ErrReported
	}
	return nil
}

// auditAliases lists the aliases live in this machine's interactive shells that the
// registry does not account for.
func (d Deps) auditAliases(ctx context.Context) error {
	known, err := d.knownAliases()
	if err != nil {
		return err
	}
	shells := []struct{ name, script string }{
		{"zsh", "print -l ${(k)aliases}"},
		{"bash", "compgen -a || true"},
	}
	failed := false
	for _, sh := range shells {
		if _, err := d.LookPath(sh.name); err != nil {
			continue
		}
		out, err := chez.Output(ctx, d.Runner, chez.Cmd{Args: []string{sh.name, "-ic", sh.script}})
		if err != nil {
			return fmt.Errorf("list %s aliases: %w", sh.name, err)
		}
		unknown := audit.Unknown(known, string(bytes.TrimSpace(out)))
		if len(unknown) == 0 {
			fmt.Fprintf(d.Stdout, "%s: all aliases accounted for\n", sh.name)
			continue
		}
		failed = true
		fmt.Fprintf(d.Stdout, "%s: aliases not in the registry:\n", sh.name)
		for _, n := range unknown {
			fmt.Fprintf(d.Stdout, "  %s\n", n)
		}
	}
	if failed {
		return ErrReported
	}
	return nil
}

// knownAliases reads the rendered alias files and the Oh My Zsh plugins they turn on.
func (d Deps) knownAliases() (map[string]bool, error) {
	cfg := d.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(d.Home, ".config")
	}
	shellDir := filepath.Join(cfg, "shell")
	rendered, err := os.ReadFile(filepath.Join(shellDir, "aliases.sh")) //nolint:gosec // the rendered alias file in the user's config dir
	if err != nil {
		return nil, fmt.Errorf("read the rendered aliases (chezmoi apply?): %w", err)
	}
	var plugin []string
	if omz, err := os.ReadFile(filepath.Join(shellDir, "omz-aliases.zsh")); err == nil { //nolint:gosec // as above
		for _, p := range audit.Plugins(string(omz)) {
			files, _ := filepath.Glob(filepath.Join(d.Home, ".oh-my-zsh", "plugins", p, "*.zsh"))
			for _, f := range files {
				if b, err := os.ReadFile(f); err == nil { //nolint:gosec // an Oh My Zsh plugin file
					plugin = append(plugin, audit.OMZAliases(string(b))...)
				}
			}
		}
	}
	return audit.Known(string(rendered), plugin), nil
}
