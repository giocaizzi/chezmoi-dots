package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/edit"
	"github.com/giocaizzi/chezmoi-dots/internal/extpin"
	"github.com/giocaizzi/chezmoi-dots/internal/inventory"
	"github.com/giocaizzi/chezmoi-dots/internal/lint"
	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
	"github.com/giocaizzi/chezmoi-dots/internal/release"
	"github.com/giocaizzi/chezmoi-dots/internal/survey"
	"github.com/giocaizzi/chezmoi-dots/internal/term"
	"github.com/giocaizzi/chezmoi-dots/internal/upgrade"
)

func (d Deps) lint(ctx context.Context, args []string) error {
	fs := newFlags("lint")
	checks, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		checks = lint.Checks()
	}
	cz, _ := d.chezmoi()
	src, err := d.sourceDir(ctx, cz)
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "dots-lint-")
	if err != nil {
		return fmt.Errorf("make scratch dir: %w", err)
	}
	defer os.RemoveAll(scratch)
	empty := filepath.Join(scratch, "empty.toml")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		return fmt.Errorf("write empty config: %w", err)
	}
	l := lint.Linter{
		Src: src, Iso: cz.With("--config", empty), Real: cz, Runner: d.Runner, LookPath: d.LookPath,
		Scratch: scratch, Errw: d.Stderr,
	}
	st := term.NewStyle(d.Stderr, d.Getenv)
	failed := false
	for _, c := range checks {
		found, err := l.Run(ctx, c)
		if err != nil {
			return fmt.Errorf("lint %s: %w", c, err)
		}
		if len(found) == 0 {
			fmt.Fprintf(d.Stderr, "%s✓%s %s\n", st.G, st.O, c)
			continue
		}
		failed = true
		fmt.Fprintf(d.Stderr, "%s✗%s %s\n%s\n", st.R, st.O, c, joinLines(found))
	}
	if failed {
		return ErrReported
	}
	return nil
}

func (d Deps) finder() release.Finder {
	return release.Finder{Runner: d.Runner, HTTP: d.HTTP, Getenv: d.Getenv}
}

// releases reads the release table of the source at src and the path of the file that holds it.
func (d Deps) releases(ctx context.Context, cz *chez.Client, src string) (string, []pkgreg.Release, error) {
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(src, ".chezmoidata", "packages.toml"), pkgreg.Releases(data), nil
}

// pkg dispatches the package lifecycle subcommands; the subcommand is the first token.
func (d Deps) pkg(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return &commandError{command: cmdPkg, err: fmt.Errorf("%w: pkg needs a command (status, bump)", ErrUsage)}
	}
	switch args[0] {
	case "status":
		return d.pkgStatus(ctx, args[1:])
	case "bump":
		return d.bump(ctx, args[1:])
	case "-h", "--help":
		return &commandError{command: cmdPkg, err: flag.ErrHelp}
	default:
		return &commandError{command: cmdPkg, err: fmt.Errorf("%w: unknown pkg command %q (commands: status, bump)", ErrUsage, args[0])}
	}
}

// pkgStatus prints the overview of every tool on this machine.
func (d Deps) pkgStatus(ctx context.Context, args []string) error {
	fs := newFlags(cmdPkgState)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return &commandError{command: cmdPkgState, err: ErrUsage}
	}
	cz, _ := d.chezmoi()
	out, err := cz.Execute(ctx, nil, []byte(inventory.SelectionTemplate), false)
	if err != nil {
		return fmt.Errorf("read the package selection: %w", err)
	}
	sel, err := inventory.ParseSelection(out)
	if err != nil {
		return fmt.Errorf("read the package selection: %w", err)
	}
	src, err := d.sourceDir(ctx, cz)
	if err != nil {
		return err
	}
	pins, skips := extpin.Find(src)
	wt := d.Getenv("CHEZMOI_WORKING_TREE")
	if wt == "" {
		wt, _ = cz.WorkingTree(ctx) // unknown is reported by the overview itself
	}
	rep := survey.Survey{
		Runner: d.Runner, Latest: d.finder().Latest, Stat: os.Stat, LookPath: d.LookPath,
		Home: d.Home, GOOS: d.GOOS, WorkingTree: wt, Offline: boolValue(fs, "offline"), Fetch: boolValue(fs, "fetch"),
		Getenv: d.Getenv, Archives: pins, ArchiveSkips: skips, Head: d.finder().Head,
	}.Run(ctx, sel)
	if ctx.Err() != nil {
		return fmt.Errorf("pkg status: %w", ctx.Err())
	}
	term.NewStyle(d.Stdout, d.Getenv).Inventory(d.Stdout, rep, time.Now())
	if boolValue(fs, "check") && rep.Failing() {
		return ErrReported
	}
	return nil
}

