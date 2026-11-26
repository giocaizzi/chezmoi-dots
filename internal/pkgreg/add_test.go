package pkgreg_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
)

func TestPackageEntryText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry pkgreg.PackageEntry
		want  string
	}{
		{
			"fixed key order",
			pkgreg.PackageEntry{
				Name: "example-tool", NPM: "typescript@5", UV: "example-tool", Winget: "Example.Tool", Apt: "example-tool",
				Cask: "example-app", Brew: "example-tool", Roles: []string{"personal", "work"}, Requires: "gui",
			},
			"[package.example-tool]\nbrew = \"example-tool\"\ncask = \"example-app\"\napt = \"example-tool\"\n" +
				"winget = \"Example.Tool\"\nuv = \"example-tool\"\nnpm = \"typescript@5\"\nroles = [\"personal\", \"work\"]\nrequires = \"gui\"\n",
		},
		{"one source", pkgreg.PackageEntry{Name: "wget", Brew: "wget"}, "[package.wget]\nbrew = \"wget\"\n"},
		{"quotes and backslashes", pkgreg.PackageEntry{Name: "x", Brew: `a"b\c`}, "[package.x]\nbrew = \"a\\\"b\\\\c\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, tt.entry.Text()); diff != "" {
				t.Errorf("Text() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReleaseEntryText(t *testing.T) {
	t.Parallel()
	e := pkgreg.ReleaseEntry{
		Name: "example-tool", Version: "1.2.3", URL: "https://example.com/{version}/tool-{target}.tar.gz",
		Targets:  map[string]string{"linux/amd64": "linux-x86_64", "darwin/arm64": "macos-arm"},
		Roles:    []string{"light"},
		Requires: "gui",
	}
	t.Run("with checksums", func(t *testing.T) {
		t.Parallel()
		e := e
		e.Sums = map[string]string{"linux/amd64": strings.Repeat("a", 64), "darwin/arm64": strings.Repeat("b", 64)}
		want := "[release.example-tool]\nversion = \"1.2.3\"\nurl = \"https://example.com/{version}/tool-{target}.tar.gz\"\n" +
			"roles = [\"light\"]\nrequires = \"gui\"\n\n" +
			"[release.example-tool.target]\n\"darwin/arm64\" = \"macos-arm\"\n\"linux/amd64\" = \"linux-x86_64\"\n\n" +
			"[release.example-tool.sha256]\n\"darwin/arm64\" = \"" + strings.Repeat("b", 64) + "\"\n\"linux/amd64\" = \"" + strings.Repeat("a", 64) + "\"\n"
		if diff := cmp.Diff(want, e.Text()); diff != "" {
			t.Errorf("Text() (-want +got):\n%s", diff)
		}
	})
	t.Run("without checksums", func(t *testing.T) {
		t.Parallel()
		got := e.Text()
		if strings.Contains(got, "sha256]") || !strings.HasSuffix(got, "\n# sha256 computed when written\n") {
			t.Errorf("Text() = %q, want no sha256 table and the comment", got)
		}
	})
}

func TestAppend(t *testing.T) {
	t.Parallel()
	const entry = "[package.a]\nbrew = \"a\"\n"
	tests := []struct {
		name, text, want string
	}{
		{"empty", "", entry},
		{"no trailing newline", "[package.b]\nbrew = \"b\"", "[package.b]\nbrew = \"b\"\n\n" + entry},
		{"one newline", "x = 1\n", "x = 1\n\n" + entry},
		{"already a blank line", "x = 1\n\n", "x = 1\n\n" + entry},
		{"crlf", "x = 1\r\n", "x = 1\r\n\r\n[package.a]\r\nbrew = \"a\"\r\n"},
		{"crlf blank line", "x = 1\r\n\r\n", "x = 1\r\n\r\n[package.a]\r\nbrew = \"a\"\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, pkgreg.Append(tt.text, entry)); diff != "" {
				t.Errorf("Append() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestKind(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"package": map[string]any{"a": map[string]any{}},
		"release": map[string]any{"b": map[string]any{}},
	}
	for name, want := range map[string]string{"a": "package", "b": "release", "c": ""} {
		kind, ok := pkgreg.Kind(data, name)
		if kind != want || ok != (want != "") {
			t.Errorf("Kind(%q) = %q, %v, want %q", name, kind, ok, want)
		}
	}
}

func TestReleaseEntryOptionalKeys(t *testing.T) {
	t.Parallel()
	e := pkgreg.ReleaseEntry{
		Name: "example-tool", Version: "1.2.3", URL: "https://example.com/{version}/tool-{target}.zip",
		Type: "archive-file", Path: "example-tool", Exe: true, Roles: []string{"work"}, Requires: "gui",
		Targets: map[string]string{"windows/amd64": "win"}, Sums: map[string]string{"windows/amd64": "abc"},
	}
	want := "[release.example-tool]\nversion = \"1.2.3\"\nurl = \"https://example.com/{version}/tool-{target}.zip\"\n" +
		"type = \"archive-file\"\npath = \"example-tool\"\nexe = true\nroles = [\"work\"]\nrequires = \"gui\"\n\n" +
		"[release.example-tool.target]\n\"windows/amd64\" = \"win\"\n\n[release.example-tool.sha256]\n\"windows/amd64\" = \"abc\"\n"
	if diff := cmp.Diff(want, e.Text()); diff != "" {
		t.Errorf("Text() (-want +got):\n%s", diff)
	}
}

func TestInsertKeys(t *testing.T) {
	t.Parallel()
	add := []string{`apt = "wget"`}
	tests := []struct {
		name, text, want string
		ok               bool
	}{
		{
			"after the last key, before the next table", "[package.wget]\nbrew = \"wget\"\n\n[package.other]\nbrew = \"o\"\n",
			"[package.wget]\nbrew = \"wget\"\napt = \"wget\"\n\n[package.other]\nbrew = \"o\"\n", true,
		},
		{
			"comments before the next header stay after", "[package.wget]\nbrew = \"wget\" # keep\n\n# about other\n[package.other]\n",
			"[package.wget]\nbrew = \"wget\" # keep\napt = \"wget\"\n\n# about other\n[package.other]\n", true,
		},
		{"comment inside the table stays", "[package.wget]\n# note\nbrew = \"wget\"\n# tail\n", "[package.wget]\n# note\nbrew = \"wget\"\napt = \"wget\"\n# tail\n", true},
		{"crlf", "[package.wget]\r\nbrew = \"wget\"\r\n\r\n[package.o]\r\n", "[package.wget]\r\nbrew = \"wget\"\r\napt = \"wget\"\r\n\r\n[package.o]\r\n", true},
		{"entry at EOF without a newline", "[package.wget]\nbrew = \"wget\"", "[package.wget]\nbrew = \"wget\"\napt = \"wget\"\n", true},
		{"header only", "[package.wget]\n", "[package.wget]\napt = \"wget\"\n", true},
		{"header with a comment and quotes", "[ package . \"wget\" ] # x\nbrew = \"w\"\n", "[ package . \"wget\" ] # x\nbrew = \"w\"\napt = \"wget\"\n", true},
		{
			"multi-line array", "[package.wget]\nroles = [\n  \"work\",\n]\n[package.wget.sub]\nk = 1\n",
			"[package.wget]\nroles = [\n  \"work\",\n]\napt = \"wget\"\n[package.wget.sub]\nk = 1\n", true,
		},
		{"inline table is not extended", "[package]\nwget = { brew = \"wget\" }\n", "[package]\nwget = { brew = \"wget\" }\n", false},
		{"other name with the same prefix", "[package.wget2]\nbrew = \"w\"\n", "[package.wget2]\nbrew = \"w\"\n", false},
		{"array of tables is not a package table", "[[package.wget]]\nbrew = \"w\"\n", "[[package.wget]]\nbrew = \"w\"\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := pkgreg.InsertKeys(tt.text, "wget", add)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("InsertKeys (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPackageEntryLines(t *testing.T) {
	t.Parallel()
	e := pkgreg.PackageEntry{Name: "x", Brew: "b", Roles: []string{"work"}}
	want := []pkgreg.Line{{Key: "brew", Text: `brew = "b"`}, {Key: "roles", Text: `roles = ["work"]`}}
	if diff := cmp.Diff(want, e.Lines()); diff != "" {
		t.Errorf("Lines() (-want +got):\n%s", diff)
	}
}

func TestValidPlatformAndRole(t *testing.T) {
	t.Parallel()
	for p, want := range map[string]bool{"linux/amd64": true, "windows/arm64": true, "linux": false, "freebsd/amd64": false, "linux/386": false} {
		if got := pkgreg.ValidPlatform(p); got != want {
			t.Errorf("ValidPlatform(%q) = %v, want %v", p, got, want)
		}
	}
	for r, want := range map[string]bool{"personal": true, "work": true, "light": true, "admin": false, "": false} {
		if got := pkgreg.ValidRole(r); got != want {
			t.Errorf("ValidRole(%q) = %v, want %v", r, got, want)
		}
	}
}
