package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/detect"
	"github.com/giocaizzi/chezmoi-dots/internal/ghrelease"
	"github.com/giocaizzi/chezmoi-dots/internal/matrix"
	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
	"github.com/giocaizzi/chezmoi-dots/internal/registry"
)

// pkgAddSpec is a checked `pkg add` command line: a package, or a release when rel is
// set or github names the repository to read it from.
type pkgAddSpec struct {
	name   string
	pkg    pkgreg.PackageEntry
	rel    *pkgreg.ReleaseEntry
	github string // owner/repo of a --github release
	detect bool   // no package-source flag: ask the managers on this machine
	yes    bool
	dry    bool
}

// roles returns the --roles the entry is limited to.
func (s pkgAddSpec) roles() []string {
	if s.rel != nil {
		return s.rel.Roles
	}
	return s.pkg.Roles
}

// checkRoles rejects a role the matrix table does not declare; with no declared
// profile any name passes.
func checkRoles(data map[string]any, roles []string) error {
	declared := matrix.DeclaredProfiles(data)
	for _, r := range roles {
		if !pkgreg.ValidRole(declared, r) {
			return pkgAddUsage("--roles takes a comma list of the declared profiles (%s), not %q", strings.Join(declared, ", "), r)
		}
	}
	return nil
}

func (s pkgAddSpec) kind() string {
	if s.rel != nil || s.github != "" {
		return kindRelease
	}
	return "package"
}

const (
	kindRelease = "release"
	keyCask     = "cask"
	keyWinget   = "winget"
)

func pkgAddUsage(format string, args ...any) error {
	return &commandError{command: cmdPkgAdd, err: fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, args...))}
}

// pkgAdd adds one package or release entry to .chezmoidata/packages.toml, or the
// missing keys to an existing package.
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

	cz, _ := d.chezmoi()
	src, err := d.sourceDir(ctx, cz)
	if err != nil {
		return err
	}
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return fmt.Errorf("read the chezmoi data: %w", err)
	}
	if err := checkRoles(data, spec.roles()); err != nil {
		return err
	}
	kind, taken := pkgreg.Kind(data, spec.name)
	if taken && (kind == kindRelease || spec.kind() == kindRelease) {
		return fmt.Errorf("%s %s already exists; change it with chezmoi dots edit or move a release with chezmoi dots pkg bump", kind, spec.name)
	}
	file := filepath.Join(src, ".chezmoidata", "packages.toml")
	if spec.kind() == "package" {
		return d.pkgAddPackage(ctx, cz, file, spec, data, taken)
	}
	return d.pkgAddRelease(ctx, cz, file, spec)
}

// pkgAddPackage adds a new package entry, or the keys an existing package lacks.
func (d Deps) pkgAddPackage(ctx context.Context, cz *chez.Client, file string, spec pkgAddSpec, data map[string]any, taken bool) error {
	have := packageKeys(data, spec.name)
	if spec.detect {
		found, err := d.detectSources(ctx, spec.name, spec.yes)
		if err != nil {
			return err
		}
		for _, f := range found {
			setSource(&spec.pkg, f)
		}
	}

	// Keys the entry already has stay as they are.
	var lines []pkgreg.Line
	var already []string
	for _, l := range spec.pkg.Lines() {
		if have[l.Key] {
			already = append(already, l.Key)
		} else {
			lines = append(lines, l)
		}
	}
	for _, k := range already {
		_, _ = fmt.Fprintf(d.Stderr, "%s already set; change it with chezmoi dots edit\n", k)
	}
	if taken && len(lines) == 0 {
		return fmt.Errorf("package %s already has these keys; change it with chezmoi dots edit", spec.name)
	}
	keys := make([]string, len(lines))
	texts := make([]string, len(lines))
	for i, l := range lines {
		keys[i], texts[i] = l.Key, l.Text
		have[l.Key] = true
	}
	notes := osNotes(spec.name, have)

	entry := spec.pkg.Text()
	if taken {
		entry = strings.Join(texts, "\n") + "\n"
	}
	if spec.dry {
		_, _ = fmt.Fprint(d.Stdout, entry+notes)
		return nil
	}

	old, _, err := readRegistry(file)
	if err != nil {
		return err
	}
	if !taken {
		return d.writePackages(ctx, cz, file, pkgreg.Append(string(old), entry), spec.name, "added package "+spec.name, notes)
	}
	text, ok := pkgreg.InsertKeys(string(old), spec.name, texts)
	if !ok {
		return fmt.Errorf("package %s is not a [package.%s] table in %s; change it with chezmoi dots edit", spec.name, spec.name, file)
	}
	return d.writePackages(ctx, cz, file, text, spec.name, "updated package "+spec.name+": "+strings.Join(keys, ", "), notes)
}