func (d Deps) bump(ctx context.Context, args []string) error {
	fs := newFlags(cmdPkgBump)
	names, err := parse(fs, args)
	if err != nil {
		return err
	}
	cz, _ := d.chezmoi()
	src, err := d.sourceDir(ctx, cz)
	if err != nil {
		return err
	}
	file, rels, err := d.releases(ctx, cz, src)
	if err != nil {
		return err
	}
	pins, skips := extpin.Find(src)
	for _, k := range skips {
		where := k.File
		if k.Entry != "" {
			where += " " + k.Entry
		}
		fmt.Fprintf(d.Stderr, "skip %s: %s\n", where, k.Reason)
	}
	if rels, pins, err = selectPins(names, rels, pins); err != nil {
		return err
	}
	texts := map[string]string{} // updated text of every changed file
	var order []string
	if len(rels) > 0 {
		updated, n, rerr := d.bumpReleases(ctx, rels, file)
		if rerr != nil {
			return rerr
		}
		if n > 0 {
			texts[file], order = updated, append(order, file)
		}
	}
	changed, err := d.bumpArchives(ctx, pins, texts)
	if err != nil {
		return err
	}
	order = append(order, changed...)
	switch {
	case len(order) == 0:
		fmt.Fprintln(d.Stdout, "all pins are up to date")
	case boolValue(fs, "dry-run"):
		fmt.Fprintln(d.Stdout, "dry run: nothing written")
	default:
		if err := writeAll(order, texts, os.Rename); err != nil {
			return err
		}
		fmt.Fprintf(d.Stdout, "pinned in %s; run chezmoi apply to install\n", strings.Join(order, ", "))
	}
	return nil
}

// writeAll replaces each file in files with its text in texts. Every new text is
// first written to a temp file next to its original (same mode), so nothing is
// changed unless all of them can be prepared; then each is renamed over its
// original. A rename that fails leaves the files already renamed in place, and
// the error names them.
func writeAll(files []string, texts map[string]string, rename func(oldpath, newpath string) error) error {
	temps := make([]string, 0, len(files))
	removeTemps := func(from int) {
		for _, t := range temps[from:] {
			_ = os.Remove(t)
		}
	}
	for _, f := range files {
		tmp, err := writeTemp(f, texts[f])
		if err != nil {
			removeTemps(0)
			return fmt.Errorf("write %s: %w", f, err)
		}
		temps = append(temps, tmp)
	}
	for i, f := range files {
		if err := rename(temps[i], f); err != nil {
			removeTemps(i)
			if i == 0 {
				return fmt.Errorf("write %s: %w", f, err)
			}
			return fmt.Errorf("write %s: %w (already written: %s)", f, err, strings.Join(files[:i], ", "))
		}
	}
	return nil
}

// writeTemp writes text to a new temp file beside file, with file's mode, and returns its path.
func writeTemp(file, text string) (name string, err error) {
	info, err := os.Stat(file)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".dots-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if err = os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

// selectPins keeps the releases and archive pins the names select (all without names);
// a name that selects neither is a usage error.
func selectPins(names []string, rels []pkgreg.Release, pins []extpin.Pin) ([]pkgreg.Release, []extpin.Pin, error) {
	if len(names) == 0 {
		return rels, pins, nil
	}
	for _, n := range names {
		if !slices.ContainsFunc(rels, func(r pkgreg.Release) bool { return r.Name == n }) &&
			!slices.ContainsFunc(pins, func(p extpin.Pin) bool { return p.Name() == n }) {
			return nil, nil, &commandError{command: cmdPkgBump, err: fmt.Errorf("%w: unknown pin in %v", ErrUsage, names)}
		}
	}
	rels = slices.DeleteFunc(rels, func(r pkgreg.Release) bool { return !slices.Contains(names, r.Name) })
	pins = slices.DeleteFunc(pins, func(p extpin.Pin) bool { return !slices.Contains(names, p.Name()) })
	return rels, pins, nil
}

// bumpReleases moves the outdated releases in the text of packages.toml and returns the new text and how many moved.
func (d Deps) bumpReleases(ctx context.Context, rels []pkgreg.Release, file string) (string, int, error) {
	raw, err := os.ReadFile(file) //nolint:gosec // file is the package registry in the user's own dotfiles source
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", file, err)
	}
	updated, f := string(raw), d.finder()
	bumped := 0
	for _, s := range f.Check(ctx, rels) {
		if s.Err != nil {
			return "", 0, fmt.Errorf("release %s: %w", s.Release.Name, s.Err)
		}
		if !s.Outdated() {
			continue
		}
		sums, err := f.Sums(ctx, s.Release, s.Latest)
		if err != nil {
			return "", 0, err
		}
		if updated, err = pkgreg.SetRelease(updated, s.Release.Name, s.Latest, sums); err != nil {
			return "", 0, err
		}
		bumped++
		fmt.Fprintf(d.Stdout, "%s %s → %s\n", s.Release.Name, s.Release.Version, s.Latest)
	}
	return updated, bumped, nil
}

