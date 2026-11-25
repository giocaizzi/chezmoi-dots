package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
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
	if len(pos) != 1 || (local && (tmpl || encrypt)) {
		return ErrUsage
	}
	if name != "" && !registry.NamePattern.MatchString(name) {
		return fmt.Errorf("%w: --name must match [A-Za-z0-9_-]+", ErrUsage)
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
	where, value, existing, err := d.addTarget(ctx, cz, reg, src, abs, local, tmpl || encrypt, flagsFor(tmpl, encrypt))
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
		if desc, err = d.askDesc(name); err != nil {
			return err
		}
	}

	probe := map[string]any{"edit": map[string]any{name: map[string]any{"desc": desc, where: value}}}
	if _, bad := registry.Parse(probe); len(bad) > 0 {
		return fmt.Errorf("invalid registry entry:\n%s", finding.Lines(bad))
	}

	file := filepath.Join(src, ".chezmoidata", "edit.toml")
	if err = writeEntry(file, name, desc, where, value); err != nil {
		return err
	}

	return d.checkRegistry(ctx, cz, src, file, name, value)
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

// addTarget settles where the entry points. It adds an unmanaged file to chezmoi
// unless local. A non-empty existing is the name of the entry that already covers the path.
func (d Deps) addTarget(ctx context.Context, cz *chez.Client, reg registry.Registry, src, abs string, local, wrap bool, flags []string) (where, value, existing string, err error) {
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

	sp, serr := cz.SourcePath(ctx, abs, nil)
	if serr != nil {
		if _, err = os.Stat(abs); err != nil {
			return "", "", "", fmt.Errorf("add %s: %w", abs, err)
		}
		if err = cz.Add(ctx, d.Stderr, d.Stderr, abs, flags...); err != nil {
			return "", "", "", err
		}
		if sp, err = cz.SourcePath(ctx, abs, nil); err != nil {
			return "", "", "", err
		}
	} else if wrap {
		return "", "", "", fmt.Errorf("%s is already managed; --template and --encrypt only apply to new files", abs)
	}
	rel, rerr := filepath.Rel(src, sp)
	if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", fmt.Errorf("source path %s is outside the source dir %s", sp, src)
	}
	rel = filepath.ToSlash(rel)
	if e, ok := reg.BySource(rel); ok {
		return "", "", e.Name, nil
	}
	return "source", rel, "", nil
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
func (d Deps) askDesc(name string) (string, error) {
	f, ok := d.Stdin.(*os.File)
	if !ok {
		return "", fmt.Errorf("%w: --desc is required when stdin is not a terminal", ErrUsage)
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&fs.ModeCharDevice == 0 {
		return "", fmt.Errorf("%w: --desc is required when stdin is not a terminal", ErrUsage)
	}
	_, _ = fmt.Fprintf(d.Stderr, "Description for %s: ", name)
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("read the description: " + err.Error())
	}
	desc := strings.TrimSpace(line)
	if desc == "" {
		return "", errors.New("the description is empty")
	}
	return desc, nil
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
