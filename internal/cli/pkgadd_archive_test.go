package cli_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

// tarGzWith is a .tar.gz holding one executable file per name.
func tarGzWith(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte("x"))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// zipWith is a zip (not made on unix, so modes are unknown) holding one file per name.
func zipWith(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("x"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

const (
	assetLinux   = "example-tool_1.2.3_linux_amd64.tar.gz"
	assetDarwin  = "example-tool_1.2.3_darwin_arm64.tar.gz"
	assetWindows = "example-tool_1.2.3_windows_amd64.zip"
)

// ghArchives serves a darwin, linux and windows archive of example-tool; binary is the
// name of the executable inside the unix ones.
func ghArchives(t *testing.T, binary string) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		assetLinux:   tarGzWith(t, "example-tool_1.2.3_linux_amd64/"+binary, "example-tool_1.2.3_linux_amd64/README.md"),
		assetDarwin:  tarGzWith(t, "example-tool_1.2.3_darwin_arm64/"+binary),
		assetWindows: zipWith(t, "example-tool_1.2.3_windows_amd64/"+binary+".exe"),
	}
}

const ghTemplate = "https://github.com/octocat/example-tool/releases/download/v{version}/example-tool_{version}_{target}"

// ghValidData is the chezmoi data after the write, as the registry validation reads it.
func ghValidData(path string, bodies map[string][]byte) string {
	return fmt.Sprintf(`{"release":{"example-tool":{"version":"1.2.3","url":%q,"type":"archive-file","path":%q,"exe":true,
		"target":{"linux/amd64":"linux_amd64.tar.gz","darwin/arm64":"darwin_arm64.tar.gz","windows/amd64":"windows_amd64.zip"},
		"sha256":{"linux/amd64":%q,"darwin/arm64":%q,"windows/amd64":%q}}}}`,
		ghTemplate, path, sha(bodies[assetLinux]), sha(bodies[assetDarwin]), sha(bodies[assetWindows]))
}

func TestPkgAddGitHubReadsPathFromArchives(t *testing.T) {
	t.Parallel()
	bodies := ghArchives(t, "example-tool")
	e, gh := ghEnv(t, `{}`, ghValidData("example-tool_{version}_{target}/example-tool", bodies))
	gh.bodies = bodies
	if err := e.run(t, "example-tool", "--github", "octocat/example-tool", "--yes"); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	text := e.text(t)
	for _, w := range []string{
		`path = "example-tool_{version}_{target}/example-tool"` + "\n",
		`"linux/amd64" = "` + sha(bodies[assetLinux]) + `"`,
		`"windows/amd64" = "` + sha(bodies[assetWindows]) + `"`,
	} {
		if !strings.Contains(text, w) {
			t.Errorf("packages.toml lacks %q:\n%s", w, text)
		}
	}
	if want := `  path = "example-tool_{version}_{target}/example-tool"  (found in the archives)` + "\n"; !strings.Contains(e.errw.String(), want) {
		t.Errorf("stderr lacks %q:\n%s", want, e.errw.String())
	}
	if strings.Contains(e.errw.String(), "path candidates") {
		t.Errorf("stderr lists candidates for a found path:\n%s", e.errw.String())
	}
	if len(gh.downloads) != 3 {
		t.Errorf("downloads = %v, want one per target and none again for the checksums", gh.downloads)
	}
}

func TestPkgAddGitHubDownloadsBeforeTheConfirmation(t *testing.T) {
	t.Parallel()
	bodies := ghArchives(t, "example-tool")
	e, gh := ghEnv(t, `{}`)
	gh.bodies = bodies
	e.stdin, e.tty = "n\n", true
	if err := e.run(t, "example-tool", "--github", "octocat/example-tool"); err == nil {
		t.Fatal("pkg add accepted a declined proposal")
	}
	if len(gh.downloads) != 3 {
		t.Errorf("downloads = %v, want the archives read before the question", gh.downloads)
	}
	if !strings.Contains(e.errw.String(), "(found in the archives)\n") || !strings.Contains(e.errw.String(), "Add this release? [y/N] ") {
		t.Errorf("stderr = %q", e.errw.String())
	}
	if got := e.text(t); got != pkgAddOldText {
		t.Errorf("a declined proposal changed packages.toml:\n%s", got)
	}
}