// archiveBumper moves archive pins to their remote HEAD, remembering what it looked up.
type archiveBumper struct {
	f       release.Finder
	texts   map[string]string // updated text of the externals files, read on first use
	heads   map[string]string // remote -> HEAD commit
	sums    map[string]string // archive URL -> SHA-256
	printed map[string]bool
	out     io.Writer
	changed []string
}

// head returns the HEAD commit of the pin's remote, asking the remote once.
func (b *archiveBumper) head(ctx context.Context, p extpin.Pin) (string, error) {
	if h, ok := b.heads[p.Remote()]; ok {
		return h, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	h, err := b.f.Head(ctx, p.Remote())
	if err != nil {
		return "", err
	}
	b.heads[p.Remote()] = h
	return h, nil
}

// sum downloads the archive of p at head once.
func (b *archiveBumper) sum(ctx context.Context, p extpin.Pin, head string) (string, error) {
	url := p.URLAt(head)
	if s, ok := b.sums[url]; ok {
		return s, nil
	}
	s, err := b.f.SHA256(ctx, url)
	if err != nil {
		return "", err
	}
	b.sums[url] = s
	return s, nil
}

// text returns the current text of an externals file.
func (b *archiveBumper) text(file string) (string, error) {
	if t, ok := b.texts[file]; ok {
		return t, nil
	}
	raw, err := os.ReadFile(file) //nolint:gosec // an externals file in the user's own dotfiles source
	if err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}
	return string(raw), nil
}

// bump moves p when it is behind its remote HEAD.
func (b *archiveBumper) bump(ctx context.Context, p extpin.Pin) error {
	head, err := b.head(ctx, p)
	if err != nil {
		return err
	}
	if head == p.SHA {
		return nil
	}
	sum := ""
	if p.Sum != "" {
		if sum, err = b.sum(ctx, p, head); err != nil {
			return err
		}
	}
	text, err := b.text(p.File)
	if err != nil {
		return err
	}
	if text, err = extpin.Set(text, p, head, sum); err != nil {
		return err
	}
	b.texts[p.File] = text
	if !slices.Contains(b.changed, p.File) {
		b.changed = append(b.changed, p.File)
	}
	if line := fmt.Sprintf("%s %s → %s\n", p.Name(), p.SHA[:7], head[:7]); !b.printed[line] {
		b.printed[line] = true
		fmt.Fprint(b.out, line)
	}
	return nil
}

// bumpArchives moves every archive pin that is behind its remote HEAD, editing the
// externals files in texts. It returns the files it changed.
func (d Deps) bumpArchives(ctx context.Context, pins []extpin.Pin, texts map[string]string) ([]string, error) {
	b := &archiveBumper{
		f: d.finder(), texts: texts, heads: map[string]string{}, sums: map[string]string{},
		printed: map[string]bool{}, out: d.Stdout,
	}
	for _, p := range pins {
		if err := b.bump(ctx, p); err != nil {
			return nil, fmt.Errorf("archive %s: %w", p.Name(), err)
		}
	}
	return b.changed, nil
}

func (d Deps) upgrade(ctx context.Context, args []string) error {
	fs := newFlags("upgrade")
	only, err := parse(fs, args)
	if err != nil {
		return err
	}
	bin := d.Getenv("CHEZMOI_EXECUTABLE")
	if bin == "" {
		bin = "chezmoi"
	}
	steps, err := upgrade.Select(upgrade.Plan(upgrade.Env{GOOS: d.GOOS, Home: d.Home, Bin: bin, LookPath: d.LookPath}), only)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	return upgrade.Run(ctx, d.Runner, steps, boolValue(fs, "dry-run"), d.Stdout, d.Stderr)
}

func (d Deps) live(ctx context.Context, args []string) error {
	fs := newFlags("live")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	global, _ := fs.Lookup("global").Value.(*stringList) // registered by newFlags
	src := stringValue(fs, "src")
	cz := chez.New(d.Runner, stringValue(fs, "bin"), append(slices.Clone(*global), "--source", src)...)
	l := edit.Live{
		CZ: cz, File: stringValue(fs, "file"), Edited: stringValue(fs, "edited"), Shadow: src,
		RealSrc: stringValue(fs, "real-src"), Dir: stringValue(fs, "dir"),
		Out: d.Stdout, Style: term.NewStyle(d.Stdout, d.Getenv), Interval: 500 * time.Millisecond,
	}
	return l.Loop(ctx)
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, " ") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }
