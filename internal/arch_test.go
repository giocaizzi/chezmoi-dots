// Package internal holds the architecture fitness tests of the module.
package internal_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
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
