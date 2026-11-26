// Package ghrelease reads the latest release of a GitHub repository and
// derives a release entry (URL template, type, targets) from its assets.
//
// # Asset mapping heuristic
//
// [Map] works on asset names only:
//
//   - Assets whose lowercased name ends in .sha256, .sha512, .sig, .asc, .pem,
//     .sbom, .json, .txt, .deb, .rpm, .apk, .msi, .pkg, .dmg or .appimage, or
//     contains "checksums" or "sbom", are left out (Unmapped).
//   - The lowercased name is split on '-', '_' and '.' (after reading
//     "x86_64" from the whole name). OS tokens: darwin, macos, apple, osx map
//     to darwin; linux to linux; windows, win, win64 to windows. Arch tokens:
//     amd64, x86_64, x64 map to amd64; arm64, aarch64 to arm64. An asset with
//     no OS, no arch or another arch (386, armv7, i686, ...) is left out.
//   - A darwin asset marked "universal" or "all" with no arch maps to both
//     darwin/amd64 and darwin/arm64, only if no arch-specific darwin asset
//     exists.
//   - An asset is an archive when it ends in .tar.gz, .tgz, .tar.xz or .zip,
//     and a bare binary when it has no extension or ends in .exe. Per os/arch
//     archives win over bare binaries; .tar.gz wins over .zip on darwin and
//     linux, .zip wins over .tar.gz on windows. Two candidates of the same kind
//     and extension are reported as ambiguous and both are left out.
//   - The chosen assets must be all archives (type "archive-file") or all bare
//     (type "file"); a mix is an error.
//   - The release version (the tag without a leading "v") is replaced by
//     {version} in the download URL and in every name. The longest common
//     prefix and suffix of the names, cut at name separators, surround
//     {target}; the differing middle of each name is that target's value. With
//     a single asset, {target} is the span from the first OS or arch token to
//     the last. Expanding the template must reproduce every chosen URL.
package ghrelease

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "https://api.github.com"
	defaultMaxBytes = 4 << 20
	defaultTimeout  = 15 * time.Second
	x8664           = "x86_64"
)

var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Client reads the latest release of a GitHub repository.
type Client struct {
	HTTP     *http.Client
	Getenv   func(string) string // GH_TOKEN, then GITHUB_TOKEN, sent as a bearer token when set; nil reads nothing
	BaseURL  string              // "" means https://api.github.com
	MaxBytes int64               // response cap; 0 means 4 MiB
	Timeout  time.Duration       // per request; 0 means 15s
}

// Asset is one downloadable file of a release.
type Asset struct{ Name, URL string } // URL: browser_download_url

// Release is a GitHub release.
type Release struct {
	Tag    string
	Assets []Asset
}

// Latest fetches the latest release of repo ("owner/name"). The request is
// bounded by the client Timeout and the response by MaxBytes.
func (c Client) Latest(ctx context.Context, repo string) (Release, error) {
	if !repoRE.MatchString(repo) || slices.ContainsFunc(strings.Split(repo, "/"), func(s string) bool { return s == "." || s == ".." }) {
		return Release{}, fmt.Errorf("invalid repository %q: want owner/name", repo)
	}
	timeout := cmp.Or(c.Timeout, defaultTimeout)
	maxBytes := cmp.Or(c.MaxBytes, defaultMaxBytes)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := strings.TrimSuffix(cmp.Or(c.BaseURL, defaultBaseURL), "/") + "/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return Release{}, fmt.Errorf("build request for %s: %w", repo, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Getenv != nil {
		if tok := cmp.Or(c.Getenv("GH_TOKEN"), c.Getenv("GITHUB_TOKEN")); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("fetch latest release of %s: %w", repo, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("fetch latest release of %s: %s", repo, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return Release{}, fmt.Errorf("read latest release of %s: %w", repo, err)
	}
	if int64(len(body)) > maxBytes {
		return Release{}, fmt.Errorf("latest release of %s: response over %d bytes", repo, maxBytes)
	}
	var raw struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, fmt.Errorf("decode latest release of %s: %w", repo, err)
	}
	rel := Release{Tag: raw.Tag}
	for _, a := range raw.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL})
	}
	return rel, nil
}

