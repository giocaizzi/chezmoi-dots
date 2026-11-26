// Package matrix is the set of configurations a dotfiles edit is rendered for: every
// OS and every profile the dotfiles declare. Each configuration's data comes from
// rendering the real .chezmoi.toml.tmpl with that profile's answers (the optional
// `matrix` declaration in the chezmoi data), so it cannot drift from what
// `chezmoi init` stores. The tool knows no profile names: without a declaration there
// is one profile, "default".
package matrix

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/finding"
)

// Config is one OS and profile.
type Config struct {
	OS      string
	Profile string
}

// OSes are the supported operating systems.
func OSes() []string { return []string{"darwin", "linux", "windows"} }

const (
	chezmoiKey     = "chezmoi"
	defaultProfile = "default"
)

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Label is "<os>/<profile>".
func (c Config) Label() string { return c.OS + "/" + c.Profile }

// Short returns the column label of each profile: the shortest prefix of its name
// that is not a prefix of any other profile's name (the whole name when there is
// none), with the first letter upper-cased.
func Short(profiles []string) map[string]string {
	out := make(map[string]string, len(profiles))
	for _, p := range profiles {
		label := p
		for n := 1; n < len(p); n++ {
			if !slices.ContainsFunc(profiles, func(o string) bool { return o != p && strings.HasPrefix(o, p[:n]) }) {
				label = p[:n]
				break
			}
		}
		out[p] = strings.ToUpper(label[:1]) + label[1:]
	}
	return out
}

// Parse reads "<os>", "<os>,<profile>" or "<os>/<profile>". The profile may be empty;
// whether it exists is for the caller, who knows the declared profiles.
func Parse(s string) (Config, error) {
	osName, profile, _ := strings.Cut(strings.NewReplacer(",", "/").Replace(s), "/")
	if !slices.Contains(OSes(), osName) {
		return Config{}, fmt.Errorf("unknown configuration %q (os %s)", s, strings.Join(OSes(), "|"))
	}
	if profile != "" && !profileName.MatchString(profile) {
		return Config{}, fmt.Errorf("invalid profile %q in %q (lowercase letters, digits, _ and -)", profile, s)
	}
	return Config{OS: osName, Profile: profile}, nil
}

// Entry is a configuration with what it renders with: ConfigFile, its rendered
// chezmoi config (passed as --config), and Overrides, the OS and a variant's data over
// it. Variant names the extra configuration of a Spec variant; it is empty for the
// base configurations.
type Entry struct {
	Config

	Variant    string
	ConfigFile string
	Overrides  chez.Overrides
}

// Label is "<os>/<profile>", followed by " [<variant>]" for a variant.
func (e Entry) Label() string {
	if e.Variant == "" {
		return e.Config.Label()
	}
	return e.Config.Label() + " [" + e.Variant + "]"
}

// Variant is an extra configuration rendered only by `lint templates`.
type Variant struct {
	Name     string
	Profiles []string // empty means every profile
	Set      map[string]any
}

// Spec is the matrix declaration of the dotfiles: the answers `chezmoi init` is given
// and the extra variants to lint.
type Spec struct {
	Seed     map[string]any            // answers for every profile
	Profile  map[string]map[string]any // answers per declared profile, over Seed
	Variants []Variant
}

const specKey = "matrix"

// ParseSpec reads the `matrix` table of the chezmoi data; absent means defaults only.
func ParseSpec(data map[string]any) (Spec, []finding.Finding) {
	raw, ok := data[specKey]
	if !ok {
		return Spec{}, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return Spec{}, []finding.Finding{finding.New(specKey, "must be a table")}
	}
	var (
		spec Spec
		fs   []finding.Finding
	)
	for _, k := range sortedKeys(m) {
		switch k {
		case "seed":
			spec.Seed, fs = parseAnswers(m[k], "matrix.seed", fs)
		case "profile":
			spec.Profile, fs = parseProfiles(m[k], fs)
		case "variant": // needs the profiles: parsed below
		default:
			fs = append(fs, finding.New(specKey+"."+k, "unknown key (allowed: seed, profile, variant)"))
		}
	}
	if v, ok := m["variant"]; ok {
		spec.Variants, fs = parseVariants(v, spec.Profiles(), fs)
	}
	return spec, fs
}

// Profiles returns the declared profile names, sorted; "default" when none is declared.
func (s Spec) Profiles() []string {
	if len(s.Profile) == 0 {
		return []string{defaultProfile}
	}
	return sortedKeys(s.Profile)
}

// Configs returns every OS and profile, OS-major.
func (s Spec) Configs() []Config {
	out := make([]Config, 0, len(OSes())*len(s.Profiles()))
	for _, o := range OSes() {
		for _, p := range s.Profiles() {
			out = append(out, Config{OS: o, Profile: p})
		}
	}
	return out
}

