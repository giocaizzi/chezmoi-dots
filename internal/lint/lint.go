// Package lint runs the checks on the dotfiles source: the registries, the config
// template and the rendered templates.
package lint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/giocaizzi/chezmoi-dots/internal/aliasreg"
	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/finding"
	"github.com/giocaizzi/chezmoi-dots/internal/matrix"
	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
	"github.com/giocaizzi/chezmoi-dots/internal/registry"
)

// Checks lists the check names, in the order `lint` runs them.
func Checks() []string {
	return []string{"registry", "aliases", "packages", "matrix", "templates"}
}

// Linter runs the checks against one source dir.
type Linter struct {
	Src      string       // the source dir
	Iso      *chez.Client // chezmoi with an empty config, so no machine answers leak in
	Real     *chez.Client // chezmoi with this machine's config
	Runner   chez.Runner
	LookPath func(string) (string, error)
	Scratch  string
	Errw     io.Writer
}

// Run runs the named check.
func (l Linter) Run(ctx context.Context, name string) ([]finding.Finding, error) {
	switch name {
	case "registry":
		return l.registry(ctx)
	case "aliases":
		return l.aliases(ctx)
	case "packages":
		return l.packages(ctx)
	case "matrix":
		return l.matrix(ctx)
	case "templates":
		return l.templates(ctx)
	}
	return nil, fmt.Errorf("unknown check %q (checks: %s)", name, strings.Join(Checks(), ", "))
}

func (l Linter) registry(ctx context.Context) ([]finding.Finding, error) {
	data, err := l.Iso.Data(ctx, nil)
	if err != nil {
		return nil, err
	}
	reg, fs := registry.Parse(data)
	return append(fs, reg.Validate(os.DirFS(l.Src))...), nil
}

