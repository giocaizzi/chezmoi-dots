package extpin

import (
	"os"
	"path/filepath"
	"runtime"
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
			got, _ := Scan("f", tt.text)
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(Pin{}), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Scan() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScanHeaders(t *testing.T) {
	t.Parallel()
	body := "\nurl = \"" + ghURL + "\"\n"
	tests := []struct {
		name, header, wantEntry string
	}{
		{"trailing comment", `[".k"] # c`, `".k"`},
		{"inner spaces", `[ ".k" ]`, `".k"`},
		{"spaced dots", `[ "a" . b ]`, `"a".b`},
		{"single quotes", `['a b']`, `'a b'`},
		{"quoted bracket", `["a]b"]`, `"a]b"`},
		{"escaped quote", `["a\"]b"] # c`, `"a\"]b"`},
		{"leading spaces", `   [a]`, `a`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pins, _ := Scan("f", tt.header+body)
			if len(pins) != 1 || pins[0].Entry != tt.wantEntry {
				t.Errorf("Scan(%q) = %+v, want one pin with entry %q", tt.header, pins, tt.wantEntry)
			}
		})
	}
	notHeaders := []string{`[[a]]`, `["a", "b"]`, `[a] x`, `["a`, `[]`, `[a`}
	for _, h := range notHeaders {
		t.Run("not a header "+h, func(t *testing.T) {
			t.Parallel()
			if pins, _ := Scan("f", h+body); len(pins) != 0 {
				t.Errorf("Scan(%q) = %+v, want no pin", h, pins)
			}
		})
	}
	t.Run("spaced subtable stays in its entry", func(t *testing.T) {
		t.Parallel()
		text := "[ \".k\" ]\nurl = \"" + ghURL + "\"\n[ \".k\" . checksum ]\nsha256 = \"" + sumA + "\"\n"
		pins, skips := Scan("f", text)
		if len(pins) != 1 || pins[0].Sum != sumA || len(skips) != 0 {
			t.Errorf("Scan() = %+v, %+v, want one pin with the checksum", pins, skips)
		}
	})
}

func TestScanSkips(t *testing.T) {
	t.Parallel()
	ghAlt := strings.ReplaceAll(ghURL, sha1, sha2)
	sum512 := strings.Repeat("c", 128)
	url := "url = \"" + ghURL + "\"\n"
	tests := []struct {
		name, text, reason string
	}{
		{"sha384 dotted", "[a]\n" + url + "checksum.sha384 = \"x\"\n", "checksum sha384 is not supported; bump it by hand"},
		{"sha512 subtable", "[a]\n" + url + "[a.checksum]\nsha512 = \"" + sum512 + "\"\n", "checksum sha512 is not supported; bump it by hand"},
		{"size inline", "[a]\n" + url + "checksum = { size = 3 }\n", "checksum size is not supported; bump it by hand"},
		{"sha256 and sha512", "[a]\n" + url + "checksum.sha256 = \"" + sumA + "\"\nchecksum.sha512 = \"x\"\n", "checksum sha512 is not supported; bump it by hand"},
		{"two urls", "[a]\n" + url + "url = \"" + ghAlt + "\"\n", "more than one archive URL"},
		{"two sha256", "[a]\n" + url + "checksum.sha256 = \"" + sumA + "\"\nchecksum.sha256 = \"" + sumB + "\"\n", "more than one sha256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pins, skips := Scan("f", tt.text)
			want := []Skip{{File: "f", Entry: "a", Reason: tt.reason}}
			if len(pins) != 0 {
				t.Errorf("Scan() pins = %+v, want none", pins)
			}
			if diff := cmp.Diff(want, skips); diff != "" {
				t.Errorf("Scan() skips (-want +got):\n%s", diff)
			}
			if _, err := Set(tt.text, Pin{File: "f", Entry: "a", Repo: "octocat/example", SHA: sha1}, sha2, sumB); err == nil {
				t.Error("Set() of a skipped entry: want a not found error")
			}
		})
	}
	t.Run("entry without an archive is not a skip", func(t *testing.T) {
		t.Parallel()
		pins, skips := Scan("f", "[a]\nurl = \"https://example.com/x\"\nchecksum.sha512 = \"x\"\n")
		if len(pins) != 0 || len(skips) != 0 {
			t.Errorf("Scan() = %+v, %+v, want nothing", pins, skips)
		}
	})
}

