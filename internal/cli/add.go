package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/edit"
	"github.com/giocaizzi/chezmoi-dots/internal/finding"
	"github.com/giocaizzi/chezmoi-dots/internal/registry"
)

// add tracks a file with chezmoi and registers it in the edit registry.
func (d Deps) add(ctx context.Context, args []string) error {
	set := newFlags(cmdAdd)
	pos, err := parse(set, args)
	if err != nil {
		return err
	}
	name, desc := stringValue(set, "name"), stringValue(set, "desc")
	tmpl, encrypt, local := boolValue(set, "template"), boolValue(set, "encrypt"), boolValue(set, "local")
	if err = checkAddArgs(pos, name, local, tmpl || encrypt); err != nil {
		return err
	}
	abs, err := d.absPath(pos[0], local)
	if err != nil {
		return err
	}

	cz, _ := d.chezmoi()
	src, reg, err := d.loadRegistry(ctx, cz)
	if err != nil {
		return err
	}
	where, value, existing, err := d.addTarget(ctx, cz, reg, src, abs, local, tmpl || encrypt)
	if err != nil {
		return err
	}
	if existing != "" {
		_, _ = fmt.Fprintf(d.Stdout, "%s is already registered as %s\n", pos[0], existing)
		return nil
	}

	if name, err = d.entryName(reg, name, abs, local); err != nil {
		return err
	}
	if desc == "" {
		if desc, err = d.askDesc(ctx, name); err != nil {
			return err
		}
	}

	file := filepath.Join(src, ".chezmoidata", "edit.toml")
	if where == "" { // unmanaged: name and description are settled, so chezmoi add now
		if value, err = d.track(ctx, cz, src, file, abs, name, desc, flagsFor(tmpl, encrypt)); err != nil {
			return err
		}
		return d.checkRegistry(ctx, cz, src, file, name, value)
	}
	if err = checkEntry(name, desc, where, value); err != nil {
		return err
	}
	if err = writeEntry(file, name, desc, where, value); err != nil {
		return err
	}
	if err = d.checkRegistry(ctx, cz, src, file, name, value); err != nil {
		return err
	}
	// No chezmoi add ran, so chezmoi's own git automation did not: follow the same
	// settings as edit for the registry file alone.
	return d.commitRegistry(ctx, cz, file)
}

// checkEntry rejects an entry the registry would not accept.
func checkEntry(name, desc, where, value string) error {
	probe := map[string]any{"edit": map[string]any{name: map[string]any{"desc": desc, where: value}}}
	if _, bad := registry.Parse(probe); len(bad) > 0 {
		return fmt.Errorf("invalid registry entry:\n%s", finding.Lines(bad))
	}
	return nil
}

// commitRegistry stages, commits and pushes only the registry file as far as the
// chezmoi git settings ask, as edit does for a source file.
func (d Deps) commitRegistry(ctx context.Context, cz *chez.Client, file string) error {
	cfg, err := cz.EditConfig(ctx)
	if err != nil {
		return fmt.Errorf("read the chezmoi git settings: %w", err)
	}
	g := cfg.Git
	if (!g.AutoAdd && !g.AutoCommit && !g.AutoPush) || cfg.WorkingTree == "" {
		return nil
	}
	rel, err := filepath.Rel(cfg.WorkingTree, file)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("registry file %s is outside the Git working tree %s", file, cfg.WorkingTree)
	}
	rel = filepath.ToSlash(rel)
	git := edit.ExecGit{Runner: d.Runner}
	if err = git.Stage(ctx, cfg.WorkingTree, rel); err != nil {
		return fmt.Errorf("stage %q: %w", rel, err)
	}
	if !g.AutoCommit && !g.AutoPush {
		return nil
	}
	if err = git.Commit(ctx, cfg.WorkingTree, rel); err != nil {
		return fmt.Errorf("commit %q: %w", rel, err)
	}
	if g.AutoPush {
		if err = git.Push(ctx, cfg.WorkingTree); err != nil {
			return fmt.Errorf("push Git changes (commit kept locally): %w", err)
		}
	}
	return nil
}

// checkAddArgs rejects command lines add cannot act on.
func checkAddArgs(pos []string, name string, local, wrap bool) error {
	if len(pos) != 1 || (local && wrap) {
		return ErrUsage
	}
	if name != "" && !registry.NamePattern.MatchString(name) {
		return fmt.Errorf("%w: --name must match [A-Za-z0-9_-]+", ErrUsage)
	}
	return nil
}

