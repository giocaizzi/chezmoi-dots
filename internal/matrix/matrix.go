// Package matrix is the set of configurations a dotfiles edit is rendered for: every
// OS and machine role. Each role's data comes from rendering the real
// .chezmoi.toml.tmpl with that role's answers (the optional `matrix` declaration in
// the chezmoi data, plus the light and work flags), so it cannot drift from what
// `chezmoi init` stores.
package matrix

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/finding"
)

// Config is one OS and machine role.
type Config struct {
	OS   string
	Role string
}

// OSes are the supported operating systems.
func OSes() []string { return []string{"darwin", "linux", "windows"} }

// Roles are the machine roles.
func Roles() []string { return []string{rolePersonal, roleWork, roleLight} }

const (
	rolePersonal = "personal"
	roleWork     = "work"
	roleLight    = "light"

	chezmoiKey = "chezmoi"
)

// All returns every configuration, OS-major.
func All() []Config {
	all := make([]Config, 0, len(OSes())*len(Roles()))
	for _, o := range OSes() {
		for _, r := range Roles() {
			all = append(all, Config{OS: o, Role: r})
		}
	}
	return all
}

// Label is "<os>/<role>".
func (c Config) Label() string { return c.OS + "/" + c.Role }

// Order returns configurations in OS-major order:
// darwin (personal, work, light), linux (personal, work, light), windows (personal, work, light).
func Order() []Config {
	return All()
}

// RoleAbbr returns a single-character abbreviation for the role:
// "P" for personal, "W" for work, and "L" for light.
func (c Config) RoleAbbr() string {
	switch c.Role {
	case rolePersonal:
		return "P"
	case roleWork:
		return "W"
	case roleLight:
		return "L"
	default:
		return ""
	}
}

// Parse reads "<os>", "<os>,<role>" or "<os>/<role>"; the role defaults to personal.
func Parse(s string) (Config, error) {
	os, role, _ := strings.Cut(strings.NewReplacer(",", "/").Replace(s), "/")
	if role == "" {
		role = rolePersonal
	}
	if !slices.Contains(OSes(), os) || !slices.Contains(Roles(), role) {
		return Config{}, fmt.Errorf("unknown configuration %q (os %s; role %s)", s, strings.Join(OSes(), "|"), strings.Join(Roles(), "|"))
	}
	return Config{OS: os, Role: role}, nil
}

// Entry is a configuration with the data it renders with. Variant names the extra
// configuration of a Spec variant; it is empty for the base configurations.
type Entry struct {
	Config

	Variant   string
	Overrides chez.Overrides
}

// Label is "<os>/<role>", followed by " [<variant>]" for a variant.
func (e Entry) Label() string {
	if e.Variant == "" {
		return e.Config.Label()
	}
	return e.Config.Label() + " [" + e.Variant + "]"
}

// Variant is an extra configuration rendered only by `lint templates`.
type Variant struct {
	Name  string
	Roles []string // empty means every role
	Set   map[string]any
}

// Spec is the matrix declaration of the dotfiles: the answers `chezmoi init` is given
// and the extra variants to lint.
type Spec struct {
	Seed     map[string]any            // answers for every role
	Role     map[string]map[string]any // answers per role, over Seed
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
		case "role":
			spec.Role, fs = parseRoles(m[k], fs)
		case "variant":
			spec.Variants, fs = parseVariants(m[k], fs)
		default:
			fs = append(fs, finding.New(specKey+"."+k, "unknown key (allowed: seed, role, variant)"))
		}
	}
	return spec, fs
}

func parseRoles(raw any, fs []finding.Finding) (map[string]map[string]any, []finding.Finding) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, append(fs, finding.New("matrix.role", "must be a table of roles"))
	}
	out := map[string]map[string]any{}
	for _, role := range sortedKeys(m) {
		where := "matrix.role." + role
		if !slices.Contains(Roles(), role) {
			fs = append(fs, finding.New(where, "unknown role (roles: %s)", strings.Join(Roles(), ", ")))
			continue
		}
		out[role], fs = parseAnswers(m[role], where, fs)
	}
	return out, fs
}