// packageKeys is the set of keys the package name has in data; empty when it has none.
func packageKeys(data map[string]any, name string) map[string]bool {
	keys := map[string]bool{}
	table, _ := data["package"].(map[string]any)
	entry, _ := table[name].(map[string]any)
	for k := range entry {
		keys[k] = true
	}
	return keys
}

// pkgAddRelease adds a new release entry, read from the command line or from GitHub.
func (d Deps) pkgAddRelease(ctx context.Context, cz *chez.Client, file string, spec pkgAddSpec) error {
	var err error
	if spec.github != "" {
		if spec.rel, err = d.githubRelease(ctx, spec); err != nil {
			return err
		}
	}
	entry := spec.rel.Text()
	// A dry run downloads nothing: the checksums are computed when the entry is written.
	// An archive release read from GitHub has them already, from the download that listed it.
	if !spec.dry && spec.rel.Sums == nil {
		rel := pkgreg.Release{Name: spec.name, Version: spec.rel.Version, URL: spec.rel.URL, Targets: spec.rel.Targets}
		if spec.rel.Sums, err = d.finder().Sums(ctx, rel, rel.Version); err != nil {
			return err
		}
		entry = spec.rel.Text()
	}
	if spec.dry {
		_, _ = fmt.Fprint(d.Stdout, entry)
		return nil
	}
	old, _, err := readRegistry(file)
	if err != nil {
		return err
	}
	return d.writePackages(ctx, cz, file, pkgreg.Append(string(old), entry), spec.name, "added release "+spec.name, "")
}

// writePackages writes text over the registry file, validates the registry and
// follows the git settings.
func (d Deps) writePackages(ctx context.Context, cz *chez.Client, file, text, name, done, notes string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(file), err)
	}
	if err := writeFiles([]string{file}, map[string]string{file: text}, os.Rename, 0o644); err != nil {
		return err
	}
	return d.checkPackages(ctx, cz, file, name, done+"\n"+notes)
}

// checkPackages validates the whole package registry after the write, as lint packages
// does, then follows the chezmoi git settings for the file alone.
func (d Deps) checkPackages(ctx context.Context, cz *chez.Client, file, name, done string) error {
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return fmt.Errorf("read the chezmoi data: %w", err)
	}
	if found := pkgreg.Validate(data); len(found) > 0 {
		_, _ = fmt.Fprintf(d.Stderr, "✗ packages\n%s\n%s was written to %s; fix it (chezmoi dots lint packages)\n", joinLines(found), name, file)
		return ErrReported
	}
	_, _ = fmt.Fprint(d.Stdout, done)
	return d.commitRegistry(ctx, cz, file)
}

// osNotes lists, for each OS none of whose registry keys the entry has, how to set them.
func osNotes(name string, have map[string]bool) string {
	var b strings.Builder
	keysOf := detect.OSKeys()
	for _, goos := range slices.Sorted(maps.Keys(keysOf)) {
		keys := keysOf[goos]
		if slices.ContainsFunc(keys, func(k string) bool { return have[k] }) {
			continue
		}
		flags := make([]string, len(keys))
		for i, k := range keys {
			flags[i] = "--" + k
		}
		fmt.Fprintf(&b, "not set for %s: %s; pass %s or run chezmoi dots pkg add %s on %s\n",
			goos, strings.Join(keys, ", "), strings.Join(flags, "/"), name, goos)
	}
	return b.String()
}