// checkRegistry validates the whole registry after the write, as lint registry does.
func (d Deps) checkRegistry(ctx context.Context, cz *chez.Client, src, file, name, value string) error {
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return fmt.Errorf("read the chezmoi data: %w", err)
	}
	full, found := registry.Parse(data)
	found = append(found, full.Validate(os.DirFS(src))...)
	if len(found) > 0 {
		_, _ = fmt.Fprintf(d.Stderr, "✗ registry\n%s\n%s was written to %s; fix the registry (chezmoi dots lint registry)\n", joinLines(found), name, file)
		return ErrReported
	}
	_, _ = fmt.Fprintf(d.Stdout, "added %s: %s\n", name, value)
	return nil
}

// entryName is the explicit name when free, else one inferred from the path.
func (d Deps) entryName(reg registry.Registry, name, abs string, isDir bool) (string, error) {
	if name != "" {
		if _, taken := reg.Get(name); taken {
			return "", fmt.Errorf("entry %q already exists", name)
		}
		return name, nil
	}
	name, ok := registry.InferName(abs, isDir, func(n string) bool { _, taken := reg.Get(n); return taken })
	if !ok {
		return "", fmt.Errorf("cannot infer a free entry name for %s; pass --name", abs)
	}
	return name, nil
}

// loadRegistry returns the source dir and the parsed registry.
func (d Deps) loadRegistry(ctx context.Context, cz *chez.Client) (string, registry.Registry, error) {
	src, err := d.sourceDir(ctx, cz)
	if err != nil {
		return "", registry.Registry{}, err
	}
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return "", registry.Registry{}, fmt.Errorf("read the chezmoi data: %w", err)
	}
	reg, problems := registry.Parse(data)
	if len(problems) > 0 {
		return "", registry.Registry{}, fmt.Errorf("the edit registry is invalid (chezmoi dots lint registry):\n%s", finding.Lines(problems))
	}
	return src, reg, nil
}

func flagsFor(tmpl, encrypt bool) []string {
	var f []string
	if tmpl {
		f = append(f, "--template")
	}
	if encrypt {
		f = append(f, "--encrypt")
	}
	return f
}

