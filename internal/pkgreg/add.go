package pkgreg

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// ValidRole reports whether r is a role a package or release can be limited to: a
// non-empty name, one of declared when any profile is declared.
func ValidRole(declared []string, r string) bool {
	return r != "" && (declared == nil || slices.Contains(declared, r))
}

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

const (
	keyRoles    = "roles"
	keyRequires = "requires"
)

// Line is one key of a table as TOML text, without its line ending.
type Line struct{ Key, Text string }

// Lines renders the entry's keys in registry order, without the table header.
func (e PackageEntry) Lines() []Line {
	var out []Line
	for _, kv := range [][2]string{
		{"brew", e.Brew}, {"cask", e.Cask}, {"apt", e.Apt}, {"winget", e.Winget}, {"uv", e.UV}, {"npm", e.NPM},
	} {
		if kv[1] != "" {
			out = append(out, Line{kv[0], kv[0] + " = " + quote(kv[1])})
		}
	}
	return append(out, commonLines(e.Roles, e.Requires)...)
}

// Text renders the entry as TOML with LF line endings.
func (e PackageEntry) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[package.%s]\n", e.Name)
	for _, l := range e.Lines() {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}

// ReleaseEntry is a new [release.<name>] table with its target and sha256 tables.
type ReleaseEntry struct {
	Name, Version, URL string
	Type, Path         string            // optional: "file" or "archive-file", and the binary inside the archive (may hold {version} and {target})
	Exe                bool              // a windows target is mapped
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
	if e.Type != "" {
		fmt.Fprintf(&b, "type = %s\n", quote(e.Type))
	}
	if e.Path != "" {
		fmt.Fprintf(&b, "path = %s\n", quote(e.Path))
	}
	if e.Exe {
		b.WriteString("exe = true\n")
	}
	for _, l := range commonLines(e.Roles, e.Requires) {
		b.WriteString(l.Text + "\n")
	}
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

func commonLines(roles []string, requires string) []Line {
	var out []Line
	if len(roles) > 0 {
		quoted := make([]string, len(roles))
		for i, r := range roles {
			quoted[i] = quote(r)
		}
		out = append(out, Line{keyRoles, keyRoles + " = [" + strings.Join(quoted, ", ") + "]"})
	}
	if requires != "" {
		out = append(out, Line{keyRequires, keyRequires + " = " + quote(requires)})
	}
	return out
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

// InsertKeys adds the key lines (LF, no line ending) to the [package.<name>] table of
// text, after its last key line and before the blank lines and comments that precede
// the next table header or the end of the file. The rest of the text is kept byte for
// byte; added lines use the text's line ending. ok is false when text has no such
// table header (an inline table or a table in another file cannot be extended here).
func InsertKeys(text, name string, lines []string) (string, bool) {
	n := regexp.QuoteMeta(name)
	header := regexp.MustCompile(`^[ \t]*\[[ \t]*package[ \t]*\.[ \t]*(?:` + n + `|"` + n + `")[ \t]*\][ \t]*(?:#.*)?$`)
	pieces := strings.SplitAfter(text, "\n")
	at := slices.IndexFunc(pieces, func(p string) bool { return header.MatchString(strings.TrimRight(p, "\r\n")) })
	if at < 0 {
		return text, false
	}
	last := at
	for i := at + 1; i < len(pieces); i++ {
		t := strings.TrimSpace(pieces[i])
		if strings.HasPrefix(t, "[") {
			break
		}
		if t != "" && !strings.HasPrefix(t, "#") {
			last = i
		}
	}
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	var b strings.Builder
	b.WriteString(strings.Join(pieces[:last+1], ""))
	if !strings.HasSuffix(pieces[last], "\n") {
		b.WriteString(eol)
	}
	for _, l := range lines {
		b.WriteString(l + eol)
	}
	b.WriteString(strings.Join(pieces[last+1:], ""))
	return b.String(), true
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