// setSource stores a detected or confirmed source on the entry.
func setSource(e *pkgreg.PackageEntry, f detect.Found) {
	switch f.Key {
	case "brew":
		e.Brew = f.ID
	case keyCask:
		e.Cask = f.ID
	case "apt":
		e.Apt = f.ID
	case keyWinget:
		e.Winget = f.ID
	case "uv":
		e.UV = f.ID
	case "npm":
		e.NPM = f.ID
	}
}

// interactive reports whether answers can be asked on stdin.
func (d Deps) interactive() bool {
	if d.Interactive != nil {
		return d.Interactive()
	}
	f, ok := d.Stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&fs.ModeCharDevice != 0
}

// prompter asks questions on stderr and reads the answers from stdin.
type prompter struct {
	ctx context.Context //nolint:containedctx // scoped to one command run
	err io.Writer
	in  *bufio.Reader
}

func (d Deps) prompter(ctx context.Context) *prompter {
	var in io.Reader = strings.NewReader("")
	if d.Stdin != nil {
		in = d.Stdin
	}
	return &prompter{ctx: ctx, err: d.Stderr, in: bufio.NewReader(in)}
}

func (p *prompter) ask(format string, args ...any) (string, error) {
	_, _ = fmt.Fprintf(p.err, format, args...)
	line, err := readLine(p.ctx, p.in)
	return strings.TrimSpace(line), err
}

// detectSources asks the package managers on this machine about name, prints what
// they know and returns the sources to write: confirmed one by one on a terminal,
// else all of them when yes is set.
func (d Deps) detectSources(ctx context.Context, name string, yes bool) ([]detect.Found, error) {
	found, none := detect.Detector{Runner: d.Runner, LookPath: d.LookPath, GOOS: d.GOOS, Home: d.Home}.Detect(ctx, name)
	for _, f := range found {
		_, _ = fmt.Fprintf(d.Stderr, "  %s = %q  (%s)\n", f.Key, f.ID, f.Confidence)
	}
	if len(none) > 0 {
		_, _ = fmt.Fprintf(d.Stderr, "  nothing found by %s\n", strings.Join(none, ", "))
	}
	if len(found) == 0 {
		asked := "any package manager on this machine"
		if len(none) > 0 {
			asked = strings.Join(none, ", ")
		}
		return nil, pkgAddUsage("nothing found for %s by %s; set the sources with --brew, --cask, --apt, --winget, --uv or --npm", name, asked)
	}
	if yes {
		return found, nil
	}
	if !d.interactive() {
		return nil, pkgAddUsage("detected sources need confirmation; pass --yes or set them with flags")
	}
	p := d.prompter(ctx)
	var kept []detect.Found
	for _, f := range found {
		keep, err := p.confirm(f)
		if err != nil {
			return nil, err
		}
		if keep != nil {
			kept = append(kept, *keep)
		}
	}
	if len(kept) == 0 {
		return nil, pkgAddUsage("no detected source was confirmed; set the sources with flags")
	}
	return kept, nil
}

// confirm asks about one detected source: yes keeps it, no drops it (nil), e takes
// another id.
func (p *prompter) confirm(f detect.Found) (*detect.Found, error) {
	for {
		ans, err := p.ask("%s = %q? [Y/n/e] ", f.Key, f.ID)
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(ans) {
		case "", "y", "yes":
			return &f, nil
		case "n", "no":
			return nil, nil //nolint:nilnil // nil means declined
		case "e":
			id, err := p.ask("%s id (empty keeps %q): ", f.Key, f.ID)
			if err != nil {
				return nil, err
			}
			if id != "" {
				f.ID = id
			}
			return &f, nil
		}
	}
}

