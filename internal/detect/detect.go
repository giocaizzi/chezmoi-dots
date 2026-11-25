// Package detect asks the package managers on this machine what they know of one
// name, so `pkg add` can propose the registry keys that apply here. It reads only
// local state and the managers' own indexes: no public APIs, no other OS.
package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/inventory"
)

const (
	outputCap      = 1 << 20 // bytes kept of a command's output
	defaultTimeout = 10 * time.Second
	wingetTimeout  = 60 * time.Second // winget export is slow

	keyApt  = "apt"
	osLinux = "linux"
)

// The Confidence values of Found.
const (
	Installed = "installed"
	Available = "available"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9@/._+-]+$`)

// Found is one registry key detected on this machine.
type Found struct {
	Key        string // "brew", "cask", "apt", "winget", "uv" or "npm"
	ID         string // the value for that key: formula, cask token, apt package, winget id, uv or npm spec
	Confidence string // "installed" (on this machine now) or "available" (the manager offers it)
}

// Detector probes the package managers present on this machine.
type Detector struct {
	Runner   chez.Runner
	LookPath func(string) (string, error)
	GOOS     string
	Home     string
	Timeout  time.Duration // per command; 0 means 10s (winget export: 60s)
}

// OSKeys maps each OS to the registry keys only it uses.
func OSKeys() map[string][]string {
	return map[string][]string{
		"darwin":  {"brew", "cask"},
		osLinux:   {keyApt},
		"windows": {"winget"},
	}
}

// Detect returns what the managers on this machine know of name, in key order
// brew, cask, apt, winget, uv, npm, and the managers it asked that found nothing.
// A manager that is not on PATH is not asked; a command that fails found nothing.
func (d Detector) Detect(ctx context.Context, name string) (found []Found, none []string) {
	if !validName.MatchString(name) || strings.HasPrefix(name, "-") {
		return nil, nil
	}
	probe := func(binary, manager string, fn func() []Found) {
		if _, err := d.LookPath(binary); err != nil {
			return
		}
		if f := fn(); len(f) > 0 {
			found = append(found, f...)
		} else {
			none = append(none, manager)
		}
	}
	switch d.GOOS {
	case "darwin":
		probe("brew", "brew", func() []Found { return d.brew(ctx, name) })
	case osLinux:
		probe("apt-cache", keyApt, func() []Found { return d.apt(ctx, name) })
	case "windows":
		probe("winget", "winget", func() []Found { return d.winget(ctx, name) })
	}
	probe("uv", "uv", func() []Found { return d.uv(ctx, name) })
	probe("npm", "npm", func() []Found { return d.npm(ctx, name) })
	return found, none
}

// capBuf keeps the first outputCap bytes written to it and drops the rest.
type capBuf struct{ b bytes.Buffer }

func (c *capBuf) Write(p []byte) (int, error) {
	if room := outputCap - c.b.Len(); room > 0 {
		c.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// exec runs argv (never through a shell) with its stdout captured and capped.
func (d Detector) exec(ctx context.Context, timeout time.Duration, env []string, args ...string) (string, error) {
	if d.Timeout > 0 {
		timeout = d.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out, errb capBuf
	err := d.Runner.Run(ctx, chez.Cmd{Args: args, Env: env, Stdout: &out, Stderr: &errb})
	return out.b.String(), err
}

func confidence(installed bool) string {
	if installed {
		return Installed
	}
	return Available
}

// brew reads `brew info --json=v2`: a formula first, else a cask.
func (d Detector) brew(ctx context.Context, name string) []Found {
	env := []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ENV_HINTS=1"}
	out, err := d.exec(ctx, defaultTimeout, env, "brew", "info", "--json=v2", name)
	if err != nil {
		return nil
	}
	var doc struct {
		Formulae []struct {
			FullName  string `json:"full_name"`
			Installed []any  `json:"installed"`
		} `json:"formulae"`
		Casks []struct {
			Token     string  `json:"token"`
			Installed *string `json:"installed"`
		} `json:"casks"`
	}
	if json.Unmarshal([]byte(out), &doc) != nil {
		return nil
	}
	if len(doc.Formulae) > 0 {
		f := doc.Formulae[0]
		return []Found{{Key: "brew", ID: f.FullName, Confidence: confidence(len(f.Installed) > 0)}}
	}
	if len(doc.Casks) > 0 {
		c := doc.Casks[0]
		return []Found{{Key: "cask", ID: c.Token, Confidence: confidence(c.Installed != nil && *c.Installed != "")}}
	}
	return nil
}

// apt reads `apt-cache policy`: installed, else a candidate.
func (d Detector) apt(ctx context.Context, name string) []Found {
	out, err := d.exec(ctx, defaultTimeout, []string{"LC_ALL=C"}, "apt-cache", "policy", name)
	if err != nil {
		return nil
	}
	v, ok := inventory.AptPolicy(out)[name]
	switch {
	case !ok:
		return nil
	case v.Installed != "":
		return []Found{{Key: keyApt, ID: name, Confidence: Installed}}
	case v.Candidate != "":
		return []Found{{Key: keyApt, ID: name, Confidence: Available}}
	}
	return nil
}

// winget reads the export of what is installed: one id equal to name or ending
// in ".name". Ids come back lower-cased (winget ids are case-insensitive).
func (d Detector) winget(ctx context.Context, name string) []Found {
	dir, err := os.MkdirTemp("", "dots-winget-")
	if err != nil {
		return nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "winget.json")
	// winget exits non-zero when some sources are unreachable yet still writes the
	// export, so a readable export wins over the exit status.
	_, _ = d.exec(ctx, wingetTimeout, nil, "winget", "export", "--output", file,
		"--include-versions", "--accept-source-agreements", "--disable-interactivity")
	b, err := os.ReadFile(file) //nolint:gosec // a file in our own temp dir
	if err != nil {
		return nil
	}
	ids, err := inventory.WingetExport(b)
	if err != nil {
		return nil
	}
	want := strings.ToLower(name)
	var match []string
	for id := range ids {
		last := id[strings.LastIndex(id, ".")+1:]
		if id == want || last == want {
			match = append(match, id)
		}
	}
	if len(match) != 1 {
		return nil
	}
	return []Found{{Key: "winget", ID: match[0], Confidence: Installed}}
}

// uv reads `uv tool list`: only an installed tool is known.
func (d Detector) uv(ctx context.Context, name string) []Found {
	out, err := d.exec(ctx, defaultTimeout, nil, "uv", "tool", "list")
	if err != nil {
		return nil
	}
	if _, ok := inventory.UVList(out)[name]; !ok {
		return nil
	}
	return []Found{{Key: "uv", ID: name, Confidence: Installed}}
}

// npm reads `npm ls -g`: only an installed package is known. npm exits 1 for
// extraneous packages yet prints the JSON, so output wins over the exit status.
func (d Detector) npm(ctx context.Context, name string) []Found {
	args := []string{"npm", "ls", "-g", "--depth=0", "--json"}
	if d.GOOS == osLinux {
		args = append(args, "--prefix", filepath.Join(d.Home, ".local"))
	}
	out, err := d.exec(ctx, defaultTimeout, nil, args...)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil
	}
	m, err := inventory.NpmLs([]byte(out))
	if err != nil {
		return nil
	}
	if _, ok := m[name]; !ok {
		return nil
	}
	return []Found{{Key: "npm", ID: name, Confidence: Installed}}
}
