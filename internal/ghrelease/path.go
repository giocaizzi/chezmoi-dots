package ghrelease

import (
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/archive"
)

// maxCandidates caps the files listed per target when nothing matches a name.
const maxCandidates = 5

// pathSeparators are the token separators of a path inside an archive.
const pathSeparators = osSeparator + "/"

// ProposePath reads the binary's path inside the archives from their listings.
// files holds the listing of each target of p; a target with no listing (nil) is
// left out of the check.
//
// Per target the candidates are the executable regular files whose base name is
// one of names (on windows targets, name + ".exe", in any case); a file whose mode
// is unknown counts as executable. ok is true when at least one target has a listing,
// each listed target has exactly one candidate, and the candidates are the same path
// once the release version and the target's value are replaced by {version} and
// {target} where they are whole tokens. The returned path never ends in ".exe".
//
// candidates is for reporting when ok is false: for every listed target the files
// that match a name, or, when none do, up to five of its executable files.
func ProposePath(names []string, p Proposal, files map[string][]archive.File) (found string, ok bool, candidates map[string][]string) {
	candidates = map[string][]string{}
	templated := map[string]bool{}
	exactlyOne := true
	for _, k := range slices.Sorted(maps.Keys(files)) {
		list := files[k]
		if list == nil {
			continue
		}
		windows := strings.HasPrefix(k, "windows/")
		var matches, executable []string
		for _, f := range list {
			if !f.Exec {
				continue
			}
			executable = append(executable, f.Path)
			if matchesName(path.Base(f.Path), names, windows) {
				matches = append(matches, f.Path)
			}
		}
		if len(matches) > 0 {
			candidates[k] = matches
		} else {
			candidates[k] = executable[:min(len(executable), maxCandidates)]
		}
		if len(matches) != 1 {
			exactlyOne = false
			continue
		}
		templated[templatePath(matches[0], p, p.Targets[k], windows)] = true
	}
	if len(candidates) == 0 || !exactlyOne || len(templated) != 1 {
		return "", false, candidates
	}
	for t := range templated {
		return t, true, candidates
	}
	return "", false, candidates
}

func matchesName(base string, names []string, windows bool) bool {
	for _, n := range names {
		if windows {
			if strings.EqualFold(base, n+extExe) {
				return true
			}
		} else if base == n {
			return true
		}
	}
	return false
}

// templatePath drops a windows ".exe" and replaces the version and the target's
// value, where whole tokens, by their placeholders. A target value that ends in an
// archive extension is also tried without it.
func templatePath(file string, p Proposal, target string, windows bool) string {
	if windows && len(file) >= len(extExe) && strings.EqualFold(file[len(file)-len(extExe):], extExe) {
		file = file[:len(file)-len(extExe)]
	}
	file = templVersionSep(file, p.Version, 0, len(file), pathSeparators)
	if got := replaceToken(file, target, "{target}"); got != file {
		return got
	}
	// A target value spans the asset's extension when the targets' extensions differ
	// (linux_amd64.tar.gz, windows_amd64.zip); a directory inside carries none of it.
	for _, ext := range []string{extTarGz, extTgz, extTarXz, extZip} {
		if v, ok := strings.CutSuffix(target, ext); ok {
			return replaceToken(file, v, "{target}")
		}
	}
	return file
}

// replaceToken replaces value in s by repl where value is a whole token: bounded by
// the ends of s or a path separator.
func replaceToken(s, value, repl string) string {
	if value == "" {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		end := i + len(value)
		if strings.HasPrefix(s[i:], value) &&
			(i == 0 || strings.IndexByte(pathSeparators, s[i-1]) >= 0) &&
			(end == len(s) || strings.IndexByte(pathSeparators, s[end]) >= 0) {
			b.WriteString(repl)
			i = end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