// printProposal writes the proposed entry to w; pathNote says how the path was settled.
func printProposal(w io.Writer, prop ghrelease.Proposal, pathNote, candidates string) {
	_, _ = fmt.Fprintf(w, "  version = %q\n  url = %q\n  type = %q\n", prop.Version, prop.URL, prop.Type)
	if prop.Path != "" {
		_, _ = fmt.Fprintf(w, "  path = %q  (%s)\n", prop.Path, pathNote)
		if candidates != "" {
			_, _ = fmt.Fprintf(w, "  path candidates: %s\n", candidates)
		}
	}
	if prop.Exe {
		_, _ = fmt.Fprintln(w, "  exe = true")
	}
	for _, k := range slices.Sorted(maps.Keys(prop.Targets)) {
		_, _ = fmt.Fprintf(w, "  %s = %q\n", k, prop.Targets[k])
	}
	if len(prop.Unmapped) > 0 {
		_, _ = fmt.Fprintf(w, "  left out: %s\n", strings.Join(prop.Unmapped, ", "))
	}
	if len(prop.Ambiguous) > 0 {
		_, _ = fmt.Fprintf(w, "  ambiguous: %s\n", strings.Join(prop.Ambiguous, "; "))
	}
}

// resolvePath settles the path of an archive release: read from the archives, which are
// downloaded here, unless a dry run or a missing way to confirm makes the command
// download nothing.
func (d Deps) resolvePath(ctx context.Context, spec pkgAddSpec, prop *ghrelease.Proposal) (sums map[string]string, note, candidates string, err error) {
	switch {
	case spec.dry:
		return nil, "unchecked: --dry-run downloads no archive", "", nil
	case !spec.yes && !d.interactive(): // the command fails after the proposal
		return nil, "unchecked: the proposal cannot be confirmed, so no archive is downloaded", "", nil
	}
	return d.inspectArchives(ctx, spec, prop)
}

// githubRelease reads the latest release of spec.github, prints the entry derived from
// its assets and has it confirmed. For archives it downloads them first: the path of the
// binary is read from their contents and the checksums are kept for the write.
func (d Deps) githubRelease(ctx context.Context, spec pkgAddSpec) (*pkgreg.ReleaseEntry, error) {
	rel, err := ghrelease.Client{HTTP: d.HTTP, Getenv: d.Getenv}.Latest(ctx, spec.github)
	if err != nil {
		return nil, err
	}
	prop, err := ghrelease.Map(spec.github, rel)
	if err != nil {
		return nil, fmt.Errorf("map the assets of %s: %w", spec.github, err)
	}
	var sums map[string]string
	var pathNote, candidates string
	if prop.Type == typeArchiveFile {
		if sums, pathNote, candidates, err = d.resolvePath(ctx, spec, &prop); err != nil {
			return nil, err
		}
	}
	printProposal(d.Stderr, prop, pathNote, candidates)
	switch {
	case spec.yes:
	case !d.interactive():
		return nil, pkgAddUsage("the proposed release needs confirmation; pass --yes or give --release with --version, --url and --target")
	default:
		ans, aerr := d.prompter(ctx).ask("Add this release? [y/N] ")
		if aerr != nil {
			return nil, aerr
		}
		if a := strings.ToLower(ans); a != "y" && a != "yes" {
			return nil, errors.New("pkg add: the proposed release was not confirmed; nothing was written")
		}
	}
	return &pkgreg.ReleaseEntry{
		Name: spec.name, Version: prop.Version, URL: prop.URL, Type: prop.Type, Path: prop.Path, Exe: prop.Exe,
		Targets: prop.Targets, Sums: sums, Roles: spec.rel.Roles, Requires: spec.rel.Requires,
	}, nil
}

const typeArchiveFile = "archive-file"