// Proposal is a release entry derived from a release's assets.
type Proposal struct {
	Version   string            // the tag without a leading "v"
	URL       string            // template with {version} and {target}
	Type      string            // "file" (bare binaries) or "archive-file" (archives)
	Path      string            // for archive-file: the binary inside the archive, the repo name until ProposePath reads the archives (extExe is not added); "" for file
	Exe       bool              // a windows target was mapped
	Targets   map[string]string // "os/arch" -> the {target} value
	Unmapped  []string          // asset names left out (checksums, signatures, packages, unknown os/arch), sorted
	Ambiguous []string          // human-readable reasons, e.g. "linux/amd64: example_linux_amd64 or example_linux_amd64.exe"
}

const (
	extTarGz    = ".tar.gz"
	extExe      = ".exe"
	extTarXz    = ".tar.xz"
	extTgz      = ".tgz"
	extZip      = ".zip"
	osDarwin    = "darwin"
	archAMD64   = "amd64"
	archARM64   = "arm64"
	osSeparator = "-_."
)

func osOf(tok string) string {
	switch tok {
	case osDarwin, "macos", "apple", "osx":
		return osDarwin
	case "linux":
		return "linux"
	case "windows", "win", "win64":
		return "windows"
	}
	return ""
}

func archOf(tok string) string {
	switch tok {
	case archAMD64, "x64":
		return archAMD64
	case archARM64, "aarch64":
		return archARM64
	}
	return ""
}

func isOtherArch(tok string) bool {
	switch tok {
	case "386", "i386", "i686", "x86", "arm", "armv5", "armv6", "armv7", "armv7l", "ppc64", "ppc64le", "s390x", "riscv64", "mips", "mips64", "mipsle", "loong64":
		return true
	}
	return false
}

// cand is an asset with the os/arch read from its name.
type cand struct {
	name, url  string
	os, arch   string // arch is "" for a universal darwin asset
	universal  bool
	ext        string // archive extension, extExe or ""
	archive    bool
	start, end int // span of the OS and arch tokens in name
}

func ignored(name string) bool {
	l := strings.ToLower(name)
	for _, s := range []string{".sha256", ".sha512", ".sig", ".asc", ".pem", ".sbom", ".json", ".txt", ".deb", ".rpm", ".apk", ".msi", ".pkg", ".dmg", ".appimage"} {
		if strings.HasSuffix(l, s) {
			return true
		}
	}
	return strings.Contains(l, "checksums") || strings.Contains(l, "sbom")
}

// parse reads os and arch from an asset name; ok is false when it maps to no target.
func parse(a Asset) (cand, bool) { //nolint:gocognit // one tokeniser loop
	lower := strings.ToLower(a.Name)
	c := cand{name: a.Name, url: a.URL, start: len(lower)}
	archs := map[string]bool{}
	oses := map[string]bool{}
	other, universal := false, false
	xEnd := -1
	grow := func(s, e int) {
		c.start = min(c.start, s)
		c.end = max(c.end, e)
	}
	if i := strings.Index(lower, x8664); i >= 0 {
		archs[archAMD64] = true
		grow(i, i+len(x8664))
		xEnd = i + len(x8664)
		lower = lower[:i] + strings.Repeat("-", len(x8664)) + lower[xEnd:]
	}
	trimmed := lower
	for _, e := range []string{extTarGz, extTgz, extTarXz, extZip} {
		if strings.HasSuffix(lower, e) {
			c.archive, c.ext = true, e
			trimmed = strings.TrimSuffix(lower, e)
			break
		}
	}
	if !c.archive && strings.HasSuffix(lower, extExe) {
		c.ext = extExe
		trimmed = strings.TrimSuffix(lower, extExe)
	}
	lastKnown := xEnd == len(trimmed)
	for s := 0; s < len(lower); {
		e := s
		for e < len(lower) && !strings.ContainsRune(osSeparator, rune(lower[e])) {
			e++
		}
		if e > s {
			tok := lower[s:e]
			known := true
			switch {
			case osOf(tok) != "":
				oses[osOf(tok)] = true
				grow(s, e)
			case archOf(tok) != "":
				archs[archOf(tok)] = true
				grow(s, e)
			case tok == "universal" || tok == "universal2" || tok == "all":
				universal = true
			case isOtherArch(tok):
				other = true
			default:
				known = false
			}
			if e <= len(trimmed) {
				lastKnown = known
			}
		}
		s = e + 1
	}
	if !c.archive && !lastKnown {
		return cand{}, false // not a bare binary: unknown extension
	}
	if other || len(oses) != 1 || len(archs) > 1 {
		return cand{}, false
	}
	for o := range oses {
		c.os = o
	}
	for ar := range archs {
		c.arch = ar
	}
	switch {
	case c.arch != "":
	case universal && c.os == osDarwin:
		c.universal = true
	default:
		return cand{}, false
	}
	return c, true
}

