// Package extpin finds the externals that chezmoi's .chezmoiexternal files pin to
// a commit archive (a GitHub or GitLab /archive/<sha> URL, usually with a
// checksum.sha256) and rewrites their commit and checksum in place. It reads the
// files as text, so templates and comments survive the edit.
package extpin

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Pin is one external whose archive URL names a commit.
type Pin struct {
	File  string // the externals file it was found in
	Entry string // the external's TOML table key as written, e.g. `".vim/pack/plugins/start/example"`
	Host  string // "github.com", or the GitLab host
	Repo  string // repository path on Host: "octocat/example", "group/sub/example"
	URL   string // the archive URL as written
	SHA   string // the pinned commit: 40 lowercase hex
	Sum   string // the entry's sha256 checksum; "" when it has none

	line    int // 0-based line of URL in File
	sumLine int // 0-based line of Sum in File; -1 when Sum is ""
}

// Name is how pkg status and pkg bump call the pin: its repository path.
func (p Pin) Name() string { return p.Repo }

// Remote is the repository's clone URL, for git ls-remote.
func (p Pin) Remote() string { return "https://" + p.Host + "/" + p.Repo + ".git" }

// URLAt is the archive URL with the commit replaced by sha.
func (p Pin) URLAt(sha string) string { return strings.ReplaceAll(p.URL, p.SHA, sha) }

var (
	header   = regexp.MustCompile(`^\s*\[(.+)\]\s*$`)
	githubRe = regexp.MustCompile(`https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/archive/([0-9a-f]{40})\.tar\.gz`)
	gitlabRe = regexp.MustCompile(`https://([A-Za-z0-9.-]+)/([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+)/-/archive/([0-9a-f]{40})/[A-Za-z0-9_.-]*\.tar\.gz`)
	sumRe    = regexp.MustCompile(`\bsha256\s*=\s*"([0-9a-fA-F]{64})"`)
	shaOK    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sumOK    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// entry collects what one TOML table holds while Scan walks the lines.
type entry struct {
	key   string
	urls  []Pin
	sums  []string
	lines []int // lines of sums
}

func (e *entry) pin() (Pin, bool) {
	if e == nil || len(e.urls) != 1 || len(e.sums) > 1 {
		return Pin{}, false
	}
	p := e.urls[0]
	p.Entry = e.key
	p.sumLine = -1
	if len(e.sums) == 1 {
		p.Sum, p.sumLine = e.sums[0], e.lines[0]
	}
	return p, true
}

// Scan finds the pins in the text of one externals file; file is recorded in each.
func Scan(file, text string) []Pin {
	var pins []Pin
	var cur *entry
	flush := func() {
		if p, ok := cur.pin(); ok {
			pins = append(pins, p)
		}
	}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if m := header.FindStringSubmatch(line); m != nil && !strings.HasPrefix(strings.TrimSpace(line), "[[") {
			if cur == nil || !strings.HasPrefix(m[1], cur.key+".") {
				flush()
				cur = &entry{key: m[1]}
			}
			continue
		}
		if cur == nil {
			continue
		}
		if m := githubRe.FindStringSubmatch(line); m != nil {
			cur.urls = append(cur.urls, Pin{File: file, Host: "github.com", Repo: m[1], URL: m[0], SHA: m[2], line: i})
		} else if m := gitlabRe.FindStringSubmatch(line); m != nil {
			cur.urls = append(cur.urls, Pin{File: file, Host: m[1], Repo: m[2], URL: m[0], SHA: m[3], line: i})
		}
		if m := sumRe.FindStringSubmatch(line); m != nil {
			cur.sums = append(cur.sums, m[1])
			cur.lines = append(cur.lines, i)
		}
	}
	flush()
	return pins
}

// Find scans every TOML externals file under the source dir src, sorted by file
// and then by line.
func Find(src string) ([]Pin, error) {
	var pins []Pin
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !isExternals(path) {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // path comes from walking the caller's source dir
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		pins = append(pins, Scan(path, string(b))...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("find externals in %s: %w", src, err)
	}
	slices.SortStableFunc(pins, func(a, b Pin) int {
		return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.line, b.line))
	})
	return pins, nil
}

func isExternals(path string) bool {
	base := filepath.Base(path)
	if base == ".chezmoiexternal.toml" || base == ".chezmoiexternal.toml.tmpl" {
		return true
	}
	return filepath.Base(filepath.Dir(path)) == ".chezmoiexternals" &&
		(strings.HasSuffix(base, ".toml") || strings.HasSuffix(base, ".toml.tmpl"))
}

// Set rewrites pin p in text (the content of p.File) to commit sha and checksum
// sum, leaving everything else as it is. sum is ignored when p has no checksum.
func Set(text string, p Pin, sha, sum string) (string, error) {
	if !shaOK.MatchString(sha) {
		return "", fmt.Errorf("pin %s in %s: invalid commit %q", p.Name(), p.File, sha)
	}
	if sum != "" && !sumOK.MatchString(sum) {
		return "", fmt.Errorf("pin %s in %s: invalid sha256 %q", p.Name(), p.File, sum)
	}
	pins := Scan(p.File, text)
	i := slices.IndexFunc(pins, func(c Pin) bool { return c.Entry == p.Entry && c.SHA == p.SHA })
	if i < 0 {
		return "", fmt.Errorf("pin %s in %s: not found", p.Name(), p.File)
	}
	cur := pins[i]
	lines := strings.Split(text, "\n")
	lines[cur.line] = strings.ReplaceAll(lines[cur.line], cur.SHA, sha)
	if cur.sumLine >= 0 && sum != "" {
		lines[cur.sumLine] = strings.Replace(lines[cur.sumLine], cur.Sum, sum, 1)
	}
	return strings.Join(lines, "\n"), nil
}
