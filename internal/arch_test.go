// Package internal holds the architecture fitness tests of the module.
package internal_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// harnessNames matches data keys and tool names that belong to one dotfiles repo.
var harnessNames = regexp.MustCompile(`(?i)\b(harnessTargets|harnessPolicy|harnessPersonalPlugins|claude|copilot|codex|opencode|antigravity)\b`)

// agentsCLIAllowed lists, file by file, where the literal "agents" is not a call to
// the agents CLI.
var agentsCLIAllowed = map[string]string{
	// "agents" is a field name of the package registry rows, not the CLI.
	"internal/pkgreg/pkgreg.go": "package registry field name",
}

// TestNoDotfilesKnowledge keeps this tool generic: it must know nothing of one
// dotfiles repo's AI-harness data nor call the agents CLI.
func TestNoDotfilesKnowledge(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == ".claude" || n == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			checkSource(t, rel, string(src))
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkSource(t *testing.T, rel, src string) {
	t.Helper()
	if m := harnessNames.FindString(src); m != "" {
		t.Errorf("%s names %q: this tool must not know one dotfiles repo's harness data; read it from the repo's own config template or data, or take it as input", rel, m)
	}
	if _, ok := agentsCLIAllowed[rel]; !ok && strings.Contains(src, strconv.Quote("agents")) {
		t.Errorf("%s calls the agents CLI (literal %q): this tool must not depend on it; leave that check to the dotfiles repo", rel, "agents")
	}
}

// repoRoot is the module root, one level above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// agentsPackage matches a package bullet of AGENTS.md and captures its directory.
var agentsPackage = regexp.MustCompile("^- `(internal/[^/`]+)/`")

// TestAgentsListsEveryPackage keeps the architecture list of AGENTS.md in step with the
// directories under internal/ that hold Go code.
func TestAgentsListsEveryPackage(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	text, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for line := range strings.Lines(string(text)) {
		if m := agentsPackage.FindStringSubmatch(line); m != nil {
			listed[m[1]] = true
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		goFiles, gerr := filepath.Glob(filepath.Join(root, "internal", e.Name(), "*.go"))
		if gerr != nil {
			t.Fatal(gerr)
		}
		if len(goFiles) > 0 {
			actual["internal/"+e.Name()] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(actual)) {
		if !listed[name] {
			t.Errorf("AGENTS.md does not list %s/", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(listed)) {
		if !actual[name] {
			t.Errorf("AGENTS.md lists %s/, which has no Go files", name)
		}
	}
}

var (
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)`)
	htmlLink     = regexp.MustCompile(`(?:src|href)="([^"]+)"`)
	urlScheme    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	heading      = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*\s*$`)
)

// outsideFences returns the lines of a Markdown text that are not in a fenced code block.
func outsideFences(text string) []string {
	var lines []string
	fenced := false
	for line := range strings.Lines(text) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced {
			lines = append(lines, line)
		}
	}
	return lines
}

// anchor is the GitHub fragment of a heading: lower case, only letters, digits, spaces,
// - and _ kept, spaces turned into -.
func anchor(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

func anchors(t *testing.T, path string) map[string]bool {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, line := range outsideFences(string(text)) {
		if m := heading.FindStringSubmatch(line); m != nil {
			found[anchor(m[1])] = true
		}
	}
	return found
}

// TestDocsRelativeLinks checks that every relative link of the README, AGENTS.md and
// docs/ points at an existing file, and at an existing heading when it has a fragment.
func TestDocsRelativeLinks(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files := []string{filepath.Join(root, "README.md"), filepath.Join(root, "AGENTS.md")}
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range append(files, docs...) {
		rel, _ := filepath.Rel(root, file)
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range outsideFences(string(text)) {
			for _, re := range []*regexp.Regexp{markdownLink, htmlLink} {
				for _, m := range re.FindAllStringSubmatch(line, -1) {
					checkLink(t, filepath.ToSlash(rel), file, m[1])
				}
			}
		}
	}
}

// checkLink reports a relative link of file whose file or heading does not exist.
func checkLink(t *testing.T, rel, file, target string) {
	t.Helper()
	if urlScheme.MatchString(target) {
		return
	}
	pathPart, fragment, _ := strings.Cut(target, "#")
	dest := file
	if pathPart != "" {
		dest = filepath.Join(filepath.Dir(file), filepath.FromSlash(pathPart))
		if _, err := os.Stat(dest); err != nil {
			t.Errorf("%s links to %q: no such file", rel, target)
			return
		}
	}
	if fragment != "" && strings.HasSuffix(dest, ".md") && !anchors(t, dest)[fragment] {
		t.Errorf("%s links to %q: no such heading", rel, target)
	}
}