// pick chooses one asset for a target; on a tie it returns the tied ones.
func pick(key string, group []cand) (cand, []cand) {
	pool := slices.DeleteFunc(slices.Clone(group), func(c cand) bool { return !c.archive })
	if len(pool) == 0 {
		pool = group
	}
	order := []string{extTarGz, extTgz, extTarXz, extZip, "", extExe}
	if strings.HasPrefix(key, "windows/") {
		order = []string{extZip, extTarGz, extTgz, extTarXz, extExe, ""}
	}
	rank := func(c cand) int { return slices.Index(order, c.ext) }
	best := slices.MinFunc(pool, func(a, b cand) int { return cmp.Compare(rank(a), rank(b)) })
	tied := slices.DeleteFunc(slices.Clone(pool), func(c cand) bool { return rank(c) != rank(best) })
	if len(tied) > 1 {
		return cand{}, tied
	}
	return best, nil
}

// Map derives a release entry for repo ("owner/name") from the assets of rel.
func Map(repo string, rel Release) (Proposal, error) {
	p := Proposal{Version: strings.TrimPrefix(rel.Tag, "v"), Targets: map[string]string{}}
	unmapped := map[string]bool{}
	groups := groupCandidates(rel.Assets, unmapped)

	chosen := map[string]cand{}
	for _, k := range slices.Sorted(maps.Keys(groups)) {
		c, tied := pick(k, groups[k])
		if tied == nil {
			chosen[k] = c
			continue
		}
		names := make([]string, len(tied))
		for i, t := range tied {
			names[i] = t.name
			unmapped[t.name] = true
		}
		slices.Sort(names)
		p.Ambiguous = append(p.Ambiguous, k+": "+strings.Join(names, " or "))
	}
	for _, c := range chosen {
		delete(unmapped, c.name) // chosen for another target (universal)
	}
	p.Unmapped = slices.Sorted(maps.Keys(unmapped))
	if len(chosen) == 0 {
		return p, errors.New("no asset maps to an os/arch target")
	}

	nArchive := 0
	for k, c := range chosen {
		if c.archive {
			nArchive++
		}
		if strings.HasPrefix(k, "windows/") {
			p.Exe = true
		}
	}
	switch nArchive {
	case len(chosen):
		p.Type = "archive-file"
		_, p.Path, _ = strings.Cut(repo, "/")
	case 0:
		p.Type = "file"
	default:
		return p, errors.New("mixed archive and binary assets")
	}
	return p, p.fillTemplate(rel.Tag, chosen)
}

// groupCandidates parses the assets and groups them by os/arch; assets that
// map nowhere are recorded in unmapped.
func groupCandidates(assets []Asset, unmapped map[string]bool) map[string][]cand {
	var cands []cand
	for _, a := range assets {
		c, ok := parse(a)
		if ignored(a.Name) || !ok {
			unmapped[a.Name] = true
			continue
		}
		cands = append(cands, c)
	}
	specificDarwin := slices.ContainsFunc(cands, func(c cand) bool { return c.os == osDarwin && !c.universal })
	groups := map[string][]cand{}
	for _, c := range cands {
		switch {
		case !c.universal:
			groups[c.os+"/"+c.arch] = append(groups[c.os+"/"+c.arch], c)
		case specificDarwin:
			unmapped[c.name] = true
		default:
			groups[osDarwin+"/"+archAMD64] = append(groups[osDarwin+"/"+archAMD64], c)
			groups[osDarwin+"/"+archARM64] = append(groups[osDarwin+"/"+archARM64], c)
		}
	}
	return groups
}

