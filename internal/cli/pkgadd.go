package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
	"github.com/giocaizzi/chezmoi-dots/internal/registry"
)

// pkgAddSpec is a checked `pkg add` command line: a package, or a release when rel is set.
type pkgAddSpec struct {
	name string
	pkg  pkgreg.PackageEntry
	rel  *pkgreg.ReleaseEntry
}

func (s pkgAddSpec) kind() string {
	if s.rel != nil {
		return "release"
	}
	return "package"
}

func pkgAddUsage(format string, args ...any) error {
	return &commandError{command: cmdPkgAdd, err: fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, args...))}
}

// pkgAdd appends one package or release entry to .chezmoidata/packages.toml.
func (d Deps) pkgAdd(ctx context.Context, args []string) error {
	fs := newFlags(cmdPkgAdd)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	spec, err := parsePkgAdd(fs, pos)
	if err != nil {
		return err
	}
	dry := boolValue(fs, "dry-run")

	cz, _ := d.chezmoi()
	src, err := d.sourceDir(ctx, cz)
	if err != nil {
		return err
	}
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return fmt.Errorf("read the chezmoi data: %w", err)
	}
	if kind, taken := pkgreg.Kind(data, spec.name); taken {
		return fmt.Errorf("%s %s already exists; change it with chezmoi dots edit or move a release with chezmoi dots pkg bump", kind, spec.name)
	}

	var entry string
	switch {
	case spec.rel == nil:
		entry = spec.pkg.Text()
	case dry: // no download: the checksums are computed when the entry is written
		entry = spec.rel.Text()
	default:
		rel := pkgreg.Release{Name: spec.name, Version: spec.rel.Version, URL: spec.rel.URL, Targets: spec.rel.Targets}
		if spec.rel.Sums, err = d.finder().Sums(ctx, rel, rel.Version); err != nil {
			return err
		}
		entry = spec.rel.Text()
	}
	if dry {
		_, _ = fmt.Fprint(d.Stdout, entry)
		return nil
	}

	file := filepath.Join(src, ".chezmoidata", "packages.toml")
	old, _, err := readRegistry(file)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(file), err)
	}
	texts := map[string]string{file: pkgreg.Append(string(old), entry)}
	if err = writeFiles([]string{file}, texts, os.Rename, 0o644); err != nil {
		return err
	}
	return d.checkPackages(ctx, cz, file, spec)
}

// checkPackages validates the whole package registry after the write, as lint packages
// does, then follows the chezmoi git settings for the file alone.
func (d Deps) checkPackages(ctx context.Context, cz *chez.Client, file string, spec pkgAddSpec) error {
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return fmt.Errorf("read the chezmoi data: %w", err)
	}
	if found := pkgreg.Validate(data); len(found) > 0 {
		_, _ = fmt.Fprintf(d.Stderr, "✗ packages\n%s\n%s was written to %s; fix it (chezmoi dots lint packages)\n", joinLines(found), spec.name, file)
		return ErrReported
	}
	_, _ = fmt.Fprintf(d.Stdout, "added %s %s\n", spec.kind(), spec.name)
	return d.commitRegistry(ctx, cz, file)
}

// parsePkgAdd checks the command line and builds the entry it describes.
func parsePkgAdd(fs *flag.FlagSet, pos []string) (pkgAddSpec, error) {
	if len(pos) != 1 {
		return pkgAddSpec{}, pkgAddUsage("pkg add needs exactly one <name>")
	}
	name := pos[0]
	if !registry.NamePattern.MatchString(name) {
		return pkgAddSpec{}, pkgAddUsage("<name> must match [A-Za-z0-9_-]+")
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	var roles []string
	if set["roles"] {
		var err error
		if roles, err = parseRoles(stringValue(fs, "roles")); err != nil {
			return pkgAddSpec{}, err
		}
	}
	requires := stringValue(fs, "requires")
	if set["requires"] && requires == "" {
		return pkgAddSpec{}, pkgAddUsage("--requires needs a [data] answer")
	}

	sources := []string{"brew", "cask", "apt", "winget", "uv", "npm"}
	releaseOnly := []string{"version", "url", "target"}
	if boolValue(fs, "release") {
		if i := slices.IndexFunc(sources, func(f string) bool { return set[f] }); i >= 0 {
			return pkgAddSpec{}, pkgAddUsage("--%s installs a package; it does not combine with --release", sources[i])
		}
		return releaseSpec(fs, name, roles, requires)
	}
	if i := slices.IndexFunc(releaseOnly, func(f string) bool { return set[f] }); i >= 0 {
		return pkgAddSpec{}, pkgAddUsage("--%s only applies with --release", releaseOnly[i])
	}
	for _, f := range []string{"cask", "winget"} {
		if set[f] && stringValue(fs, f) == "" {
			return pkgAddSpec{}, pkgAddUsage("--%s needs a value", f)
		}
	}
	opt := func(f string) string {
		o, _ := fs.Lookup(f).Value.(*optValue) // newFlags registers these as optValue
		return o.resolve(name)
	}
	e := pkgreg.PackageEntry{
		Name: name, Brew: opt("brew"), Cask: stringValue(fs, "cask"), Apt: opt("apt"), Winget: stringValue(fs, "winget"),
		UV: opt("uv"), NPM: opt("npm"), Roles: roles, Requires: requires,
	}
	if e.Brew+e.Cask+e.Apt+e.Winget+e.UV+e.NPM == "" {
		return pkgAddSpec{}, pkgAddUsage("name at least one install source (--brew, --cask, --apt, --winget, --uv, --npm) or use --release")
	}
	return pkgAddSpec{name: name, pkg: e}, nil
}

// parseRoles splits the --roles list, keeping its order and dropping repeats.
func parseRoles(list string) ([]string, error) {
	var roles []string
	for r := range strings.SplitSeq(list, ",") {
		r = strings.TrimSpace(r)
		if !pkgreg.ValidRole(r) {
			return nil, pkgAddUsage("--roles takes a comma list of personal, work, light, not %q", r)
		}
		if !slices.Contains(roles, r) {
			roles = append(roles, r)
		}
	}
	return roles, nil
}

// releaseSpec builds the release entry of a `pkg add --release` command line.
func releaseSpec(fs *flag.FlagSet, name string, roles []string, requires string) (pkgAddSpec, error) {
	version, url := stringValue(fs, "version"), stringValue(fs, "url")
	if version == "" || url == "" {
		return pkgAddSpec{}, pkgAddUsage("--release needs --version, --url and at least one --target")
	}
	if !strings.Contains(url, "{version}") {
		return pkgAddSpec{}, pkgAddUsage("--url must contain {version}")
	}
	list, _ := fs.Lookup("target").Value.(*stringList) // registered by newFlags
	if len(*list) == 0 {
		return pkgAddSpec{}, pkgAddUsage("--release needs --version, --url and at least one --target")
	}
	targets := map[string]string{}
	for _, t := range *list {
		platform, asset, ok := strings.Cut(t, "=")
		if !ok || asset == "" || !pkgreg.ValidPlatform(platform) {
			return pkgAddSpec{}, pkgAddUsage("--target %q must be <os>/<arch>=<name> (os darwin, linux or windows; arch amd64 or arm64)", t)
		}
		if _, dup := targets[platform]; dup {
			return pkgAddSpec{}, pkgAddUsage("--target %s is given twice", platform)
		}
		targets[platform] = asset
	}
	return pkgAddSpec{name: name, rel: &pkgreg.ReleaseEntry{
		Name: name, Version: version, URL: url, Targets: targets, Roles: roles, Requires: requires,
	}}, nil
}