func (l Linter) aliases(ctx context.Context) ([]finding.Finding, error) {
	data, err := l.Iso.Data(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := aliasreg.Validate(data)
	// The private registry needs this machine's age key; where there is none it is skipped.
	realData, err := l.Real.Data(ctx, nil)
	if err != nil {
		return nil, err
	}
	if identity := ageIdentity(realData); identity != "" {
		if _, statErr := os.Stat(identity); statErr == nil {
			private, privErr := l.privateAliases(ctx)
			if privErr != nil {
				return nil, privErr
			}
			out = append(out, private...)
		}
	}
	return out, nil
}

// privateAliases validates the encrypted private registry, decrypted with this machine's key.
func (l Linter) privateAliases(ctx context.Context) ([]finding.Finding, error) {
	tmpl := `{{ include ".aliases.private.toml.age" | decrypt | fromToml | toJson }}`
	b, err := l.Real.Execute(ctx, nil, []byte(tmpl), false)
	if err != nil {
		return nil, err
	}
	var private map[string]any
	if err = json.Unmarshal(b, &private); err != nil {
		return nil, fmt.Errorf("decode private aliases: %w", err)
	}
	var out []finding.Finding
	for _, f := range aliasreg.Validate(private) {
		out = append(out, finding.New("private "+f.Where, "%s", f.Message))
	}
	return out, nil
}

func ageIdentity(data map[string]any) string {
	c, _ := data["chezmoi"].(map[string]any)
	cfg, _ := c["config"].(map[string]any)
	age, _ := cfg["age"].(map[string]any)
	id, _ := age["identity"].(string)
	return id
}

func (l Linter) packages(ctx context.Context) ([]finding.Finding, error) {
	data, err := l.Iso.Data(ctx, nil)
	if err != nil {
		return nil, err
	}
	return pkgreg.Validate(data), nil
}

// matrix checks the matrix declaration in the chezmoi data.
func (l Linter) matrix(ctx context.Context) ([]finding.Finding, error) {
	data, err := l.Iso.Data(ctx, nil)
	if err != nil {
		return nil, err
	}
	_, fs := matrix.ParseSpec(data)
	return fs, nil
}

// templates renders the script and alias templates for every configuration, and for
// every variant the matrix declares, and runs
// the linter of their language over the result; a template that renders empty does
// not apply to that configuration and is skipped, like chezmoi skips it.
func (l Linter) templates(ctx context.Context) ([]finding.Finding, error) {
	data, err := l.Iso.Data(ctx, nil)
	if err != nil {
		return nil, err
	}
	spec, err := matrix.SpecFrom(data)
	if err != nil {
		return nil, err
	}
	entries, err := matrix.Build(ctx, l.Real, l.Src, l.Scratch, spec)
	if err != nil {
		return nil, err
	}
	entries = spec.Expand(entries)
	files, err := l.templateFiles()
	if err != nil {
		return nil, err
	}
	// A template is rendered only where chezmoi manages it: .chezmoiignore, rendered
	// with the configuration's data, keeps gated templates (secrets, network) out.
	managed := make([]map[string]bool, len(entries))
	var mg errgroup.Group
	mg.SetLimit(4)
	for i, e := range entries {
		mg.Go(func() error {
			m, err := l.Iso.Managed(ctx, e.Overrides)
			if err != nil {
				return fmt.Errorf("list managed files for %s: %w", e.Label(), err)
			}
			managed[i] = map[string]bool{}
			for _, src := range m {
				managed[i][src] = true
			}
			return nil
		})
	}
	if err := mg.Wait(); err != nil {
		return nil, err
	}
	var (
		out []finding.Finding
		mu  sync.Mutex
		g   errgroup.Group
	)
	g.SetLimit(8)
	for _, f := range files {
		tmpl, err := os.ReadFile(filepath.Join(l.Src, filepath.FromSlash(f)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		for i, e := range entries {
			if !managed[i][f] {
				continue
			}
			g.Go(func() error {
				fs := l.renderAndLint(ctx, f, tmpl, e, i)
				mu.Lock()
				out = append(out, fs...)
				mu.Unlock()
				return nil
			})
		}
	}
	_ = g.Wait()
	return out, nil
}

// templateFiles lists the templates worth rendering whole: every `.tmpl` file whose
// target is a script or data file linterArgv handles. It skips what cannot render
// standalone or must not run: hidden directories (partials need arguments) other
// than .chezmoiscripts, chezmoi's own `.chezmoi*` templates, encrypted, symlink and
// modify files, and templates whose actions call a password-manager, secret or
// decrypt function, which would reach external services. The matrix declaration is
// the main isolation mechanism. Paths are sorted and slash-separated.
func (l Linter) templateFiles() ([]string, error) {
	var out []string
	err := filepath.WalkDir(l.Src, func(p string, d iofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(l.Src, p)
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", p, err)
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if hiddenPath(rel) {
			if d.IsDir() {
				return iofs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !templateName(filepath.Base(rel)) {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // p comes from walking the source dir
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		if callsSecret(b) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", l.Src, err)
	}
	sort.Strings(out)
	return out, nil
}

// hiddenPath reports whether a slash-separated source path has a component that
// starts with "." other than .chezmoiscripts.
func hiddenPath(rel string) bool {
	for c := range strings.SplitSeq(rel, "/") {
		if strings.HasPrefix(c, ".") && c != ".chezmoiscripts" {
			return true
		}
	}
	return false
}

// templateName reports whether a source base name is a plain template the linter can
// render: a `.tmpl` file with a handled target extension that is not a chezmoi
// config template and not an encrypted, symlink or modify file.
func templateName(base string) bool {
	target, ok := strings.CutSuffix(base, ".tmpl")
	if !ok || strings.HasPrefix(base, ".chezmoi") {
		return false
	}
	// the target extensions linterArgv handles
	if !slices.Contains([]string{".sh", ".bash", ".zsh", ".ps1", ".json"}, strings.ToLower(filepath.Ext(target))) {
		return false
	}
	// chezmoi's source-name attribute prefixes
	attributePrefixes := []string{
		"after_", "before_", "create_", "dot_", "empty_", "encrypted_", "exact_", "executable_",
		"external_", "literal_", "modify_", "once_", "onchange_", "private_", "readonly_",
		"remove_", "run_", "symlink_",
	}
	for rest, found := base, true; found; {
		found = false
		for _, pre := range attributePrefixes {
			after, ok := strings.CutPrefix(rest, pre)
			if !ok {
				continue
			}
			switch pre {
			case "encrypted_", "symlink_", "modify_":
				return false
			}
			rest, found = after, true
			break
		}
	}
	return true
}

var (
	// templateAction matches one `{{ ... }}` action.
	templateAction = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	// secretFunc matches a call to a chezmoi password-manager, secret or decrypt
	// function; field accesses (.pass) and variables ($pass) do not match.
	secretFunc = regexp.MustCompile(`(?:^|[^\w.$])(?:` +
		`(?:bitwarden|onepassword|keepassxc|lastpass|gopass|keeper|awsSecretsManager|dashlane|doppler|ejson|protonPass|rbw)\w*` +
		`|pass|passFields|passRaw|passhole|vault|secret|secretJSON|azureKeyVault|keyring|decrypt)\b`)
)

// callsSecret reports whether a template action calls a secret or decrypt function.
func callsSecret(tmpl []byte) bool {
	for _, a := range templateAction.FindAll(tmpl, -1) {
		if secretFunc.Match(a[2 : len(a)-2]) {
			return true
		}
	}
	return false
}

// linterArgv picks the linter from the target extension: shellcheck for shell, zsh -n
// for zsh, a PowerShell parse for ps1 (Windows configurations only), and a JSON
// validity check (jsonCheck) for json. Shell without a shebang is checked as bash. A
// nil argv with jsonCheck false means nothing to run.
func linterArgv(ext string, rendered []byte, path, goos string) (argv []string, jsonCheck bool) {
	switch ext {
	case ".json":
		return nil, true
	case ".sh", ".bash":
		argv = []string{"shellcheck", "-e", "SC2034"}
		if !bytes.HasPrefix(rendered, []byte("#!")) {
			argv = append(argv, "-s", "bash")
		}
		return append(argv, path), false
	case ".zsh":
		return []string{"zsh", "-n", path}, false
	case ".ps1":
		if goos != "windows" {
			return nil, false
		}
		return []string{"pwsh", "-NoProfile", "-Command", fmt.Sprintf(
			"$e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('%s', [ref]$null, [ref]$e); if ($e) { $e; exit 1 }", path)}, false
	}
	return nil, false
}

// renderAndLint renders one template for one matrix entry and lints the result with
// the linter its target extension selects; a template that renders empty does not
// apply to that entry and is skipped.
func (l Linter) renderAndLint(ctx context.Context, file string, tmpl []byte, e matrix.Entry, n int) []finding.Finding {
	where := fmt.Sprintf("%s (%s)", file, e.Label())
	rendered, err := l.Iso.Execute(ctx, e.Overrides, tmpl, false)
	if err != nil {
		return []finding.Finding{finding.New(where, "does not render: %v", err)}
	}
	if strings.TrimSpace(string(rendered)) == "" {
		return nil
	}
	ext := filepath.Ext(strings.TrimSuffix(file, ".tmpl"))
	path := filepath.Join(l.Scratch, fmt.Sprintf("render-%d-%s%s", n, strings.ReplaceAll(file, "/", "_"), ext))
	if err := os.WriteFile(path, rendered, 0o600); err != nil { //nolint:gosec // path is inside the linter's own scratch dir
		return []finding.Finding{finding.New(where, "write render: %v", err)}
	}
	argv, jsonCheck := linterArgv(ext, rendered, path, e.OS)
	if jsonCheck {
		if !json.Valid(rendered) {
			return []finding.Finding{finding.New(where, "renders invalid JSON")}
		}
		return nil
	}
	if argv == nil {
		return nil
	}
	if _, err := l.LookPath(argv[0]); err != nil {
		return nil // the tool is not installed here; CI has it
	}
	if _, err := chez.Output(ctx, l.Runner, chez.Cmd{Args: argv}); err != nil {
		return []finding.Finding{finding.New(where, "%v", err)}
	}
	return nil
}