// absPath expands a leading home marker, makes the path absolute and cleans it;
// for a local entry it is the folder that is, or holds, the path.
func (d Deps) absPath(p string, local bool) (string, error) {
	switch {
	case p == "~":
		p = d.Home
	case strings.HasPrefix(p, "~/") || (d.GOOS == "windows" && strings.HasPrefix(p, `~\`)):
		p = filepath.Join(d.Home, p[2:])
	}
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", p, err)
		}
		p = filepath.Join(wd, p)
	}
	p = filepath.Clean(p)
	if local {
		return localFolder(p)
	}
	return p, nil
}

// addTarget settles where the entry points without changing anything. A non-empty
// existing is the name of the entry that already covers the path; an empty where
// means the file is not managed yet and track must add it.
func (d Deps) addTarget(ctx context.Context, cz *chez.Client, reg registry.Registry, src, abs string, local, wrap bool) (where, value, existing string, err error) {
	if local {
		rel, rerr := filepath.Rel(d.Home, abs)
		if rerr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", "", "", fmt.Errorf("%s is not a folder inside the home dir %s", abs, d.Home)
		}
		rel = filepath.ToSlash(rel)
		for _, n := range reg.Names() {
			if e, _ := reg.Get(n); e.Local == rel {
				return "", "", n, nil
			}
		}
		return "local", rel, "", nil
	}

	info, statErr := os.Lstat(abs) // a symlink to a folder is added as a symlink, not walked
	if statErr == nil && info.IsDir() {
		return "", "", "", fmt.Errorf("%w: %s is a folder; use --local to register a folder, or add one file", ErrUsage, abs)
	}
	sp, serr := cz.SourcePath(ctx, abs, nil)
	if serr != nil {
		if statErr != nil {
			return "", "", "", fmt.Errorf("add %s: %w", abs, statErr)
		}
		return "", "", "", nil
	}
	if wrap {
		return "", "", "", fmt.Errorf("%s is already managed; --template and --encrypt only apply to new files", abs)
	}
	rel, err := sourceRel(src, sp)
	if err != nil {
		return "", "", "", err
	}
	if e, ok := reg.BySource(rel); ok {
		return "", "", e.Name, nil
	}
	return "source", rel, "", nil
}

// track registers an unmanaged file, then runs chezmoi add and returns its source
// path. The entry is written first so that chezmoi's git automation, which runs
// inside chezmoi add, commits and pushes the source file together with its entry.
func (d Deps) track(ctx context.Context, cz *chez.Client, src, file, abs, name, desc string, flags []string) (string, error) {
	planned, err := cz.AddPlan(ctx, abs, flags...)
	if err != nil {
		return "", err
	}
	if err = checkEntry(name, desc, "source", planned); err != nil {
		return "", err
	}
	old, existed, err := readRegistry(file)
	if err != nil {
		return "", err
	}
	if err = writeEntry(file, name, desc, "source", planned); err != nil {
		return "", err
	}
	if err = cz.Add(ctx, d.Stderr, d.Stderr, abs, flags...); err != nil {
		// chezmoi add can fail after it created the source file (a failed autoPush
		// after the commit): the entry then belongs with it and stays.
		if _, serr := os.Lstat(filepath.Join(src, filepath.FromSlash(planned))); serr == nil {
			return "", fmt.Errorf("%w (%s was added, so its entry %s stays in %s)", err, planned, name, file)
		}
		return "", errors.Join(err, restoreRegistry(file, old, existed))
	}
	sp, err := cz.SourcePath(ctx, abs, nil)
	if err != nil {
		return "", err
	}
	rel, err := sourceRel(src, sp)
	if err != nil {
		return "", err
	}
	if rel != planned {
		return "", fmt.Errorf("the entry %s was written for %s but chezmoi created %s; fix %s", name, planned, rel, file)
	}
	return rel, nil
}

// readRegistry returns the registry file's bytes and whether it exists.
func readRegistry(file string) (old []byte, existed bool, err error) {
	old, err = os.ReadFile(file) //nolint:gosec // the user's own source dir
	switch {
	case err == nil:
		return old, true, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("read %s: %w", file, err)
	}
}

// restoreRegistry puts the registry file back as it was before the entry.
func restoreRegistry(file string, old []byte, existed bool) error {
	if !existed {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", file, err)
		}
		return nil
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(file); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(file, old, mode); err != nil {
		return fmt.Errorf("restore %s: %w", file, err)
	}
	return nil
}

// sourceRel is sp relative to the source dir, with forward slashes.
func sourceRel(src, sp string) (string, error) {
	rel, err := filepath.Rel(src, sp)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source path %s is outside the source dir %s", sp, src)
	}
	return filepath.ToSlash(rel), nil
}

// localFolder is path itself for a folder, else the folder that holds the file.
func localFolder(p string) (string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", p, err)
	}
	if info.IsDir() {
		return p, nil
	}
	return filepath.Dir(p), nil
}

// askDesc prompts for a description on a terminal; elsewhere --desc is required.
func (d Deps) askDesc(ctx context.Context, name string) (string, error) {
	f, ok := d.Stdin.(*os.File)
	if !ok {
		return "", fmt.Errorf("%w: --desc is required when stdin is not a terminal", ErrUsage)
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&fs.ModeCharDevice == 0 {
		return "", fmt.Errorf("%w: --desc is required when stdin is not a terminal", ErrUsage)
	}
	_, _ = fmt.Fprintf(d.Stderr, "Description for %s: ", name)
	line, err := readLine(ctx, f)
	if err != nil {
		return "", err
	}
	desc := strings.TrimSpace(line)
	if desc == "" {
		return "", errors.New("the description is empty")
	}
	return desc, nil
}

// readLine reads one line from r, or returns when ctx is cancelled. A blocked read
// cannot be interrupted, so its goroutine ends with the process the caller is about
// to exit, or when r yields.
func readLine(ctx context.Context, r io.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(r).ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("read the description: %w", ctx.Err())
	case res := <-ch:
		if res.err != nil && res.line == "" {
			return "", fmt.Errorf("read the description: %w", res.err)
		}
		return res.line, nil
	}
}

// writeEntry appends the entry to the registry file, creating it when missing and
// keeping its mode otherwise.
func writeEntry(file, name, desc, where, value string) error {
	mode := os.FileMode(0o644)
	var old string
	b, err := os.ReadFile(file) //nolint:gosec // the user's own source dir
	switch {
	case err == nil:
		old = string(b)
		if info, serr := os.Stat(file); serr == nil {
			mode = info.Mode().Perm()
		}
	case errors.Is(err, fs.ErrNotExist):
		if merr := os.MkdirAll(filepath.Dir(file), 0o750); merr != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(file), merr)
		}
	default:
		return fmt.Errorf("read %s: %w", file, err)
	}
	if err = os.WriteFile(file, []byte(registry.Append(old, name, desc, where, value)), mode); err != nil { //nolint:gosec // the user's own source dir
		return fmt.Errorf("write %s: %w", file, err)
	}
	return nil
}
