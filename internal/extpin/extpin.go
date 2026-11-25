// Package extpin finds the externals that chezmoi's .chezmoiexternal files pin to
// a commit archive (a GitHub or GitLab /archive/<sha> URL, usually with a
// checksum.sha256) and rewrites their commit and checksum in place. It reads the
// files as text, so templates and comments survive the edit.
package extpin

import (
	"errors"
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

// Scan finds the pins in the text of one externals file; file is recorded in each.
func Scan(file, text string) []Pin {
	_, _ = file, text
	return nil
}

// Find scans every TOML externals file under the source dir src, sorted by file
// and then by line.
func Find(src string) ([]Pin, error) {
	_ = src
	return nil, errors.New("extpin.Find: not implemented")
}

// Set rewrites pin p in text (the content of p.File) to commit sha and checksum
// sum, leaving everything else as it is. sum is ignored when p has no checksum.
func Set(text string, p Pin, sha, sum string) (string, error) {
	_, _, _, _ = text, p, sha, sum
	return "", errors.New("extpin.Set: not implemented")
}