func parseProfiles(raw any, fs []finding.Finding) (map[string]map[string]any, []finding.Finding) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, append(fs, finding.New("matrix.profile", "must be a table of profiles"))
	}
	out := map[string]map[string]any{}
	for _, name := range sortedKeys(m) {
		where := "matrix.profile." + name
		if !profileName.MatchString(name) {
			fs = append(fs, finding.New(where, "invalid profile name (lowercase letters, digits, _ and -; starting with a letter or digit)"))
			continue
		}
		out[name], fs = parseAnswers(m[name], where, fs)
	}
	return out, fs
}

func parseVariants(raw any, profiles []string, fs []finding.Finding) ([]Variant, []finding.Finding) {
	list, ok := raw.([]any)
	if !ok {
		return nil, append(fs, finding.New("matrix.variant", "must be an array of tables"))
	}
	var out []Variant
	seen := map[string]bool{}
	for i, item := range list {
		where := fmt.Sprintf("matrix.variant[%d]", i)
		m, ok := item.(map[string]any)
		if !ok {
			fs = append(fs, finding.New(where, "must be a table"))
			continue
		}
		var (
			v     Variant
			named bool // name was given but is not a string: already reported
		)
		for _, k := range sortedKeys(m) {
			switch k {
			case "name":
				name, isString := m[k].(string)
				if !isString {
					fs = append(fs, finding.New(where+".name", "name must be a string"))
					named = true
				}
				v.Name = name
			case "profiles":
				v.Profiles, fs = parseVariantProfiles(m[k], where, profiles, fs)
			case "set":
				v.Set, fs = parseAnswers(m[k], where+".set", fs)
			default:
				fs = append(fs, finding.New(where+"."+k, "unknown key (allowed: name, profiles, set)"))
			}
		}
		switch {
		case named:
			continue
		case v.Name == "":
			fs = append(fs, finding.New(where, "missing name"))
			continue
		case seen[v.Name]:
			fs = append(fs, finding.New(where, "duplicate name %q", v.Name))
			continue
		}
		seen[v.Name] = true
		if len(v.Set) == 0 {
			fs = append(fs, finding.New(where, "%q has no set (the data to override)", v.Name))
		}
		out = append(out, v)
	}
	return out, fs
}

func parseVariantProfiles(raw any, where string, known []string, fs []finding.Finding) ([]string, []finding.Finding) {
	list, ok := raw.([]any)
	if !ok {
		return nil, append(fs, finding.New(where+".profiles", "must be a list of profiles"))
	}
	if len(list) == 0 {
		return nil, append(fs, finding.New(where+".profiles", "profiles is empty; omit it for every profile"))
	}
	var out []string
	for _, item := range list {
		name, _ := item.(string)
		if !slices.Contains(known, name) {
			fs = append(fs, finding.New(where+".profiles", "unknown profile %v (profiles: %s)", item, strings.Join(known, ", ")))
			continue
		}
		out = append(out, name)
	}
	return out, fs
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// parseAnswers reads a table of answers: strings, booleans and lists of strings,
// with the chezmoi key reserved.
func parseAnswers(raw any, where string, fs []finding.Finding) (map[string]any, []finding.Finding) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, append(fs, finding.New(where, "must be a table"))
	}
	out := map[string]any{}
	for _, k := range sortedKeys(m) {
		switch {
		case k == chezmoiKey:
			fs = append(fs, finding.New(where+"."+k, "reserved: the matrix sets it per OS"))
		case !bareKey.MatchString(k):
			fs = append(fs, finding.New(where+"."+k, "not a bare TOML key (letters, digits, _ and -)"))
		case !validAnswer(m[k]):
			fs = append(fs, finding.New(where+"."+k, "must be a string, a boolean or a list of strings"))
		default:
			out[k] = m[k]
		}
	}
	return out, fs
}