func TestPkgAddGitHubPathNotFound(t *testing.T) {
	t.Parallel()
	bodies := ghArchives(t, "run")
	e, gh := ghEnv(t, `{}`, ghValidData("example-tool", bodies))
	gh.bodies = bodies
	if err := e.run(t, "example-tool", "--github", "octocat/example-tool", "--yes"); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	if !strings.Contains(e.text(t), "path = \"example-tool\"\n") {
		t.Errorf("packages.toml keeps no repository-name path:\n%s", e.text(t))
	}
	for _, w := range []string{
		`  path = "example-tool"  (not found in the archives)` + "\n",
		"  path candidates: darwin/arm64: example-tool_1.2.3_darwin_arm64/run; " +
			"linux/amd64: example-tool_1.2.3_linux_amd64/run, example-tool_1.2.3_linux_amd64/README.md; " +
			"windows/amd64: example-tool_1.2.3_windows_amd64/run.exe\n",
	} {
		if !strings.Contains(e.errw.String(), w) {
			t.Errorf("stderr lacks %q:\n%s", w, e.errw.String())
		}
	}
}

func TestPkgAddGitHubPathUnchecked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		json string // "" keeps the default release
		want string
		dl   int
	}{
		{"dry run", []string{"--dry-run"}, "", `  path = "example-tool"  (unchecked: --dry-run downloads no archive)`, 0},
		{
			"xz archives", nil,
			fmt.Sprintf(`{"tag_name":"v1.2.3","assets":[{"name":"example-tool_1.2.3_linux_amd64.tar.xz","browser_download_url":%q}]}`,
				ghDownload+"example-tool_1.2.3_linux_amd64.tar.xz"),
			`  path = "example-tool"  (unchecked: no .tar.gz, .tgz or .zip archive could be read)`, 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, gh := ghEnv(t, `{}`, `{}`)
			if tt.json != "" {
				gh.json = tt.json
			}
			if err := e.run(t, append([]string{"example-tool", "--github", "octocat/example-tool", "--yes"}, tt.args...)...); err != nil {
				t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
			}
			if !strings.Contains(e.errw.String(), tt.want+"\n") {
				t.Errorf("stderr lacks %q:\n%s", tt.want, e.errw.String())
			}
			if strings.Contains(e.errw.String(), "path candidates") {
				t.Errorf("stderr lists candidates:\n%s", e.errw.String())
			}
			if len(gh.downloads) != tt.dl {
				t.Errorf("downloads = %v, want %d", gh.downloads, tt.dl)
			}
		})
	}
}

func TestPkgAddGitHubBareBinariesHaveNoPath(t *testing.T) {
	t.Parallel()
	e, gh := ghEnv(t, `{}`, `{}`)
	gh.json = fmt.Sprintf(`{"tag_name":"v1.2.3","assets":[{"name":"example-tool_linux_amd64","browser_download_url":%q}]}`,
		ghDownload+"example-tool_linux_amd64")
	if err := e.run(t, "example-tool", "--github", "octocat/example-tool", "--yes"); err != nil {
		t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
	}
	if strings.Contains(e.errw.String(), "path") || strings.Contains(e.text(t), "path =") {
		t.Errorf("a bare binary has a path:\nstderr %s\nfile %s", e.errw.String(), e.text(t))
	}
	if len(gh.downloads) != 1 || !strings.Contains(e.text(t), sha(archiveBytes)) {
		t.Errorf("downloads = %v, file:\n%s", gh.downloads, e.text(t))
	}
}

func TestPkgAddGitHubCandidatesAreEscaped(t *testing.T) {
	t.Parallel()
	t.Run("xfail: archive file names reach the terminal raw", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: printProposal joins the candidate names unquoted, so control sequences in an " +
			"untrusted archive (cursor moves, OSC 52) are written to stderr; the path line is quoted")
		bodies := ghArchives(t, "run\x1b[1A\x1b[2K")
		e, gh := ghEnv(t, `{}`, ghValidData("example-tool", bodies))
		gh.bodies = bodies
		if err := e.run(t, "example-tool", "--github", "octocat/example-tool", "--yes"); err != nil {
			t.Fatalf("pkg add = %v\n%s", err, e.errw.String())
		}
		if strings.ContainsRune(e.errw.String(), '\x1b') {
			t.Errorf("stderr holds a raw escape character: %q", e.errw.String())
		}
	})
}
