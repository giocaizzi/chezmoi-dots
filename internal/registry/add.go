package registry

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// NamePattern matches a name usable as a bare TOML key.
var NamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var unsafeName = regexp.MustCompile(`[^a-z0-9_-]+`)

// sanitize lowercases s, replaces characters outside [a-z0-9_-] by "-" and trims "-".
func sanitize(s string) string {
	return strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// InferName derives an entry name from a path: the base name without leading dots
// and, for a file, without its final extension. When that is empty or taken, the
// parent folder is prefixed. It reports false when no free name results.
func InferName(path string, isDir bool, taken func(string) bool) (string, bool) {
	path = filepath.Clean(path)
	stem := strings.TrimLeft(filepath.Base(path), ".")
	if !isDir {
		stem = strings.TrimSuffix(stem, filepath.Ext(stem))
	}
	stem = sanitize(stem)
	if stem != "" && !taken(stem) {
		return stem, true
	}
	parent := sanitize(strings.TrimLeft(filepath.Base(filepath.Dir(path)), "."))
	if parent == "" || stem == "" {
		return "", false
	}
	if name := parent + "-" + stem; !taken(name) {
		return name, true
	}
	return "", false
}

// quoteTOML renders s as a TOML basic string.
func quoteTOML(s string) string {
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

// Append returns the registry file text with a new entry added. where is "source"
// or "local".
func Append(old, name, desc, where, value string) string {
	text := old
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text != "" {
		text += "\n"
	}
	return text + "[edit." + name + "]\ndesc = " + quoteTOML(desc) + "\n" + where + " = " + quoteTOML(value) + "\n"
}