func parseVariants(raw any, fs []finding.Finding) ([]Variant, []finding.Finding) {
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
			case "roles":
				v.Roles, fs = parseVariantRoles(m[k], where, fs)
			case "set":
				v.Set, fs = parseAnswers(m[k], where+".set", fs)
			default:
				fs = append(fs, finding.New(where+"."+k, "unknown key (allowed: name, roles, set)"))
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

func parseVariantRoles(raw any, where string, fs []finding.Finding) ([]string, []finding.Finding) {
	list, ok := raw.([]any)
	if !ok {
		return nil, append(fs, finding.New(where+".roles", "must be a list of roles"))
	}
	if len(list) == 0 {
		return nil, append(fs, finding.New(where+".roles", "roles is empty; omit it for every role"))
	}
	var roles []string
	for _, item := range list {
		role, _ := item.(string)
		if !slices.Contains(Roles(), role) {
			fs = append(fs, finding.New(where+".roles", "unknown role %v (roles: %s)", item, strings.Join(Roles(), ", ")))
			continue
		}
		roles = append(roles, role)
	}
	return roles, fs
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// parseAnswers reads a table of answers: strings, booleans and lists of strings,
// with the per-role flags reserved.
func parseAnswers(raw any, where string, fs []finding.Finding) (map[string]any, []finding.Finding) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, append(fs, finding.New(where, "must be a table"))
	}
	out := map[string]any{}
	for _, k := range sortedKeys(m) {
		switch {
		case k == roleLight || k == roleWork:
			fs = append(fs, finding.New(where+"."+k, "reserved: the matrix sets it per role"))
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

// Answers returns the answers `chezmoi init` is given for role: the seed, then the
// role's own, then the light and work flags. The map is the caller's to change.
func (s Spec) Answers(role string) map[string]any {
	a := map[string]any{}
	maps.Copy(a, s.Seed)
	maps.Copy(a, s.Role[role])
	a[roleLight] = role == roleLight
	a[roleWork] = role == roleWork
	return a
}

// Expand returns entries followed by the variants' configurations: for each variant,
// a copy of every base entry of its roles with the variant's data set over it.
func (s Spec) Expand(entries []Entry) []Entry {
	out := slices.Clone(entries)
	for _, v := range s.Variants {
		for _, e := range entries {
			if e.Variant != "" || (len(v.Roles) > 0 && !slices.Contains(v.Roles, e.Role)) {
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
			fmt.Fprintf(&b, "%s = %s\n", k, strconv.Quote(v))
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

func quoteAll(v []string) string {
	q := make([]string, len(v))
	for i, s := range v {
		q[i] = strconv.Quote(s)
	}
	return strings.Join(q, ", ")
}

// Build renders the config template once per role and returns the base
// configuration of every OS and role with its overrides. cz must target the source
// dir src; scratch is a temp dir. Every role carries the same keys: one the role's
// render leaves out is off (the zero value), never inherited from this machine.
func Build(ctx context.Context, cz *chez.Client, src, scratch string, spec Spec) ([]Entry, error) {
	tmpl, err := os.ReadFile(filepath.Join(src, ".chezmoi.toml.tmpl")) //nolint:gosec // src is the user's own dotfiles source dir
	if err != nil {
		return nil, fmt.Errorf("read config template: %w", err)
	}
	machine, err := cz.ConfigData(ctx)
	if err != nil {
		return nil, err
	}
	data := map[string]map[string]any{}
	for _, role := range Roles() {
		seedPath := filepath.Join(scratch, "seed-"+role+".toml")
		if err := os.WriteFile(seedPath, []byte(TOML(spec.Answers(role))), 0o600); err != nil {
			return nil, fmt.Errorf("write seed config: %w", err)
		}
		rendered, err := cz.With("--config", seedPath).Execute(ctx, nil, tmpl, true)
		if err != nil {
			return nil, fmt.Errorf("render config template for %s: %w", role, err)
		}
		renderedPath := filepath.Join(scratch, "config-"+role+".toml")
		if err = os.WriteFile(renderedPath, rendered, 0o600); err != nil { //nolint:gosec // renderedPath is inside the scratch dir this process created
			return nil, fmt.Errorf("write rendered config: %w", err)
		}
		got, err := cz.With("--config", renderedPath).ConfigData(ctx)
		if err != nil {
			return nil, err
		}
		data[role] = got
	}
	uniform(data, machine)
	var entries []Entry
	for _, c := range All() {
		o := chez.Overrides(data[c.Role]).Clone(chez.Overrides{"chezmoi": map[string]any{"os": c.OS}})
		entries = append(entries, Entry{Config: c, Overrides: o})
	}
	return entries, nil
}

// uniform gives every role the union of the roles' keys and of this machine's config
// data (minus chezmoi's own): a role missing a key gets the zero value of the type
// another role, or the machine, has for it, so nothing from the machine's config
// reaches a configuration the roles do not render.
func uniform(data map[string]map[string]any, machine map[string]any) {
	zeros := map[string]any{}
	for _, role := range Roles() {
		for k, v := range data[role] {
			if _, ok := zeros[k]; !ok {
				zeros[k] = zeroOf(v)
			}
		}
	}
	for k, v := range machine {
		if _, ok := zeros[k]; !ok && k != chezmoiKey {
			zeros[k] = zeroOf(v)
		}
	}
	for _, d := range data {
		for k, z := range zeros {
			if _, ok := d[k]; !ok {
				d[k] = z
			}
		}
	}
}

func zeroOf(v any) any {
	switch v.(type) {
	case bool:
		return false
	case []any:
		return []any{}
	case float64:
		return float64(0)
	case map[string]any:
		return map[string]any{}
	default:
		return ""
	}
}