// inspectArchives downloads the archive of every target once, for the checksums and the
// listing, and sets prop.Path to the binary found in them. note says how that went;
// candidates, when the path was not found, lists what each archive holds.
func (d Deps) inspectArchives(ctx context.Context, spec pkgAddSpec, prop *ghrelease.Proposal) (sums map[string]string, note, candidates string, err error) {
	rel := pkgreg.Release{Name: spec.name, Version: prop.Version, URL: prop.URL, Targets: prop.Targets}
	sums, files, err := d.finder().Inspect(ctx, rel, rel.Version)
	if err != nil {
		return nil, "", "", err
	}
	_, repo, _ := strings.Cut(spec.github, "/")
	found, ok, cands := ghrelease.ProposePath([]string{spec.name, repo}, *prop, files)
	switch {
	case ok:
		prop.Path = found
		return sums, "found in the archives", "", nil
	case len(cands) == 0:
		return sums, "unchecked: no .tar.gz, .tgz or .zip archive could be read", "", nil
	}
	parts := make([]string, 0, len(cands))
	for _, k := range slices.Sorted(maps.Keys(cands)) {
		list := "none"
		if len(cands[k]) > 0 {
			quoted := make([]string, len(cands[k]))
			for i, c := range cands[k] {
				quoted[i] = strconv.Quote(c) // archive names are untrusted: no raw control sequences
			}
			list = strings.Join(quoted, ", ")
		}
		parts = append(parts, k+": "+list)
	}
	return sums, "not found in the archives", strings.Join(parts, "; "), nil
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

	sources := []string{"brew", keyCask, "apt", keyWinget, "uv", "npm"}
	releaseOnly := []string{"version", "url", "target"}
	common := pkgAddSpec{name: name, yes: boolValue(fs, "yes"), dry: boolValue(fs, "dry-run")}
	if set["github"] {
		if err := githubSpec(fs, set, append(slices.Clone(sources), releaseOnly...)); err != nil {
			return pkgAddSpec{}, err
		}
		common.github = stringValue(fs, "github")
		common.rel = &pkgreg.ReleaseEntry{Name: name, Roles: roles, Requires: requires}
		return common, nil
	}
	if boolValue(fs, "release") {
		if i := slices.IndexFunc(sources, func(f string) bool { return set[f] }); i >= 0 {
			return pkgAddSpec{}, pkgAddUsage("--%s installs a package; it does not combine with --release", sources[i])
		}
		spec, err := releaseSpec(fs, name, roles, requires)
		spec.yes, spec.dry = common.yes, common.dry
		return spec, err
	}
	if i := slices.IndexFunc(releaseOnly, func(f string) bool { return set[f] }); i >= 0 {
		return pkgAddSpec{}, pkgAddUsage("--%s only applies with --release", releaseOnly[i])
	}
	for _, f := range []string{keyCask, keyWinget} {
		if set[f] && stringValue(fs, f) == "" {
			return pkgAddSpec{}, pkgAddUsage("--%s needs a value", f)
		}
	}
	opt := func(f string) string {
		o, _ := fs.Lookup(f).Value.(*optValue) // newFlags registers these as optValue
		return o.resolve(name)
	}
	common.pkg = pkgreg.PackageEntry{
		Name: name, Brew: opt("brew"), Cask: stringValue(fs, keyCask), Apt: opt("apt"), Winget: stringValue(fs, keyWinget),
		UV: opt("uv"), NPM: opt("npm"), Roles: roles, Requires: requires,
	}
	// Without a source flag the managers on this machine are asked.
	common.detect = !slices.ContainsFunc(sources, func(f string) bool { return set[f] })
	return common, nil
}

// githubSpec rejects --github with the flags that name a release or a package by hand.
func githubSpec(fs *flag.FlagSet, set map[string]bool, exclusive []string) error {
	if stringValue(fs, "github") == "" {
		return pkgAddUsage("--github needs owner/repo")
	}
	if i := slices.IndexFunc(exclusive, func(f string) bool { return set[f] }); i >= 0 {
		return pkgAddUsage("--%s does not combine with --github", exclusive[i])
	}
	return nil
}

// parseRoles splits the --roles list, keeping its order and dropping repeats.
func parseRoles(list string) ([]string, error) {
	var roles []string
	for r := range strings.SplitSeq(list, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			return nil, pkgAddUsage("--roles takes a comma list of non-empty names")
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
