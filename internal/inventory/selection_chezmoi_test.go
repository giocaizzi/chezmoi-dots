package inventory_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/inventory"
	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
)

// The registry fixture: the templates a dotfiles repository provides (role,
// releases, packages) and the data they read.
var selectionFixture = map[string]string{
	filepath.Join(".chezmoitemplates", "role"):     `personal`,
	filepath.Join(".chezmoitemplates", "releases"): `{"example": {"dest": ".local/bin/example"}}`,
	filepath.Join(".chezmoitemplates", "packages"): `{{- $m := .manager -}}
{{- $out := list -}}
{{- range $name, $e := .ctx.package -}}
{{-   if hasKey $e $m -}}
{{-     $out = append $out (dict "name" $name "id" (index $e $m) "entry" $e) -}}
{{-   end -}}
{{- end -}}
{{- $out | toJson -}}`,
	filepath.Join(".chezmoidata", "packages.toml"): `[release.example]
version = "1.2.3"
url = "https://github.com/octocat/example/releases/download/v{version}/example.tar.gz"

[package.example-brew]
brew = "example-brew"

[package.example-apt]
apt = "example-apt"

[package.example-winget]
winget = "Example.Winget"

[package.example-uv]
uv = "example-uv"
`,
}

// TestSelectionTemplateThroughChezmoi renders SelectionTemplate with a real chezmoi
// and decodes it, so the template and ParseSelection cannot drift apart.
func TestSelectionTemplateThroughChezmoi(t *testing.T) {
	t.Parallel()
	bin, err := exec.LookPath("chezmoi")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("chezmoi is not on PATH: CI installs it so this test never skips there")
		}
		t.Skip("chezmoi is not on PATH")
	}
	src, dst := t.TempDir(), t.TempDir()
	for name, content := range selectionFixture {
		p := filepath.Join(src, name)
		if err = os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(t.TempDir(), "chezmoi.toml")
	if err = os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), bin, "--source", src, "--config", cfg, "--destination", dst, "execute-template")
	cmd.Stdin = bytes.NewReader([]byte(inventory.SelectionTemplate))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("chezmoi execute-template: %v\n%s", err, stderr.String())
	}
	got, err := inventory.ParseSelection(out)
	if err != nil {
		t.Fatalf("ParseSelection(%s): %v", out, err)
	}

	pkg := func(name, id string) inventory.PackageSel {
		return inventory.PackageSel{Name: name, ID: id, Cmd: name, VersionCmd: []string{name, "--version"}}
	}
	want := inventory.Selection{
		OS:   runtime.GOOS,
		Role: "personal",
		Releases: []inventory.ReleaseSel{{
			Release: pkgreg.Release{
				Name:    "example",
				Version: "1.2.3",
				URL:     "https://github.com/octocat/example/releases/download/v{version}/example.tar.gz",
				Targets: map[string]string{},
			},
			Dest:       ".local/bin/example",
			VersionCmd: []string{"{dest}", "--version"},
		}},
		Packages: map[inventory.Source][]inventory.PackageSel{
			inventory.UV:     {pkg("example-uv", "example-uv")},
			inventory.Npm:    {},
			inventory.Script: {},
		},
	}
	switch runtime.GOOS {
	case "darwin":
		want.Packages[inventory.Brew] = []inventory.PackageSel{pkg("example-brew", "example-brew")}
	case "linux":
		want.Packages[inventory.Apt] = []inventory.PackageSel{pkg("example-apt", "example-apt")}
	case "windows":
		want.Packages[inventory.Winget] = []inventory.PackageSel{pkg("example-winget", "Example.Winget")}
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("selection (-want +got):\n%s", diff)
	}
}
