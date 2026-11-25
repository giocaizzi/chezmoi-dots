package pkgreg

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ValidRole reports whether r is a role a package or release can be limited to.
func ValidRole(r string) bool { return slices.Contains(roles(), r) }

// ValidPlatform reports whether p is an <os>/<arch> release target.
func ValidPlatform(p string) bool { return platform.MatchString(p) }

// Kind reports whether name is a package or a release in data, and which.
func Kind(data map[string]any, name string) (string, bool) {
	for _, kind := range []string{"package", "release"} {
		table, _ := data[kind].(map[string]any)
		if _, ok := table[name]; ok {
			return kind, true
		}
	}
	return "", false
}

// PackageEntry is a new [package.<name>] table; empty fields are left out.
type PackageEntry struct {
	Name                             string
	Brew, Cask, Apt, Winget, UV, NPM string
	Roles                            []string
	Requires                         string
}

// Text renders the entry as TOML with LF line endings.
func (e PackageEntry) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[package.%s]\n", e.Name)
	for _, kv := range [][2]string{
		{"brew", e.Brew}, {"cask", e.Cask}, {"apt", e.Apt}, {"winget", e.Winget}, {"uv", e.UV}, {"npm", e.NPM},
	} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "%s = %s\n", kv[0], quote(kv[1]))
		}
	}
	writeCommon(&b, e.Roles, e.Requires)
	return b.String()
}

// ReleaseEntry is a new [release.<name>] table with its target and sha256 tables.
type ReleaseEntry struct {
	Name, Version, URL string
	Targets            map[string]string // "<os>/<arch>" to the release's name for it
	Sums               map[string]string // "<os>/<arch>" to the sha256; nil leaves the table out
	Roles              []string
	Requires           string
}

// Text renders the entry as TOML with LF line endings. Without Sums it ends with a
// comment where the sha256 table goes.
func (e ReleaseEntry) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[release.%s]\nversion = %s\nurl = %s\n", e.Name, quote(e.Version), quote(e.URL))
	writeCommon(&b, e.Roles, e.Requires)
	fmt.Fprintf(&b, "\n[release.%s.target]\n", e.Name)
	writeMap(&b, e.Targets)
	if e.Sums == nil {
		b.WriteString("\n# sha256 computed when written\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\n[release.%s.sha256]\n", e.Name)
	writeMap(&b, e.Sums)
	return b.String()
}

func writeCommon(b *strings.Builder, roles []string, requires string) {
	if len(roles) > 0 {
		quoted := make([]string, len(roles))
		for i, r := range roles {
			quoted[i] = quote(r)
		}
		fmt.Fprintf(b, "roles = [%s]\n", strings.Join(quoted, ", "))
	}
	if requires != "" {
		fmt.Fprintf(b, "requires = %s\n", quote(requires))
	}
}

func writeMap(b *strings.Builder, m map[string]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "%s = %s\n", quote(k), quote(m[k]))
	}
}

// Append adds entry (LF line endings) at the end of the registry text, after one
// blank line unless the text already ends with one, in the text's own line ending.
// The existing text is kept byte for byte.
func Append(text, entry string) string {
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
		entry = strings.ReplaceAll(entry, "\n", eol)
	}
	if text == "" {
		return entry
	}
	if !strings.HasSuffix(text, "\n") {
		text += eol
	}
	if !strings.HasSuffix(text, eol+eol) {
		text += eol
	}
	return text + entry
}

// quote renders s as a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
