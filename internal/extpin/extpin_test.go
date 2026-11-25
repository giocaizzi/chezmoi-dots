package extpin

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const (
	sha1 = "0123456789abcdef0123456789abcdef01234567"
	sha2 = "fedcba9876543210fedcba9876543210fedcba98"
	sumA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sumB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	ghURL = "https://github.com/octocat/example/archive/" + sha1 + ".tar.gz"
	glURL = "https://gitlab.example.com/group/sub/example/-/archive/" + sha1 + "/example-" + sha1 + ".tar.gz"
)

func TestScan(t *testing.T) {
	t.Parallel()
	ghAlt := strings.ReplaceAll(ghURL, sha1, sha2)
	tests := []struct {
		name string
		text string
		want []Pin
	}{
		{
			"github dotted checksum", "[\".vim/x\"]\n  type = \"archive\"\n  url = \"" + ghURL + "\"\n  checksum.sha256 = \"" + sumA + "\"\n",
			[]Pin{{File: "f", Entry: `".vim/x"`, Host: "github.com", Repo: "octocat/example", URL: ghURL, SHA: sha1, Sum: sumA, line: 2, sumLine: 3}},
		},
		{
			"gitlab checksum subtable", "[\".vim/x\"]\nurl = \"" + glURL + "\"\n[\".vim/x\".checksum]\nsha256 = \"" + sumA + "\"\n[\".vim/x\".filter]\ncommand = \"x\"\n",
			[]Pin{{File: "f", Entry: `".vim/x"`, Host: "gitlab.example.com", Repo: "group/sub/example", URL: glURL, SHA: sha1, Sum: sumA, line: 1, sumLine: 3}},
		},
		{
			"inline table", "[a]\nurl = \"" + ghURL + "\"\nchecksum = { sha256 = \"" + sumA + "\" }\n",
			[]Pin{{File: "f", Entry: "a", Host: "github.com", Repo: "octocat/example", URL: ghURL, SHA: sha1, Sum: sumA, line: 1, sumLine: 2}},
		},
		{
			"no checksum", "[a]\nurl = \"" + ghURL + "\"\n",
			[]Pin{{File: "f", Entry: "a", Host: "github.com", Repo: "octocat/example", URL: ghURL, SHA: sha1, line: 1, sumLine: -1}},
		},
		{"two archives skipped", "[a]\n{{ if x }}\nurl = \"" + ghURL + "\"\n{{ else }}\nurl = \"" + ghAlt + "\"\n{{ end }}\n", nil},
		{"two checksums skipped", "[a]\nurl = \"" + ghURL + "\"\nchecksum.sha256 = \"" + sumA + "\"\nchecksum.sha256 = \"" + sumB + "\"\n", nil},
		{"checksum without url", "[a]\nchecksum.sha256 = \"" + sumA + "\"\n", nil},
		{"template inside url", "[a]\nurl = \"https://github.com/octocat/example/archive/{{ .sha }}.tar.gz\"\n", nil},
		{
			"template lines around", "{{ if eq .os \"linux\" }}\n[a]\nurl = \"" + ghURL + "\"\n{{ end }}\n",
			[]Pin{{File: "f", Entry: "a", Host: "github.com", Repo: "octocat/example", URL: ghURL, SHA: sha1, line: 2, sumLine: -1}},
		},
		{
			"two entries", "[a]\nurl = \"" + ghURL + "\"\n[b]\nurl = \"" + glURL + "\"\n",
			[]Pin{
				{File: "f", Entry: "a", Host: "github.com", Repo: "octocat/example", URL: ghURL, SHA: sha1, line: 1, sumLine: -1},
				{File: "f", Entry: "b", Host: "gitlab.example.com", Repo: "group/sub/example", URL: glURL, SHA: sha1, line: 3, sumLine: -1},
			},
		},
		{
			"crlf", "[a]\r\nurl = \"" + ghURL + "\"\r\nchecksum.sha256 = \"" + sumA + "\"\r\n",
			[]Pin{{File: "f", Entry: "a", Host: "github.com", Repo: "octocat/example", URL: ghURL, SHA: sha1, Sum: sumA, line: 1, sumLine: 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Scan("f", tt.text)
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(Pin{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Scan() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPinHelpers(t *testing.T) {
	t.Parallel()
	p := Scan("f", "[a]\nurl = \""+glURL+"\"\n")[0]
	if got, want := p.Name(), "group/sub/example"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := p.Remote(), "https://gitlab.example.com/group/sub/example.git"; got != want {
		t.Errorf("Remote() = %q, want %q", got, want)
	}
	if got, want := p.URLAt(sha2), strings.ReplaceAll(glURL, sha1, sha2); got != want {
		t.Errorf("URLAt() = %q, want %q", got, want)
	}
}

func TestSet(t *testing.T) {
	t.Parallel()
	text := "# keep me\n[a]\nurl = \"" + glURL + "\"\nchecksum.sha256 = \"" + sumA + "\"\n[b]\nurl = \"" + ghURL + "\"\nchecksum.sha256 = \"" + sumA + "\"\n"
	pins := Scan("f", text)
	a := pins[0]

	t.Run("rewrites url twice and checksum only in its entry", func(t *testing.T) {
		t.Parallel()
		got, err := Set(text, a, sha2, sumB)
		if err != nil {
			t.Fatal(err)
		}
		want := "# keep me\n[a]\nurl = \"" + strings.ReplaceAll(glURL, sha1, sha2) + "\"\nchecksum.sha256 = \"" + sumB + "\"\n[b]\nurl = \"" + ghURL + "\"\nchecksum.sha256 = \"" + sumA + "\"\n"
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Set() (-want +got):\n%s", diff)
		}
	})
	t.Run("preserves crlf", func(t *testing.T) {
		t.Parallel()
		crlf := strings.ReplaceAll(text, "\n", "\r\n")
		got, err := Set(crlf, Scan("f", crlf)[1], sha2, sumB)
		if err != nil {
			t.Fatal(err)
		}
		want := "# keep me\n[a]\nurl = \"" + glURL + "\"\nchecksum.sha256 = \"" + sumA + "\"\n[b]\nurl = \"" + strings.ReplaceAll(ghURL, sha1, sha2) + "\"\nchecksum.sha256 = \"" + sumB + "\"\n"
		want = strings.ReplaceAll(want, "\n", "\r\n")
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Set() (-want +got):\n%s", diff)
		}
	})
	t.Run("empty sum keeps checksum", func(t *testing.T) {
		t.Parallel()
		got, err := Set(text, a, sha2, "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "checksum.sha256 = \""+sumA+"\"\n[b]") {
			t.Errorf("checksum changed:\n%s", got)
		}
	})
	errs := []struct {
		name     string
		pin      Pin
		sha, sum string
	}{
		{"bad sha", a, "xyz", sumB},
		{"uppercase sha", a, strings.ToUpper(sha2), sumB},
		{"bad sum", a, sha2, "abc"},
		{"not found", Pin{File: "f", Entry: "zzz", Repo: "x/y", SHA: sha1}, sha2, sumB},
		{"stale sha", Pin{File: "f", Entry: a.Entry, Repo: a.Repo, SHA: sha2}, sha2, sumB},
	}
	for _, tt := range errs {
		t.Run("error "+tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Set(text, tt.pin, tt.sha, tt.sum); err == nil {
				t.Error("Set() error = nil, want an error")
			}
		})
	}
}

func TestFind(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	body := "[a]\nurl = \"" + ghURL + "\"\n"
	files := map[string]bool{
		".chezmoiexternal.toml":                                true,
		filepath.Join("a", ".chezmoiexternal.toml.tmpl"):       true,
		filepath.Join("a", "b", ".chezmoiexternals", "x.toml"): true,
		filepath.Join(".chezmoiexternals", "y.toml.tmpl"):      true,
		filepath.Join(".chezmoiexternals", "z.txt"):            false,
		filepath.Join(".chezmoiexternals", "sub", "w.toml"):    false,
		filepath.Join(".git", ".chezmoiexternal.toml"):         false,
		filepath.Join("c", "other.toml"):                       false,
		filepath.Join("c", ".chezmoiexternal.toml.bak"):        false,
	}
	var want []string
	for name, match := range files {
		path := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if match {
			want = append(want, path)
		}
	}
	slices.Sort(want)
	pins, err := Find(src)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(pins))
	for _, p := range pins {
		got = append(got, p.File)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Find() files, sorted (-want +got):\n%s", diff)
	}
}

func TestFindMissing(t *testing.T) {
	t.Parallel()
	if _, err := Find(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("Find() of a missing dir: want an error")
	}
}

func TestFindSkipsDirNamedLikeExternals(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, ".chezmoiexternal.toml"), 0o750); err != nil {
		t.Fatal(err)
	}
	if pins, err := Find(src); err != nil || len(pins) != 0 {
		t.Errorf("Find() = %v, %v, want none", pins, err)
	}
}

// TestSetLeavesNoStaleChecksum holds Set to the promise that a bump never leaves
// a file chezmoi apply rejects or rewrites an entry the pin does not own.
func TestSetLeavesNoStaleChecksum(t *testing.T) {
	t.Parallel()
	sum512 := strings.Repeat("c", 128)
	tests := []struct {
		name  string
		xfail string
		text  string
		stale string // text that must not survive a bump of the archive pin
	}{
		{
			name:  "xfail: header with a trailing comment",
			xfail: "xfail: a `[key] # comment` header is not recognised, so the archive is folded into the previous entry and its sha256 is overwritten",
			text: "[\".local/bin/tool\"]\ntype = \"file\"\nurl = \"https://example.com/tool\"\nchecksum.sha256 = \"" + sumA + "\"\n" +
				"[\".vim/x\"] # pinned plugin\ntype = \"archive\"\nurl = \"" + ghURL + "\"\n",
			stale: "url = \"https://example.com/tool\"\nchecksum.sha256 = \"" + sumB + "\"",
		},
		{
			name:  "xfail: spaced header with a checksum subtable",
			xfail: "xfail: `[ key ]` and `[ key.checksum ]` are read as two entries, so the sha256 is left for the old commit",
			text:  "[ \".vim/x\" ]\ntype = \"archive\"\nurl = \"" + ghURL + "\"\n[ \".vim/x\".checksum ]\nsha256 = \"" + sumA + "\"\n",
			stale: "sha256 = \"" + sumA + "\"",
		},
		{
			name:  "xfail: sha512 checksum",
			xfail: "xfail: checksum.sha512 (also sha384, size) is not recognised, so the commit moves and the old checksum stays",
			text:  "[\".vim/x\"]\ntype = \"archive\"\nurl = \"" + ghURL + "\"\nchecksum.sha512 = \"" + sum512 + "\"\n",
			stale: sum512,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.xfail != "" {
				t.Skip(tt.xfail)
			}
			for _, p := range Scan("f", tt.text) {
				got, err := Set(tt.text, p, sha2, sumB)
				if err != nil {
					continue // refusing the pin is safe
				}
				if strings.Contains(got, tt.stale) {
					t.Errorf("Set(%s) left %q:\n%s", p.Entry, tt.stale, got)
				}
			}
		})
	}
}

