// Package vaultcheck is the secrets registry (.chezmoidata/secrets.toml): validation,
// parsing, and a read-only probe of the secret-manager CLIs it declares.
package vaultcheck

import (
	"fmt"
	"maps"
	"path/filepath"
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

// declared returns the entries of data.secret: the tables that have a kind key. The
// data key secret may hold a user's unrelated data, so anything else is ignored.
func declared(data map[string]any) map[string]map[string]any {
	table, _ := data["secret"].(map[string]any)
	out := map[string]map[string]any{}
	for n, v := range table {
		if e, ok := v.(map[string]any); ok {
			if _, has := e["kind"]; has {
				out[n] = e
			}
		}
	}
	return out
}

// Ignored returns the sorted names under the data key secret that are not registry
// entries: tables without a kind and values that are no table. A secret that is itself
// no table is named "secret".
func Ignored(data map[string]any) []string {
	raw, ok := data["secret"]
	if !ok {
		return nil
	}
	table, ok := raw.(map[string]any)
	if !ok {
		return []string{"secret"}
	}
	entries := declared(data)
	var out []string
	for _, n := range sortedKeys(table) {
		if _, ok := entries[n]; !ok {
			out = append(out, n)
		}
	}
	return out
}

// Validate checks the entries of data.secret; see declared for what is one.
func Validate(data map[string]any) []finding.Finding {
	entries := declared(data)
	out := make([]finding.Finding, 0, len(entries))
	for _, n := range slices.Sorted(maps.Keys(entries)) {
		out = append(out, validateEntry(n, entries[n])...)
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
	if c, ok := e["command"]; ok {
		if msg := commandProblem(c); msg != "" {
			bad("%s", msg)
		}
	}
	kind := e["kind"] // declared guarantees the key
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
	table := declared(data)
	out := make([]Entry, 0, len(table))
	for _, n := range slices.Sorted(maps.Keys(table)) {
		t := table[n]
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

// commandProblem is what is wrong with a declared command, or empty.
func commandProblem(v any) string {
	s, _ := v.(string)
	switch {
	case s == "":
		return "command must be a non-empty string"
	case !validCommand(s):
		return "command must be an absolute path, ~/..., or a bare name looked up on PATH"
	}
	return ""
}

// validCommand reports whether c cannot resolve against the working directory: a bare
// name (looked up on PATH), ~/..., an absolute path or a Windows volume path. The
// volume form is accepted on every OS so a registry lints the same everywhere.
func validCommand(c string) bool {
	if !strings.ContainsAny(c, `/\`) || strings.HasPrefix(c, "~/") || filepath.IsAbs(c) {
		return true
	}
	return len(c) >= 3 && isLetter(c[0]) && c[1] == ':' && (c[2] == '/' || c[2] == '\\')
}

func isLetter(b byte) bool { return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' }

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