func validAnswer(v any) bool {
	switch v := v.(type) {
	case string, bool:
		return true
	case []any:
		return !slices.ContainsFunc(v, func(i any) bool { _, ok := i.(string); return !ok })
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// Answers returns the answers `chezmoi init` is given for profile: the seed, then the
// profile's own. The map is the caller's to change.
func (s Spec) Answers(profile string) map[string]any {
	a := map[string]any{}
	maps.Copy(a, s.Seed)
	maps.Copy(a, s.Profile[profile])
	return a
}

// Expand returns entries followed by the variants' configurations: for each variant,
// a copy of every base entry of its profiles with the variant's data set over it.
func (s Spec) Expand(entries []Entry) []Entry {
	out := slices.Clone(entries)
	for _, v := range s.Variants {
		for _, e := range entries {
			if e.Variant != "" || (len(v.Profiles) > 0 && !slices.Contains(v.Profiles, e.Profile)) {
				continue
			}
			e.Variant = v.Name
			e.Overrides = e.Overrides.Clone(chez.Overrides(v.Set))
			out = append(out, e)
		}
	}
	return out
}

// Load reads the matrix declaration from the chezmoi data of cz.
func Load(ctx context.Context, cz *chez.Client) (Spec, error) {
	data, err := cz.Data(ctx, nil)
	if err != nil {
		return Spec{}, err
	}
	return SpecFrom(data)
}

// SpecFrom parses the matrix declaration of data, failing when it is invalid.
func SpecFrom(data map[string]any) (Spec, error) {
	spec, fs := ParseSpec(data)
	if len(fs) > 0 {
		return Spec{}, fmt.Errorf("the matrix declaration is invalid (chezmoi dots lint matrix):\n%s", finding.Lines(fs))
	}
	return spec, nil
}

// TOML renders answers as the [data] table of a chezmoi config file.
func TOML(seed map[string]any) string {
	var b strings.Builder
	b.WriteString("[data]\n")
	for _, k := range sortedKeys(seed) {
		switch v := seed[k].(type) {
		case bool:
			fmt.Fprintf(&b, "%s = %t\n", k, v)
		case string:
			fmt.Fprintf(&b, "%s = %s\n", k, quote(v))
		case []string:
			fmt.Fprintf(&b, "%s = [%s]\n", k, quoteAll(v))
		case []any:
			items := make([]string, 0, len(v))
			for _, i := range v {
				if s, ok := i.(string); ok {
					items = append(items, s)
				}
			}
			fmt.Fprintf(&b, "%s = [%s]\n", k, quoteAll(items))
		}
	}
	return b.String()
}

// quote returns s as a TOML basic string: quote, backslash and control characters are
// escaped the way TOML spells them (not Go's \x, \a or \v).
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func quoteAll(v []string) string {
	q := make([]string, len(v))
	for i, s := range v {
		q[i] = quote(s)
	}
	return strings.Join(q, ", ")
}

// configTemplates are chezmoi's config template names; the rendered config keeps
// the extension of its template.
func configTemplates() []string {
	return []string{".chezmoi.toml.tmpl", ".chezmoi.yaml.tmpl", ".chezmoi.json.tmpl", ".chezmoi.jsonc.tmpl"}
}

// Build renders the config template once per OS and profile, writes each result into
// scratch, and returns every configuration with the path of its config file: running
// chezmoi with that file as --config gives templates the data `chezmoi init` would
// store, and the Overrides then only set the OS. A source with no config template
// gets a config holding the profile's answers as [data]. cz must target the source
// dir src; scratch is a temp dir that must outlive the renders that use the entries.
func Build(ctx context.Context, cz *chez.Client, src, scratch string, spec Spec) ([]Entry, error) {
	tmpl, ext, err := readConfigTemplate(src)
	if err != nil {
		return nil, err
	}
	cz = cz.With("--source", src)
	configs := spec.Configs()
	files := make([]string, len(configs))
	var g errgroup.Group
	g.SetLimit(4)
	for i, c := range configs {
		g.Go(func() error {
			var err error
			files[i], err = writeConfig(ctx, cz, tmpl, ext, scratch, c, spec.Answers(c.Profile))
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(configs))
	for i, c := range configs {
		entries = append(entries, Entry{
			Config:     c,
			ConfigFile: files[i],
			Overrides:  chez.Overrides{chezmoiKey: map[string]any{"os": c.OS}},
		})
	}
	return entries, nil
}

// readConfigTemplate returns the config template of src and its format's extension
// ("toml", "yaml", "json" or "jsonc"); a nil template means the source has none.
// Several templates are an error, as in chezmoi.
func readConfigTemplate(src string) ([]byte, string, error) {
	var found []string
	for _, name := range configTemplates() {
		_, err := os.Stat(filepath.Join(src, name))
		switch {
		case err == nil:
			found = append(found, name)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, "", fmt.Errorf("read config template: %w", err)
		}
	}
	switch len(found) {
	case 0:
		return nil, "toml", nil
	case 1:
		b, err := os.ReadFile(filepath.Join(src, found[0])) //nolint:gosec // src is the user's own dotfiles source dir
		if err != nil {
			return nil, "", fmt.Errorf("read config template: %w", err)
		}
		return b, strings.TrimSuffix(strings.TrimPrefix(found[0], ".chezmoi."), ".tmpl"), nil
	default:
		return nil, "", fmt.Errorf("multiple config file templates: %s", strings.Join(found, ", "))
	}
}

// writeConfig writes the config file of c into scratch and returns its path: the
// config template rendered with answers as the seed config, or the answers as TOML
// when there is no template.
func writeConfig(ctx context.Context, cz *chez.Client, tmpl []byte, ext, scratch string, c Config, answers map[string]any) (string, error) {
	name := c.OS + "-" + c.Profile
	seed := []byte(TOML(answers))
	out := seed
	if tmpl != nil {
		seedPath := filepath.Join(scratch, "seed-"+name+".toml")
		if err := os.WriteFile(seedPath, seed, 0o600); err != nil {
			return "", fmt.Errorf("write seed config: %w", err)
		}
		var err error
		out, err = cz.With("--config", seedPath).Execute(ctx, chez.Overrides{chezmoiKey: map[string]any{"os": c.OS}}, tmpl, true)
		if err != nil {
			return "", fmt.Errorf("render config template for %s: %w", c.Label(), err)
		}
	}
	path := filepath.Join(scratch, "config-"+name+"."+ext)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return "", fmt.Errorf("write rendered config: %w", err)
	}
	return path, nil
}