// templVersion returns name[lo:hi] with version replaced by "{version}" where
// it is a whole token of name: bounded by the ends of name or a separator.
func templVersion(name, version string, lo, hi int) string {
	return templVersionSep(name, version, lo, hi, osSeparator)
}

// templVersionSep is templVersion with seps as the token separators.
func templVersionSep(name, version string, lo, hi int, seps string) string {
	if version == "" {
		return name[lo:hi]
	}
	var b strings.Builder
	for i := lo; i < hi; {
		end := i + len(version)
		if strings.HasPrefix(name[i:hi], version) &&
			tokenStart(name, i, seps) &&
			(end == len(name) || strings.IndexByte(seps, name[end]) >= 0) {
			b.WriteString("{version}")
			i = end
			continue
		}
		b.WriteByte(name[i])
		i++
	}
	return b.String()
}

// tokenStart reports whether a token of name may start at i: at the start of
// name, after a separator, or after a lone "v" that does (a "v1.2.3" tag form).
func tokenStart(name string, i int, seps string) bool {
	if i > 0 && name[i-1] == 'v' {
		i--
	}
	return i == 0 || strings.IndexByte(seps, name[i-1]) >= 0
}

// fillTemplate sets URL and Targets from the chosen assets and checks that
// the template expands back to every chosen URL.
func (p *Proposal) fillTemplate(tag string, chosen map[string]cand) error {
	templ := func(s string) string { return templVersion(s, p.Version, 0, len(s)) }
	tagTempl := tag
	if p.Version != "" {
		tagTempl = strings.ReplaceAll(tag, p.Version, "{version}")
	}
	uniq := map[string]cand{}
	for _, c := range chosen {
		uniq[c.name] = c
	}
	var prefix, suffix string
	values := map[string]string{} // asset name -> {target} value
	if len(uniq) == 1 {
		for _, c := range uniq {
			prefix, suffix = templVersion(c.name, p.Version, 0, c.start), templVersion(c.name, p.Version, c.end, len(c.name))
			values[templ(c.name)] = c.name[c.start:c.end]
		}
	} else {
		names := make([]string, 0, len(uniq))
		for n := range uniq {
			names = append(names, templ(n))
		}
		slices.Sort(names)
		prefix, suffix = commonAffixes(names)
		for _, nm := range names {
			values[nm] = nm[len(prefix) : len(nm)-len(suffix)]
		}
	}

	bad := map[string]bool{}
	for k, c := range chosen {
		dir := strings.TrimSuffix(c.url, c.name)
		if dir == c.url || !strings.HasSuffix(dir, "/"+tag+"/") {
			bad[c.name] = true
			continue
		}
		p.URL = strings.TrimSuffix(dir, tag+"/") + tagTempl + "/" + prefix + "{target}" + suffix
		p.Targets[k] = values[templ(c.name)]
	}
	for k, c := range chosen {
		got := strings.NewReplacer("{version}", p.Version, "{target}", p.Targets[k]).Replace(p.URL)
		if got != c.url {
			bad[c.name] = true
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("assets do not share one name pattern: %s", strings.Join(slices.Sorted(maps.Keys(bad)), ", "))
	}
	return nil
}

// endsEqual reports whether every name has the same byte m places from its end.
func endsEqual(names []string, m int) bool {
	b := names[0][len(names[0])-1-m]
	for _, n := range names[1:] {
		if n[len(n)-1-m] != b {
			return false
		}
	}
	return true
}

// commonAffixes returns the longest common prefix and suffix of the sorted
// names, cut at name separators so {target} spans whole tokens.
func commonAffixes(names []string) (string, string) {
	first, last := names[0], names[len(names)-1] // the extremes bound the prefix of all
	n := 0
	for n < len(first) && n < len(last) && first[n] == last[n] {
		n++
	}
	prefix := first[:n]
	prefix = prefix[:strings.LastIndexAny(prefix, osSeparator)+1]
	room := len(first)
	for _, nm := range names {
		room = min(room, len(nm))
	}
	m := 0
	for m < room-len(prefix) && endsEqual(names, m) {
		m++
	}
	suffix := first[len(first)-m:]
	if i := strings.IndexAny(suffix, osSeparator); i >= 0 {
		return prefix, suffix[i:]
	}
	return prefix, ""
}
