// Package vaultcheck is the secrets registry (.chezmoidata/secrets.toml): validation,
// parsing, and a read-only probe of the secret-manager CLIs it declares.
package vaultcheck

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/finding"
)

// Kinds of secret manager.
const (
	KindBws = "bws"
	KindBw  = "bw"
	KindRbw = "rbw"
)

func kinds() []string { return []string{KindBws, KindBw, KindRbw} }

const (
	keyProject     = "project"
	keyProjectFrom = "projectFrom"
)

func entryKeys() []string {
	return []string{"kind", keyProject, keyProjectFrom, "keyring", "command"}
}

// Entry is one declared secret manager.
type Entry struct {
	Name        string
	Kind        string // "bws", "bw", "rbw"
	Project     string // bws: literal project id
	ProjectFrom string // bws: name of the [data] answer holding the project id
	Service     string // bws: keyring service
	User        string // bws: keyring user
	Command     string // optional override of the CLI path
}

// Validate checks the secret table of data.
func Validate(data map[string]any) []finding.Finding {
	raw, ok := data["secret"]
	if !ok {
		return nil
	}
	table, ok := raw.(map[string]any)
	if !ok {
		return []finding.Finding{finding.New("secret", "must be a table of entries")}
	}
	var out []finding.Finding
	for _, n := range sortedKeys(table) {
		e, ok := table[n].(map[string]any)
		if !ok {
			out = append(out, finding.New("secret "+n, "must be a table"))
			continue
		}
		out = append(out, validateEntry(n, e)...)
	}
	return out
}

func validateEntry(n string, e map[string]any) []finding.Finding {
	var out []finding.Finding
	bad := func(format string, args ...any) { out = append(out, finding.New("secret "+n, format, args...)) }
	for _, k := range sortedKeys(e) {
		if !slices.Contains(entryKeys(), k) {
			bad("unknown key %s", k)
		}
	}
	if c, ok := e["command"]; ok && !nonEmptyString(c) {
		bad("command must be a non-empty string")
	}
	kind, hasKind := e["kind"]
	if !hasKind {
		bad("missing kind (one of %s)", strings.Join(kinds(), ", "))
		return out
	}
	if ks, _ := kind.(string); !slices.Contains(kinds(), ks) {
		bad("unknown kind %q (one of %s)", fmt.Sprint(kind), strings.Join(kinds(), ", "))
		return out
	}
	if kind != KindBws {
		for _, k := range []string{keyProject, keyProjectFrom, "keyring"} {
			if _, ok := e[k]; ok {
				bad("%s is only for kind bws", k)
			}
		}
		return out
	}
	_, hasProject := e[keyProject]
	_, hasFrom := e[keyProjectFrom]
	switch {
	case !hasProject && !hasFrom:
		bad("kind bws needs project or projectFrom")
	case hasProject && hasFrom:
		bad("set project or projectFrom, not both")
	}
	for _, k := range []string{keyProject, keyProjectFrom} {
		if v, ok := e[k]; ok && !nonEmptyString(v) {
			bad("%s must be a non-empty string", k)
		}
	}
	return append(out, validateKeyring(n, e)...)
}

func validateKeyring(n string, e map[string]any) []finding.Finding {
	var out []finding.Finding
	bad := func(format string, args ...any) { out = append(out, finding.New("secret "+n, format, args...)) }
	raw, ok := e["keyring"]
	if !ok {
		bad("kind bws needs keyring = { service, user }")
		return out
	}
	k, ok := raw.(map[string]any)
	if !ok {
		bad("keyring must be a table")
		return out
	}
	for _, key := range sortedKeys(k) {
		if key != "service" && key != "user" {
			bad("unknown key keyring.%s", key)
		}
	}
	for _, key := range []string{"service", "user"} {
		if !nonEmptyString(k[key]) {
			bad("keyring needs %s", key)
		}
	}
	return out
}

// Parse reads the entries of data, sorted by name, and the Validate findings. Callers
// do not probe when there are findings.
func Parse(data map[string]any) ([]Entry, []finding.Finding) {
	if fs := Validate(data); len(fs) > 0 {
		return nil, fs
	}
	table, _ := data["secret"].(map[string]any)
	out := make([]Entry, 0, len(table))
	for _, n := range sortedKeys(table) {
		t, _ := table[n].(map[string]any)
		e := Entry{Name: n}
		e.Kind, _ = t["kind"].(string)
		e.Project, _ = t[keyProject].(string)
		e.ProjectFrom, _ = t[keyProjectFrom].(string)
		e.Command, _ = t["command"].(string)
		if k, ok := t["keyring"].(map[string]any); ok {
			e.Service, _ = k["service"].(string)
			e.User, _ = k["user"].(string)
		}
		out = append(out, e)
	}
	return out, nil
}

func nonEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && s != ""
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