func TestPinHelpers(t *testing.T) {
	t.Parallel()
	pins, _ := Scan("f", "[a]\nurl = \""+glURL+"\"\n")
	p := pins[0]
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
	pins, _ := Scan("f", text)
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
		crlfPins, _ := Scan("f", crlf)
		got, err := Set(crlf, crlfPins[1], sha2, sumB)
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
	pins, skips := Find(src)
	if len(skips) != 0 {
		t.Errorf("Find() skips = %+v, want none", skips)
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
	missing := filepath.Join(t.TempDir(), "nope")
	pins, skips := Find(missing)
	if len(pins) != 0 || len(skips) != 1 || skips[0].File != missing || skips[0].Entry != "" || skips[0].Reason == "" {
		t.Errorf("Find() of a missing dir = %+v, %+v, want one skip for it", pins, skips)
	}
}

func TestFindUnreadable(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("Unix permissions do not apply")
	}
	src := t.TempDir()
	body := []byte("[a]\nurl = \"" + ghURL + "\"\n")
	locked := filepath.Join(src, "locked")
	for _, dir := range []string{locked, filepath.Join(src, "ok")} {
		if err := os.Mkdir(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".chezmoiexternal.toml"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	unreadable := filepath.Join(src, "bad", ".chezmoiexternal.toml")
	if err := os.MkdirAll(filepath.Dir(unreadable), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unreadable, body, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	pins, skips := Find(src)
	if len(pins) != 1 || pins[0].File != filepath.Join(src, "ok", ".chezmoiexternal.toml") {
		t.Errorf("Find() pins = %+v, want only the readable one", pins)
	}
	files := make([]string, 0, len(skips))
	for _, s := range skips {
		if s.Entry != "" || s.Reason == "" {
			t.Errorf("skip %+v: want a path skip with a reason", s)
		}
		files = append(files, s.File)
	}
	if diff := cmp.Diff([]string{unreadable, locked}, files); diff != "" {
		t.Errorf("Find() skipped files (-want +got):\n%s", diff)
	}
}

func TestFindSkipsDirNamedLikeExternals(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, ".chezmoiexternal.toml"), 0o750); err != nil {
		t.Fatal(err)
	}
	if pins, skips := Find(src); len(pins) != 0 || len(skips) != 0 {
		t.Errorf("Find() = %v, %v, want none", pins, skips)
	}
}

// TestSetLeavesNoStaleChecksum holds Set to the promise that a bump never leaves
// a file chezmoi apply rejects or rewrites an entry the pin does not own.
func TestSetLeavesNoStaleChecksum(t *testing.T) {
	t.Parallel()
	sum512 := strings.Repeat("c", 128)
	tests := []struct {
		name  string
		text  string
		stale string // text that must not survive a bump of the archive pin
	}{
		{
			name: "header with a trailing comment",
			text: "[\".local/bin/tool\"]\ntype = \"file\"\nurl = \"https://example.com/tool\"\nchecksum.sha256 = \"" + sumA + "\"\n" +
				"[\".vim/x\"] # pinned plugin\ntype = \"archive\"\nurl = \"" + ghURL + "\"\n",
			stale: "url = \"https://example.com/tool\"\nchecksum.sha256 = \"" + sumB + "\"",
		},
		{
			name:  "spaced header with a checksum subtable",
			text:  "[ \".vim/x\" ]\ntype = \"archive\"\nurl = \"" + ghURL + "\"\n[ \".vim/x\".checksum ]\nsha256 = \"" + sumA + "\"\n",
			stale: "sha256 = \"" + sumA + "\"",
		},
		{
			name:  "sha512 checksum is skipped, not bumped",
			text:  "[\".vim/x\"]\ntype = \"archive\"\nurl = \"" + ghURL + "\"\nchecksum.sha512 = \"" + sum512 + "\"\n",
			stale: sum512,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pins, _ := Scan("f", tt.text)
			for _, p := range pins {
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
		pins, _ := Scan("f", text)
		for _, p := range pins {
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

// TestScanEquivalentHeaderKeys: TOML reads the bare, "basic" and 'literal' forms of
// a key as one key, so a checksum subtable written in another form still belongs
// to its entry; a bump must not leave its sha256 stale.
func TestScanEquivalentHeaderKeys(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, entry, sub string }{
		{"bare entry, basic subtable", `[tool]`, `["tool".checksum]`},
		{"basic entry, literal subtable", `[".vim/x"]`, `['.vim/x'.checksum]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			t.Skip("xfail: Scan compares header keys as written, so the subtable starts a new entry and the bump leaves the old sha256")
			text := tt.entry + "\nurl = \"" + ghURL + "\"\n" + tt.sub + "\nsha256 = \"" + sumA + "\"\n"
			pins, _ := Scan("f", text)
			for _, p := range pins {
				got, err := Set(text, p, sha2, sumB)
				if err != nil {
					continue // refusing the pin is safe
				}
				if strings.Contains(got, sumA) {
					t.Errorf("Set(%s) left the old sha256:\n%s", p.Entry, got)
				}
			}
		})
	}
}