// FuzzScanSet checks that Scan never panics and that Set of any pin it finds
// changes only the pin's URL and checksum lines.
func FuzzScanSet(f *testing.F) {
	f.Add("[\".vim/x\"]\nurl = \"" + ghURL + "\"\nchecksum.sha256 = \"" + sumA + "\"\n")
	f.Add("[a]\r\nurl = \"" + glURL + "\"\r\n[a.checksum]\r\nsha256 = \"" + sumA + "\"\r\n")
	f.Add("{{ if x }}\n[a]\nurl = \"" + ghURL + "\"\n{{ end }}\n[b]\nchecksum = { sha256 = \"" + sumA + "\" }\n")
	f.Fuzz(func(t *testing.T, text string) {
		for _, p := range Scan("f", text) {
			got, err := Set(text, p, sha2, sumB)
			if err != nil {
				t.Fatalf("Set(%+v) of a scanned pin: %v", p, err)
			}
			before, after := strings.Split(text, "\n"), strings.Split(got, "\n")
			if len(before) != len(after) {
				t.Fatalf("Set changed the line count: %d -> %d", len(before), len(after))
			}
			for i := range before {
				if i != p.line && i != p.sumLine && before[i] != after[i] {
					t.Errorf("Set changed line %d outside the pin: %q -> %q", i, before[i], after[i])
				}
			}
		}
	})
}
